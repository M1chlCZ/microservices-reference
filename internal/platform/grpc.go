package platform

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

const grpcShutdownTimeout = 10 * time.Second

// NewGRPCServer returns a gRPC server with the health service, reflection
// and a logging interceptor.
func NewGRPCServer(logger *slog.Logger) *grpc.Server {
	server := grpc.NewServer(grpc.UnaryInterceptor(loggingInterceptor(logger)))
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(server, healthServer)
	reflection.Register(server)
	return server
}

// RunGRPC serves the server until the context is canceled, then stops it.
func RunGRPC(ctx context.Context, server *grpc.Server, addr string, logger *slog.Logger) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	logger.Info("grpc listening", "addr", addr)
	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}
	stopped := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(grpcShutdownTimeout):
		server.Stop()
	}
	return nil
}

// Dial returns a client connection to an internal gRPC target. The local
// stack uses insecure credentials; TLS is a deployment concern.
func Dial(target string) (*grpc.ClientConn, error) {
	return grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

func loggingInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		request any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		start := time.Now()
		response, err := handler(ctx, request)
		logger.Info(
			"grpc_request",
			"method", info.FullMethod,
			"duration_ms", time.Since(start).Milliseconds(),
			"error", err,
		)
		return response, err
	}
}
