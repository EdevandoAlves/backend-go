#!/bin/sh
set -eu

region="${AWS_DEFAULT_REGION:-us-east-1}"
account="000000000000"
endpoint="http://localhost:4566"

awslocal --region "$region" sqs create-queue \
  --queue-name operations-dlq.fifo \
  --attributes FifoQueue=true,ContentBasedDeduplication=true

dlq_url="$endpoint/$account/operations-dlq.fifo"
redrive_policy=$(printf '%s' "{\"deadLetterTargetArn\":\"arn:aws:sqs:$region:$account:operations-dlq.fifo\",\"maxReceiveCount\":\"5\"}")

awslocal --region "$region" sqs create-queue \
  --queue-name operations.fifo \
  --attributes FifoQueue=true,ContentBasedDeduplication=true

operations_url="$endpoint/$account/operations.fifo"
set_queue_input=$(printf '%s' "{\"QueueUrl\":\"$operations_url\",\"Attributes\":{\"RedrivePolicy\":\"{\\\"deadLetterTargetArn\\\":\\\"arn:aws:sqs:$region:$account:operations-dlq.fifo\\\",\\\"maxReceiveCount\\\":\\\"5\\\"}\"}}")
awslocal --region "$region" sqs set-queue-attributes --cli-input-json "$set_queue_input"

awslocal --region "$region" sqs create-queue \
  --queue-name events.fifo \
  --attributes FifoQueue=true,ContentBasedDeduplication=true

printf '%s\n' "LocalStack queues initialized: $dlq_url, operations.fifo, events.fifo"
