// SPDX-FileCopyrightText: 2018 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package authapi

import (
	"errors"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/sapcc/go-bits/errext"
	"github.com/sapcc/go-bits/httpapi"
	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/respondwith"

	"github.com/sapcc/keppel/internal/auth"
	"github.com/sapcc/keppel/internal/keppel"
	"github.com/sapcc/keppel/internal/models"
)

// API contains state variables used by the Auth API endpoint.
type API struct {
	cfg        keppel.Configuration
	authDriver keppel.AuthDriver
	fd         keppel.FederationDriver
	db         *keppel.DB
}

// NewAPI constructs a new API instance.
func NewAPI(cfg keppel.Configuration, ad keppel.AuthDriver, fd keppel.FederationDriver, db *keppel.DB) *API {
	return &API{cfg, ad, fd, db}
}

// AddTo implements the api.API interface.
func (a *API) AddTo(r *mux.Router) {
	r.Methods("GET").Path("/keppel/v1/auth").HandlerFunc(a.handleGetAuth)
	r.Methods("POST").Path("/keppel/v1/auth/peering").HandlerFunc(a.handlePostPeering)
}

func respondWithError(w http.ResponseWriter, code int, err error) bool {
	if rerr, ok := errext.As[*keppel.RegistryV2Error](err); ok {
		if rerr != nil {
			rerr.WriteAsAuthResponseTo(w)
			return true
		}
		return false
	}

	if err != nil {
		respondwith.JSON(w, code, map[string]string{"details": err.Error()})
		return true
	}
	return false
}

func (a *API) handleGetAuth(w http.ResponseWriter, r *http.Request) {
	httpapi.IdentifyEndpoint(r, "/keppel/v1/auth")

	// parse request
	req, err := parseRequest(r.URL.RawQuery, a.cfg)
	if respondWithError(w, http.StatusBadRequest, err) {
		return
	}

	// special cases for anycast requests
	if req.IntendedAudience.IsAnycast {
		if len(req.Scopes) > 1 {
			//NOTE: This is not a fundamental restriction, there was just no demand for
			// it yet. If the requirement comes up, we could ask all relevant upstreams
			// for tokens and issue one token that grants the sum of all accesses.
			respondWithError(w, http.StatusInternalServerError, errors.New("anycast tokens cannot be issued for multiple scopes at once"))
			return
		}

		if len(req.Scopes) == 1 {
			scope := req.Scopes[0]
			if scope.ResourceType == "repository" {
				repoScope := scope.ParseRepositoryScope(req.IntendedAudience)
				accountExists, err := keppel.DoesAccountExist(a.db, repoScope.AccountName)
				if respondWithError(w, http.StatusInternalServerError, err) {
					return
				}

				// if we don't have this account locally, but the request is an anycast
				// request and one of our peers has the account, ask them to issue the token
				if !accountExists {
					err := a.reverseProxyTokenReqToUpstream(w, r, req.IntendedAudience, repoScope.AccountName)
					if !errors.Is(err, keppel.ErrNoSuchPrimaryAccount) {
						respondWithError(w, http.StatusInternalServerError, err)
						return
					}
				}
			}
		}
	}

	authz, _, rerr := auth.IncomingRequest{
		HTTPRequest:              r,
		Scopes:                   req.Scopes,
		AllowsAnycast:            true,
		AllowsDomainRemapping:    true,
		AudienceForTokenIssuance: &req.IntendedAudience,
		PartialAccessAllowed:     true,
	}.Authorize(r.Context(), a.cfg, a.authDriver, a.db)
	if rerr != nil {
		rerr.WriteAsAuthResponseTo(w)
		return
	}

	tokenResponse, err := authz.IssueToken(a.cfg)
	if respondWithError(w, http.StatusBadRequest, err) {
		return
	}
	respondwith.JSON(w, http.StatusOK, tokenResponse)
}

func (a *API) reverseProxyTokenReqToUpstream(w http.ResponseWriter, r *http.Request, audience auth.Audience, accountName models.AccountName) error {
	primaryHostName, err := a.fd.FindPrimaryAccount(r.Context(), accountName)
	if err != nil {
		return err
	}

	// protect against infinite forwarding loops in case different Keppels have
	// different ideas about who is the primary account
	if forwardedBy := r.URL.Query().Get("X-Keppel-Forwarded-By"); forwardedBy != "" {
		logg.Error("not forwarding anycast token request for account %q to %s because request was already forwarded to us by %s",
			accountName, primaryHostName, forwardedBy)
		return errors.New("request blocked by reverse-proxy loop protection")
	}

	return a.cfg.ReverseProxyAnycastRequestToPeer(w, r, audience.MapPeerHostname(primaryHostName))
}
