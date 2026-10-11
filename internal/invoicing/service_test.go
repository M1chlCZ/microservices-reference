package invoicing

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
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

type fakeStore struct {
	invoices map[uuid.UUID]Invoice
	counters map[int]int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		invoices: map[uuid.UUID]Invoice{},
		counters: map[int]int{},
	}
}

func (store *fakeStore) Issue(_ context.Context, orderID uuid.UUID, totalMinor int64) (Invoice, bool, error) {
	if invoice, ok := store.invoices[orderID]; ok {
		return invoice, true, nil
	}
	issuedAt := time.Now().UTC()
	year := issuedAt.Year()
	store.counters[year]++
	invoice := Invoice{
		ID:         uuid.New(),
		Number:     formatInvoiceNumber(year, store.counters[year]),
		OrderID:    orderID,
		TotalMinor: totalMinor,
		IssuedAt:   issuedAt,
	}
	store.invoices[orderID] = invoice
	return invoice, false, nil
}

func (store *fakeStore) InvoiceForOrder(_ context.Context, orderID uuid.UUID) (Invoice, error) {
	invoice, ok := store.invoices[orderID]
	if !ok {
		return Invoice{}, ErrUnknownInvoice
	}
	return invoice, nil
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

func TestIssueNumbersInvoicesPerOrder(t *testing.T) {
	t.Parallel()
	service := NewService(newFakeStore())
	year := time.Now().UTC().Year()

	first, duplicate, err := service.Issue(context.Background(), uuid.New(), 3998)
	if err != nil || duplicate {
		t.Fatalf("first issue: duplicate %v, err %v", duplicate, err)
	}
	if want := fmt.Sprintf("%d-000001", year); first.Number != want {
		t.Fatalf("first number: %q", first.Number)
	}
	second, duplicate, err := service.Issue(context.Background(), uuid.New(), 500)
	if err != nil || duplicate {
		t.Fatalf("second issue: duplicate %v, err %v", duplicate, err)
	}
	if want := fmt.Sprintf("%d-000002", year); second.Number != want {
		t.Fatalf("second number: %q", second.Number)
	}
}

func TestIssueReturnsStoredInvoiceForRepeat(t *testing.T) {
	t.Parallel()
	service := NewService(newFakeStore())
	orderID := uuid.New()

	first, duplicate, err := service.Issue(context.Background(), orderID, 3998)
	if err != nil || duplicate {
		t.Fatalf("first issue: duplicate %v, err %v", duplicate, err)
	}
	repeated, duplicate, err := service.Issue(context.Background(), orderID, 3998)
	if err != nil {
		t.Fatal(err)
	}
	if !duplicate {
		t.Fatal("repeat must report a duplicate")
	}
	if repeated.ID != first.ID || repeated.Number != first.Number {
		t.Fatalf("repeat returned %#v", repeated)
	}
}

func TestIssueRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	service := NewService(newFakeStore())

	if _, _, err := service.Issue(context.Background(), uuid.Nil, 100); !errors.Is(err, ErrInvalidIssue) {
		t.Fatalf("nil order: %v", err)
	}
	if _, _, err := service.Issue(context.Background(), uuid.New(), -1); !errors.Is(err, ErrInvalidIssue) {
		t.Fatalf("negative total: %v", err)
	}
	if _, err := service.InvoiceForOrder(context.Background(), uuid.Nil); !errors.Is(err, ErrUnknownInvoice) {
		t.Fatalf("nil lookup: %v", err)
	}
}

func TestEventEndpointIssuesInvoice(t *testing.T) {
	t.Parallel()
	app := newTestApp(newFakeStore())
	orderID := uuid.New()

	response, body := doRequest(t, app, http.MethodPost, "/events",
		fmt.Sprintf(`{"kind":"order.paid","data":{"orderId":%q,"totalMinor":3998}}`, orderID))
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("issue: %d %s", response.StatusCode, body)
	}
	var invoice Invoice
	if err := json.Unmarshal([]byte(body), &invoice); err != nil {
		t.Fatal(err)
	}
	if invoice.OrderID != orderID || invoice.TotalMinor != 3998 || !strings.HasSuffix(invoice.Number, "-000001") {
		t.Fatalf("invoice: %#v", invoice)
	}

	response, body = doRequest(t, app, http.MethodPost, "/events",
		fmt.Sprintf(`{"kind":"order.paid","data":{"orderId":%q,"totalMinor":3998}}`, orderID))
	if response.StatusCode != http.StatusOK {
		t.Fatalf("duplicate issue: %d %s", response.StatusCode, body)
	}
	var repeated Invoice
	if err := json.Unmarshal([]byte(body), &repeated); err != nil {
		t.Fatal(err)
	}
	if repeated.ID != invoice.ID || repeated.Number != invoice.Number {
		t.Fatalf("duplicate invoice: %#v", repeated)
	}
}

func TestEventEndpointRejections(t *testing.T) {
	t.Parallel()
	app := newTestApp(newFakeStore())

	cases := []struct {
		name string
		body string
		want int
	}{
		{"invalid body", `{"kind":`, http.StatusUnprocessableEntity},
		{"other kind", `{"kind":"order.created","data":{"orderId":"` + uuid.New().String() + `","totalMinor":1}}`, http.StatusUnprocessableEntity},
		{"missing data", `{"kind":"order.paid"}`, http.StatusUnprocessableEntity},
		{"invalid order id", `{"kind":"order.paid","data":{"orderId":"not-a-uuid","totalMinor":1}}`, http.StatusUnprocessableEntity},
		{"negative total", `{"kind":"order.paid","data":{"orderId":"` + uuid.New().String() + `","totalMinor":-1}}`, http.StatusUnprocessableEntity},
	}
	for _, testCase := range cases {
		response, body := doRequest(t, app, http.MethodPost, "/events", testCase.body)
		if response.StatusCode != testCase.want {
			t.Fatalf("%s: %d %s", testCase.name, response.StatusCode, body)
		}
	}
}

func TestInvoiceLookup(t *testing.T) {
	t.Parallel()
	app := newTestApp(newFakeStore())
	orderID := uuid.New()

	_, _ = doRequest(t, app, http.MethodPost, "/events",
		fmt.Sprintf(`{"kind":"order.paid","data":{"orderId":%q,"totalMinor":3998}}`, orderID))

	response, body := doRequest(t, app, http.MethodGet, "/invoices/"+orderID.String(), "")
	if response.StatusCode != http.StatusOK || !strings.Contains(body, orderID.String()) {
		t.Fatalf("lookup: %d %s", response.StatusCode, body)
	}
	response, _ = doRequest(t, app, http.MethodGet, "/invoices/"+uuid.New().String(), "")
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("lookup unknown: %d", response.StatusCode)
	}
	response, _ = doRequest(t, app, http.MethodGet, "/invoices/not-a-uuid", "")
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("lookup invalid id: %d", response.StatusCode)
	}
}
