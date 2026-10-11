// Package orders implements order creation with a stock reservation and a
// transactional outbox event.
package orders

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrInvalidOrder reports a malformed order request.
var ErrInvalidOrder = errors.New("invalid order")

// ErrStockUnavailable reports that the inventory service refused the
// reservation.
var ErrStockUnavailable = errors.New("stock unavailable")

// ErrUnknownOrder reports a lookup for an order that does not exist.
var ErrUnknownOrder = errors.New("unknown order")

const (
	maxCustomerRefLength = 120
	maxOrderLines        = 50
)

// Line is one order line.
type Line struct {
	SKU            string `json:"sku"`
	Quantity       int    `json:"quantity"`
	UnitPriceMinor int64  `json:"unitPriceMinor"`
}

// Order is a placed order.
type Order struct {
	ID          uuid.UUID `json:"id"`
	CustomerRef string    `json:"customerRef"`
	TotalMinor  int64     `json:"totalMinor"`
	Lines       []Line    `json:"lines"`
	CreatedAt   time.Time `json:"createdAt"`
}

// CreateRequest is one order creation request.
type CreateRequest struct {
	CustomerRef string `json:"customerRef"`
	Lines       []Line `json:"lines"`
}

// InventoryClient reserves and releases stock in the inventory service.
type InventoryClient interface {
	Reserve(ctx context.Context, lines []Line) (uuid.UUID, error)
	Release(ctx context.Context, reservationID uuid.UUID) error
}

// Store persists orders and their outbox events.
type Store interface {
	Create(ctx context.Context, order Order) error
	Get(ctx context.Context, id uuid.UUID) (Order, error)
}

// Service implements the order use cases.
type Service struct {
	store     Store
	inventory InventoryClient
}

// NewService returns a Service.
func NewService(store Store, inventory InventoryClient) *Service {
	return &Service{store: store, inventory: inventory}
}

// Create validates the request, reserves stock, and stores the order with its
// outbox event in one transaction. A failed store call releases the
// reservation again.
func (service *Service) Create(ctx context.Context, request CreateRequest) (Order, error) {
	request.CustomerRef = strings.TrimSpace(request.CustomerRef)
	if request.CustomerRef == "" || len(request.CustomerRef) > maxCustomerRefLength {
		return Order{}, ErrInvalidOrder
	}
	if err := validateLines(request.Lines); err != nil {
		return Order{}, err
	}
	var total int64
	for _, line := range request.Lines {
		total += int64(line.Quantity) * line.UnitPriceMinor
	}
	reservationID, err := service.inventory.Reserve(ctx, request.Lines)
	if err != nil {
		if errors.Is(err, ErrStockUnavailable) {
			return Order{}, ErrStockUnavailable
		}
		return Order{}, fmt.Errorf("reserve stock: %w", err)
	}
	order := Order{
		ID:          uuid.New(),
		CustomerRef: request.CustomerRef,
		TotalMinor:  total,
		Lines:       request.Lines,
		CreatedAt:   time.Now().UTC(),
	}
	if err = service.store.Create(ctx, order); err != nil {
		// Compensate: release the reservation when the order does not commit.
		_ = service.inventory.Release(ctx, reservationID)
		return Order{}, err
	}
	return order, nil
}

// Get returns one order.
func (service *Service) Get(ctx context.Context, id uuid.UUID) (Order, error) {
	if id == uuid.Nil {
		return Order{}, ErrUnknownOrder
	}
	return service.store.Get(ctx, id)
}

func validateLines(lines []Line) error {
	if len(lines) == 0 || len(lines) > maxOrderLines {
		return ErrInvalidOrder
	}
	seen := make(map[string]bool, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line.SKU) == "" || line.Quantity <= 0 || line.UnitPriceMinor < 0 {
			return ErrInvalidOrder
		}
		if seen[line.SKU] {
			return ErrInvalidOrder
		}
		seen[line.SKU] = true
	}
	return nil
}
