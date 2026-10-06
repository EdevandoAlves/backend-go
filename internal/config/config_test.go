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

func TestLoadAcceptsRequiredEnvironment(t *testing.T) {
	values := map[string]string{
		"HTTP_ADDR":                ":18080",
		"DATABASE_URL":             "postgres://example",
		"OIDC_ISSUER_URL":          "https://issuer.example",
		"OIDC_AUDIENCE":            "backend-go",
		"SQS_ENDPOINT":             "http://localhost:4566",
		"SQS_REGION":               "us-east-1",
		"SQS_OPERATIONS_QUEUE_URL": "http://localhost:4566/000000000000/operations",
		"SQS_EVENTS_QUEUE_URL":     "http://localhost:4566/000000000000/events",
	}
	for key, value := range values {
		t.Setenv(key, value)
	}
	if _, err := Load(); err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
}
