// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package processor

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"

	"github.com/sapcc/keppel/internal/auth"
	peerclient "github.com/sapcc/keppel/internal/client/peer"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"

	"github.com/sapcc/go-api-declarations/cadf"
	"github.com/sapcc/go-bits/audittools"
	"github.com/sapcc/go-bits/sqlext"
)

// GetPlatformFilterFromPrimaryAccount takes a replica account and queries the peer holding the primary account for that account.
func (p *Processor) GetPlatformFilterFromPrimaryAccount(ctx context.Context, peer models.Peer, replicaAccount models.Account) (models.PlatformFilter, error) {
	viewScope := auth.Scope{
		ResourceType: "keppel_account",
		ResourceName: string(replicaAccount.Name),
		Actions:      []string{"view"},
	}
	client, err := peerclient.New(ctx, p.cfg, peer, viewScope)
	if err != nil {
		return nil, err
	}

	var upstreamAccount keppel.Account
	err = client.GetForeignAccountConfigurationInto(ctx, &upstreamAccount, replicaAccount.Name)
	if err != nil {
		return nil, err
	}
	return upstreamAccount.PlatformFilter, nil
}

var looksLikeAPIVersionRx = regexp.MustCompile(`^v[0-9][1-9]*$`)
var ErrAccountNameEmpty = errors.New("account name cannot be empty string")

// CreateOrUpdate can be used on an API account and returns the database representation of it.
func (p *Processor) CreateOrUpdateAccount(ctx context.Context, account keppel.Account, userInfo audittools.UserInfo, r *http.Request, getSubleaseToken func(models.Peer) (keppel.SubleaseToken, error), setCustomFields func(*models.Account) *keppel.RegistryV2Error) (models.Account, *keppel.RegistryV2Error) {
	if account.Name == "" {
		return models.Account{}, keppel.AsRegistryV2Error(ErrAccountNameEmpty)
	}
	// reserve identifiers for internal pseudo-accounts and anything that might
	// appear like the first path element of a legal endpoint path on any of our
	// APIs (we will soon start recognizing image-like URLs such as
	// keppel.example.org/account/repo and offer redirection to a suitable UI;
	// this requires the account name to not overlap with API endpoint paths)
	if strings.HasPrefix(string(account.Name), "keppel") {
		return models.Account{}, keppel.AsRegistryV2Error(errors.New(`account names with the prefix "keppel" are reserved for internal use`)).WithStatus(http.StatusUnprocessableEntity)
	}
	if looksLikeAPIVersionRx.MatchString(string(account.Name)) {
		return models.Account{}, keppel.AsRegistryV2Error(errors.New(`account names that look like API versions (e.g. v1) are reserved for internal use`)).WithStatus(http.StatusUnprocessableEntity)
	}

	// check if account already exists
	originalAccount, err := keppel.FindAccount(p.db, account.Name)
	if err != nil {
		return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusInternalServerError)
	}
	if originalAccount != nil && originalAccount.AuthTenantID != account.AuthTenantID {
		return models.Account{}, keppel.AsRegistryV2Error(errors.New(`account name already in use by a different tenant`)).WithStatus(http.StatusConflict)
	}

	// PUT can either create a new account or update an existing account;
	// this distinction is important because several fields can only be set at creation
	var targetAccount models.Account
	if originalAccount == nil {
		targetAccount = models.Account{
			Name:                     account.Name,
			AuthTenantID:             account.AuthTenantID,
			SecurityScanPoliciesJSON: "[]",
			TagPoliciesJSON:          "[]",
			// all other attributes are set below or in the ApplyToAccount() methods called below
		}
	} else {
		targetAccount = *originalAccount
	}

	// validate and update fields as requested
	targetAccount.IsDeleting = account.State == "deleting"

	// validate GC policies
	if len(account.GCPolicies) == 0 {
		targetAccount.GCPoliciesJSON = "[]"
	} else {
		for _, policy := range account.GCPolicies {
			err := policy.Validate()
			if err != nil {
				return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusUnprocessableEntity)
			}
		}
		buf, _ := json.Marshal(account.GCPolicies)
		targetAccount.GCPoliciesJSON = string(buf)
	}

	// validate Tag policies
	if len(account.TagPolicies) == 0 {
		targetAccount.TagPoliciesJSON = "[]"
	} else {
		for _, policy := range account.TagPolicies {
			err := policy.Validate()
			if err != nil {
				return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusUnprocessableEntity)
			}
		}
		buf, _ := json.Marshal(account.TagPolicies)
		targetAccount.TagPoliciesJSON = string(buf)
	}

	// validate replication policy (for OnFirstUseStrategy, the peer hostname is
	// checked for correctness down below when validating the platform filter)
	var originalStrategy keppel.ReplicationStrategy
	if originalAccount != nil {
		rp := keppel.RenderReplicationPolicy(*originalAccount)
		if rp == nil {
			originalStrategy = keppel.NoReplicationStrategy
		} else {
			originalStrategy = rp.Strategy
		}
	}

	var replicationStrategy keppel.ReplicationStrategy
	if account.ReplicationPolicy == nil {
		if originalAccount == nil {
			replicationStrategy = keppel.NoReplicationStrategy
		} else {
			// PUT on existing account can omit replication policy to reuse existing policy
			replicationStrategy = originalStrategy
		}
	} else {
		// on existing accounts, we do not allow changing the strategy
		rp := *account.ReplicationPolicy
		if originalAccount != nil && originalStrategy != rp.Strategy {
			return models.Account{}, keppel.AsRegistryV2Error(keppel.ErrIncompatibleReplicationPolicy).WithStatus(http.StatusConflict)
		}

		err := rp.ApplyToAccount(&targetAccount)
		if errors.Is(err, keppel.ErrIncompatibleReplicationPolicy) {
			return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusConflict)
		} else if err != nil {
			return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusUnprocessableEntity)
		}
		replicationStrategy = rp.Strategy
	}

	// validate RBAC policies
	if len(account.RBACPolicies) == 0 {
		targetAccount.RBACPoliciesJSON = ""
	} else {
		for idx, policy := range account.RBACPolicies {
			err := policy.ValidateAndNormalize(replicationStrategy)
			if err != nil {
				return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusUnprocessableEntity)
			}
			account.RBACPolicies[idx] = policy
		}
		buf, _ := json.Marshal(account.RBACPolicies)
		targetAccount.RBACPoliciesJSON = string(buf)
	}

	// validate validation policy
	if account.ValidationPolicy != nil {
		rerr := account.ValidationPolicy.ApplyToAccount(&targetAccount)
		if rerr != nil {
			return models.Account{}, rerr
		}
	}

	var peer models.Peer
	if targetAccount.UpstreamPeerHostName != "" {
		// NOTE: This validates UpstreamPeerHostName as a side effect.
		peer, err = keppel.GetPeerFromAccount(p.db, targetAccount)
		if errors.Is(err, sql.ErrNoRows) {
			msg := fmt.Errorf(`unknown peer registry: %q`, targetAccount.UpstreamPeerHostName)
			return models.Account{}, keppel.AsRegistryV2Error(msg).WithStatus(http.StatusUnprocessableEntity)
		}
		if err != nil {
			return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusInternalServerError)
		}
	}

	// validate platform filter
	if originalAccount == nil {
		switch replicationStrategy {
		case keppel.NoReplicationStrategy:
			if account.PlatformFilter != nil {
				return models.Account{}, keppel.AsRegistryV2Error(errors.New(`platform filter is only allowed on replica accounts`)).WithStatus(http.StatusUnprocessableEntity)
			}
		case keppel.FromExternalOnFirstUseStrategy:
			targetAccount.PlatformFilter = account.PlatformFilter
		case keppel.OnFirstUseStrategy:
			// for internal replica accounts, the platform filter must match that of the primary account,
			// either by specifying the same filter explicitly or omitting it
			upstreamPlatformFilter, err := p.GetPlatformFilterFromPrimaryAccount(ctx, peer, targetAccount)
			if err != nil {
				return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusInternalServerError)
			}

			if account.PlatformFilter != nil && !upstreamPlatformFilter.IsEqualTo(account.PlatformFilter) {
				jsonPlatformFilter, _ := json.Marshal(account.PlatformFilter)
				jsonFilter, _ := json.Marshal(upstreamPlatformFilter)
				msg := fmt.Sprintf("peer account filter needs to match primary account filter: local account %s, peer account %s ", jsonPlatformFilter, jsonFilter)
				return models.Account{}, keppel.AsRegistryV2Error(errors.New(msg)).WithStatus(http.StatusConflict)
			}
			targetAccount.PlatformFilter = upstreamPlatformFilter
		}
	} else if account.PlatformFilter != nil && !originalAccount.PlatformFilter.IsEqualTo(account.PlatformFilter) {
		return models.Account{}, keppel.AsRegistryV2Error(errors.New(`cannot change platform filter on existing account`)).WithStatus(http.StatusConflict)
	}

	rerr := setCustomFields(&targetAccount)
	if rerr != nil {
		return models.Account{}, rerr
	}

	// create account if required
	if originalAccount == nil {
		// sublease tokens are only relevant when creating replica accounts
		subleaseTokenSecret := ""
		if targetAccount.UpstreamPeerHostName != "" {
			subleaseToken, err := getSubleaseToken(peer)
			if err != nil {
				return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusBadRequest)
			}
			subleaseTokenSecret = subleaseToken.Secret
		}

		// check permission to claim account name (this only happens here because
		// it's only relevant for account creations, not for updates)
		claimResult, err := p.fd.ClaimAccountName(ctx, targetAccount, subleaseTokenSecret)
		switch claimResult {
		case keppel.ClaimSucceeded:
			// nothing to do
		case keppel.ClaimFailed:
			// user error
			return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusForbidden)
		case keppel.ClaimErrored:
			// server error
			return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusInternalServerError)
		}

		err = p.sd.CanSetupAccount(ctx, targetAccount.Reduced())
		if err != nil {
			msg := fmt.Errorf("cannot set up backing storage for this account: %w", err)
			return models.Account{}, keppel.AsRegistryV2Error(msg).WithStatus(http.StatusConflict)
		}

		tx, err := p.db.Begin()
		if err != nil {
			return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusInternalServerError)
		}
		defer sqlext.RollbackUnlessCommitted(tx)

		err = tx.Insert(&targetAccount)
		if err != nil {
			return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusInternalServerError)
		}

		// commit the changes
		err = tx.Commit()
		if err != nil {
			return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusInternalServerError)
		}

		if userInfo != nil {
			p.auditor.Record(audittools.Event{
				Time:       p.timeNow(),
				Request:    r,
				User:       userInfo,
				ReasonCode: http.StatusOK,
				Action:     cadf.CreateAction,
				Target:     AuditAccount{Account: targetAccount},
			})
		}
	} else {
		// originalAccount != nil: update if necessary
		if !reflect.DeepEqual(*originalAccount, targetAccount) {
			_, err := p.db.Update(&targetAccount)
			if err != nil {
				return models.Account{}, keppel.AsRegistryV2Error(err).WithStatus(http.StatusInternalServerError)
			}
		}

		// audit log is necessary for all changes except to InMaintenance
		if userInfo != nil {
			originalAccount.IsDeleting = targetAccount.IsDeleting
			if !reflect.DeepEqual(*originalAccount, targetAccount) {
				p.auditor.Record(audittools.Event{
					Time:       p.timeNow(),
					Request:    r,
					User:       userInfo,
					ReasonCode: http.StatusOK,
					Action:     cadf.UpdateAction,
					Target:     AuditAccount{Account: targetAccount},
				})
			}
		}
	}

	return targetAccount, nil
}

var (
	markAccountForDeletion = `UPDATE accounts SET is_deleting = TRUE, next_deletion_attempt_at = $1 WHERE name = $2`
)

func (p *Processor) MarkAccountForDeletion(account models.Account, actx keppel.AuditContext) error {
	_, err := p.db.Exec(markAccountForDeletion, p.timeNow(), account.Name)
	if err != nil {
		return err
	}

	if userInfo := actx.UserIdentity.UserInfo(); userInfo != nil {
		p.auditor.Record(audittools.Event{
			Time:       p.timeNow(),
			Request:    actx.Request,
			User:       userInfo,
			ReasonCode: http.StatusOK,
			Action:     cadf.DeleteAction,
			Target:     AuditAccount{Account: account},
		})
	}

	return nil
}
