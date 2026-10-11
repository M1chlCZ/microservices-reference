package shipping

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

type fakeStore struct {
	byOrder    map[uuid.UUID]Shipment
	byTracking map[string]uuid.UUID
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		byOrder:    map[uuid.UUID]Shipment{},
		byTracking: map[string]uuid.UUID{},
	}
}

func (store *fakeStore) Create(_ context.Context, shipment Shipment) (Shipment, bool, error) {
	if existing, ok := store.byOrder[shipment.OrderID]; ok {
		return existing, true, nil
	}
	store.byOrder[shipment.OrderID] = shipment
	store.byTracking[shipment.TrackingCode] = shipment.OrderID
	return shipment, false, nil
}

func (store *fakeStore) UpdateStatus(_ context.Context, trackingCode, status string) (Shipment, error) {
	orderID, ok := store.byTracking[trackingCode]
	if !ok {
		return Shipment{}, ErrUnknownShipment
	}
	shipment := store.byOrder[orderID]
	if shipment.Status == status {
		return shipment, nil
	}
	if !canTransition(shipment.Status, status) {
		return Shipment{}, ErrInvalidState
	}
	shipment.Status = status
	shipment.UpdatedAt = time.Now().UTC()
	store.byOrder[orderID] = shipment
	return shipment, nil
}

func (store *fakeStore) ShipmentForOrder(_ context.Context, orderID uuid.UUID) (Shipment, error) {
	shipment, ok := store.byOrder[orderID]
	if !ok {
		return Shipment{}, ErrUnknownShipment
	}
	return shipment, nil
}

func newTestApp(store *fakeStore) *fiber.App {
	app := fiber.New()
	RegisterRoutes(app, NewService(store, "generic"))
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

func createTestShipment(t *testing.T, store *fakeStore, orderID uuid.UUID) Shipment {
	t.Helper()
	shipment, duplicate, err := NewService(store, "generic").CreateForOrder(context.Background(), orderID)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate {
		t.Fatalf("duplicate shipment for %s", orderID)
	}
	return shipment
}

func TestCarrierWebhookTransitions(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	app := newTestApp(store)
	orderID := uuid.New()
	shipment := createTestShipment(t, store, orderID)

	for _, status := range []string{"in_transit", "in_transit", "delivered", "delivered"} {
		response, body := doRequest(t, app, http.MethodPost, "/webhooks/carrier",
			`{"trackingCode":"`+shipment.TrackingCode+`","status":"`+status+`"}`)
		if response.StatusCode != http.StatusOK || !strings.Contains(body, `"status":"`+status+`"`) {
			t.Fatalf("%s: %d %s", status, response.StatusCode, body)
		}
	}
	if store.byOrder[orderID].Status != "delivered" {
		t.Fatalf("stored status: %s", store.byOrder[orderID].Status)
	}
}

func TestCarrierWebhookRejections(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	app := newTestApp(store)
	orderID := uuid.New()
	shipment := createTestShipment(t, store, orderID)

	response, body := doRequest(t, app, http.MethodPost, "/webhooks/carrier",
		`{"trackingCode":"`+shipment.TrackingCode+`","status":"delivered"}`)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("skipped status: %d %s", response.StatusCode, body)
	}
	response, body = doRequest(t, app, http.MethodPost, "/webhooks/carrier",
		`{"trackingCode":"`+shipment.TrackingCode+`","status":"lost"}`)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("unknown status: %d %s", response.StatusCode, body)
	}
	response, body = doRequest(t, app, http.MethodPost, "/webhooks/carrier",
		`{"trackingCode":"TRK-0000000000","status":"in_transit"}`)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown shipment: %d %s", response.StatusCode, body)
	}
	for _, invalid := range []string{`{"trackingCode":`, `{}`, `{"trackingCode":"X"}`, `{"status":"in_transit"}`} {
		response, body = doRequest(t, app, http.MethodPost, "/webhooks/carrier", invalid)
		if response.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("invalid body %q: %d %s", invalid, response.StatusCode, body)
		}
	}

	_, _ = doRequest(t, app, http.MethodPost, "/webhooks/carrier",
		`{"trackingCode":"`+shipment.TrackingCode+`","status":"in_transit"}`)
	_, _ = doRequest(t, app, http.MethodPost, "/webhooks/carrier",
		`{"trackingCode":"`+shipment.TrackingCode+`","status":"delivered"}`)
	response, body = doRequest(t, app, http.MethodPost, "/webhooks/carrier",
		`{"trackingCode":"`+shipment.TrackingCode+`","status":"in_transit"}`)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("backwards status: %d %s", response.StatusCode, body)
	}
}

func TestShipmentRead(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	app := newTestApp(store)
	orderID := uuid.New()
	shipment := createTestShipment(t, store, orderID)

	response, body := doRequest(t, app, http.MethodGet, "/shipments/"+orderID.String(), "")
	if response.StatusCode != http.StatusOK || !strings.Contains(body, `"trackingCode":"`+shipment.TrackingCode+`"`) {
		t.Fatalf("read: %d %s", response.StatusCode, body)
	}
	response, _ = doRequest(t, app, http.MethodGet, "/shipments/"+uuid.New().String(), "")
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("read unknown: %d", response.StatusCode)
	}
	response, _ = doRequest(t, app, http.MethodGet, "/shipments/not-a-uuid", "")
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("read invalid id: %d", response.StatusCode)
	}
}
