// Package payments implements payment intents and provider capture.
package payments

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrInvalidIntent reports a malformed payment intent request.
var ErrInvalidIntent = errors.New("invalid payment intent")

// ErrIdempotencyConflict reports a reused idempotency key with a different request.
var ErrIdempotencyConflict = errors.New("idempotency conflict")

// ErrUnknownIntent reports a lookup for a payment intent that does not exist.
var ErrUnknownIntent = errors.New("unknown payment intent")

// ErrInvalidState reports an operation that the intent status forbids.
var ErrInvalidState = errors.New("invalid payment intent state")

const (
	statusRequiresPayment = "requires_payment"
	statusCaptured        = "captured"
	statusRefunded        = "refunded"
)

// Intent is one payment intent.
type Intent struct {
	ID             uuid.UUID `json:"id"`
	OrderID        uuid.UUID `json:"orderId"`
	AmountMinor    int64     `json:"amountMinor"`
	Currency       string    `json:"currency"`
	Status         string    `json:"status"`
	ProviderRef    string    `json:"providerRef"`
	IdempotencyKey string    `json:"idempotencyKey"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// CreateIntentRequest is one payment intent creation request.
type CreateIntentRequest struct {
	OrderID     uuid.UUID `json:"orderId"`
	AmountMinor int64     `json:"amountMinor"`
	Currency    string    `json:"currency"`
}

// ProviderWebhookRequest is one provider webhook call.
type ProviderWebhookRequest struct {
	ProviderRef string `json:"providerRef"`
	Status      string `json:"status"`
}

// Store is the persistence boundary of the service.
type Store interface {
	CreateIntent(ctx context.Context, intent Intent) (Intent, bool, error)
	Capture(ctx context.Context, providerRef string) (Intent, bool, error)
	Intent(ctx context.Context, id uuid.UUID) (Intent, error)
}

// Service implements the payment use cases.
type Service struct {
	store Store
}

// NewService returns a Service.
func NewService(store Store) *Service {
	return &Service{store: store}
}

// CreateIntent validates the request and stores a payment intent with the
// status requires_payment. It returns the stored intent and a duplicate flag
// when the idempotency key or the order already has an intent. A reused key
// with a different request returns ErrIdempotencyConflict.
func (service *Service) CreateIntent(ctx context.Context, orderID uuid.UUID, amountMinor int64, currency, idempotencyKey string) (Intent, bool, error) {
	currency = strings.TrimSpace(currency)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if amountMinor <= 0 || !validCurrency(currency) || idempotencyKey == "" {
		return Intent{}, false, ErrInvalidIntent
	}
	providerRef, err := newProviderRef()
	if err != nil {
		return Intent{}, false, err
	}
	now := time.Now().UTC()
	return service.store.CreateIntent(ctx, Intent{
		ID:             uuid.New(),
		OrderID:        orderID,
		AmountMinor:    amountMinor,
		Currency:       currency,
		Status:         statusRequiresPayment,
		ProviderRef:    providerRef,
		IdempotencyKey: idempotencyKey,
		CreatedAt:      now,
		UpdatedAt:      now,
	})
}

// Capture marks a requires_payment intent captured and writes the
// payment.captured outbox event in one transaction. It returns a duplicate
// flag when the intent is captured already. Unknown provider references
// return ErrUnknownIntent, and refunded intents return ErrInvalidState.
func (service *Service) Capture(ctx context.Context, providerRef string) (Intent, bool, error) {
	return service.store.Capture(ctx, providerRef)
}

// Intent returns one payment intent.
func (service *Service) Intent(ctx context.Context, id uuid.UUID) (Intent, error) {
	if id == uuid.Nil {
		return Intent{}, ErrUnknownIntent
	}
	return service.store.Intent(ctx, id)
}

func newProviderRef() (string, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate provider reference: %w", err)
	}
	return "pay_" + hex.EncodeToString(raw[:]), nil
}

func validCurrency(currency string) bool {
	if len(currency) != 3 {
		return false
	}
	for _, letter := range currency {
		if letter < 'A' || letter > 'Z' {
			return false
		}
	}
	return true
}
