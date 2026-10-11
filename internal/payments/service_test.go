package payments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

type fakeStore struct {
	byID    map[uuid.UUID]*Intent
	byKey   map[string]uuid.UUID
	byOrder map[uuid.UUID]uuid.UUID
	byRef   map[string]uuid.UUID
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		byID:    map[uuid.UUID]*Intent{},
		byKey:   map[string]uuid.UUID{},
		byOrder: map[uuid.UUID]uuid.UUID{},
		byRef:   map[string]uuid.UUID{},
	}
}

func (store *fakeStore) CreateIntent(_ context.Context, intent Intent) (Intent, bool, error) {
	if id, ok := store.byKey[intent.IdempotencyKey]; ok {
		stored := *store.byID[id]
		if stored.OrderID == intent.OrderID && stored.AmountMinor == intent.AmountMinor &&
			stored.Currency == intent.Currency {
			return stored, true, nil
		}
		return Intent{}, false, ErrIdempotencyConflict
	}
	if id, ok := store.byOrder[intent.OrderID]; ok {
		return *store.byID[id], true, nil
	}
	stored := intent
	store.byID[intent.ID] = &stored
	store.byKey[intent.IdempotencyKey] = intent.ID
	store.byOrder[intent.OrderID] = intent.ID
	store.byRef[intent.ProviderRef] = intent.ID
	return intent, false, nil
}

func (store *fakeStore) Capture(_ context.Context, providerRef string) (Intent, bool, error) {
	id, ok := store.byRef[providerRef]
	if !ok {
		return Intent{}, false, ErrUnknownIntent
	}
	intent := store.byID[id]
	if intent.Status == statusCaptured {
		return *intent, true, nil
	}
	if intent.Status != statusRequiresPayment {
		return Intent{}, false, ErrInvalidState
	}
	intent.Status = statusCaptured
	return *intent, false, nil
}

func (store *fakeStore) Intent(_ context.Context, id uuid.UUID) (Intent, error) {
	intent, ok := store.byID[id]
	if !ok {
		return Intent{}, ErrUnknownIntent
	}
	return *intent, nil
}

func newTestApp(store *fakeStore) *fiber.App {
	app := fiber.New()
	RegisterRoutes(app, NewService(store))
	return app
}

func doRequest(t *testing.T, app *fiber.App, method, path, body string) (*http.Response, string) {
	t.Helper()
	return doRequestWithKey(t, app, method, path, body, "")
}

func doRequestWithKey(t *testing.T, app *fiber.App, method, path, body, idempotencyKey string) (*http.Response, string) {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
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

func TestCreateIntentIsIdempotent(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	service := NewService(store)
	orderID := uuid.New()

	intent, duplicate, err := service.CreateIntent(context.Background(), orderID, 1500, "CZK", "key-1")
	if err != nil || duplicate {
		t.Fatalf("create: %v %v", err, duplicate)
	}
	if intent.Status != statusRequiresPayment || len(intent.ProviderRef) != 20 ||
		!strings.HasPrefix(intent.ProviderRef, "pay_") {
		t.Fatalf("intent: %#v", intent)
	}
	again, duplicate, err := service.CreateIntent(context.Background(), orderID, 1500, "CZK", "key-1")
	if err != nil || !duplicate {
		t.Fatalf("duplicate: %v %v", err, duplicate)
	}
	if again.ID != intent.ID || len(store.byID) != 1 {
		t.Fatalf("duplicate intent: %#v", again)
	}
}

func TestCreateIntentConflict(t *testing.T) {
	t.Parallel()
	service := NewService(newFakeStore())
	orderID := uuid.New()

	first, _, err := service.CreateIntent(context.Background(), orderID, 1500, "CZK", "key-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = service.CreateIntent(context.Background(), orderID, 2500, "CZK", "key-1"); !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("conflict: %v", err)
	}
	second, duplicate, err := service.CreateIntent(context.Background(), orderID, 900, "EUR", "key-2")
	if err != nil || !duplicate {
		t.Fatalf("order duplicate: %v %v", err, duplicate)
	}
	if second.ID != first.ID {
		t.Fatalf("order duplicate intent: %#v", second)
	}
}

func TestCreateIntentValidation(t *testing.T) {
	t.Parallel()
	service := NewService(newFakeStore())
	orderID := uuid.New()

	cases := []struct {
		name          string
		amountMinor   int64
		currency, key string
	}{
		{"zero amount", 0, "CZK", "key-1"},
		{"negative amount", -5, "CZK", "key-2"},
		{"lowercase currency", 100, "czk", "key-3"},
		{"short currency", 100, "CZ", "key-4"},
		{"blank key", 100, "CZK", "  "},
	}
	for _, testCase := range cases {
		if _, _, err := service.CreateIntent(context.Background(), orderID, testCase.amountMinor, testCase.currency, testCase.key); !errors.Is(err, ErrInvalidIntent) {
			t.Fatalf("%s: %v", testCase.name, err)
		}
	}
}

func TestCaptureOnceAndRepeated(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	service := NewService(store)

	intent, _, err := service.CreateIntent(context.Background(), uuid.New(), 1500, "CZK", "key-1")
	if err != nil {
		t.Fatal(err)
	}
	captured, duplicate, err := service.Capture(context.Background(), intent.ProviderRef)
	if err != nil || duplicate {
		t.Fatalf("capture: %v %v", err, duplicate)
	}
	if captured.Status != statusCaptured {
		t.Fatalf("capture status: %s", captured.Status)
	}
	again, duplicate, err := service.Capture(context.Background(), intent.ProviderRef)
	if err != nil || !duplicate {
		t.Fatalf("repeat capture: %v %v", err, duplicate)
	}
	if again.ID != intent.ID || again.Status != statusCaptured {
		t.Fatalf("repeat intent: %#v", again)
	}
}

func TestCaptureRejections(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	service := NewService(store)

	if _, _, err := service.Capture(context.Background(), "pay_missing"); !errors.Is(err, ErrUnknownIntent) {
		t.Fatalf("unknown reference: %v", err)
	}
	intent, _, err := service.CreateIntent(context.Background(), uuid.New(), 1500, "CZK", "key-1")
	if err != nil {
		t.Fatal(err)
	}
	store.byID[intent.ID].Status = statusRefunded
	if _, _, err = service.Capture(context.Background(), intent.ProviderRef); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("refunded capture: %v", err)
	}
}

func TestCreateIntentHandler(t *testing.T) {
	t.Parallel()
	app := newTestApp(newFakeStore())
	body := fmt.Sprintf(`{"orderId":"%s","amountMinor":1500,"currency":"CZK"}`, uuid.New())

	response, payload := doRequestWithKey(t, app, http.MethodPost, "/payment-intents", body, "key-1")
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d %s", response.StatusCode, payload)
	}
	var created Intent
	if err := json.Unmarshal([]byte(payload), &created); err != nil {
		t.Fatal(err)
	}
	response, payload = doRequestWithKey(t, app, http.MethodPost, "/payment-intents", body, "key-1")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("duplicate create: %d %s", response.StatusCode, payload)
	}
	var duplicate Intent
	if err := json.Unmarshal([]byte(payload), &duplicate); err != nil {
		t.Fatal(err)
	}
	if duplicate.ID != created.ID {
		t.Fatalf("duplicate ID: %s", duplicate.ID)
	}
}

func TestCreateIntentHandlerRejections(t *testing.T) {
	t.Parallel()
	app := newTestApp(newFakeStore())
	orderID := uuid.New()
	valid := fmt.Sprintf(`{"orderId":"%s","amountMinor":1500,"currency":"CZK"}`, orderID)
	different := fmt.Sprintf(`{"orderId":"%s","amountMinor":2500,"currency":"CZK"}`, orderID)
	lowercase := fmt.Sprintf(`{"orderId":"%s","amountMinor":1500,"currency":"czk"}`, uuid.New())

	response, _ := doRequestWithKey(t, app, http.MethodPost, "/payment-intents", valid, "key-1")
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d", response.StatusCode)
	}
	response, _ = doRequestWithKey(t, app, http.MethodPost, "/payment-intents", different, "key-1")
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("conflict: %d", response.StatusCode)
	}
	response, _ = doRequestWithKey(t, app, http.MethodPost, "/payment-intents", valid, "")
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("missing key: %d", response.StatusCode)
	}
	response, _ = doRequestWithKey(t, app, http.MethodPost, "/payment-intents", `{"orderId":`, "key-2")
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("invalid body: %d", response.StatusCode)
	}
	response, _ = doRequestWithKey(t, app, http.MethodPost, "/payment-intents", lowercase, "key-3")
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("lowercase currency: %d", response.StatusCode)
	}
}

func TestProviderWebhookHandler(t *testing.T) {
	t.Parallel()
	service := NewService(newFakeStore())
	intent, _, err := service.CreateIntent(context.Background(), uuid.New(), 1500, "CZK", "key-1")
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	RegisterRoutes(app, service)
	body := fmt.Sprintf(`{"providerRef":%q,"status":"paid"}`, intent.ProviderRef)

	response, payload := doRequest(t, app, http.MethodPost, "/webhooks/provider", body)
	if response.StatusCode != http.StatusOK || !strings.Contains(payload, `"status":"paid"`) {
		t.Fatalf("webhook: %d %s", response.StatusCode, payload)
	}
	response, _ = doRequest(t, app, http.MethodPost, "/webhooks/provider", body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("repeated webhook: %d", response.StatusCode)
	}
	response, _ = doRequest(t, app, http.MethodPost, "/webhooks/provider",
		`{"providerRef":"pay_missing","status":"paid"}`)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown reference: %d", response.StatusCode)
	}
	response, _ = doRequest(t, app, http.MethodPost, "/webhooks/provider", `{"providerRef":`)
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("invalid body: %d", response.StatusCode)
	}
	response, _ = doRequest(t, app, http.MethodPost, "/webhooks/provider",
		`{"providerRef":"pay_x","status":"refunded"}`)
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("invalid status: %d", response.StatusCode)
	}
}

func TestGetIntentHandler(t *testing.T) {
	t.Parallel()
	service := NewService(newFakeStore())
	intent, _, err := service.CreateIntent(context.Background(), uuid.New(), 1500, "CZK", "key-1")
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	RegisterRoutes(app, service)

	response, payload := doRequest(t, app, http.MethodGet, "/payment-intents/"+intent.ID.String(), "")
	if response.StatusCode != http.StatusOK || !strings.Contains(payload, `"status":"requires_payment"`) {
		t.Fatalf("get: %d %s", response.StatusCode, payload)
	}
	response, _ = doRequest(t, app, http.MethodGet, "/payment-intents/"+uuid.New().String(), "")
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("get unknown: %d", response.StatusCode)
	}
	response, _ = doRequest(t, app, http.MethodGet, "/payment-intents/not-a-uuid", "")
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("get invalid id: %d", response.StatusCode)
	}
}
