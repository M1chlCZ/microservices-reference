// Command notifications records events that other services deliver.
package main

import (
	"fmt"
	"os"

	"github.com/m1chlcz/fiber-problem"
	"github.com/m1chlcz/microservices-reference/internal/notifications"
	"github.com/m1chlcz/microservices-reference/internal/platform"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "notifications:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := platform.SignalContext()
	defer stop()
	logger := fiberproblem.NewJSONLogger(os.Stdout)
	shutdown, err := platform.SetupTracing(ctx, "notifications")
	if err != nil {
		return err
	}
	defer shutdown()
	serverTLS, err := platform.ServerTLSFromEnv()
	if err != nil {
		return err
	}
	databaseURL, err := platform.RequireEnv("NOTIFICATIONS_DATABASE_URL")
	if err != nil {
		return err
	}
	pool, err := platform.ConnectDatabase(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	service := notifications.NewService(notifications.NewPostgresStore(pool))

	server, err := platform.NewGRPCServer(logger, serverTLS)
	if err != nil {
		return err
	}
	notifications.RegisterGRPC(server, service)
	go func() {
		grpcErr := platform.RunGRPC(ctx, server, platform.EnvOr("NOTIFICATIONS_GRPC_ADDR", ":9083"), logger)
		if grpcErr != nil && ctx.Err() == nil {
			logger.Error("grpc stopped", "error", grpcErr)
			stop()
		}
	}()

	app := platform.NewApp(logger)
	notifications.RegisterRoutes(app, service)
	return platform.Run(ctx, app, platform.EnvOr("NOTIFICATIONS_ADDR", ":8083"), logger)
}
