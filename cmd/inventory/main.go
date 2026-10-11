// Command inventory serves stock positions and reservations over HTTP and gRPC.
package main

import (
	"fmt"
	"os"

	"github.com/m1chlcz/fiber-problem"
	"github.com/m1chlcz/microservices-reference/internal/inventory"
	"github.com/m1chlcz/microservices-reference/internal/platform"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "inventory:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := platform.SignalContext()
	defer stop()
	logger := fiberproblem.NewJSONLogger(os.Stdout)
	databaseURL, err := platform.RequireEnv("INVENTORY_DATABASE_URL")
	if err != nil {
		return err
	}
	pool, err := platform.ConnectDatabase(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	app := platform.NewApp(logger)
	service := inventory.NewService(inventory.NewPostgresStore(pool))
	inventory.RegisterRoutes(app, service)
	grpcServer := platform.NewGRPCServer(logger)
	inventory.RegisterGRPC(grpcServer, service)
	go func() {
		if grpcErr := platform.RunGRPC(ctx, grpcServer, platform.EnvOr("INVENTORY_GRPC_ADDR", ":9082"), logger); grpcErr != nil && ctx.Err() == nil {
			logger.Error("grpc server stopped", "error", grpcErr)
			stop()
		}
	}()
	return platform.Run(ctx, app, platform.EnvOr("INVENTORY_ADDR", ":8082"), logger)
}
