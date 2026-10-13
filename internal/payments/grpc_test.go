package payments

import (
	"context"
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"

	"github.com/google/uuid"
	paymentsv1 "github.com/m1chlcz/microservices-reference/api/payments/v1"
	"github.com/m1chlcz/microservices-reference/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func startPaymentsGRPC(t *testing.T, service *Service) string {
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

func paymentsClientFor(t *testing.T, address string) paymentsv1.PaymentsClient {
	t.Helper()
	connection, err := platform.Dial(address, platform.TLSFiles{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	return paymentsv1.NewPaymentsClient(connection)
}

func TestGRPCCreateIntent(t *testing.T) {
	t.Parallel()
	service := NewService(newFakeStore())
	client := paymentsClientFor(t, startPaymentsGRPC(t, service))
	orderID := uuid.New()

	response, err := client.CreateIntent(context.Background(), &paymentsv1.CreateIntentRequest{
		OrderId:        orderID.String(),
		AmountMinor:    1500,
		Currency:       "CZK",
		IdempotencyKey: "key-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetDuplicate() {
		t.Fatalf("duplicate: %#v", response)
	}
	if !strings.HasPrefix(response.GetProviderRef(), "pay_") {
		t.Fatalf("provider reference: %q", response.GetProviderRef())
	}
	intentID, err := uuid.Parse(response.GetIntentId())
	if err != nil {
		t.Fatal(err)
	}
	intent, err := service.Intent(context.Background(), intentID)
	if err != nil {
		t.Fatal(err)
	}
	if intent.OrderID != orderID || intent.AmountMinor != 1500 || intent.Currency != "CZK" {
		t.Fatalf("intent: %#v", intent)
	}
}

func TestGRPCCreateIntentIdempotent(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	client := paymentsClientFor(t, startPaymentsGRPC(t, NewService(store)))
	request := &paymentsv1.CreateIntentRequest{
		OrderId:        uuid.New().String(),
		AmountMinor:    1500,
		Currency:       "CZK",
		IdempotencyKey: "key-1",
	}

	first, err := client.CreateIntent(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.CreateIntent(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !second.GetDuplicate() || second.GetIntentId() != first.GetIntentId() || len(store.byID) != 1 {
		t.Fatalf("duplicate: %#v %#v", first, second)
	}
}

func TestGRPCCreateIntentConflict(t *testing.T) {
	t.Parallel()
	client := paymentsClientFor(t, startPaymentsGRPC(t, NewService(newFakeStore())))
	orderID := uuid.New().String()

	if _, err := client.CreateIntent(context.Background(), &paymentsv1.CreateIntentRequest{
		OrderId:        orderID,
		AmountMinor:    1500,
		Currency:       "CZK",
		IdempotencyKey: "key-1",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateIntent(context.Background(), &paymentsv1.CreateIntentRequest{
		OrderId:        orderID,
		AmountMinor:    2500,
		Currency:       "CZK",
		IdempotencyKey: "key-1",
	}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("conflict: %v", err)
	}
}

func TestGRPCCreateIntentInvalidOrderID(t *testing.T) {
	t.Parallel()
	client := paymentsClientFor(t, startPaymentsGRPC(t, NewService(newFakeStore())))

	if _, err := client.CreateIntent(context.Background(), &paymentsv1.CreateIntentRequest{
		OrderId:        "not-a-uuid",
		AmountMinor:    1500,
		Currency:       "CZK",
		IdempotencyKey: "key-1",
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("invalid order id: %v", err)
	}
}
