// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"context"
	"errors"
	"time"

	"github.com/sapcc/keppel/internal/models"

	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/pluggable"
)

// ClaimResult is an enum returned by FederationDriver.ClaimAccountName().
type ClaimResult int

const (
	// ClaimSucceeded indicates that ClaimAccountName() returned with a nil error.
	ClaimSucceeded ClaimResult = iota
	// ClaimFailed indicates that ClaimAccountName() returned with an error
	// because the user did not have permission to claim the account in question.
	ClaimFailed
	// ClaimErrored indicates that ClaimAccountName() returned with an error
	// because of an unexpected problem on the server side.
	ClaimErrored
)

// ErrNoSuchPrimaryAccount is returned by FederationDriver.FindPrimaryAccount if
// no peer has the given primary account.
var ErrNoSuchPrimaryAccount = errors.New("no such primary account")

// FederationDriver is the abstract interface for a strategy that coordinates
// the claiming of account names across Keppel deployments.
type FederationDriver interface {
	pluggable.Plugin
	// Init is called before any other interface methods, and allows the plugin to
	// perform first-time initialization.
	//
	// Implementations should inspect the auth driver to ensure that the
	// federation driver can work with this authentication method, or return
	// ErrAuthDriverMismatch otherwise.
	Init(context.Context, AuthDriver, Configuration) error

	// ClaimAccountName is called when creating a new account, and returns nil if
	// and only if this Keppel is allowed to use `account.Name` for the given new
	// `account`.
	//
	// For some drivers, creating a replica account requires confirmation from the
	// Keppel hosting the primary account. This is done by issuing a sublease
	// token secret on the primary account using IssueSubleaseTokenSecret(), then
	// presenting this `subleaseTokenSecret` to this method.
	//
	// The implementation MUST be idempotent. If a call returned nil, a subsequent
	// call with the same `account` must also return nil unless
	// ForfeitAccountName() was called in between.
	ClaimAccountName(ctx context.Context, account models.Account, subleaseTokenSecret string) (ClaimResult, error)

	// IssueSubleaseTokenSecret may only be called on existing primary accounts,
	// not on replica accounts. It generates a secret one-time token that other
	// Keppels can use to verify that the caller is allowed to create a replica
	// account for this primary account.
	//
	// Sublease tokens are optional. If ClaimAccountName does not inspect its
	// `subleaseTokenSecret` parameter, this method shall return ("", nil).
	IssueSubleaseTokenSecret(ctx context.Context, account models.Account) (string, error)

	// ForfeitAccountName is the inverse operation of ClaimAccountName. It is used
	// when deleting an account and releases this Keppel's claim on the account
	// name.
	ForfeitAccountName(ctx context.Context, account models.Account) error

	// RecordExistingAccount is called regularly for each account in our database.
	// The driver implementation can use this call to ensure that the existence of
	// this account is tracked in its storage. (We don't expect this to require
	// any actual work during normal operation. The purpose of this mechanism is
	// to aid in switching between federation drivers.)
	//
	// The `now` argument contains the value of time.Now(). It may refer to an
	// artificial wall clock during unit tests.
	RecordExistingAccount(ctx context.Context, account models.Account, now time.Time) error

	// FindPrimaryAccount is used to redirect anycast requests for accounts that
	// do not exist locally. It shell return the hostname of the peer that hosts
	// the primary account. If no account with this name exists anywhere,
	// ErrNoSuchPrimaryAccount shall be returned.
	FindPrimaryAccount(ctx context.Context, accountName models.AccountName) (peerHostName string, err error)
}

// FederationDriverRegistry is a pluggable.Registry for FederationDriver implementations.
var FederationDriverRegistry pluggable.Registry[FederationDriver]

// NewFederationDriver creates a new FederationDriver using one of the plugins
// registered with FederationDriverRegistry.
func NewFederationDriver(ctx context.Context, pluginTypeID string, ad AuthDriver, cfg Configuration) (FederationDriver, error) {
	logg.Debug("initializing federation driver %q...", pluginTypeID)

	fd := FederationDriverRegistry.Instantiate(pluginTypeID)
	if fd == nil {
		return nil, errors.New("no such federation driver: " + pluginTypeID)
	}
	return fd, fd.Init(ctx, ad, cfg)
}
