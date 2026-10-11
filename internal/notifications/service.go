// Package notifications records events that other services deliver.
package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// ErrInvalidEvent reports a malformed event.
var ErrInvalidEvent = errors.New("invalid event")

const maxKindLength = 100

// Notification is one received event.
type Notification struct {
	ID         int64           `json:"id"`
	Kind       string          `json:"kind"`
	Payload    json.RawMessage `json:"payload"`
	ReceivedAt time.Time       `json:"receivedAt"`
}

// Store persists notifications.
type Store interface {
	Insert(ctx context.Context, kind string, payload []byte) (Notification, error)
	List(ctx context.Context, limit int) ([]Notification, error)
}

// Service implements the notification use cases.
type Service struct {
	store Store
}

// NewService returns a Service.
func NewService(store Store) *Service {
	return &Service{store: store}
}

// Receive validates one event and stores it.
func (service *Service) Receive(ctx context.Context, kind string, payload []byte) (Notification, error) {
	kind = strings.TrimSpace(kind)
	if kind == "" || len(kind) > maxKindLength || !validPayload(payload) {
		return Notification{}, ErrInvalidEvent
	}
	return service.store.Insert(ctx, kind, payload)
}

// List returns the most recent notifications.
func (service *Service) List(ctx context.Context, limit int) ([]Notification, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return service.store.List(ctx, limit)
}

func validPayload(payload []byte) bool {
	trimmed := bytes.TrimSpace(payload)
	return len(trimmed) > 0 && trimmed[0] == '{' && json.Valid(trimmed)
}
