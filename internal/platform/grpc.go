package platform

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

const grpcShutdownTimeout = 10 * time.Second

// NewGRPCServer returns a gRPC server with the health service, reflection,
// OpenTelemetry tracing and a logging interceptor. When tlsFiles names a
// server certificate, the server serves TLS and requires a client certificate
// signed by the configured CA.
func NewGRPCServer(logger *slog.Logger, tlsFiles TLSFiles) (*grpc.Server, error) {
	transportCredentials, err := tlsFiles.serverCredentials()
	if err != nil {
		return nil, err
	}
	options := []grpc.ServerOption{
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.UnaryInterceptor(loggingInterceptor(logger)),
	}
	if transportCredentials != nil {
		options = append(options, grpc.Creds(transportCredentials))
	}
	server := grpc.NewServer(options...)
	healthServer := health.NewServer()
	healthServer.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(server, healthServer)
	reflection.Register(server)
	return server, nil
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

// Dial returns a client connection to an internal gRPC target. The connection
// carries OpenTelemetry tracing. When tlsFiles names a client certificate, the
// client verifies the server against the configured CA and presents its own
// certificate.
func Dial(target string, tlsFiles TLSFiles) (*grpc.ClientConn, error) {
	transportCredentials, err := tlsFiles.clientCredentials()
	if err != nil {
		return nil, err
	}
	if transportCredentials == nil {
		transportCredentials = insecure.NewCredentials()
	}
	return grpc.NewClient(target,
		grpc.WithTransportCredentials(transportCredentials),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
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
