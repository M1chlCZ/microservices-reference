// Package inventory implements stock positions and reservations.
package inventory

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
)

// ErrInvalidReservation reports a malformed reservation request.
var ErrInvalidReservation = errors.New("invalid reservation")

// ErrUnknownSKU reports a stock lookup for a SKU that does not exist.
var ErrUnknownSKU = errors.New("unknown sku")

// ErrInsufficientStock reports that a reservation exceeds the available stock.
var ErrInsufficientStock = errors.New("insufficient stock")

const maxReservationItems = 50

// Item is one stock position.
type Item struct {
	SKU       string `json:"sku"`
	Available int    `json:"available"`
}

// ReservationItem is one SKU and quantity in a reservation.
type ReservationItem struct {
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
}

// ReservationRequest is one multi-item reservation request.
type ReservationRequest struct {
	Items []ReservationItem `json:"items"`
}

// Store is the persistence boundary of the service.
type Store interface {
	Item(ctx context.Context, sku string) (Item, error)
	Reserve(ctx context.Context, items []ReservationItem) (uuid.UUID, error)
	Release(ctx context.Context, reservationID uuid.UUID) error
}

// Service implements the inventory use cases.
type Service struct {
	store Store
}

// NewService returns a Service.
func NewService(store Store) *Service {
	return &Service{store: store}
}

// Item returns one stock position.
func (service *Service) Item(ctx context.Context, sku string) (Item, error) {
	return service.store.Item(ctx, sku)
}

// Reserve reserves every item in one transaction and returns the reservation
// ID. The reservation either reserves every item or none.
func (service *Service) Reserve(ctx context.Context, request ReservationRequest) (uuid.UUID, error) {
	if err := validateItems(request.Items); err != nil {
		return uuid.Nil, err
	}
	return service.store.Reserve(ctx, request.Items)
}

// Release returns the reserved quantities to stock. A release of an unknown
// reservation is a no-op.
func (service *Service) Release(ctx context.Context, reservationID uuid.UUID) error {
	if reservationID == uuid.Nil {
		return ErrInvalidReservation
	}
	return service.store.Release(ctx, reservationID)
}

func validateItems(items []ReservationItem) error {
	if len(items) == 0 || len(items) > maxReservationItems {
		return ErrInvalidReservation
	}
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.SKU) == "" || item.Quantity <= 0 {
			return ErrInvalidReservation
		}
		if seen[item.SKU] {
			return ErrInvalidReservation
		}
		seen[item.SKU] = true
	}
	return nil
}
