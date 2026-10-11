package notifications

import (
	"context"
	"errors"

	eventsv1 "github.com/m1chlcz/microservices-reference/api/events/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// grpcService serves the EventSink gRPC API.
type grpcService struct {
	eventsv1.UnimplementedEventSinkServer
	service *Service
}

// RegisterGRPC registers the notification event sink on a gRPC server.
func RegisterGRPC(server *grpc.Server, service *Service) {
	eventsv1.RegisterEventSinkServer(server, &grpcService{service: service})
}

// Deliver stores one received event and reports it as a new delivery.
func (sink *grpcService) Deliver(
	ctx context.Context,
	event *eventsv1.Event,
) (*eventsv1.DeliverResponse, error) {
	if _, err := sink.service.Receive(ctx, event.Kind, event.Data); err != nil {
		if errors.Is(err, ErrInvalidEvent) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Error(codes.Internal, err.Error())
	}
	return &eventsv1.DeliverResponse{Duplicate: false}, nil
}
