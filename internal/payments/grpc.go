package payments

import (
	"context"
	"errors"

	"github.com/google/uuid"
	paymentsv1 "github.com/m1chlcz/microservices-reference/api/payments/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type grpcService struct {
	paymentsv1.UnimplementedPaymentsServer
	service *Service
}

// RegisterGRPC registers the payments gRPC service on the server.
func RegisterGRPC(server *grpc.Server, service *Service) {
	paymentsv1.RegisterPaymentsServer(server, &grpcService{service: service})
}

// CreateIntent creates one payment intent.
func (s *grpcService) CreateIntent(
	ctx context.Context,
	request *paymentsv1.CreateIntentRequest,
) (*paymentsv1.CreateIntentResponse, error) {
	orderID, err := uuid.Parse(request.GetOrderId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "order_id must be a UUID")
	}
	intent, duplicate, err := s.service.CreateIntent(ctx, orderID, request.GetAmountMinor(),
		request.GetCurrency(), request.GetIdempotencyKey())
	if err != nil {
		return nil, status.Error(grpcCode(err), err.Error())
	}
	return &paymentsv1.CreateIntentResponse{
		IntentId:    intent.ID.String(),
		ProviderRef: intent.ProviderRef,
		Duplicate:   duplicate,
	}, nil
}

func grpcCode(err error) codes.Code {
	switch {
	case errors.Is(err, ErrInvalidIntent):
		return codes.InvalidArgument
	case errors.Is(err, ErrIdempotencyConflict):
		return codes.AlreadyExists
	case errors.Is(err, ErrUnknownIntent):
		return codes.NotFound
	case errors.Is(err, ErrInvalidState):
		return codes.FailedPrecondition
	default:
		return codes.Internal
	}
}
