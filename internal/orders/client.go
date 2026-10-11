package orders

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	inventoryv1 "github.com/m1chlcz/microservices-reference/api/inventory/v1"
	paymentsv1 "github.com/m1chlcz/microservices-reference/api/payments/v1"
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

// Reserve reserves the order lines and returns the reservation ID. It
// returns ErrStockUnavailable when the inventory service refuses.
func (client *GRPCInventoryClient) Reserve(ctx context.Context, lines []Line) (uuid.UUID, error) {
	items := make([]*inventoryv1.ReservationItem, 0, len(lines))
	for _, line := range lines {
		items = append(items, &inventoryv1.ReservationItem{
			Sku:      line.SKU,
			Quantity: int32(line.Quantity),
		})
	}
	response, err := client.client.Reserve(ctx, &inventoryv1.ReserveRequest{Items: items})
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

// Release releases one reservation.
func (client *GRPCInventoryClient) Release(ctx context.Context, reservationID uuid.UUID) error {
	_, err := client.client.Release(ctx, &inventoryv1.ReleaseRequest{
		ReservationId: reservationID.String(),
	})
	if err != nil {
		return fmt.Errorf("release reservation: %w", err)
	}
	return nil
}

// GRPCPaymentsClient creates payment intents over gRPC.
type GRPCPaymentsClient struct {
	client paymentsv1.PaymentsClient
}

// NewGRPCPaymentsClient returns a GRPCPaymentsClient for a connection.
func NewGRPCPaymentsClient(connection *grpc.ClientConn) *GRPCPaymentsClient {
	return &GRPCPaymentsClient{client: paymentsv1.NewPaymentsClient(connection)}
}

// CreateIntent creates the payment intent for an order.
func (client *GRPCPaymentsClient) CreateIntent(
	ctx context.Context,
	orderID uuid.UUID,
	amountMinor int64,
	currency string,
	idempotencyKey string,
) (Payment, error) {
	response, err := client.client.CreateIntent(ctx, &paymentsv1.CreateIntentRequest{
		OrderId:        orderID.String(),
		AmountMinor:    amountMinor,
		Currency:       currency,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return Payment{}, fmt.Errorf("create payment intent: %w", err)
	}
	intentID, err := uuid.Parse(response.GetIntentId())
	if err != nil {
		return Payment{}, fmt.Errorf("parse intent ID: %w", err)
	}
	return Payment{IntentID: intentID, ProviderRef: response.GetProviderRef()}, nil
}
