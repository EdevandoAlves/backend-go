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

type Claims struct {
	ProviderID string
	Scope      string
	Roles      []string
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
	providerID, _, _, err := v.VerifyClaims(req, raw)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(providerID) == "" {
		return "", fmt.Errorf("provider_id claim is missing")
	}
	return providerID, nil
}

func (v *TokenVerifier) VerifyClaims(req *http.Request, raw string) (string, string, []string, error) {
	if v == nil || v.verifier == nil {
		return "", "", nil, fmt.Errorf("OIDC verifier unavailable")
	}
	token, err := v.verifier.Verify(req.Context(), raw)
	if err != nil {
		return "", "", nil, err
	}
	var claims Claims
	var rawClaims struct {
		ProviderID  string   `json:"provider_id"`
		Scope       string   `json:"scope"`
		Roles       []string `json:"roles"`
		WalletWrite bool     `json:"wallet_write"`
		RealmAccess struct {
			Roles []string `json:"roles"`
		} `json:"realm_access"`
	}
	if err := token.Claims(&rawClaims); err != nil {
		return "", "", nil, err
	}
	claims.ProviderID, claims.Scope, claims.Roles = rawClaims.ProviderID, rawClaims.Scope, append(rawClaims.Roles, rawClaims.RealmAccess.Roles...)
	if rawClaims.WalletWrite {
		claims.Roles = append(claims.Roles, "wallet:write")
	}
	return claims.ProviderID, claims.Scope, claims.Roles, nil
}
