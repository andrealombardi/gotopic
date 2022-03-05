package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sns"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/google/uuid"
)

const (
	DefaultRegion = "eu-west-1"

	Reset  = "\033[0m"
	Red    = "\033[31m"
	Green  = "\033[32m"
	Yellow = "\033[33m"
	Blue   = "\033[34m"
	Purple = "\033[35m"
	Cyan   = "\033[36m"
	Gray   = "\033[37m"
	White  = "\033[97m"
)

func main() {

	region := flag.String("region", DefaultRegion, "Override the default region")
	flag.Parse()
	topicArn := flag.Arg(0)

	if topicArn == "" {
		fmt.Println("Usage: gotopic [-region] topic-arn")
		flag.PrintDefaults()
		os.Exit(1)
	}

	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))
	sqssvc := sqs.New(sess)
	snssvc := sns.New(sess)
	stssvc := sts.New(sess)

	getAccountId := NewGetAccount(stssvc)
	createQueue := NewCreateQueue(sqssvc)
	deleteQueue := NewDeleteQueue(sqssvc)
	createSubscription := NewCreateSubscription(snssvc, topicArn)
	deleteSubscription := NewDeleteSubscription(snssvc)
	ctx := context.Background()

	accountId := getAccountId(ctx)
	queueURL, queueARN := createQueue(ctx, *region, accountId, topicArn)
	defer deleteQueue(ctx, queueURL)
	subscriptionArn := createSubscription(ctx, queueARN)
	defer deleteSubscription(ctx, subscriptionArn)

	go func() {
		for {
			messageOutput, _ := sqssvc.ReceiveMessageWithContext(ctx, &sqs.ReceiveMessageInput{QueueUrl: &queueURL})
			for _, message := range messageOutput.Messages {

				var body map[string]interface{}
				if err := json.Unmarshal([]byte(*message.Body), &body); err != nil {
					fmt.Println(err)
				}

				fmt.Printf(paint(Green, "\n\n%v\n\n"), body["Message"])

				_, _ = sqssvc.DeleteMessageWithContext(ctx, &sqs.DeleteMessageInput{
					QueueUrl:      &queueURL,
					ReceiptHandle: message.ReceiptHandle,
				})

			}
		}
	}()

	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	fmt.Println(paint(Red, "\nPress CTRL + C to exit\n"))
	<-c
	fmt.Println()

}

type CreateQueue func(ctx context.Context, region, accountId, topicArn string) (queueURL, queueARN string)

func NewCreateQueue(sqssvc *sqs.SQS) CreateQueue {
	return func(ctx context.Context, region, accountId, topicArn string) (string, string) {

		queueName := fmt.Sprint(uuid.New())
		fmt.Printf(paint(Cyan, "region: %s, accountId: %s, queueName: %s\n"), region, accountId, queueName)

		policy := fmt.Sprintf(`{
                "Version": "2012-10-17",
                "Statement": [
                    {
                       "Effect": "Allow",
                       "Principal": {
                            "Service": "sns.amazonaws.com"
                        },
                       "Action": "SQS:SendMessage",
                       "Resource": "arn:aws:sqs:%s:%s:%s",
                       "Condition": {
                            "ArnEquals": {
                                "aws:SourceArn": "%s"
                            }
                        }
                    }
                ]
        }`, region, accountId, queueName, topicArn)

		createQueueOutput, err := sqssvc.CreateQueueWithContext(ctx, &sqs.CreateQueueInput{
			QueueName: aws.String(queueName),
			Attributes: map[string]*string{
				"Policy":                 aws.String(policy),
				"MessageRetentionPeriod": aws.String("86400"),
			},
		})
		if err != nil {
			panic(err)
		}
		fmt.Printf(paint(Cyan, "created queue with url: %s\n"), *createQueueOutput.QueueUrl)

		sqsAttributesRequest := &sqs.GetQueueAttributesInput{
			QueueUrl: createQueueOutput.QueueUrl,
			AttributeNames: []*string{
				aws.String(sqs.QueueAttributeNameQueueArn),
			},
		}
		getAttributeOutput, err := sqssvc.GetQueueAttributesWithContext(ctx, sqsAttributesRequest)
		if err != nil {
			panic(err)
		}
		fmt.Printf(paint(Cyan, "queue arn: %s\n"), *getAttributeOutput.Attributes[sqs.QueueAttributeNameQueueArn])
		return *createQueueOutput.QueueUrl, *getAttributeOutput.Attributes[sqs.QueueAttributeNameQueueArn]
	}
}

type DeleteQueue func(ctx context.Context, queueArn string)

func NewDeleteQueue(sqssvc *sqs.SQS) DeleteQueue {
	return func(ctx context.Context, queueUrl string) {
		fmt.Printf(paint(Cyan, "cleanup: deleting queue %s\n"), queueUrl)
		sqssvc.DeleteQueueWithContext(ctx, &sqs.DeleteQueueInput{QueueUrl: aws.String(queueUrl)})
	}
}

type CreateTopicSubscription func(ctx context.Context, queueArn string) string

func NewCreateSubscription(snssvc *sns.SNS, topicArn string) CreateTopicSubscription {
	return func(ctx context.Context, queueArn string) string {
		result, err := snssvc.SubscribeWithContext(ctx, &sns.SubscribeInput{
			Endpoint:              aws.String(queueArn),
			Protocol:              aws.String("sqs"),
			ReturnSubscriptionArn: aws.Bool(true),
			TopicArn:              aws.String(topicArn),
		})
		if err != nil {
			panic(err)
		}
		fmt.Printf(paint(Cyan, "created subscription: %s\n"), *result.SubscriptionArn)

		return *result.SubscriptionArn
	}
}

type DeleteSubscription func(ctx context.Context, topicArn string)

func NewDeleteSubscription(snssvc *sns.SNS) DeleteSubscription {
	return func(ctx context.Context, topicArn string) {
		fmt.Printf(paint(Cyan, "cleanup: deleting subscription to topic %s\n"), topicArn)
		snssvc.UnsubscribeWithContext(ctx, &sns.UnsubscribeInput{
			SubscriptionArn: aws.String(topicArn),
		})
	}
}

type GetAccount func(ctx context.Context) string

func NewGetAccount(stssvc *sts.STS) GetAccount {
	return func(ctx context.Context) string {
		callerIdentity, err := stssvc.GetCallerIdentityWithContext(ctx, &sts.GetCallerIdentityInput{})
		if err != nil {
			panic(err)
		}
		fmt.Printf(paint(Cyan, "using aws account: %s\n"), *callerIdentity.Account)
		return *callerIdentity.Account
	}
}

func paint(color, text string) string {
	if runtime.GOOS == "windows" {
		return text
	}
	return color + text + Reset
}
