package orders

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

type fakeStore struct {
	orders map[uuid.UUID]Order
	fail   error
}

func newFakeStore() *fakeStore {
	return &fakeStore{orders: map[uuid.UUID]Order{}}
}

func (store *fakeStore) Create(_ context.Context, order Order) error {
	if store.fail != nil {
		return store.fail
	}
	store.orders[order.ID] = order
	return nil
}

func (store *fakeStore) Get(_ context.Context, id uuid.UUID) (Order, error) {
	order, ok := store.orders[id]
	if !ok {
		return Order{}, ErrUnknownOrder
	}
	return order, nil
}

func (store *fakeStore) MarkPaid(_ context.Context, id uuid.UUID) (Order, bool, error) {
	order, ok := store.orders[id]
	if !ok {
		return Order{}, false, ErrUnknownOrder
	}
	if order.Status == statusPaid {
		return order, true, nil
	}
	order.Status = statusPaid
	store.orders[id] = order
	return order, false, nil
}

type fakeInventory struct {
	reservations []uuid.UUID
	releases     []uuid.UUID
	failReserve  error
}

func (client *fakeInventory) Reserve(_ context.Context, _ []Line) (uuid.UUID, error) {
	if client.failReserve != nil {
		return uuid.Nil, client.failReserve
	}
	id := uuid.New()
	client.reservations = append(client.reservations, id)
	return id, nil
}

func (client *fakeInventory) Release(_ context.Context, reservationID uuid.UUID) error {
	client.releases = append(client.releases, reservationID)
	return nil
}

type fakePayments struct {
	intents    map[uuid.UUID]Payment
	failCreate error
}

func (client *fakePayments) CreateIntent(
	_ context.Context,
	orderID uuid.UUID,
	_ int64,
	_, _ string,
) (Payment, error) {
	if client.failCreate != nil {
		return Payment{}, client.failCreate
	}
	if client.intents == nil {
		client.intents = map[uuid.UUID]Payment{}
	}
	payment := Payment{IntentID: uuid.New(), ProviderRef: "pay_test"}
	client.intents[orderID] = payment
	return payment, nil
}

func newTestApp(store *fakeStore, inventory *fakeInventory, payments *fakePayments) *fiber.App {
	app := fiber.New()
	RegisterRoutes(app, NewService(store, inventory, payments))
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

func TestCreateOrderReservesAndStores(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	inventory := &fakeInventory{}
	payments := &fakePayments{}
	app := newTestApp(store, inventory, payments)

	response, body := doRequest(t, app, http.MethodPost, "/orders",
		`{"customerRef":"demo","lines":[{"sku":"WIDGET-001","quantity":2,"unitPriceMinor":1999}]}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d %s", response.StatusCode, body)
	}
	var order Order
	if err := json.Unmarshal([]byte(body), &order); err != nil {
		t.Fatal(err)
	}
	if order.TotalMinor != 3998 || order.Status != statusPendingPayment {
		t.Fatalf("order: %#v", order)
	}
	if order.Payment == nil || order.Payment.ProviderRef == "" {
		t.Fatalf("payment: %#v", order.Payment)
	}
	if len(store.orders) != 1 || len(inventory.reservations) != 1 || len(payments.intents) != 1 {
		t.Fatalf("stores: orders %d, reservations %d, intents %d",
			len(store.orders), len(inventory.reservations), len(payments.intents))
	}
	if len(inventory.releases) != 0 {
		t.Fatalf("unexpected releases: %d", len(inventory.releases))
	}
}

func TestCreateOrderCompensatesOnStoreFailure(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	store.fail = errors.New("store is down")
	inventory := &fakeInventory{}
	app := newTestApp(store, inventory, &fakePayments{})

	response, _ := doRequest(t, app, http.MethodPost, "/orders",
		`{"customerRef":"demo","lines":[{"sku":"WIDGET-001","quantity":1,"unitPriceMinor":100}]}`)
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("create with failing store: %d", response.StatusCode)
	}
	if len(inventory.releases) != 1 || inventory.releases[0] != inventory.reservations[0] {
		t.Fatalf("compensation missing: releases %v", inventory.releases)
	}
}

func TestCreateOrderCompensatesOnPaymentFailure(t *testing.T) {
	t.Parallel()
	inventory := &fakeInventory{}
	payments := &fakePayments{failCreate: errors.New("payments are down")}
	store := newFakeStore()
	app := newTestApp(store, inventory, payments)

	response, _ := doRequest(t, app, http.MethodPost, "/orders",
		`{"customerRef":"demo","lines":[{"sku":"WIDGET-001","quantity":1,"unitPriceMinor":100}]}`)
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("create with failing payments: %d", response.StatusCode)
	}
	if len(inventory.releases) != 1 || inventory.releases[0] != inventory.reservations[0] {
		t.Fatalf("compensation missing: releases %v", inventory.releases)
	}
	if len(store.orders) != 0 {
		t.Fatalf("order stored without a payment: %d", len(store.orders))
	}
}

func TestCreateOrderRejections(t *testing.T) {
	t.Parallel()
	inventory := &fakeInventory{}
	app := newTestApp(newFakeStore(), inventory, &fakePayments{})

	cases := []struct {
		name string
		body string
		want int
	}{
		{"invalid body", `{"customerRef":`, http.StatusUnprocessableEntity},
		{"missing customer", `{"lines":[{"sku":"WIDGET-001","quantity":1,"unitPriceMinor":100}]}`, http.StatusUnprocessableEntity},
		{"empty lines", `{"customerRef":"demo","lines":[]}`, http.StatusUnprocessableEntity},
		{"zero quantity", `{"customerRef":"demo","lines":[{"sku":"WIDGET-001","quantity":0,"unitPriceMinor":100}]}`, http.StatusUnprocessableEntity},
		{"duplicate sku", `{"customerRef":"demo","lines":[{"sku":"A","quantity":1,"unitPriceMinor":1},{"sku":"A","quantity":1,"unitPriceMinor":1}]}`, http.StatusUnprocessableEntity},
	}
	for _, testCase := range cases {
		response, body := doRequest(t, app, http.MethodPost, "/orders", testCase.body)
		if response.StatusCode != testCase.want {
			t.Fatalf("%s: %d %s", testCase.name, response.StatusCode, body)
		}
	}

	inventory.failReserve = ErrStockUnavailable
	response, _ := doRequest(t, app, http.MethodPost, "/orders",
		`{"customerRef":"demo","lines":[{"sku":"WIDGET-001","quantity":1,"unitPriceMinor":100}]}`)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("stock unavailable: %d", response.StatusCode)
	}
}

func TestGetOrder(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	app := newTestApp(store, &fakeInventory{}, &fakePayments{})

	_, body := doRequest(t, app, http.MethodPost, "/orders",
		`{"customerRef":"demo","lines":[{"sku":"WIDGET-001","quantity":1,"unitPriceMinor":100}]}`)
	var created Order
	if err := json.Unmarshal([]byte(body), &created); err != nil {
		t.Fatal(err)
	}

	response, body := doRequest(t, app, http.MethodGet, "/orders/"+created.ID.String(), "")
	if response.StatusCode != http.StatusOK || !strings.Contains(body, `"status":"pending_payment"`) {
		t.Fatalf("get: %d %s", response.StatusCode, body)
	}
	response, _ = doRequest(t, app, http.MethodGet, "/orders/"+uuid.New().String(), "")
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("get unknown: %d", response.StatusCode)
	}
	response, _ = doRequest(t, app, http.MethodGet, "/orders/not-a-uuid", "")
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("get invalid id: %d", response.StatusCode)
	}
}
