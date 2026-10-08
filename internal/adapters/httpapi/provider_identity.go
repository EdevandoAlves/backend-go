package httpapi

import (
	"errors"
	"net/http"
	"strings"
)

var ErrUnauthenticated = errors.New("unauthenticated")
var ErrIdentityUnavailable = errors.New("identity unavailable")

type ProviderPrincipal struct{ ProviderID string }
type ProviderIdentity interface {
	Authenticate(*http.Request) (ProviderPrincipal, error)
}

type AccessTokenVerifier interface {
	Verify(*http.Request, string) (string, error)
}

type OIDCProviderIdentity struct{ Verifier AccessTokenVerifier }

func (i OIDCProviderIdentity) Authenticate(r *http.Request) (ProviderPrincipal, error) {
	if i.Verifier == nil {
		return ProviderPrincipal{}, ErrIdentityUnavailable
	}
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return ProviderPrincipal{}, ErrUnauthenticated
	}
	providerID, err := i.Verifier.Verify(r, parts[1])
	if err != nil {
		return ProviderPrincipal{}, ErrUnauthenticated
	}
	if strings.TrimSpace(providerID) == "" {
		return ProviderPrincipal{}, ErrUnauthenticated
	}
	return ProviderPrincipal{ProviderID: providerID}, nil
}

type FailClosedIdentity struct{}

func (FailClosedIdentity) Authenticate(*http.Request) (ProviderPrincipal, error) {
	return ProviderPrincipal{}, ErrIdentityUnavailable
}
