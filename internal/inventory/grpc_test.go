package inventory

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"

	"github.com/google/uuid"
	inventoryv1 "github.com/m1chlcz/microservices-reference/api/inventory/v1"
	"github.com/m1chlcz/microservices-reference/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func startInventoryGRPC(t *testing.T, service *Service) string {
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

func inventoryGRPCClient(t *testing.T, address string) inventoryv1.InventoryClient {
	t.Helper()
	connection, err := platform.Dial(address, platform.TLSFiles{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	return inventoryv1.NewInventoryClient(connection)
}

func TestGRPCReserve(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	client := inventoryGRPCClient(t, startInventoryGRPC(t, NewService(store)))

	response, err := client.Reserve(context.Background(), &inventoryv1.ReserveRequest{
		Items: []*inventoryv1.ReservationItem{{Sku: "WIDGET-001", Quantity: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(response.GetReservationId()); err != nil {
		t.Fatalf("reservation id: %q", response.GetReservationId())
	}
	if store.items["WIDGET-001"] != 3 {
		t.Fatalf("stock after reserve: %d", store.items["WIDGET-001"])
	}
}

func TestGRPCReserveRejections(t *testing.T) {
	t.Parallel()
	client := inventoryGRPCClient(t, startInventoryGRPC(t, NewService(newFakeStore())))

	cases := []struct {
		name string
		item *inventoryv1.ReservationItem
		want codes.Code
	}{
		{"unknown sku", &inventoryv1.ReservationItem{Sku: "MISSING", Quantity: 1}, codes.FailedPrecondition},
		{"insufficient stock", &inventoryv1.ReservationItem{Sku: "WIDGET-001", Quantity: 99}, codes.FailedPrecondition},
		{"invalid item", &inventoryv1.ReservationItem{Sku: "WIDGET-001", Quantity: 0}, codes.InvalidArgument},
	}
	for _, testCase := range cases {
		_, err := client.Reserve(context.Background(), &inventoryv1.ReserveRequest{
			Items: []*inventoryv1.ReservationItem{testCase.item},
		})
		if status.Code(err) != testCase.want {
			t.Fatalf("%s: %v", testCase.name, err)
		}
	}
}

func TestGRPCReleaseRestoresStock(t *testing.T) {
	t.Parallel()
	store := newFakeStore()
	client := inventoryGRPCClient(t, startInventoryGRPC(t, NewService(store)))

	reserved, err := client.Reserve(context.Background(), &inventoryv1.ReserveRequest{
		Items: []*inventoryv1.ReservationItem{{Sku: "WIDGET-001", Quantity: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = client.Release(context.Background(), &inventoryv1.ReleaseRequest{
		ReservationId: reserved.GetReservationId(),
	}); err != nil {
		t.Fatal(err)
	}
	if store.items["WIDGET-001"] != 5 {
		t.Fatalf("stock after release: %d", store.items["WIDGET-001"])
	}
	if _, err = client.Release(context.Background(), &inventoryv1.ReleaseRequest{
		ReservationId: "not-a-uuid",
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("invalid id: %v", err)
	}
}
