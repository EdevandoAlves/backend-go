package oidc

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
)

type TokenVerifier struct {
	verifier *gooidc.IDTokenVerifier
}

// NewTokenVerifier performs discovery now so an unavailable identity provider
// makes startup fail instead of silently disabling authentication.
func NewTokenVerifier(ctx context.Context, issuer, audience string) (*TokenVerifier, error) {
	if strings.TrimSpace(issuer) == "" || strings.TrimSpace(audience) == "" {
		return nil, fmt.Errorf("OIDC issuer and audience are required")
	}
	provider, err := gooidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery: %w", err)
	}
	return &TokenVerifier{verifier: provider.Verifier(&gooidc.Config{ClientID: audience})}, nil
}

func (v *TokenVerifier) Verify(req *http.Request, raw string) (string, error) {
	if v == nil || v.verifier == nil {
		return "", fmt.Errorf("OIDC verifier unavailable")
	}
	token, err := v.verifier.Verify(req.Context(), raw)
	if err != nil {
		return "", err
	}
	var claims struct {
		ProviderID string `json:"provider_id"`
	}
	if err := token.Claims(&claims); err != nil {
		return "", err
	}
	if strings.TrimSpace(claims.ProviderID) == "" {
		return "", fmt.Errorf("provider_id claim is missing")
	}
	return claims.ProviderID, nil
}
