package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io/ioutil"
	"net/http"

	log "github.com/sirupsen/logrus"

	"github.com/realkinetic/cloud-native-meetup-2019/trip-service/service"
	"github.com/realkinetic/cloud-native-meetup-2019/util"
)

const port = ":8000"

var notrace = flag.Bool("notrace", false, "disable tracing")

type server struct {
	service service.TripService
}

func main() {
	flag.Parse()
	if err := util.Init("trip-service", *notrace); err != nil {
		panic(err)
	}

	tripService, err := service.NewTripService()
	if err != nil {
		panic(err)
	}

	s := &server{service: tripService}
	http.HandleFunc("/trips/booking", s.bookingHandler)
	handler := util.NewContextHandler(http.DefaultServeMux)

	log.Infof("Trip service listening on %s...", port)
	if err := http.ListenAndServe(port, handler); err != nil {
		panic(err)
	}
}

func (s *server) bookingHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	switch r.Method {
	case "GET":
		s.getBooking(ctx, w, r)
	case "POST":
		s.bookTrip(ctx, w, r)
	default:
		log.WithContext(ctx).WithFields(log.Fields{
			"error": errors.New("invalid HTTP method"),
		}).Error("Invalid HTTP method for endpoint")
		http.Error(w, "Invalid HTTP method", http.StatusBadRequest)
	}
}

func (s *server) getBooking(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	ref := r.URL.Query().Get("ref")
	ctx = util.WithRef(ctx, ref)
	confirmation, err := s.service.GetBooking(ctx, ref)
	if err != nil {
		log.WithContext(ctx).WithFields(log.Fields{
			"error": err,
		}).Error("Failed to fetch booking")
		if err == service.ErrNoSuchBooking {
			http.Error(w, err.Error(), http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	resp, err := json.Marshal(confirmation)
	if err != nil {
		panic(err)
	}

	log.WithContext(ctx).Info("Fetched booking")
	w.Header().Set("Content-Type", "application/json")
	w.Write(resp)
}

func (s *server) bookTrip(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	booking, err := s.deserializeBookingRequest(r)
	if err != nil {
		log.WithContext(ctx).WithFields(log.Fields{
			"error": err,
		}).Error("Failed to deserialize request")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := booking.Validate(); err != nil {
		log.WithContext(ctx).WithFields(log.Fields{
			"error": err,
		}).Error("Invalid booking request")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	confirmation, err := s.service.BookTrip(ctx, booking)
	if err != nil {
		log.WithContext(ctx).WithFields(log.Fields{
			"error": err,
		}).Error("Failed to book trip")
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	ctx = util.WithRef(ctx, confirmation.Ref)

	resp, err := json.Marshal(confirmation)
	if err != nil {
		log.WithContext(ctx).WithFields(log.Fields{
			"error": err,
		}).Fatal("Failed to marshal response")
	}

	log.WithContext(ctx).Info("Booked trip")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	w.Write(resp)
}

func (s *server) deserializeBookingRequest(r *http.Request) (*service.BookTripRequest, error) {
	defer r.Body.Close()
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	var req service.BookTripRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, err
	}

	return &req, nil
}
