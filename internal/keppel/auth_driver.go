// SPDX-FileCopyrightText: 2018 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package keppel

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"

	"github.com/redis/go-redis/v9"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/pluggable"
)

// Permission is an enum used by AuthDriver.
type Permission string

const (
	// CanViewAccount is the permission for viewing account metadata.
	CanViewAccount Permission = "view"
	// CanPullFromAccount is the permission for pulling images from this account.
	CanPullFromAccount Permission = "pull"
	// CanPushToAccount is the permission for pushing images to this account.
	CanPushToAccount Permission = "push"
	// CanDeleteFromAccount is the permission for deleting manifests from this account.
	CanDeleteFromAccount Permission = "delete"
	// CanChangeAccount is the permission for creating and updating accounts.
	CanChangeAccount Permission = "change"
	// CanViewQuotas is the permission for viewing an auth tenant's quotas.
	CanViewQuotas Permission = "viewquota"
	// CanChangeQuotas is the permission for changing an auth tenant's quotas.
	CanChangeQuotas Permission = "changequota"
)

// AuthDriver represents an authentication backend that supports multiple
// tenants. A tenant is a scope where users can be authorized to perform certain
// actions. For example, in OpenStack, a Keppel tenant is a Keystone project.
type AuthDriver interface {
	pluggable.Plugin
	// Init is called before any other interface methods, and allows the plugin to
	// perform first-time initialization. The supplied *redis.Client can be stored
	// for caching authorizations, but only if it is non-nil.
	Init(context.Context, *redis.Client) error

	// AuthenticateUser authenticates the user identified by the given username
	// and password. Note that usernames may not contain colons, because
	// credentials are encoded by clients in the "username:password" format.
	AuthenticateUser(ctx context.Context, userName, password string) (UserIdentity, *RegistryV2Error)
	// AuthenticateUserFromRequest reads credentials from the given incoming HTTP
	// request to authenticate the user which makes this request. The
	// implementation shall follow the conventions of the concrete backend, e.g. a
	// OAuth backend could try to read a Bearer token from the Authorization
	// header, whereas an OpenStack auth driver would look for a Keystone token in the
	// X-Auth-Token header.
	//
	// If the request contains no auth headers at all, (nil, nil) shall be
	// returned to trigger the codepath for anonymous users.
	AuthenticateUserFromRequest(r *http.Request) (UserIdentity, *RegistryV2Error)
}

// AuthDriverRegistry is a pluggable.Registry for AuthDriver implementations.
var AuthDriverRegistry pluggable.Registry[AuthDriver]

// NewAuthDriver creates a new AuthDriver using one of the plugins registered
// with AuthDriverRegistry.
func NewAuthDriver(ctx context.Context, pluginTypeID string, rc *redis.Client) (AuthDriver, error) {
	logg.Debug("initializing auth driver %q...", pluginTypeID)

	ad := AuthDriverRegistry.Instantiate(pluginTypeID)
	if ad == nil {
		return nil, errors.New("no such auth driver: " + pluginTypeID)
	}
	return ad, ad.Init(ctx, rc)
}

// BuildBasicAuthHeader constructs the value of an "Authorization" HTTP header for the given basic auth credentials.
func BuildBasicAuthHeader(userName, password string) string {
	creds := userName + ":" + password
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(creds))
}
