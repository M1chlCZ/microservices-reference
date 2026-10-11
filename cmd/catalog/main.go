// Command catalog serves the product catalog over HTTP.
package main

import (
	"fmt"
	"os"

	"github.com/m1chlcz/fiber-problem"
	"github.com/m1chlcz/microservices-reference/internal/catalog"
	"github.com/m1chlcz/microservices-reference/internal/platform"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "catalog:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := platform.SignalContext()
	defer stop()
	logger := fiberproblem.NewJSONLogger(os.Stdout)
	databaseURL, err := platform.RequireEnv("CATALOG_DATABASE_URL")
	if err != nil {
		return err
	}
	pool, err := platform.ConnectDatabase(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	service := catalog.NewService(catalog.NewPostgresStore(pool))
	grpcServer := platform.NewGRPCServer(logger)
	go func() {
		grpcAddr := platform.EnvOr("CATALOG_GRPC_ADDR", ":9085")
		if grpcErr := platform.RunGRPC(ctx, grpcServer, grpcAddr, logger); grpcErr != nil && ctx.Err() == nil {
			logger.Error("grpc server stopped", "error", grpcErr)
			stop()
		}
	}()
	app := platform.NewApp(logger)
	catalog.RegisterRoutes(app, service)
	return platform.Run(ctx, app, platform.EnvOr("CATALOG_ADDR", ":8085"), logger)
}
