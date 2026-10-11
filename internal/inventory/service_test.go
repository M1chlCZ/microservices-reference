package inventory

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

type fakeStore struct {
	items    map[string]int
	reserved map[uuid.UUID][]ReservationItem
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		items:    map[string]int{"WIDGET-001": 5},
		reserved: map[uuid.UUID][]ReservationItem{},
	}
}

func (store *fakeStore) Item(_ context.Context, sku string) (Item, error) {
	available, ok := store.items[sku]
	if !ok {
		return Item{}, ErrUnknownSKU
	}
	return Item{SKU: sku, Available: available}, nil
}

func (store *fakeStore) Reserve(_ context.Context, items []ReservationItem) (uuid.UUID, error) {
	for _, item := range items {
		available, ok := store.items[item.SKU]
		if !ok {
			return uuid.Nil, ErrUnknownSKU
		}
		if available < item.Quantity {
			return uuid.Nil, ErrInsufficientStock
		}
	}
	for _, item := range items {
		store.items[item.SKU] -= item.Quantity
	}
	id := uuid.New()
	store.reserved[id] = items
	return id, nil
}

func (store *fakeStore) Release(_ context.Context, reservationID uuid.UUID) error {
	for _, item := range store.reserved[reservationID] {
		store.items[item.SKU] += item.Quantity
	}
	delete(store.reserved, reservationID)
	return nil
}

func newTestApp(store *fakeStore) *fiber.App {
	app := fiber.New()
	RegisterRoutes(app, NewService(store))
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

func TestStockLookup(t *testing.T) {
	t.Parallel()
	app := newTestApp(newFakeStore())

	response, body := doRequest(t, app, http.MethodGet, "/stock/WIDGET-001", "")
	if response.StatusCode != http.StatusOK || !strings.Contains(body, `"available":5`) {
		t.Fatalf("lookup: %d %s", response.StatusCode, body)
	}
	response, _ = doRequest(t, app, http.MethodGet, "/stock/MISSING", "")
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("missing lookup: %d", response.StatusCode)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "application/problem+json" {
		t.Fatalf("problem content type: %q", contentType)
	}
}

func TestReserveAndRelease(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	app := newTestApp(store)

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
	if store.items["WIDGET-001"] != 3 {
		t.Fatalf("stock after reserve: %d", store.items["WIDGET-001"])
	}
	reservationID, err := uuid.Parse(payload.ReservationID)
	if err != nil {
		t.Fatal(err)
	}

	response, _ = doRequest(t, app, http.MethodDelete, "/reservations/"+reservationID.String(), "")
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("release: %d", response.StatusCode)
	}
	if store.items["WIDGET-001"] != 5 {
		t.Fatalf("stock after release: %d", store.items["WIDGET-001"])
	}
}

func TestReserveRejections(t *testing.T) {
	t.Parallel()
	app := newTestApp(newFakeStore())

	cases := []struct {
		name string
		body string
		want int
	}{
		{"invalid body", `{"items":`, http.StatusUnprocessableEntity},
		{"empty items", `{"items":[]}`, http.StatusUnprocessableEntity},
		{"zero quantity", `{"items":[{"sku":"WIDGET-001","quantity":0}]}`, http.StatusUnprocessableEntity},
		{"duplicate sku", `{"items":[{"sku":"WIDGET-001","quantity":1},{"sku":"WIDGET-001","quantity":1}]}`, http.StatusUnprocessableEntity},
		{"unknown sku", `{"items":[{"sku":"MISSING","quantity":1}]}`, http.StatusConflict},
		{"insufficient stock", `{"items":[{"sku":"WIDGET-001","quantity":99}]}`, http.StatusConflict},
	}
	for _, testCase := range cases {
		response, body := doRequest(t, app, http.MethodPost, "/reservations", testCase.body)
		if response.StatusCode != testCase.want {
			t.Fatalf("%s: %d %s", testCase.name, response.StatusCode, body)
		}
	}
}

func TestReleaseUnknownReservationIsNoOp(t *testing.T) {
	t.Parallel()
	app := newTestApp(newFakeStore())

	response, _ := doRequest(t, app, http.MethodDelete, "/reservations/"+uuid.New().String(), "")
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("release unknown: %d", response.StatusCode)
	}
	response, _ = doRequest(t, app, http.MethodDelete, "/reservations/not-a-uuid", "")
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("release invalid id: %d", response.StatusCode)
	}
	response, _ = doRequest(t, app, http.MethodDelete,
		"/reservations/00000000-0000-0000-0000-000000000000", "")
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("release nil id: %d", response.StatusCode)
	}
}
