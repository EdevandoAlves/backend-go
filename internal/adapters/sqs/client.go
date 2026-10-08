package sqs

import (
	"context"
	"net/url"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

type Client struct {
	API      *sqs.Client
	QueueURL string
}

func NewClient(ctx context.Context, region, baseEndpoint, queueURL string) (*Client, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, err
	}
	if baseEndpoint != "" {
		queueURL = routeQueueURL(baseEndpoint, queueURL)
	}
	return &Client{API: sqs.NewFromConfig(cfg, func(o *sqs.Options) {
		if baseEndpoint != "" {
			o.BaseEndpoint = aws.String(baseEndpoint)
		}
	}), QueueURL: queueURL}, nil
}

// LocalStack can return queue URLs using a host-only alias that is not
// resolvable from the app container. The endpoint is authoritative in the
// app, while the queue URL path identifies the queue.
func routeQueueURL(endpoint, queueURL string) string {
	e, err := url.Parse(endpoint)
	q, qerr := url.Parse(queueURL)
	if err != nil || qerr != nil || e.Scheme == "" || e.Host == "" || q.Path == "" {
		return queueURL
	}
	q.Scheme, q.Host = e.Scheme, e.Host
	return q.String()
}
