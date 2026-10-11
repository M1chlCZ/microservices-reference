// Package invoicing issues invoices with a transactional outbox event.
package invoicing

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrInvalidIssue reports a malformed invoice request.
var ErrInvalidIssue = errors.New("invalid issue")

// ErrUnknownInvoice reports a lookup for an invoice that does not exist.
var ErrUnknownInvoice = errors.New("unknown invoice")

// Invoice is one issued invoice.
type Invoice struct {
	ID         uuid.UUID `json:"id"`
	Number     string    `json:"number"`
	OrderID    uuid.UUID `json:"orderId"`
	TotalMinor int64     `json:"totalMinor"`
	IssuedAt   time.Time `json:"issuedAt"`
}

// Store persists invoices and their outbox events.
type Store interface {
	Issue(ctx context.Context, orderID uuid.UUID, totalMinor int64) (Invoice, bool, error)
	InvoiceForOrder(ctx context.Context, orderID uuid.UUID) (Invoice, error)
}

// Service implements the invoice use cases.
type Service struct {
	store Store
}

// NewService returns a Service.
func NewService(store Store) *Service {
	return &Service{store: store}
}

// Issue returns the invoice of one order. A repeated call returns the stored
// invoice and reports a duplicate.
func (service *Service) Issue(ctx context.Context, orderID uuid.UUID, totalMinor int64) (Invoice, bool, error) {
	if orderID == uuid.Nil || totalMinor < 0 {
		return Invoice{}, false, ErrInvalidIssue
	}
	return service.store.Issue(ctx, orderID, totalMinor)
}

// InvoiceForOrder returns the invoice of one order.
func (service *Service) InvoiceForOrder(ctx context.Context, orderID uuid.UUID) (Invoice, error) {
	if orderID == uuid.Nil {
		return Invoice{}, ErrUnknownInvoice
	}
	return service.store.InvoiceForOrder(ctx, orderID)
}
