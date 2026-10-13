package platform

import (
	"context"
	"io"
	"log/slog"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"
)

func TestGRPCServerServesHealth(t *testing.T) {
	t.Parallel()
	listener := bufconn.Listen(1024 * 1024)
	server, err := NewGRPCServer(slog.New(slog.NewTextHandler(io.Discard, nil)), TLSFiles{})
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	connection, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	response, err := healthpb.NewHealthClient(connection).Check(
		context.Background(),
		&healthpb.HealthCheckRequest{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != healthpb.HealthCheckResponse_SERVING {
		t.Fatalf("health status: %v", response.Status)
	}
}
