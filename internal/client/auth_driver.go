// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"errors"
	"net/http"

	"github.com/sapcc/go-bits/logg"
)

// AuthDriver is the client-side counterpart of keppel.AuthDriver. It implements
// support for talking to the Keppel API using the corresponding server-side
// authentication driver.
type AuthDriver interface {
	// MatchesEnvironment checks the process's environment variables to see if
	// they contain credentials for this authentication method. This is how we
	// decide which AuthDriver to use.
	MatchesEnvironment() bool

	// Connect sets up a connection to a Keppel server, using the credentials from
	// the process's environment variables.
	Connect(context.Context) error

	// CurrentAuthTenantID returns the ID of the auth tenant where the client is
	// authenticated.
	CurrentAuthTenantID() string
	// ServerHost returns the server's hostname. May be of the form "host:port".
	// May panic when called before Connect().
	ServerHost() string
	// ServerScheme returns "http" or "https" to indicate whether the server
	// exposes an encrypted or unencrypted API.
	ServerScheme() string
	// SendHTTPRequest sends a HTTP request to the Keppel API. The implementation
	// will fill in the correct server hostname and add any required auth headers.
	// May panic when called before Connect().
	SendHTTPRequest(req *http.Request) (*http.Response, error)

	// CredentialsForRegistryAPI returns a pair of username and password that can
	// be used on the Registry API of the same Keppel instance to obtain
	// equivalent access.
	CredentialsForRegistryAPI() (userName, password string)
}

var authDriverFactories = make(map[string]func() AuthDriver)

// RegisterAuthDriver registers an AuthDriver. Call this from func init() of the
// package defining the AuthDriver.
func RegisterAuthDriver(name string, factory func() AuthDriver) {
	if _, exists := authDriverFactories[name]; exists {
		panic("attempted to register multiple auth drivers with name = " + name)
	}
	authDriverFactories[name] = factory
}

var errNoMatchingAuthDriver = errors.New("no auth driver selected (did you set all the required environment variables?)")

// NewAuthDriver selects the correct AuthDriver and executes its Connect() method.
func NewAuthDriver(ctx context.Context) (AuthDriver, error) {
	for id, factory := range authDriverFactories {
		ad := factory()
		if ad.MatchesEnvironment() {
			logg.Debug("using auth driver %q", id)
			err := ad.Connect(ctx)
			return ad, err
		}
	}
	return nil, errNoMatchingAuthDriver
}
