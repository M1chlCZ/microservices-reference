package notifications

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

type fakeStore struct {
	notifications []Notification
}

func (store *fakeStore) Insert(_ context.Context, kind string, payload []byte) (Notification, error) {
	notification := Notification{
		ID:      int64(len(store.notifications) + 1),
		Kind:    kind,
		Payload: json.RawMessage(payload),
	}
	store.notifications = append(store.notifications, notification)
	return notification, nil
}

func (store *fakeStore) List(_ context.Context, limit int) ([]Notification, error) {
	if limit > len(store.notifications) {
		limit = len(store.notifications)
	}
	recent := make([]Notification, 0, limit)
	for index := len(store.notifications) - 1; index >= 0 && len(recent) < limit; index-- {
		recent = append(recent, store.notifications[index])
	}
	return recent, nil
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

func TestReceiveEvent(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	app := newTestApp(store)

	response, body := doRequest(t, app, http.MethodPost, "/events",
		`{"kind":"order.created","data":{"orderId":"abc","totalMinor":3998}}`)
	if response.StatusCode != http.StatusCreated || !strings.Contains(body, `"kind":"order.created"`) {
		t.Fatalf("receive: %d %s", response.StatusCode, body)
	}
	if len(store.notifications) != 1 {
		t.Fatalf("stored notifications: %d", len(store.notifications))
	}
}

func TestReceiveEventRejections(t *testing.T) {
	t.Parallel()
	app := newTestApp(&fakeStore{})

	cases := []struct {
		name string
		body string
		want int
	}{
		{"invalid body", `{"kind":`, http.StatusUnprocessableEntity},
		{"missing kind", `{"data":{"a":1}}`, http.StatusUnprocessableEntity},
		{"missing data", `{"kind":"order.created"}`, http.StatusUnprocessableEntity},
		{"data not an object", `{"kind":"order.created","data":[1,2]}`, http.StatusUnprocessableEntity},
	}
	for _, testCase := range cases {
		response, body := doRequest(t, app, http.MethodPost, "/events", testCase.body)
		if response.StatusCode != testCase.want {
			t.Fatalf("%s: %d %s", testCase.name, response.StatusCode, body)
		}
	}
}

func TestListNotifications(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	app := newTestApp(store)
	for _, kind := range []string{"order.created", "order.created"} {
		doRequest(t, app, http.MethodPost, "/events", `{"kind":"`+kind+`","data":{"a":1}}`)
	}

	response, body := doRequest(t, app, http.MethodGet, "/notifications?limit=1", "")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("list: %d %s", response.StatusCode, body)
	}
	var listed []Notification
	if err := json.Unmarshal([]byte(body), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 {
		t.Fatalf("listed: %d", len(listed))
	}
	response, _ = doRequest(t, app, http.MethodGet, "/notifications?limit=abc", "")
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("invalid limit: %d", response.StatusCode)
	}
}
