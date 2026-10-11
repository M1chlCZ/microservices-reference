package orders

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"

	"github.com/google/uuid"
	eventsv1 "github.com/m1chlcz/microservices-reference/api/events/v1"
	"github.com/m1chlcz/microservices-reference/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func startOrdersGRPC(t *testing.T, service *Service) string {
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

func eventSinkClient(t *testing.T, address string) eventsv1.EventSinkClient {
	t.Helper()
	connection, err := platform.Dial(address)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	return eventsv1.NewEventSinkClient(connection)
}

func TestEventSinkMarksOrderPaid(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	order := Order{ID: uuid.New(), CustomerRef: "demo", Status: statusPendingPayment}
	store.orders[order.ID] = order
	client := eventSinkClient(t, startOrdersGRPC(t,
		NewService(store, &fakeInventory{}, &fakePayments{})))

	deliver := func() (*eventsv1.DeliverResponse, error) {
		return client.Deliver(context.Background(), &eventsv1.Event{
			Kind: eventPaymentCaptured,
			Data: []byte(`{"orderId":"` + order.ID.String() + `"}`),
		})
	}
	response, err := deliver()
	if err != nil || response.GetDuplicate() {
		t.Fatalf("first delivery: %v %v", err, response)
	}
	if store.orders[order.ID].Status != statusPaid {
		t.Fatalf("status after delivery: %s", store.orders[order.ID].Status)
	}
	response, err = deliver()
	if err != nil || !response.GetDuplicate() {
		t.Fatalf("second delivery: %v %v", err, response)
	}
}

func TestEventSinkRejections(t *testing.T) {
	t.Parallel()
	client := eventSinkClient(t, startOrdersGRPC(t,
		NewService(newFakeStore(), &fakeInventory{}, &fakePayments{})))

	cases := []struct {
		name string
		kind string
		data string
		want codes.Code
	}{
		{"other kind", "order.created", `{"orderId":"x"}`, codes.InvalidArgument},
		{"malformed data", eventPaymentCaptured, `{"orderId":`, codes.InvalidArgument},
		{"bad order id", eventPaymentCaptured, `{"orderId":"nope"}`, codes.InvalidArgument},
		{"unknown order", eventPaymentCaptured, `{"orderId":"` + uuid.New().String() + `"}`, codes.NotFound},
	}
	for _, testCase := range cases {
		_, err := client.Deliver(context.Background(), &eventsv1.Event{
			Kind: testCase.kind,
			Data: []byte(testCase.data),
		})
		if status.Code(err) != testCase.want {
			t.Fatalf("%s: %v", testCase.name, err)
		}
	}
}
