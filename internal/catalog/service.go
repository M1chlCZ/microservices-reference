// Package catalog implements the product catalog.
package catalog

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrInvalidProduct reports a malformed product request.
var ErrInvalidProduct = errors.New("invalid product")

// ErrUnknownProduct reports a lookup for a product that does not exist.
var ErrUnknownProduct = errors.New("unknown product")

// ErrDuplicateSKU reports a create for a SKU that already exists.
var ErrDuplicateSKU = errors.New("duplicate sku")

// ErrInvalidTransition reports a status change that the state machine
// forbids.
var ErrInvalidTransition = errors.New("invalid transition")

// Product statuses.
const (
	// StatusDraft marks a product that is not for sale yet.
	StatusDraft = "draft"
	// StatusActive marks a product that is for sale.
	StatusActive = "active"
	// StatusArchived marks a retired product and is terminal.
	StatusArchived = "archived"
)

const (
	maxSKULength  = 64
	maxNameLength = 200
	minLimit      = 1
	maxLimit      = 100
	defaultLimit  = 20
)

// Product is one catalog product.
type Product struct {
	ID         uuid.UUID `json:"id"`
	SKU        string    `json:"sku"`
	Name       string    `json:"name"`
	PriceMinor int64     `json:"priceMinor"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// CreateRequest is one product creation request.
type CreateRequest struct {
	SKU        string `json:"sku"`
	Name       string `json:"name"`
	PriceMinor int64  `json:"priceMinor"`
}

// PatchRequest is one partial product update. An absent field stays
// unchanged.
type PatchRequest struct {
	Name       *string `json:"name"`
	PriceMinor *int64  `json:"priceMinor"`
	Status     *string `json:"status"`
}

// Store is the persistence boundary of the service.
type Store interface {
	Create(ctx context.Context, product Product) (Product, error)
	Get(ctx context.Context, id uuid.UUID) (Product, error)
	List(ctx context.Context, limit int, cursor string) ([]Product, error)
	Update(ctx context.Context, product Product) (Product, error)
}

// Service implements the catalog use cases.
type Service struct {
	store Store
}

// NewService returns a Service.
func NewService(store Store) *Service {
	return &Service{store: store}
}

// Create validates one request and stores a new product in draft.
func (service *Service) Create(ctx context.Context, request CreateRequest) (Product, error) {
	sku := strings.TrimSpace(request.SKU)
	name := strings.TrimSpace(request.Name)
	if !validSKU(sku) || !validName(name) || request.PriceMinor < 0 {
		return Product{}, ErrInvalidProduct
	}
	now := time.Now().UTC()
	return service.store.Create(ctx, Product{
		ID:         uuid.New(),
		SKU:        sku,
		Name:       name,
		PriceMinor: request.PriceMinor,
		Status:     StatusDraft,
		CreatedAt:  now,
		UpdatedAt:  now,
	})
}

// Get returns one product in any status.
func (service *Service) Get(ctx context.Context, id uuid.UUID) (Product, error) {
	if id == uuid.Nil {
		return Product{}, ErrUnknownProduct
	}
	return service.store.Get(ctx, id)
}

// List returns active products ordered by SKU after the cursor. The cursor
// is the last SKU of the previous page.
func (service *Service) List(ctx context.Context, limit int, cursor string) ([]Product, error) {
	return service.store.List(ctx, clampLimit(limit), strings.TrimSpace(cursor))
}

// Patch applies one partial update and returns the stored product. An empty
// patch returns the current product.
func (service *Service) Patch(ctx context.Context, id uuid.UUID, request PatchRequest) (Product, error) {
	name := ""
	if request.Name != nil {
		name = strings.TrimSpace(*request.Name)
		if !validName(name) {
			return Product{}, ErrInvalidProduct
		}
	}
	if request.PriceMinor != nil && *request.PriceMinor < 0 {
		return Product{}, ErrInvalidProduct
	}
	if request.Status != nil && !validStatus(*request.Status) {
		return Product{}, ErrInvalidProduct
	}
	product, err := service.Get(ctx, id)
	if err != nil {
		return Product{}, err
	}
	if request.Name == nil && request.PriceMinor == nil && request.Status == nil {
		return product, nil
	}
	if request.Name != nil {
		product.Name = name
	}
	if request.PriceMinor != nil {
		product.PriceMinor = *request.PriceMinor
	}
	if request.Status != nil {
		if err = applyTransition(&product, *request.Status); err != nil {
			return Product{}, err
		}
	}
	product.UpdatedAt = time.Now().UTC()
	return service.store.Update(ctx, product)
}

func validSKU(sku string) bool {
	return sku != "" && len(sku) <= maxSKULength
}

func validName(name string) bool {
	return name != "" && len(name) <= maxNameLength
}

func validStatus(status string) bool {
	switch status {
	case StatusDraft, StatusActive, StatusArchived:
		return true
	default:
		return false
	}
}

func applyTransition(product *Product, status string) error {
	switch {
	case product.Status == StatusDraft && status == StatusActive:
		product.Status = StatusActive
		return nil
	case product.Status == StatusActive && status == StatusArchived:
		product.Status = StatusArchived
		return nil
	default:
		return ErrInvalidTransition
	}
}

func clampLimit(limit int) int {
	if limit < minLimit {
		return minLimit
	}
	if limit > maxLimit {
		return maxLimit
	}
	return limit
}
