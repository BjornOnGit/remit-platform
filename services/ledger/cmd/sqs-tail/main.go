// Dev helper: prints (and deletes) messages from the events queue.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"remit-platform/ledger/internal/events"
)

func main() {
	ctx := context.Background()
	name := os.Getenv("SQS_QUEUE_NAME")
	if name == "" {
		log.Fatal("SQS_QUEUE_NAME is required")
	}
	client, err := events.NewSQSClient(ctx, os.Getenv("SQS_ENDPOINT"))
	if err != nil {
		log.Fatal(err)
	}
	url, err := events.QueueURL(ctx, client, name, os.Getenv("SQS_CREATE_QUEUE") == "true")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("listening on", url)

	for {
		out, err := client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:              aws.String(url),
			MaxNumberOfMessages:   10,
			WaitTimeSeconds:       5,
			MessageAttributeNames: []string{"All"},
		})
		if err != nil {
			log.Fatal(err)
		}
		for _, m := range out.Messages {
			et := ""
			if v, ok := m.MessageAttributes["event_type"]; ok && v.StringValue != nil {
				et = *v.StringValue
			}
			fmt.Printf("[%s] %s\n", et, *m.Body)
			if _, err := client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
				QueueUrl: aws.String(url), ReceiptHandle: m.ReceiptHandle,
			}); err != nil {
				log.Printf("delete: %v", err)
			}
		}
	}
}