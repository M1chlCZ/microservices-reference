package eventrelay

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

type recordingSink struct {
	eventsv1.UnimplementedEventSinkServer
	received chan *eventsv1.Event
	fail     bool
}

func (sink *recordingSink) Deliver(
	_ context.Context,
	event *eventsv1.Event,
) (*eventsv1.DeliverResponse, error) {
	if sink.fail {
		return nil, status.Error(codes.Unavailable, "sink is down")
	}
	sink.received <- event
	return &eventsv1.DeliverResponse{}, nil
}

func startSink(t *testing.T, sink *recordingSink) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server, err := platform.NewGRPCServer(slog.New(slog.NewTextHandler(io.Discard, nil)), platform.TLSFiles{})
	if err != nil {
		t.Fatal(err)
	}
	eventsv1.RegisterEventSinkServer(server, sink)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return listener.Addr().String()
}

func clientFor(t *testing.T, address string) eventsv1.EventSinkClient {
	t.Helper()
	connection, err := platform.Dial(address, platform.TLSFiles{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	return eventsv1.NewEventSinkClient(connection)
}

func TestDeliverPostsToEveryTarget(t *testing.T) {
	t.Parallel()
	sink := &recordingSink{received: make(chan *eventsv1.Event, 2)}
	address := startSink(t, sink)
	clients := map[string]eventsv1.EventSinkClient{address: clientFor(t, address)}

	err := deliver(context.Background(), clients, []string{address, address},
		"order.paid", []byte(`{"orderId":"abc"}`))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		event := <-sink.received
		if event.Kind != "order.paid" || string(event.Data) != `{"orderId":"abc"}` {
			t.Fatalf("event: %#v", event)
		}
	}
}

func TestDeliverFailsOnSinkError(t *testing.T) {
	t.Parallel()
	sink := &recordingSink{received: make(chan *eventsv1.Event, 1), fail: true}
	address := startSink(t, sink)
	clients := map[string]eventsv1.EventSinkClient{address: clientFor(t, address)}

	err := deliver(context.Background(), clients, []string{address}, "order.paid", []byte(`{}`))
	if err == nil {
		t.Fatal("sink error accepted")
	}
}
