# gotopic

Receive SNS notifications in your terminal (and a shameless ripoff of [ontopic](https://github.com/ziggy42/ontopic), but written in go.)

`gotopic` creates a temporary SQS queue subscribed to the topic and polls it. 
On exit the created resources are removed.

## Installation

```
❯ go get github.com/andrealombardi/gotopic
```

## Usage
Basic usage:
```
gotopic <TOPIC_ARN>
```

For more see `-h`:
```
❯ gotopic -h
Usage of gotopic:
  -region string
    	Override the default region (default "eu-west-1")
```
