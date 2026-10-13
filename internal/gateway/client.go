package gateway

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	inventoryv1 "github.com/m1chlcz/microservices-reference/api/inventory/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GRPCInventoryClient calls the inventory service over gRPC.
type GRPCInventoryClient struct {
	client inventoryv1.InventoryClient
}

// NewGRPCInventoryClient returns a GRPCInventoryClient for a connection.
func NewGRPCInventoryClient(connection *grpc.ClientConn) *GRPCInventoryClient {
	return &GRPCInventoryClient{client: inventoryv1.NewInventoryClient(connection)}
}

// Reserve reserves every item and returns the reservation ID. It returns
// ErrStockUnavailable when the inventory service refuses the reservation.
func (client *GRPCInventoryClient) Reserve(ctx context.Context, items []Item) (uuid.UUID, error) {
	request := make([]*inventoryv1.ReservationItem, 0, len(items))
	for _, item := range items {
		request = append(request, &inventoryv1.ReservationItem{
			Sku:      item.SKU,
			Quantity: int32(item.Quantity),
		})
	}
	response, err := client.client.Reserve(ctx, &inventoryv1.ReserveRequest{Items: request})
	if err != nil {
		if status.Code(err) == codes.FailedPrecondition {
			return uuid.Nil, ErrStockUnavailable
		}
		return uuid.Nil, fmt.Errorf("reserve stock: %w", err)
	}
	reservationID, err := uuid.Parse(response.GetReservationId())
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse reservation ID: %w", err)
	}
	return reservationID, nil
}
