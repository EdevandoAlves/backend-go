package httpapi

import (
	"errors"
	"net/http"
)

var ErrUnauthenticated = errors.New("unauthenticated")
var ErrIdentityUnavailable = errors.New("identity unavailable")

type ProviderPrincipal struct{ ProviderID string }
type ProviderIdentity interface {
	Authenticate(*http.Request) (ProviderPrincipal, error)
}
type FailClosedIdentity struct{}

func (FailClosedIdentity) Authenticate(*http.Request) (ProviderPrincipal, error) {
	return ProviderPrincipal{}, ErrIdentityUnavailable
}
