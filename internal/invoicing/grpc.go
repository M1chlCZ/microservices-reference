package invoicing

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	eventsv1 "github.com/m1chlcz/microservices-reference/api/events/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const eventOrderPaid = "order.paid"

// RegisterGRPC registers the invoice event sink on the gRPC server.
func RegisterGRPC(server *grpc.Server, service *Service) {
	eventsv1.RegisterEventSinkServer(server, &grpcService{service: service})
}

type grpcService struct {
	eventsv1.UnimplementedEventSinkServer
	service *Service
}

// Deliver issues the invoice of one order.paid event and reports a duplicate.
func (sink *grpcService) Deliver(ctx context.Context, event *eventsv1.Event) (*eventsv1.DeliverResponse, error) {
	if event.GetKind() != eventOrderPaid {
		return nil, status.Error(codes.InvalidArgument, "event kind must be order.paid")
	}
	var data struct {
		OrderID    string `json:"orderId"`
		TotalMinor int64  `json:"totalMinor"`
	}
	if err := json.Unmarshal(event.GetData(), &data); err != nil {
		return nil, status.Error(codes.InvalidArgument, "event data must be a JSON object with an orderId UUID and a totalMinor")
	}
	orderID, err := uuid.Parse(data.OrderID)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "event data must be a JSON object with an orderId UUID and a totalMinor")
	}
	_, duplicate, err := sink.service.Issue(ctx, orderID, data.TotalMinor)
	if errors.Is(err, ErrInvalidIssue) {
		return nil, status.Error(codes.InvalidArgument, "event data must hold a non-negative totalMinor")
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "issue invoice: %v", err)
	}
	return &eventsv1.DeliverResponse{Duplicate: duplicate}, nil
}
