package main

import (
	"context"
	"github.com/aws/aws-sdk-go/service/sts"
	"log"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sns"
	"github.com/aws/aws-sdk-go/service/sqs"
	"github.com/google/uuid"

	"fmt"
	"os"
)

func main() {

	if len(os.Args) != 2 {
		log.Fatal("You must specify the topic arn")
	}
	topicArn := os.Args[1]

	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))
	sqssvc := sqs.New(sess)
	snssvc := sns.New(sess)
	stssvc := sts.New(sess)
	ctx := context.Background()

	callerIdentity, err := stssvc.GetCallerIdentityWithContext(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		log.Fatal(err.Error())
	}

	region := "eu-west-1" //todo
	accountId := *callerIdentity.Account
	queueName := fmt.Sprint(uuid.New())
	log.Printf("region: %s, accountId: %s, queueName: %s\n", region, accountId, queueName)

	policy := fmt.Sprintf(`{
                "Version": "2012-10-17",
                "Statement": [
                    {
                       "Effect": "Allow",
                       "Principal": "*",
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

	log.Println(policy)

	createQueueOutput, err := sqssvc.CreateQueueWithContext(ctx, &sqs.CreateQueueInput{
		QueueName: aws.String(queueName),
		Attributes: map[string]*string{
			"Policy":                 aws.String(policy),
			"MessageRetentionPeriod": aws.String("86400"),
		},
	})
	if err != nil {
		log.Fatal(err.Error())
	}
	defer sqssvc.DeleteQueue(&sqs.DeleteQueueInput{
		QueueUrl: createQueueOutput.QueueUrl,
	})
	log.Printf("created queue with url: %s\n", *createQueueOutput.QueueUrl)

	sqsAttributesRequest := &sqs.GetQueueAttributesInput{
		QueueUrl: createQueueOutput.QueueUrl,
		AttributeNames: []*string{
			aws.String(sqs.QueueAttributeNameQueueArn),
		},
	}
	getAttributeOutput, err := sqssvc.GetQueueAttributesWithContext(ctx, sqsAttributesRequest)
	if err != nil {
		log.Fatal(err.Error())
	}
	log.Printf("queue arn: %s\n", *getAttributeOutput.Attributes[sqs.QueueAttributeNameQueueArn])

	result, err := snssvc.SubscribeWithContext(ctx, &sns.SubscribeInput{
		Endpoint:              getAttributeOutput.Attributes[sqs.QueueAttributeNameQueueArn],
		Protocol:              aws.String("sqs"),
		ReturnSubscriptionArn: aws.Bool(true),
		TopicArn:              &topicArn,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
	defer snssvc.UnsubscribeWithContext(ctx, &sns.UnsubscribeInput{
		SubscriptionArn: result.SubscriptionArn,
	})
	log.Printf("created subscription: %s\n", *result.SubscriptionArn)

	go func() {
		for {
			messageOutput, _ := sqssvc.ReceiveMessageWithContext(ctx, &sqs.ReceiveMessageInput{QueueUrl: createQueueOutput.QueueUrl})
			for _, message := range messageOutput.Messages {
				log.Println(*message.Body)
			}
		}
	}()

	log.Println("Press Enter to stop")
	fmt.Scanln()

}
