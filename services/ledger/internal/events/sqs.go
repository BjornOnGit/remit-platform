package events

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// NewSQSClient builds an SQS client. Region and credentials come from the standard AWS
// environment/instance role. endpoint is optional (ElasticMQ in dev).
func NewSQSClient(ctx context.Context, endpoint string) (*sqs.Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	return sqs.NewFromConfig(cfg, func(o *sqs.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	}), nil
}

// QueueURL resolves a queue by name, optionally creating it (dev only).
func QueueURL(ctx context.Context, client *sqs.Client, name string, createIfMissing bool) (string, error) {
	out, err := client.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{QueueName: aws.String(name)})
	if err == nil {
		return *out.QueueUrl, nil
	}
	var missing *types.QueueDoesNotExist
	if errors.As(err, &missing) && createIfMissing {
		created, err := client.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: aws.String(name)})
		if err != nil {
			return "", fmt.Errorf("create queue %s: %w", name, err)
		}
		return *created.QueueUrl, nil
	}
	return "", fmt.Errorf("get queue %s: %w", name, err)
}

// SQSPublisher publishes outbox events to one SQS queue.
type SQSPublisher struct {
	client   *sqs.Client
	queueURL string
}

func NewSQSPublisher(client *sqs.Client, queueURL string) *SQSPublisher {
	return &SQSPublisher{client: client, queueURL: queueURL}
}

func (p *SQSPublisher) Publish(ctx context.Context, eventType, body string) error {
	_, err := p.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(p.queueURL),
		MessageBody: aws.String(body),
		MessageAttributes: map[string]types.MessageAttributeValue{
			"event_type": {DataType: aws.String("String"), StringValue: aws.String(eventType)},
		},
	})
	return err
}