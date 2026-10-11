// Package shipping creates one shipment per paid order and records carrier
// status reports.
package shipping

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrUnknownShipment reports a lookup for a shipment that does not exist.
var ErrUnknownShipment = errors.New("unknown shipment")

// ErrInvalidState reports a status that does not follow the shipment status
// order.
var ErrInvalidState = errors.New("invalid state")

const (
	statusCreated   = "created"
	statusInTransit = "in_transit"
	statusDelivered = "delivered"
)

// Shipment is one parcel for an order.
type Shipment struct {
	ID           uuid.UUID `json:"id"`
	OrderID      uuid.UUID `json:"orderId"`
	Carrier      string    `json:"carrier"`
	TrackingCode string    `json:"trackingCode"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// Store persists shipments and their outbox events.
type Store interface {
	// Create stores one shipment and its outbox event. When a shipment
	// exists for the order, it returns the stored shipment with a duplicate
	// flag.
	Create(ctx context.Context, shipment Shipment) (Shipment, bool, error)

	// UpdateStatus applies one status report and returns the stored shipment.
	UpdateStatus(ctx context.Context, trackingCode, status string) (Shipment, error)

	// ShipmentForOrder returns the shipment of one order.
	ShipmentForOrder(ctx context.Context, orderID uuid.UUID) (Shipment, error)
}

// Service implements the shipping use cases.
type Service struct {
	store   Store
	carrier string
}

// NewService returns a Service.
func NewService(store Store, carrier string) *Service {
	return &Service{store: store, carrier: carrier}
}

// CreateForOrder creates one shipment for an order and returns it with a
// duplicate flag. A repeated call returns the stored shipment.
func (service *Service) CreateForOrder(ctx context.Context, orderID uuid.UUID) (Shipment, bool, error) {
	now := time.Now().UTC()
	shipment := Shipment{
		ID:           uuid.New(),
		OrderID:      orderID,
		Carrier:      service.carrier,
		TrackingCode: newTrackingCode(),
		Status:       statusCreated,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	return service.store.Create(ctx, shipment)
}

// UpdateStatus applies a carrier status report and returns the updated
// shipment. The same status is a duplicate no-op. A backwards or unknown
// transition returns ErrInvalidState. An unknown tracking code returns
// ErrUnknownShipment.
func (service *Service) UpdateStatus(ctx context.Context, trackingCode, status string) (Shipment, error) {
	trackingCode = strings.TrimSpace(trackingCode)
	if trackingCode == "" {
		return Shipment{}, ErrUnknownShipment
	}
	return service.store.UpdateStatus(ctx, trackingCode, strings.TrimSpace(status))
}

// ShipmentForOrder returns the shipment of one order.
func (service *Service) ShipmentForOrder(ctx context.Context, orderID uuid.UUID) (Shipment, error) {
	if orderID == uuid.Nil {
		return Shipment{}, ErrUnknownShipment
	}
	return service.store.ShipmentForOrder(ctx, orderID)
}

func newTrackingCode() string {
	buf := make([]byte, 5)
	_, _ = rand.Read(buf)
	return "TRK-" + strings.ToUpper(hex.EncodeToString(buf))
}

func canTransition(from, to string) bool {
	switch from {
	case statusCreated:
		return to == statusInTransit
	case statusInTransit:
		return to == statusDelivered
	}
	return false
}
