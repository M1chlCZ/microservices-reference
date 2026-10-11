package notifications

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"

	eventsv1 "github.com/m1chlcz/microservices-reference/api/events/v1"
	"github.com/m1chlcz/microservices-reference/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func startSink(t *testing.T, service *Service) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := platform.NewGRPCServer(slog.New(slog.NewTextHandler(io.Discard, nil)))
	RegisterGRPC(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return listener.Addr().String()
}

func clientFor(t *testing.T, address string) eventsv1.EventSinkClient {
	t.Helper()
	connection, err := platform.Dial(address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	return eventsv1.NewEventSinkClient(connection)
}

// TestDeliverStoresEvent checks that a valid event is stored once.
func TestDeliverStoresEvent(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	client := clientFor(t, startSink(t, NewService(store)))

	response, err := client.Deliver(context.Background(), &eventsv1.Event{
		Kind: "order.created",
		Data: []byte(`{"orderId":"abc","totalMinor":3998}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetDuplicate() {
		t.Fatal("event reported as duplicate")
	}
	if len(store.notifications) != 1 || store.notifications[0].Kind != "order.created" {
		t.Fatalf("stored notifications: %#v", store.notifications)
	}
}

// TestDeliverRejections checks that malformed events return InvalidArgument.
func TestDeliverRejections(t *testing.T) {
	t.Parallel()
	store := &fakeStore{}
	client := clientFor(t, startSink(t, NewService(store)))

	cases := []struct {
		name  string
		event *eventsv1.Event
	}{
		{"missing kind", &eventsv1.Event{Data: []byte(`{"a":1}`)}},
		{"missing data", &eventsv1.Event{Kind: "order.created"}},
		{"data not an object", &eventsv1.Event{Kind: "order.created", Data: []byte(`[1,2]`)}},
	}
	for _, testCase := range cases {
		_, err := client.Deliver(context.Background(), testCase.event)
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("%s: %v", testCase.name, err)
		}
	}
	if len(store.notifications) != 0 {
		t.Fatalf("stored notifications: %d", len(store.notifications))
	}
}
