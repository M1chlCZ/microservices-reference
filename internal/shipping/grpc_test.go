package shipping

import (
	"context"
	"io"
	"log/slog"
	"net"
	"regexp"
	"testing"

	"github.com/google/uuid"
	eventsv1 "github.com/m1chlcz/microservices-reference/api/events/v1"
	"github.com/m1chlcz/microservices-reference/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var trackingCodePattern = regexp.MustCompile(`^TRK-[0-9A-F]{10}$`)

func startShippingServer(t *testing.T, service *Service) string {
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

func eventSinkClientFor(t *testing.T, address string) eventsv1.EventSinkClient {
	t.Helper()
	connection, err := platform.Dial(address, platform.TLSFiles{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	return eventsv1.NewEventSinkClient(connection)
}

func paidEvent(orderID uuid.UUID) *eventsv1.Event {
	return &eventsv1.Event{
		Kind: "order.paid",
		Data: []byte(`{"orderId":"` + orderID.String() + `"}`),
	}
}

func TestDeliverCreatesShipment(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	service := NewService(store, "generic")
	client := eventSinkClientFor(t, startShippingServer(t, service))
	orderID := uuid.New()

	response, err := client.Deliver(context.Background(), paidEvent(orderID))
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if response.GetDuplicate() {
		t.Fatal("first delivery reported a duplicate")
	}
	shipment, err := service.ShipmentForOrder(context.Background(), orderID)
	if err != nil {
		t.Fatal(err)
	}
	if shipment.OrderID != orderID || shipment.Carrier != "generic" || shipment.Status != "created" {
		t.Fatalf("shipment: %#v", shipment)
	}
	if !trackingCodePattern.MatchString(shipment.TrackingCode) {
		t.Fatalf("tracking code: %q", shipment.TrackingCode)
	}

	response, err = client.Deliver(context.Background(), paidEvent(orderID))
	if err != nil {
		t.Fatalf("duplicate deliver: %v", err)
	}
	if !response.GetDuplicate() {
		t.Fatal("repeated delivery did not report a duplicate")
	}
	if count := len(store.byOrder); count != 1 {
		t.Fatalf("shipments: %d", count)
	}
}

func TestDeliverRejections(t *testing.T) {
	t.Parallel()
	client := eventSinkClientFor(t, startShippingServer(t, NewService(newFakeStore(), "generic")))

	cases := []struct {
		name  string
		event *eventsv1.Event
	}{
		{"other kind", &eventsv1.Event{Kind: "order.created", Data: []byte(`{"orderId":"` + uuid.New().String() + `"}`)}},
		{"missing data", &eventsv1.Event{Kind: "order.paid"}},
		{"data not an object", &eventsv1.Event{Kind: "order.paid", Data: []byte(`[1,2]`)}},
		{"missing order id", &eventsv1.Event{Kind: "order.paid", Data: []byte(`{}`)}},
		{"invalid order id", &eventsv1.Event{Kind: "order.paid", Data: []byte(`{"orderId":"abc"}`)}},
		{"malformed data", &eventsv1.Event{Kind: "order.paid", Data: []byte(`{"orderId":`)}},
	}
	for _, testCase := range cases {
		_, err := client.Deliver(context.Background(), testCase.event)
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("%s: %v", testCase.name, err)
		}
	}
}
