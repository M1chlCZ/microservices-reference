package orders

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

const inventoryRequestTimeout = 5 * time.Second

type reservationItem struct {
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
}

// HTTPInventoryClient calls the inventory service over HTTP.
type HTTPInventoryClient struct {
	baseURL string
	client  *http.Client
}

// NewHTTPInventoryClient returns an HTTPInventoryClient for baseURL.
func NewHTTPInventoryClient(baseURL string) *HTTPInventoryClient {
	return &HTTPInventoryClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  &http.Client{Timeout: inventoryRequestTimeout},
	}
}

// Reserve reserves the order lines and returns the reservation ID. It returns
// ErrStockUnavailable when the inventory service refuses the reservation.
func (client *HTTPInventoryClient) Reserve(ctx context.Context, lines []Line) (uuid.UUID, error) {
	items := make([]reservationItem, 0, len(lines))
	for _, line := range lines {
		items = append(items, reservationItem{SKU: line.SKU, Quantity: line.Quantity})
	}
	body, err := json.Marshal(map[string][]reservationItem{"items": items})
	if err != nil {
		return uuid.Nil, fmt.Errorf("encode reservation: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost,
		client.baseURL+"/reservations", bytes.NewReader(body))
	if err != nil {
		return uuid.Nil, fmt.Errorf("build reservation request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.client.Do(request)
	if err != nil {
		return uuid.Nil, fmt.Errorf("call inventory: %w", err)
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusCreated:
	case response.StatusCode == http.StatusConflict || response.StatusCode == http.StatusNotFound:
		return uuid.Nil, ErrStockUnavailable
	default:
		return uuid.Nil, fmt.Errorf("inventory answered with status %d", response.StatusCode)
	}
	var payload struct {
		ReservationID string `json:"reservationId"`
	}
	if err = json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return uuid.Nil, fmt.Errorf("decode reservation: %w", err)
	}
	reservationID, err := uuid.Parse(payload.ReservationID)
	if err != nil {
		return uuid.Nil, fmt.Errorf("parse reservation ID: %w", err)
	}
	return reservationID, nil
}

// Release releases a reservation. It treats an unknown reservation as
// released.
func (client *HTTPInventoryClient) Release(ctx context.Context, reservationID uuid.UUID) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		client.baseURL+"/reservations/"+reservationID.String(), nil)
	if err != nil {
		return fmt.Errorf("build release request: %w", err)
	}
	response, err := client.client.Do(request)
	if err != nil {
		return fmt.Errorf("call inventory: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusNotFound {
		return fmt.Errorf("inventory answered with status %d", response.StatusCode)
	}
	return nil
}
