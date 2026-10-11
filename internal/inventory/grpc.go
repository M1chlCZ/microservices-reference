package inventory

import (
	"context"
	"errors"

	"github.com/google/uuid"
	inventoryv1 "github.com/m1chlcz/microservices-reference/api/inventory/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RegisterGRPC adds the inventory gRPC service to the server.
func RegisterGRPC(server *grpc.Server, service *Service) {
	inventoryv1.RegisterInventoryServer(server, &grpcService{inventory: service})
}

type grpcService struct {
	inventoryv1.UnimplementedInventoryServer
	inventory *Service
}

// Reserve reserves every item in one transaction and returns the reservation ID.
func (service *grpcService) Reserve(
	ctx context.Context,
	request *inventoryv1.ReserveRequest,
) (*inventoryv1.ReserveResponse, error) {
	items := make([]ReservationItem, 0, len(request.GetItems()))
	for _, item := range request.GetItems() {
		items = append(items, ReservationItem{
			SKU:      item.GetSku(),
			Quantity: int(item.GetQuantity()),
		})
	}
	reservationID, err := service.inventory.Reserve(ctx, ReservationRequest{Items: items})
	if err != nil {
		return nil, reservationError(err)
	}
	return &inventoryv1.ReserveResponse{ReservationId: reservationID.String()}, nil
}

// Release returns the reserved quantities to stock.
func (service *grpcService) Release(
	ctx context.Context,
	request *inventoryv1.ReleaseRequest,
) (*inventoryv1.ReleaseResponse, error) {
	reservationID, err := uuid.Parse(request.GetReservationId())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "reservation id must be a UUID")
	}
	if err = service.inventory.Release(ctx, reservationID); err != nil {
		return nil, reservationError(err)
	}
	return &inventoryv1.ReleaseResponse{}, nil
}

func reservationError(err error) error {
	switch {
	case errors.Is(err, ErrInvalidReservation):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, ErrUnknownSKU), errors.Is(err, ErrInsufficientStock):
		return status.Error(codes.FailedPrecondition, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}
