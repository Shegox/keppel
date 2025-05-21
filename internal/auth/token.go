// SPDX-FileCopyrightText: 2018-2021 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"crypto"
	"crypto/ed25519"
	"crypto/rsa"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gofrs/uuid/v5"
	"github.com/golang-jwt/jwt/v5"

	"github.com/sapcc/keppel/internal/keppel"
)

func init() {
	// required for backwards-compatibility with existing tokens
	jwt.MarshalSingleStringAsArray = false
}

// Type representation for JWT claims issued by Keppel.
type tokenClaims struct {
	jwt.RegisteredClaims
	Access   []Scope              `json:"access"`
	Embedded embeddedUserIdentity `json:"kea"` // kea = keppel embedded authorization ("UserIdentity" used to be called "Authorization")
}

func parseToken(cfg keppel.Configuration, ad keppel.AuthDriver, audience Audience, tokenStr string) (*Authorization, *keppel.RegistryV2Error) {
	// this function is used by jwt.ParseWithClaims() to select which public key to use for validation
	keyFunc := func(t *jwt.Token) (any, error) {
		// check the token header to see which key we used for signing
		ourIssuerKeys := audience.IssuerKeys(cfg)
		for _, ourIssuerKey := range ourIssuerKeys {
			if t.Header["jwk"] == serializePublicKey(ourIssuerKey) {
				// check that the signing method matches what we generate
				ourSigningMethod := chooseSigningMethod(ourIssuerKey)
				if !equalSigningMethods(ourSigningMethod, t.Method) {
					return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
				}

				// jwt.Parse needs the public key to validate the token
				return derivePublicKey(ourIssuerKey), nil
			}
		}

		return nil, errors.New("token signed by unknown key")
	}

	// parse JWT
	publicHost := audience.Hostname(cfg)
	parserOpts := []jwt.ParserOption{
		jwt.WithStrictDecoding(),
		jwt.WithLeeway(3 * time.Second),
		jwt.WithAudience(publicHost),
	}
	if !audience.IsAnycast {
		// For anycast tokens, we don't verify the issuer. Any of our peers could
		// have issued the token.
		parserOpts = append(parserOpts, jwt.WithIssuer("keppel-api@"+publicHost))
	}

	var claims tokenClaims
	claims.Embedded.AuthDriver = ad
	token, err := jwt.ParseWithClaims(tokenStr, &claims, keyFunc, parserOpts...)
	if err != nil {
		return nil, keppel.ErrUnauthorized.With(err.Error())
	}
	if !token.Valid {
		//NOTE: This branch is defense in depth. As of the time of this writing,
		// token.Valid == false if and only if err != nil.
		return nil, keppel.ErrUnauthorized.With("token invalid")
	}

	var ss ScopeSet
	for _, scope := range claims.Access {
		ss.Add(scope)
	}
	return &Authorization{
		UserIdentity: claims.Embedded.UserIdentity,
		ScopeSet:     ss,
		Audience:     audience,
	}, nil
}

// TokenResponse is the format expected by Docker in an auth response. The Token
// field contains a Java Web Token (JWT).
type TokenResponse struct {
	Token     string `json:"token"`
	ExpiresIn uint64 `json:"expires_in"`
	IssuedAt  string `json:"issued_at"`
}

// IssueToken renders the given Authorization into a JWT token that can be used
// as a Bearer token to authenticate on Keppel's various APIs.
func (a Authorization) IssueToken(cfg keppel.Configuration) (*TokenResponse, error) {
	return a.IssueTokenWithExpires(cfg, 4*time.Hour)
}

// IssueTokenWithExpires renders the given Authorization into a JWT token that can be used
// as a Bearer token to authenticate on Keppel's various APIs with configurable expiring time
func (a Authorization) IssueTokenWithExpires(cfg keppel.Configuration, expiresIn time.Duration) (*TokenResponse, error) {
	now := time.Now()
	expiresAt := now.Add(expiresIn)

	issuerKeys := a.Audience.IssuerKeys(cfg)
	if len(issuerKeys) == 0 {
		return nil, errors.New("no issuer keys configured for this audience")
	}
	issuerKey := issuerKeys[0]
	method := chooseSigningMethod(issuerKey)

	// fill the "issuer" field with a dummy audience that has anycast forced to
	// false to reveal the identity of the Keppel API that issued the token
	issuer := Audience{IsAnycast: false, AccountName: a.Audience.AccountName}

	uuidV4, err := uuid.NewV4()
	if err != nil {
		return nil, err
	}
	publicHost := a.Audience.Hostname(cfg)
	token := jwt.NewWithClaims(method, tokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuidV4.String(),
			Audience:  jwt.ClaimStrings{publicHost},
			Issuer:    "keppel-api@" + issuer.Hostname(cfg),
			Subject:   a.UserIdentity.UserName(),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			NotBefore: jwt.NewNumericDate(now.Add(-1 * time.Second)), // set slightly in the past to account for clock skew between token issuer and user
			IssuedAt:  jwt.NewNumericDate(now),
		},
		// access permissions granted to this token
		Access:   a.ScopeSet.Flatten(),
		Embedded: embeddedUserIdentity{UserIdentity: a.UserIdentity},
	})
	// we need to remember which key we used for this token, to choose the right
	// key for validation during parseToken()
	token.Header["jwk"] = serializePublicKey(issuerKey)

	tokenStr, err := token.SignedString(issuerKey)
	return &TokenResponse{
		Token:     tokenStr,
		ExpiresIn: uint64(expiresAt.Sub(now).Seconds()),
		IssuedAt:  now.Format(time.RFC3339),
	}, err
}

func chooseSigningMethod(key crypto.PrivateKey) jwt.SigningMethod {
	switch key.(type) {
	case ed25519.PrivateKey:
		return jwt.SigningMethodEdDSA
	case *rsa.PrivateKey:
		return jwt.SigningMethodRS256
	default:
		panic(fmt.Sprintf("do not know which JWT method to use for issuerKey.type = %T", key))
	}
}

func derivePublicKey(key crypto.PrivateKey) crypto.PublicKey {
	switch key := key.(type) {
	case ed25519.PrivateKey:
		return key.Public()
	case *rsa.PrivateKey:
		return key.Public()
	default:
		panic(fmt.Sprintf("do not know which JWT method to use for issuerKey.type = %T", key))
	}
}

func serializePublicKey(key crypto.PrivateKey) string {
	switch key := key.(type) {
	case ed25519.PrivateKey:
		pubkey := key.Public().(ed25519.PublicKey)
		return hex.EncodeToString([]byte(pubkey))
	case *rsa.PrivateKey:
		pubkey := key.Public().(*rsa.PublicKey)
		return fmt.Sprintf("%x:%s", pubkey.E, pubkey.N.Text(16))
	default:
		panic(fmt.Sprintf("do not know which JWT method to use for issuerKey.type = %T", key))
	}
}

func equalSigningMethods(m1, m2 jwt.SigningMethod) bool {
	switch m1 := m1.(type) {
	case *jwt.SigningMethodEd25519:
		if m2, ok := m2.(*jwt.SigningMethodEd25519); ok {
			return *m1 == *m2
		}
		return false
	case *jwt.SigningMethodECDSA:
		if m2, ok := m2.(*jwt.SigningMethodECDSA); ok {
			return *m1 == *m2
		}
		return false
	case *jwt.SigningMethodRSA:
		if m2, ok := m2.(*jwt.SigningMethodRSA); ok {
			return *m1 == *m2
		}
		return false
	default:
		panic(fmt.Sprintf("do not know how to compare signing methods of type %T", m1))
	}
}

////////////////////////////////////////////////////////////////////////////////
// type embeddedUserIdentity

// Wraps an UserIdentity such that it can be serialized into JSON.
type embeddedUserIdentity struct {
	UserIdentity keppel.UserIdentity
	// AuthDriver is ignored during serialization, but must be filled prior to
	// deserialization because some types of UserIdentity require their
	// respective AuthDriver to deserialize properly.
	AuthDriver keppel.AuthDriver
}

// MarshalJSON implements the json.Marshaler interface.
func (e embeddedUserIdentity) MarshalJSON() ([]byte, error) {
	payload, err := e.UserIdentity.SerializeToJSON()
	if err != nil {
		return nil, err
	}

	// The straight-forward approach would be to serialize as
	// `{"type":"foo","payload":"something"}`, but we serialize as
	// `{"foo":"something"}` instead to shave off a few bytes.
	typeID := e.UserIdentity.PluginTypeID()
	return json.Marshal(map[string]json.RawMessage{typeID: json.RawMessage(payload)})
}

// UnmarshalJSON implements the json.Marshaler interface.
func (e *embeddedUserIdentity) UnmarshalJSON(in []byte) error {
	if e.AuthDriver == nil {
		return errors.New("cannot unmarshal EmbeddedAuthorization without an AuthDriver")
	}

	m := make(map[string]json.RawMessage)
	err := json.Unmarshal(in, &m)
	if err != nil {
		return err
	}
	if len(m) != 1 {
		return fmt.Errorf("cannot unmarshal EmbeddedAuthorization with %d components", len(m))
	}

	for typeID, payload := range m {
		e.UserIdentity, err = keppel.DeserializeUserIdentity(typeID, []byte(payload), e.AuthDriver)
		if err != nil {
			return fmt.Errorf("cannot unmarshal EmbeddedAuthorization of type %q: %w", typeID, err)
		}
		return nil
	}

	// the loop body executes exactly once, therefore this location is unreachable
	panic("unreachable")
}
