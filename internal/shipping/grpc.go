package shipping

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	eventsv1 "github.com/m1chlcz/microservices-reference/api/events/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const eventOrderPaid = "order.paid"

// grpcService receives outbox events for the shipping service.
type grpcService struct {
	eventsv1.UnimplementedEventSinkServer
	service *Service
}

// RegisterGRPC adds the shipping event sink to the gRPC server.
func RegisterGRPC(server *grpc.Server, service *Service) {
	eventsv1.RegisterEventSinkServer(server, &grpcService{service: service})
}

// Deliver creates one shipment for an order.paid event. A repeated delivery
// returns the duplicate flag of the service.
func (sink *grpcService) Deliver(ctx context.Context, event *eventsv1.Event) (*eventsv1.DeliverResponse, error) {
	if event.GetKind() != eventOrderPaid {
		return nil, status.Errorf(codes.InvalidArgument, "invalid event kind %q", event.GetKind())
	}
	var data struct {
		OrderID string `json:"orderId"`
	}
	if err := json.Unmarshal(event.GetData(), &data); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid event data: %v", err)
	}
	orderID, err := uuid.Parse(data.OrderID)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid event data: %v", err)
	}
	_, duplicate, err := sink.service.CreateForOrder(ctx, orderID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create shipment: %v", err)
	}
	return &eventsv1.DeliverResponse{Duplicate: duplicate}, nil
}
