package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

type fakeInventory struct {
	reserved []Item
	err      error
}

func (client *fakeInventory) Reserve(_ context.Context, items []Item) (uuid.UUID, error) {
	client.reserved = items
	if client.err != nil {
		return uuid.Nil, client.err
	}
	return uuid.New(), nil
}

func newTestApp(inventory InventoryClient) *fiber.App {
	app := fiber.New()
	RegisterRoutes(app, NewService(inventory))
	return app
}

func doRequest(t *testing.T, app *fiber.App, method, path, body string) (*http.Response, string) {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response, string(payload)
}

func TestReserve(t *testing.T) {
	t.Parallel()
	inventory := &fakeInventory{}
	app := newTestApp(inventory)

	response, body := doRequest(t, app, http.MethodPost, "/reservations",
		`{"items":[{"sku":"WIDGET-001","quantity":2}]}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("reserve: %d %s", response.StatusCode, body)
	}
	var payload struct {
		ReservationID string `json:"reservationId"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(payload.ReservationID); err != nil {
		t.Fatalf("reservation id: %q", payload.ReservationID)
	}
	if len(inventory.reserved) != 1 || inventory.reserved[0].SKU != "WIDGET-001" ||
		inventory.reserved[0].Quantity != 2 {
		t.Fatalf("reserved items: %#v", inventory.reserved)
	}
}

func TestReserveRejections(t *testing.T) {
	t.Parallel()
	app := newTestApp(&fakeInventory{})

	cases := []struct {
		name string
		body string
		want int
	}{
		{"invalid body", `{"items":`, http.StatusUnprocessableEntity},
		{"empty items", `{"items":[]}`, http.StatusUnprocessableEntity},
		{"blank sku", `{"items":[{"sku":" ","quantity":1}]}`, http.StatusUnprocessableEntity},
		{"zero quantity", `{"items":[{"sku":"WIDGET-001","quantity":0}]}`, http.StatusUnprocessableEntity},
		{"duplicate sku", `{"items":[{"sku":"WIDGET-001","quantity":1},{"sku":"WIDGET-001","quantity":1}]}`, http.StatusUnprocessableEntity},
	}
	for _, testCase := range cases {
		response, body := doRequest(t, app, http.MethodPost, "/reservations", testCase.body)
		if response.StatusCode != testCase.want {
			t.Fatalf("%s: %d %s", testCase.name, response.StatusCode, body)
		}
	}
}

func TestReserveMapsStockConflict(t *testing.T) {
	t.Parallel()
	app := newTestApp(&fakeInventory{err: ErrStockUnavailable})

	response, _ := doRequest(t, app, http.MethodPost, "/reservations",
		`{"items":[{"sku":"WIDGET-001","quantity":99}]}`)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("stock conflict: %d", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "application/problem+json" {
		t.Fatalf("problem content type: %q", contentType)
	}
}

func TestReservePassesThroughClientErrors(t *testing.T) {
	t.Parallel()
	app := newTestApp(&fakeInventory{err: errors.New("dial tcp: connection refused")})

	response, _ := doRequest(t, app, http.MethodPost, "/reservations",
		`{"items":[{"sku":"WIDGET-001","quantity":1}]}`)
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("internal error: %d", response.StatusCode)
	}
}
