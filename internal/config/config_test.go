package config

import "testing"

func TestLoadRejectsMissingRequiredEnvironment(t *testing.T) {
	for _, key := range requiredKeys {
		t.Setenv(key, "")
	}
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted missing required environment")
	}
}

func TestLoadReadsOIDCDiscoveryTimeout(t *testing.T) {
	for _, key := range requiredKeys {
		t.Setenv(key, "value")
	}
	t.Setenv("OIDC_DISCOVERY_TIMEOUT", "750ms")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.OIDCDiscoveryTimeout.String() != "750ms" {
		t.Fatalf("OIDCDiscoveryTimeout = %s", cfg.OIDCDiscoveryTimeout)
	}
}

func TestLoadAcceptsRequiredEnvironment(t *testing.T) {
	values := map[string]string{
		"HTTP_ADDR":                ":18080",
		"DATABASE_URL":             "postgres://example",
		"OIDC_ISSUER_URL":          "http://keycloak/realms/backend-go",
		"OIDC_AUDIENCE":            "backend-go",
		"OIDC_DISCOVERY_TIMEOUT":   "5s",
		"SQS_ENDPOINT":             "http://localhost:4566",
		"SQS_REGION":               "us-east-1",
		"SQS_OPERATIONS_QUEUE_URL": "http://localhost/operations.fifo",
		"SQS_EVENTS_QUEUE_URL":     "http://localhost/events.fifo",
	}
	for key, value := range values {
		t.Setenv(key, value)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.OIDCIssuerURL != values["OIDC_ISSUER_URL"] || cfg.OIDCAudience != values["OIDC_AUDIENCE"] {
		t.Fatalf("OIDC config = issuer %q, audience %q", cfg.OIDCIssuerURL, cfg.OIDCAudience)
	}
}

func TestLoadRejectsInvalidOIDCDiscoveryTimeout(t *testing.T) {
	for _, key := range requiredKeys {
		t.Setenv(key, "value")
	}
	t.Setenv("OIDC_DISCOVERY_TIMEOUT", "not-a-duration")
	if _, err := Load(); err == nil {
		t.Fatal("Load() accepted invalid OIDC discovery timeout")
	}
}
