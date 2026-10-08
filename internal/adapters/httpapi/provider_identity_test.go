package httpapi

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeAccessTokenVerifier struct {
	providerID string
	err        error
	token      string
}

func (v *fakeAccessTokenVerifier) Verify(_ *http.Request, token string) (string, error) {
	v.token = token
	return v.providerID, v.err
}

func TestOIDCProviderIdentityAuthenticatesBearerToken(t *testing.T) {
	verifier := &fakeAccessTokenVerifier{providerID: "provider-a"}
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Bearer access-token")

	principal, err := (OIDCProviderIdentity{Verifier: verifier}).Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if principal.ProviderID != "provider-a" || verifier.token != "access-token" {
		t.Fatalf("Authenticate() = %#v, verifier token = %q", principal, verifier.token)
	}
}

func TestOIDCProviderIdentityRejectsInvalidProviderIdentity(t *testing.T) {
	tests := []struct {
		name   string
		header string
		verify *fakeAccessTokenVerifier
	}{
		{name: "missing bearer", verify: &fakeAccessTokenVerifier{}},
		{name: "wrong scheme", header: "Basic abc", verify: &fakeAccessTokenVerifier{}},
		{name: "malformed bearer", header: "Bearer", verify: &fakeAccessTokenVerifier{}},
		{name: "bearer with extra fields", header: "Bearer token extra", verify: &fakeAccessTokenVerifier{}},
		{name: "invalid token", header: "Bearer token", verify: &fakeAccessTokenVerifier{err: errors.New("invalid")}},
		{name: "internal token without provider claim", header: "Bearer token", verify: &fakeAccessTokenVerifier{}},
		{name: "blank provider claim", header: "Bearer token", verify: &fakeAccessTokenVerifier{providerID: " \t"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/", nil)
			req.Header.Set("Authorization", tt.header)
			_, err := (OIDCProviderIdentity{Verifier: tt.verify}).Authenticate(req)
			if !errors.Is(err, ErrUnauthenticated) {
				t.Fatalf("Authenticate() error = %v, want ErrUnauthenticated", err)
			}
		})
	}
}

func TestOIDCProviderIdentityMapsVerifierErrorsToUnauthenticated(t *testing.T) {
	secret := errors.New("token signature details must not escape")
	verifier := &fakeAccessTokenVerifier{err: secret}
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")

	_, err := (OIDCProviderIdentity{Verifier: verifier}).Authenticate(req)
	if !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("Authenticate() error = %v, want ErrUnauthenticated", err)
	}
	if errors.Is(err, secret) || err.Error() == secret.Error() {
		t.Fatalf("Authenticate() leaked verifier error: %v", err)
	}
}

func TestOIDCProviderIdentityRejectsUnavailableVerifier(t *testing.T) {
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("Authorization", "Bearer token")
	_, err := (OIDCProviderIdentity{}).Authenticate(req)
	if !errors.Is(err, ErrIdentityUnavailable) {
		t.Fatalf("Authenticate() error = %v, want ErrIdentityUnavailable", err)
	}
}
