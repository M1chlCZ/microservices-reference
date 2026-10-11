package orders

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

const eventPaymentCaptured = "payment.captured"

// RegisterGRPC adds the order event sink to the server.
func RegisterGRPC(server *grpc.Server, service *Service) {
	eventsv1.RegisterEventSinkServer(server, &eventSink{orders: service})
}

// RelayRoutes returns the event kinds of the order service and their gRPC
// targets.
func RelayRoutes(notificationsTarget, shippingTarget, invoicingTarget string) map[string][]string {
	return map[string][]string{
		eventOrderCreated: {notificationsTarget},
		eventOrderPaid:    {notificationsTarget, shippingTarget, invoicingTarget},
	}
}

type eventSink struct {
	eventsv1.UnimplementedEventSinkServer
	orders *Service
}

// Deliver handles one event. It marks an order paid on payment.captured.
func (sink *eventSink) Deliver(
	ctx context.Context,
	event *eventsv1.Event,
) (*eventsv1.DeliverResponse, error) {
	if event.GetKind() != eventPaymentCaptured {
		return nil, status.Error(codes.InvalidArgument, "unsupported event kind")
	}
	var data struct {
		OrderID string `json:"orderId"`
	}
	if err := json.Unmarshal(event.GetData(), &data); err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid event data")
	}
	orderID, err := uuid.Parse(data.OrderID)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "order id must be a UUID")
	}
	_, duplicate, err := sink.orders.MarkPaid(ctx, orderID)
	if errors.Is(err, ErrUnknownOrder) {
		return nil, status.Error(codes.NotFound, "unknown order")
	}
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &eventsv1.DeliverResponse{Duplicate: duplicate}, nil
}
