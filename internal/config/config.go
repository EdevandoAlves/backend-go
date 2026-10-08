package config

import (
	"fmt"
	"os"
	"time"
)

var requiredKeys = []string{
	"HTTP_ADDR",
	"DATABASE_URL",
	"OIDC_ISSUER_URL",
	"OIDC_AUDIENCE",
	"OIDC_DISCOVERY_TIMEOUT",
	"SQS_ENDPOINT",
	"SQS_REGION",
	"SQS_OPERATIONS_QUEUE_URL",
	"SQS_EVENTS_QUEUE_URL",
}

type Config struct {
	HTTPAddr              string
	DatabaseURL           string
	OIDCIssuerURL         string
	OIDCAudience          string
	OIDCDiscoveryTimeout  time.Duration
	SQSEndpoint           string
	SQSRegion             string
	SQSOperationsQueueURL string
	SQSEventsQueueURL     string
}

func Load() (Config, error) {
	values := make(map[string]string, len(requiredKeys))
	for _, key := range requiredKeys {
		value := os.Getenv(key)
		if value == "" {
			return Config{}, fmt.Errorf("required environment variable %s is missing", key)
		}
		values[key] = value
	}
	parsed, err := time.ParseDuration(values["OIDC_DISCOVERY_TIMEOUT"])
	if err != nil || parsed <= 0 {
		return Config{}, fmt.Errorf("OIDC_DISCOVERY_TIMEOUT must be a positive duration")
	}
	return Config{HTTPAddr: values["HTTP_ADDR"], DatabaseURL: values["DATABASE_URL"], OIDCIssuerURL: values["OIDC_ISSUER_URL"], OIDCAudience: values["OIDC_AUDIENCE"], OIDCDiscoveryTimeout: parsed, SQSEndpoint: values["SQS_ENDPOINT"], SQSRegion: values["SQS_REGION"], SQSOperationsQueueURL: values["SQS_OPERATIONS_QUEUE_URL"], SQSEventsQueueURL: values["SQS_EVENTS_QUEUE_URL"]}, nil
}
