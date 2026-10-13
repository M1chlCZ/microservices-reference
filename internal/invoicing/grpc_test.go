package invoicing

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/google/uuid"
	eventsv1 "github.com/m1chlcz/microservices-reference/api/events/v1"
	"github.com/m1chlcz/microservices-reference/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func startInvoiceSink(t *testing.T, service *Service) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server, err := platform.NewGRPCServer(slog.New(slog.NewTextHandler(io.Discard, nil)), platform.TLSFiles{})
	if err != nil {
		t.Fatal(err)
	}
	RegisterGRPC(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	return listener.Addr().String()
}

func sinkClient(t *testing.T, address string) eventsv1.EventSinkClient {
	t.Helper()
	connection, err := platform.Dial(address, platform.TLSFiles{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	return eventsv1.NewEventSinkClient(connection)
}

func paidEvent(orderID uuid.UUID, totalMinor int64) *eventsv1.Event {
	return &eventsv1.Event{
		Kind: eventOrderPaid,
		Data: []byte(fmt.Sprintf(`{"orderId":%q,"totalMinor":%d}`, orderID, totalMinor)),
	}
}

func TestDeliverIssuesInvoice(t *testing.T) {
	t.Parallel()
	service := NewService(newFakeStore())
	client := sinkClient(t, startInvoiceSink(t, service))
	orderID := uuid.New()

	response, err := client.Deliver(context.Background(), paidEvent(orderID, 3998))
	if err != nil {
		t.Fatal(err)
	}
	if response.Duplicate {
		t.Fatal("first delivery must not be a duplicate")
	}
	invoice, err := service.InvoiceForOrder(context.Background(), orderID)
	if err != nil {
		t.Fatal(err)
	}
	if invoice.OrderID != orderID || invoice.TotalMinor != 3998 {
		t.Fatalf("invoice: %#v", invoice)
	}
	if want := fmt.Sprintf("%d-000001", time.Now().UTC().Year()); invoice.Number != want {
		t.Fatalf("first number: %q", invoice.Number)
	}
}

func TestDeliverReturnsDuplicate(t *testing.T) {
	t.Parallel()
	service := NewService(newFakeStore())
	client := sinkClient(t, startInvoiceSink(t, service))
	orderID := uuid.New()

	first, err := client.Deliver(context.Background(), paidEvent(orderID, 3998))
	if err != nil || first.Duplicate {
		t.Fatalf("first delivery: duplicate %v, err %v", first.Duplicate, err)
	}
	issued, err := service.InvoiceForOrder(context.Background(), orderID)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := client.Deliver(context.Background(), paidEvent(orderID, 3998))
	if err != nil {
		t.Fatal(err)
	}
	if !repeated.Duplicate {
		t.Fatal("repeat must report a duplicate")
	}
	lookedUp, err := service.InvoiceForOrder(context.Background(), orderID)
	if err != nil {
		t.Fatal(err)
	}
	if lookedUp.Number != issued.Number {
		t.Fatalf("duplicate number: %q", lookedUp.Number)
	}
}

func TestDeliverRejectsInvalidEvents(t *testing.T) {
	t.Parallel()
	client := sinkClient(t, startInvoiceSink(t, NewService(newFakeStore())))

	cases := []struct {
		name  string
		event *eventsv1.Event
	}{
		{"other kind", &eventsv1.Event{
			Kind: "order.created",
			Data: []byte(fmt.Sprintf(`{"orderId":%q,"totalMinor":1}`, uuid.New())),
		}},
		{"malformed data", &eventsv1.Event{Kind: eventOrderPaid, Data: []byte(`{"orderId":`)}},
		{"missing data", &eventsv1.Event{Kind: eventOrderPaid}},
		{"invalid order id", &eventsv1.Event{Kind: eventOrderPaid, Data: []byte(`{"orderId":"not-a-uuid","totalMinor":1}`)}},
		{"negative total", &eventsv1.Event{
			Kind: eventOrderPaid,
			Data: []byte(fmt.Sprintf(`{"orderId":%q,"totalMinor":-1}`, uuid.New())),
		}},
	}
	for _, testCase := range cases {
		_, err := client.Deliver(context.Background(), testCase.event)
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("%s: %v", testCase.name, err)
		}
	}
}
