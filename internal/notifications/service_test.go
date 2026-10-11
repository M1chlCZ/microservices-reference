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

func TestListNotifications(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	service := NewService(store)
	for _, kind := range []string{"order.created", "order.created"} {
		if _, err := service.Receive(context.Background(), kind, []byte(`{"a":1}`)); err != nil {
			t.Fatal(err)
		}
	}
	app := fiber.New()
	RegisterRoutes(app, service)

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
