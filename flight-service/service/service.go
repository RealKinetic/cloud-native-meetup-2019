package service

import (
	"context"
	"errors"
	"math/rand"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbattribute"
	"github.com/nats-io/nuid"
	"github.com/opentracing-contrib/go-aws-sdk"
	"github.com/opentracing/opentracing-go"
	tracelog "github.com/opentracing/opentracing-go/log"
	log "github.com/sirupsen/logrus"
)

var (
	ErrNoSuchBooking = errors.New("no such booking")
	flightsTable     = "flights"
)

type FlightConfirmation struct {
	Ref    string             `json:"ref"`
	Flight *BookFlightRequest `json:"flight"`
}

type BookFlightRequest struct {
	Airline      string    `json:"airline"`
	FlightNumber string    `json:"flight_number"`
	Time         time.Time `json:"time"`
	Passengers   []string  `json:"passengers"`
}

func (b *BookFlightRequest) Validate() error {
	if b.Airline == "" {
		return errors.New("invalid airline")
	}
	if b.FlightNumber == "" {
		return errors.New("invalid flight number")
	}
	if b.Time.IsZero() {
		return errors.New("invalid flight time")
	}
	if len(b.Passengers) == 0 {
		return errors.New("invalid passengers")
	}
	for _, p := range b.Passengers {
		if len(p) == 0 {
			return errors.New("invalid passenger name")
		}
	}
	return nil
}

type FlightService interface {
	BookFlight(context.Context, *BookFlightRequest) (*FlightConfirmation, error)
	GetBooking(ctx context.Context, ref string) (*FlightConfirmation, error)
}

type dynamoService struct {
	db *dynamodb.DynamoDB
}

func NewFlightService() (FlightService, error) {
	rand.Seed(time.Now().Unix())
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
		Config:            aws.Config{Region: aws.String("us-east-1")},
	}))
	db := dynamodb.New(sess)
	otaws.AddOTHandlers(db.Client)

	input := &dynamodb.CreateTableInput{
		AttributeDefinitions: []*dynamodb.AttributeDefinition{
			{
				AttributeName: aws.String("ref"),
				AttributeType: aws.String("S"),
			},
		},
		KeySchema: []*dynamodb.KeySchemaElement{
			{
				AttributeName: aws.String("ref"),
				KeyType:       aws.String("HASH"),
			},
		},
		ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
			ReadCapacityUnits:  aws.Int64(2),
			WriteCapacityUnits: aws.Int64(2),
		},
		TableName: aws.String(flightsTable),
	}
	_, err := db.CreateTable(input)
	if err != nil {
		if awsError, ok := err.(awserr.Error); ok {
			if awsError.Code() != dynamodb.ErrCodeResourceInUseException {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	return &dynamoService{db: db}, nil
}

func (d *dynamoService) BookFlight(ctx context.Context, r *BookFlightRequest) (*FlightConfirmation, error) {
	confirmation := &FlightConfirmation{Ref: nuid.Next(), Flight: r}
	av, err := dynamodbattribute.MarshalMap(confirmation)
	if err != nil {
		return nil, err
	}

	input := &dynamodb.PutItemInput{
		Item:      av,
		TableName: aws.String(flightsTable),
	}
	_, err = d.db.PutItemWithContext(ctx, input)

	return confirmation, err
}

func (d *dynamoService) GetBooking(ctx context.Context, ref string) (*FlightConfirmation, error) {
	result, err := d.db.GetItemWithContext(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(flightsTable),
		Key: map[string]*dynamodb.AttributeValue{
			"ref": {
				S: aws.String(ref),
			},
		},
	})
	if err != nil {
		return nil, err
	}

	var confirmation *FlightConfirmation
	if err := dynamodbattribute.UnmarshalMap(result.Item, &confirmation); err != nil {
		return nil, err
	}
	if confirmation.Ref == "" {
		return nil, ErrNoSuchBooking
	}

	span, ctx := opentracing.StartSpanFromContext(ctx, "validateFlightReservation")
	span.LogFields(
		tracelog.String("ref", confirmation.Ref),
		tracelog.String("airline", confirmation.Flight.Airline),
		tracelog.String("flight", confirmation.Flight.FlightNumber),
	)
	err = d.validateFlightReservation(ctx, confirmation)
	span.Finish()

	return confirmation, nil
}

func (d *dynamoService) validateFlightReservation(ctx context.Context, confirmation *FlightConfirmation) error {
	// Do some work.
	sleep := 500*time.Millisecond + time.Duration(rand.Intn(1))*time.Second
	time.Sleep(sleep)
	log.WithContext(ctx).WithFields(log.Fields{
		"airline":    confirmation.Flight.Airline,
		"flight":     confirmation.Flight.FlightNumber,
		"passengers": confirmation.Flight.Passengers,
	}).Infof("Validated flight reservation")
	return nil
}
