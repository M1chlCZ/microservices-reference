package eventrelay

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/m1chlcz/pgoutbox"
)

func TestDeliverPostsToEveryTarget(t *testing.T) {
	t.Parallel()
	received := make(chan Envelope, 2)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		payload, err := io.ReadAll(request.Body)
		if err != nil {
			t.Error(err)
		}
		var envelope Envelope
		if err = json.Unmarshal(payload, &envelope); err != nil {
			t.Error(err)
		}
		received <- envelope
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	job := pgoutbox.Job{Kind: "order.paid", Payload: []byte(`{"orderId":"abc"}`)}
	if err := deliver(context.Background(), server.Client(), []string{server.URL, server.URL}, Envelope{
		Kind: job.Kind,
		Data: job.Payload,
	}); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		envelope := <-received
		if envelope.Kind != "order.paid" || string(envelope.Data) != `{"orderId":"abc"}` {
			t.Fatalf("envelope: %#v", envelope)
		}
	}
}

func TestDeliverFailsOnErrorStatus(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	err := deliver(context.Background(), server.Client(), []string{server.URL}, Envelope{Kind: "order.paid"})
	if err == nil {
		t.Fatal("error status accepted")
	}
}
