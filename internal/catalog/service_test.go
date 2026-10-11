package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

type fakeStore struct {
	products map[uuid.UUID]Product
}

func newFakeStore() *fakeStore {
	return &fakeStore{products: map[uuid.UUID]Product{}}
}

func (store *fakeStore) Create(_ context.Context, product Product) (Product, error) {
	for _, existing := range store.products {
		if existing.SKU == product.SKU {
			return Product{}, ErrDuplicateSKU
		}
	}
	store.products[product.ID] = product
	return product, nil
}

func (store *fakeStore) Get(_ context.Context, id uuid.UUID) (Product, error) {
	product, ok := store.products[id]
	if !ok {
		return Product{}, ErrUnknownProduct
	}
	return product, nil
}

func (store *fakeStore) List(_ context.Context, limit int, cursor string) ([]Product, error) {
	products := make([]Product, 0, limit)
	for _, product := range store.products {
		if product.Status == StatusActive && product.SKU > cursor {
			products = append(products, product)
		}
	}
	sort.Slice(products, func(i, j int) bool { return products[i].SKU < products[j].SKU })
	if len(products) > limit {
		products = products[:limit]
	}
	return products, nil
}

func (store *fakeStore) Update(_ context.Context, product Product) (Product, error) {
	if _, ok := store.products[product.ID]; !ok {
		return Product{}, ErrUnknownProduct
	}
	store.products[product.ID] = product
	return product, nil
}

func seedProduct(store *fakeStore, sku, status string) Product {
	now := time.Now().UTC()
	product := Product{
		ID:         uuid.New(),
		SKU:        sku,
		Name:       "Product " + sku,
		PriceMinor: 100,
		Status:     status,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	store.products[product.ID] = product
	return product
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

func TestPatchTransitions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		from    string
		to      string
		want    string
		wantErr error
	}{
		{"draft to active", StatusDraft, StatusActive, StatusActive, nil},
		{"active to archived", StatusActive, StatusArchived, StatusArchived, nil},
		{"draft to archived", StatusDraft, StatusArchived, StatusDraft, ErrInvalidTransition},
		{"draft to draft", StatusDraft, StatusDraft, StatusDraft, ErrInvalidTransition},
		{"active to active", StatusActive, StatusActive, StatusActive, ErrInvalidTransition},
		{"active to draft", StatusActive, StatusDraft, StatusActive, ErrInvalidTransition},
		{"archived to active", StatusArchived, StatusActive, StatusArchived, ErrInvalidTransition},
		{"archived to archived", StatusArchived, StatusArchived, StatusArchived, ErrInvalidTransition},
		{"unknown status", StatusDraft, "bogus", StatusDraft, ErrInvalidProduct},
	}
	for _, testCase := range cases {
		store := newFakeStore()
		product := seedProduct(store, "SKU-001", testCase.from)
		service := NewService(store)
		status := testCase.to
		updated, err := service.Patch(context.Background(), product.ID, PatchRequest{Status: &status})
		if !errors.Is(err, testCase.wantErr) {
			t.Fatalf("%s: err %v, want %v", testCase.name, err, testCase.wantErr)
		}
		stored, err := store.Get(context.Background(), product.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status != testCase.want {
			t.Fatalf("%s: status %q, want %q", testCase.name, stored.Status, testCase.want)
		}
		if testCase.wantErr == nil && updated.Status != testCase.want {
			t.Fatalf("%s: updated status %q, want %q", testCase.name, updated.Status, testCase.want)
		}
	}
}

func TestListPagination(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	seedProduct(store, "SKU-002", StatusActive)
	seedProduct(store, "SKU-001", StatusActive)
	seedProduct(store, "SKU-004", StatusActive)
	seedProduct(store, "SKU-003", StatusDraft)
	seedProduct(store, "SKU-005", StatusArchived)
	service := NewService(store)

	page, err := service.List(context.Background(), 2, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || page[0].SKU != "SKU-001" || page[1].SKU != "SKU-002" {
		t.Fatalf("first page: %+v", page)
	}
	page, err = service.List(context.Background(), 2, page[len(page)-1].SKU)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || page[0].SKU != "SKU-004" {
		t.Fatalf("second page: %+v", page)
	}
	page, err = service.List(context.Background(), 2, page[len(page)-1].SKU)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 0 {
		t.Fatalf("third page: %+v", page)
	}
}

func TestListClampsLimit(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	for index := 1; index <= 105; index++ {
		seedProduct(store, fmt.Sprintf("SKU-%03d", index), StatusActive)
	}
	service := NewService(store)

	page, err := service.List(context.Background(), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 {
		t.Fatalf("low clamp: %d", len(page))
	}
	page, err = service.List(context.Background(), 500, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 100 {
		t.Fatalf("high clamp: %d", len(page))
	}
}

func TestCreateProduct(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	app := newTestApp(store)

	response, body := doRequest(t, app, http.MethodPost, "/products",
		`{"sku":"WIDGET-001","name":"Widget","priceMinor":1999}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create: %d %s", response.StatusCode, body)
	}
	var product Product
	if err := json.Unmarshal([]byte(body), &product); err != nil {
		t.Fatal(err)
	}
	if product.Status != StatusDraft || product.SKU != "WIDGET-001" || product.PriceMinor != 1999 {
		t.Fatalf("created product: %+v", product)
	}

	response, body = doRequest(t, app, http.MethodPost, "/products",
		`{"sku":"WIDGET-001","name":"Other","priceMinor":1}`)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate: %d %s", response.StatusCode, body)
	}
}

func TestCreateProductRejections(t *testing.T) {
	t.Parallel()
	app := newTestApp(newFakeStore())

	cases := []struct {
		name string
		body string
		want int
	}{
		{"invalid body", `{"sku":`, http.StatusUnprocessableEntity},
		{"empty sku", `{"sku":"  ","name":"Widget","priceMinor":1}`, http.StatusUnprocessableEntity},
		{"empty name", `{"sku":"WIDGET-001","name":" ","priceMinor":1}`, http.StatusUnprocessableEntity},
		{"negative price", `{"sku":"WIDGET-001","name":"Widget","priceMinor":-1}`, http.StatusUnprocessableEntity},
	}
	for _, testCase := range cases {
		response, body := doRequest(t, app, http.MethodPost, "/products", testCase.body)
		if response.StatusCode != testCase.want {
			t.Fatalf("%s: %d %s", testCase.name, response.StatusCode, body)
		}
	}
}

func TestGetProduct(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	app := newTestApp(store)
	product := seedProduct(store, "SKU-001", StatusArchived)

	response, body := doRequest(t, app, http.MethodGet, "/products/"+product.ID.String(), "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("get: %d %s", response.StatusCode, body)
	}
	response, body = doRequest(t, app, http.MethodGet, "/products/"+uuid.New().String(), "")
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("missing get: %d %s", response.StatusCode, body)
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "application/problem+json" {
		t.Fatalf("problem content type: %q", contentType)
	}
	response, _ = doRequest(t, app, http.MethodGet, "/products/not-a-uuid", "")
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("malformed id: %d", response.StatusCode)
	}
}

func TestListProducts(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	seedProduct(store, "SKU-002", StatusActive)
	seedProduct(store, "SKU-001", StatusActive)
	seedProduct(store, "SKU-003", StatusDraft)
	app := newTestApp(store)

	response, body := doRequest(t, app, http.MethodGet, "/products?limit=1", "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("list: %d %s", response.StatusCode, body)
	}
	var products []Product
	if err := json.Unmarshal([]byte(body), &products); err != nil {
		t.Fatal(err)
	}
	if len(products) != 1 || products[0].SKU != "SKU-001" {
		t.Fatalf("first page: %+v", products)
	}

	response, body = doRequest(t, app, http.MethodGet, "/products?limit=1&cursor=SKU-001", "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("cursor list: %d %s", response.StatusCode, body)
	}
	if err := json.Unmarshal([]byte(body), &products); err != nil {
		t.Fatal(err)
	}
	if len(products) != 1 || products[0].SKU != "SKU-002" {
		t.Fatalf("cursor page: %+v", products)
	}

	response, _ = doRequest(t, app, http.MethodGet, "/products?limit=abc", "")
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("invalid limit: %d", response.StatusCode)
	}
}

func TestPatchProduct(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	app := newTestApp(store)
	product := seedProduct(store, "SKU-001", StatusDraft)

	response, body := doRequest(t, app, http.MethodPatch, "/products/"+product.ID.String(),
		`{"name":"Renamed","priceMinor":2500}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("patch fields: %d %s", response.StatusCode, body)
	}
	var patched Product
	if err := json.Unmarshal([]byte(body), &patched); err != nil {
		t.Fatal(err)
	}
	if patched.Name != "Renamed" || patched.PriceMinor != 2500 || patched.Status != StatusDraft {
		t.Fatalf("patched product: %+v", patched)
	}

	response, body = doRequest(t, app, http.MethodPatch, "/products/"+product.ID.String(), `{"status":"active"}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("patch activate: %d %s", response.StatusCode, body)
	}
	response, body = doRequest(t, app, http.MethodPatch, "/products/"+product.ID.String(), `{"status":"draft"}`)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("patch reactivate: %d %s", response.StatusCode, body)
	}
}

func TestPatchProductRejections(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	app := newTestApp(store)
	product := seedProduct(store, "SKU-001", StatusArchived)

	cases := []struct {
		name string
		path string
		body string
		want int
	}{
		{"malformed id", "/products/not-a-uuid", `{"name":"X"}`, http.StatusUnprocessableEntity},
		{"unknown product", "/products/" + uuid.New().String(), `{"name":"X"}`, http.StatusNotFound},
		{"invalid body", "/products/" + product.ID.String(), `{"name":`, http.StatusUnprocessableEntity},
		{"empty name", "/products/" + product.ID.String(), `{"name":" "}`, http.StatusUnprocessableEntity},
		{"negative price", "/products/" + product.ID.String(), `{"priceMinor":-5}`, http.StatusUnprocessableEntity},
		{"unknown status", "/products/" + product.ID.String(), `{"status":"bogus"}`, http.StatusUnprocessableEntity},
		{"invalid transition", "/products/" + product.ID.String(), `{"status":"active"}`, http.StatusConflict},
	}
	for _, testCase := range cases {
		response, body := doRequest(t, app, http.MethodPatch, testCase.path, testCase.body)
		if response.StatusCode != testCase.want {
			t.Fatalf("%s: %d %s", testCase.name, response.StatusCode, body)
		}
	}
}
