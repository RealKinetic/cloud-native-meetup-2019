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
	rentalsTable     = "rentals"
)

type BookCarRentalRequest struct {
	Agent           string    `json:"agent"`
	PickUp          time.Time `json:"pick_up"`
	PickUpLocation  string    `json:"pick_up_location"`
	DropOff         time.Time `json:"drop_off"`
	DropOffLocation string    `json:"drop_off_location"`
	Name            string    `json:"name"`
	VehicleClass    string    `json:"vehicle_class"`
}

func (b *BookCarRentalRequest) Validate() error {
	if b.Agent == "" {
		return errors.New("invalid agent")
	}
	if b.PickUp.IsZero() {
		return errors.New("invalid pick up")
	}
	if len(b.PickUpLocation) == 0 {
		return errors.New("invalid pick up location")
	}
	if b.DropOff.IsZero() {
		return errors.New("invalid drop off")
	}
	if len(b.DropOffLocation) == 0 {
		return errors.New("invalid drop off location")
	}
	if len(b.Name) == 0 {
		return errors.New("invalid name")
	}
	if len(b.VehicleClass) == 0 {
		return errors.New("invalid vehicle class")
	}
	return nil
}

type CarRentalConfirmation struct {
	Ref       string                `json:"ref"`
	CarRental *BookCarRentalRequest `json:"car_rental"`
}

type CarRentalService interface {
	BookCarRental(context.Context, *BookCarRentalRequest) (*CarRentalConfirmation, error)
	GetBooking(ctx context.Context, ref string) (*CarRentalConfirmation, error)
}

type dynamoService struct {
	db *dynamodb.DynamoDB
}

func NewCarRentalService() (CarRentalService, error) {
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
		TableName: aws.String(rentalsTable),
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

func (d *dynamoService) BookCarRental(ctx context.Context, r *BookCarRentalRequest) (*CarRentalConfirmation, error) {
	confirmation := &CarRentalConfirmation{Ref: nuid.Next(), CarRental: r}
	av, err := dynamodbattribute.MarshalMap(confirmation)
	if err != nil {
		return nil, err
	}

	input := &dynamodb.PutItemInput{
		Item:      av,
		TableName: aws.String(rentalsTable),
	}
	_, err = d.db.PutItemWithContext(ctx, input)

	return confirmation, err
}

func (d *dynamoService) GetBooking(ctx context.Context, ref string) (*CarRentalConfirmation, error) {
	result, err := d.db.GetItemWithContext(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(rentalsTable),
		Key: map[string]*dynamodb.AttributeValue{
			"ref": {
				S: aws.String(ref),
			},
		},
	})
	if err != nil {
		return nil, err
	}

	var confirmation *CarRentalConfirmation
	if err := dynamodbattribute.UnmarshalMap(result.Item, &confirmation); err != nil {
		return nil, err
	}
	if confirmation.Ref == "" {
		return nil, ErrNoSuchBooking
	}

	span, ctx := opentracing.StartSpanFromContext(ctx, "validateCarReservation")
	span.LogFields(
		tracelog.String("ref", confirmation.Ref),
		tracelog.String("agent", confirmation.CarRental.Agent),
		tracelog.String("name", confirmation.CarRental.Name),
		tracelog.String("vehicle_class", confirmation.CarRental.VehicleClass),
	)
	err = d.validateCarReservation(ctx, confirmation)
	span.Finish()

	return confirmation, nil
}

func (d *dynamoService) validateCarReservation(ctx context.Context, confirmation *CarRentalConfirmation) error {
	// Do some work.
	sleep := 500*time.Millisecond + time.Duration(rand.Intn(1))*time.Second
	time.Sleep(sleep)
	log.WithContext(ctx).WithFields(log.Fields{
		"agent":             confirmation.CarRental.Agent,
		"pick_up":           confirmation.CarRental.PickUp,
		"pick_up_location":  confirmation.CarRental.PickUpLocation,
		"drop_off":          confirmation.CarRental.DropOff,
		"drop_off_location": confirmation.CarRental.DropOffLocation,
		"name":              confirmation.CarRental.Name,
		"vehicle_class":     confirmation.CarRental.VehicleClass,
	}).Infof("Validated flight reservation")
	return nil
}
