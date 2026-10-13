// Package gateway implements the gateway example: a REST entry point that
// calls the inventory service over the internal gRPC connection.
package gateway

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
)

// ErrInvalidRequest reports a malformed reservation request.
var ErrInvalidRequest = errors.New("invalid request")

// ErrStockUnavailable reports that the inventory service refused the
// reservation.
var ErrStockUnavailable = errors.New("stock unavailable")

const maxItems = 50

// Item is one requested SKU and quantity.
type Item struct {
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
}

// ReservationRequest asks the inventory service for stock.
type ReservationRequest struct {
	Items []Item `json:"items"`
}

// Reservation is one reservation made through the gateway.
type Reservation struct {
	ID uuid.UUID `json:"reservationId"`
}

// InventoryClient reserves stock in the inventory service.
type InventoryClient interface {
	Reserve(ctx context.Context, items []Item) (uuid.UUID, error)
}

// Service implements the gateway use case.
type Service struct {
	inventory InventoryClient
}

// NewService returns a Service.
func NewService(inventory InventoryClient) *Service {
	return &Service{inventory: inventory}
}

// Reserve validates the request and reserves the items in the inventory
// service. The gRPC call runs with the caller context, so its span continues
// the trace of the HTTP request.
func (service *Service) Reserve(ctx context.Context, request ReservationRequest) (Reservation, error) {
	if err := validateItems(request.Items); err != nil {
		return Reservation{}, err
	}
	reservationID, err := service.inventory.Reserve(ctx, request.Items)
	if err != nil {
		return Reservation{}, err
	}
	return Reservation{ID: reservationID}, nil
}

func validateItems(items []Item) error {
	if len(items) == 0 || len(items) > maxItems {
		return ErrInvalidRequest
	}
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.SKU) == "" || item.Quantity <= 0 {
			return ErrInvalidRequest
		}
		if seen[item.SKU] {
			return ErrInvalidRequest
		}
		seen[item.SKU] = true
	}
	return nil
}
