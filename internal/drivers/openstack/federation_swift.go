// SPDX-FileCopyrightText: 2021 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package openstack

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"time"

	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/majewsky/schwift/v2"
	"github.com/majewsky/schwift/v2/gopherschwift"
	"github.com/sapcc/go-bits/gophercloudext"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/osext"

	"slices"

	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

type federationDriverSwift struct {
	Container   *schwift.Container
	OwnHostName string
}

func init() {
	keppel.FederationDriverRegistry.Add(func() keppel.FederationDriver { return &federationDriverSwift{} })
}

// PluginTypeID implements the keppel.FederationDriver interface.
func (fd *federationDriverSwift) PluginTypeID() string { return "swift" }

// Init implements the keppel.FederationDriver interface.
func (fd *federationDriverSwift) Init(ctx context.Context, ad keppel.AuthDriver, cfg keppel.Configuration) (err error) {
	fd.OwnHostName = cfg.APIPublicHostname
	fd.Container, err = initSwiftContainerConnection(ctx, "KEPPEL_FEDERATION_")
	return err
}

func initSwiftContainerConnection(ctx context.Context, envPrefix string) (*schwift.Container, error) {
	// connect to Swift
	provider, eo, err := gophercloudext.NewProviderClient(ctx, &gophercloudext.ClientOpts{EnvPrefix: envPrefix + "OS_"})
	if err != nil {
		return nil, err
	}
	swiftV1, err := openstack.NewObjectStorageV1(provider, eo)
	if err != nil {
		return nil, errors.New("cannot find Swift v1 API for federation driver: " + err.Error())
	}

	// create Swift container if necessary
	swiftAccount, err := gopherschwift.Wrap(swiftV1, nil)
	if err != nil {
		return nil, err
	}
	container, err := swiftAccount.Container(osext.MustGetenv(envPrefix + "SWIFT_CONTAINER")).EnsureExists(ctx)
	if err != nil {
		return nil, err
	}

	return container, nil
}

type accountFile struct {
	AccountName         models.AccountName `json:"-"`
	PrimaryHostName     string             `json:"primary_hostname"`
	ReplicaHostNames    []string           `json:"replica_hostnames"`
	SubleaseTokenSecret string             `json:"sublease_token_secret"`
}

func (fd *federationDriverSwift) accountFileObj(accountName models.AccountName) *schwift.Object {
	return fd.Container.Object(fmt.Sprintf("accounts/%s.json", accountName))
}

// Downloads and parses an account file from the Swift container.
func (fd *federationDriverSwift) readAccountFile(ctx context.Context, accountName models.AccountName) (accountFile, error) {
	buf, err := fd.accountFileObj(accountName).Download(ctx, nil).AsByteSlice()
	if err != nil {
		if schwift.Is(err, http.StatusNotFound) {
			// account file does not exist -> create an empty one that we can fill now
			return accountFile{AccountName: accountName}, nil
		}
		return accountFile{}, err
	}

	var file accountFile
	err = json.Unmarshal(buf, &file)
	file.AccountName = accountName
	return file, err
}

// Base implementation for all write operations performed by this driver. Swift
// does not have strong consistency, so we reduce the likelihood of accidental
// inconsistencies by performing a write once, then reading the result back
// after a short wait and checking whether our write was persisted.
func (fd *federationDriverSwift) modifyAccountFile(ctx context.Context, accountName models.AccountName, modify func(file *accountFile, firstPass bool) error) error {
	fileOld, err := fd.readAccountFile(ctx, accountName)
	if err != nil {
		return err
	}

	// check if we are actually changing anything at all (this is a very important
	// optimization for RecordExistingAccount which is a no-op most of the time)
	fileOldModified := fileOld
	err = modify(&fileOldModified, true)
	if err != nil {
		return err
	}
	sort.Strings(fileOldModified.ReplicaHostNames) // to avoid useless inequality
	if reflect.DeepEqual(fileOld, fileOldModified) {
		return nil
	}

	// perform the write
	buf, err := json.Marshal(fileOldModified)
	if err != nil {
		return err
	}
	obj := fd.accountFileObj(accountName)
	logg.Info("federation: writing account file %s", obj.FullName())
	hdr := schwift.NewObjectHeaders()
	hdr.ContentType().Set("application/json")
	err = obj.Upload(ctx, bytes.NewReader(buf), nil, hdr.ToOpts())
	if err != nil {
		return err
	}

	// wait a bit, then check if the write was persisted
	time.Sleep(250 * time.Millisecond)
	fileNew, err := fd.readAccountFile(ctx, accountName)
	if err != nil {
		return err
	}
	fileNewModified := fileNew
	err = modify(&fileNewModified, false)
	if err != nil {
		return err
	}
	sort.Strings(fileNewModified.ReplicaHostNames) // to avoid useless inequality
	if !reflect.DeepEqual(fileNew, fileNewModified) {
		// ^ NOTE: It's tempting to just do `reflect.DeepEqual(fileNew,
		// fildOldModified)` here, but that would be too strict of a condition. We
		// don't care whether someone edited the file right after us, we care
		// whether the contents of our write are still there.
		return fmt.Errorf("write collision while trying to update the account file for %q, please retry", accountName)
	}

	return nil
}

// ClaimAccountName implements the keppel.FederationDriver interface.
func (fd *federationDriverSwift) ClaimAccountName(ctx context.Context, account models.Account, subleaseTokenSecret string) (keppel.ClaimResult, error) {
	var (
		isUserError bool
		err         error
	)
	if account.UpstreamPeerHostName != "" {
		isUserError, err = fd.claimReplicaAccount(ctx, account, subleaseTokenSecret)
	} else {
		isUserError, err = fd.claimPrimaryAccount(ctx, account, subleaseTokenSecret)
	}

	if err != nil {
		if isUserError {
			return keppel.ClaimFailed, err
		}
		return keppel.ClaimErrored, err
	}
	return keppel.ClaimSucceeded, nil
}

func (fd *federationDriverSwift) claimPrimaryAccount(ctx context.Context, account models.Account, subleaseTokenSecret string) (isUserError bool, err error) {
	// defense in depth - the caller should already have verified this
	if subleaseTokenSecret != "" {
		return true, errors.New("cannot check sublease token when claiming a primary account")
	}

	isUserError = false
	err = fd.modifyAccountFile(ctx, account.Name, func(file *accountFile, firstPass bool) error {
		_ = firstPass

		if file.PrimaryHostName == "" || file.PrimaryHostName == fd.OwnHostName {
			file.PrimaryHostName = fd.OwnHostName
			return nil
		}
		isUserError = true
		return fmt.Errorf("account name %s is already in use at %s", account.Name, file.PrimaryHostName)
	})
	return isUserError, err
}

func (fd *federationDriverSwift) claimReplicaAccount(ctx context.Context, account models.Account, subleaseTokenSecret string) (isUserError bool, err error) {
	// defense in depth - the caller should already have verified this
	if subleaseTokenSecret == "" {
		return true, errors.New("missing sublease token")
	}

	isUserError = false
	err = fd.modifyAccountFile(ctx, account.Name, func(file *accountFile, firstPass bool) error {
		// verify the sublease token only on first pass (in the second pass, it was already cleared)
		if firstPass {
			if file.SubleaseTokenSecret != subleaseTokenSecret {
				isUserError = true
				return errors.New("invalid sublease token (or token was already used)")
			}
			file.SubleaseTokenSecret = ""
		}

		// validate the primary account
		err := fd.verifyAccountOwnership(*file, account.UpstreamPeerHostName)
		if err != nil {
			return err
		}

		// all good - add ourselves to the list of replicas
		file.ReplicaHostNames = addStringToList(file.ReplicaHostNames, fd.OwnHostName)
		return nil
	})
	return isUserError, err
}

// IssueSubleaseTokenSecret implements the keppel.FederationDriver interface.
func (fd *federationDriverSwift) IssueSubleaseTokenSecret(ctx context.Context, account models.Account) (string, error) {
	// generate a random token with 16 Base64 chars
	tokenBytes := make([]byte, 12)
	_, err := rand.Read(tokenBytes)
	if err != nil {
		return "", fmt.Errorf("could not generate token: %s", err.Error())
	}
	tokenStr := base64.StdEncoding.EncodeToString(tokenBytes)

	return tokenStr, fd.modifyAccountFile(ctx, account.Name, func(file *accountFile, firstPass bool) error {
		_ = firstPass

		// defense in depth - the caller should already have verified this
		if account.UpstreamPeerHostName != "" {
			return errors.New("operation not allowed for replica accounts")
		}

		// more defense in depth
		err := fd.verifyAccountOwnership(*file, fd.OwnHostName)
		if err != nil {
			return err
		}

		file.SubleaseTokenSecret = tokenStr
		return nil
	})
}

// ForfeitAccountName implements the keppel.FederationDriver interface.
func (fd *federationDriverSwift) ForfeitAccountName(ctx context.Context, account models.Account) error {
	// case 1: replica account -> just remove ourselves from the set of replicas
	if account.UpstreamPeerHostName != "" {
		return fd.modifyAccountFile(ctx, account.Name, func(file *accountFile, _ bool) error {
			file.ReplicaHostNames = removeStringFromList(file.ReplicaHostNames, fd.OwnHostName)
			return nil
		})
	}

	// case 2: primary account -> perform sanity checks, then delete entire account file
	file, err := fd.readAccountFile(ctx, account.Name)
	if err != nil {
		return err
	}
	err = fd.verifyAccountOwnership(file, fd.OwnHostName)
	if err != nil {
		return err
	}
	if len(file.ReplicaHostNames) > 0 {
		return fmt.Errorf("cannot delete primary account %s: %d replicas are still attached to it", account.Name, len(file.ReplicaHostNames))
	}
	return fd.accountFileObj(account.Name).Delete(ctx, nil, nil)
}

// RecordExistingAccount implements the keppel.FederationDriver interface.
func (fd *federationDriverSwift) RecordExistingAccount(ctx context.Context, account models.Account, now time.Time) error {
	// Inconsistencies can arise since we have multiple sources of truth in the
	// Keppels' own database and in the shared Swift container. These
	// inconsistencies are incredibly unlikely, however, so making this driver
	// more complicated to better guard against them is a bad tradeoff in my
	// opinion. Instead, we just make sure that the driver loudly complains once
	// it finds an inconsistency, so the operator can take care of fixing it.
	return fd.modifyAccountFile(ctx, account.Name, func(file *accountFile, _ bool) error {
		// check that the primary hostname is correct, or fill in if missing
		var expectedPrimaryHostName string
		if account.UpstreamPeerHostName == "" {
			expectedPrimaryHostName = fd.OwnHostName
		} else {
			expectedPrimaryHostName = account.UpstreamPeerHostName
		}
		switch file.PrimaryHostName {
		case "", expectedPrimaryHostName:
			file.PrimaryHostName = expectedPrimaryHostName
		default:
			return fmt.Errorf("expected primary for account %s to be hosted by %s, but is actually hosted by %q",
				account.Name, expectedPrimaryHostName, file.PrimaryHostName)
		}

		// if we are a replica, make sure our name is entered in the ReplicaHostNames
		if account.UpstreamPeerHostName != "" {
			file.ReplicaHostNames = addStringToList(file.ReplicaHostNames, fd.OwnHostName)
		}

		return nil
	})
}

func (fd *federationDriverSwift) verifyAccountOwnership(file accountFile, expectedPrimaryHostName string) error {
	if file.PrimaryHostName != expectedPrimaryHostName {
		return fmt.Errorf("expected primary for account %s to be hosted by %s, but is actually hosted by %q",
			file.AccountName, expectedPrimaryHostName, file.PrimaryHostName)
	}
	return nil
}

// FindPrimaryAccount implements the keppel.FederationDriver interface.
func (fd *federationDriverSwift) FindPrimaryAccount(ctx context.Context, accountName models.AccountName) (peerHostName string, err error) {
	file, err := fd.readAccountFile(ctx, accountName)
	if err != nil {
		return "", err
	}
	if file.PrimaryHostName == "" {
		return "", keppel.ErrNoSuchPrimaryAccount
	}
	return file.PrimaryHostName, nil
}

func addStringToList(list []string, value string) []string {
	if slices.Contains(list, value) {
		return list
	}
	return append(list, value)
}

func removeStringFromList(list []string, value string) []string {
	result := make([]string, 0, len(list))
	for _, elem := range list {
		if elem != value {
			result = append(result, elem)
		}
	}
	return result
}
