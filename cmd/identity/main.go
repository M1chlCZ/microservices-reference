// Command identity serves accounts, sessions and TOTP over HTTP.
package main

import (
	"fmt"
	"os"

	"github.com/m1chlcz/fiber-problem"
	"github.com/m1chlcz/microservices-reference/internal/identity"
	"github.com/m1chlcz/microservices-reference/internal/platform"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "identity:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := platform.SignalContext()
	defer stop()
	logger := fiberproblem.NewJSONLogger(os.Stdout)
	databaseURL, err := platform.RequireEnv("IDENTITY_DATABASE_URL")
	if err != nil {
		return err
	}
	pool, err := platform.ConnectDatabase(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	service := identity.NewService(identity.NewPostgresStore(pool))
	grpcServer := platform.NewGRPCServer(logger)
	go func() {
		grpcAddr := platform.EnvOr("IDENTITY_GRPC_ADDR", ":9084")
		if grpcErr := platform.RunGRPC(ctx, grpcServer, grpcAddr, logger); grpcErr != nil && ctx.Err() == nil {
			logger.Error("grpc server stopped", "error", grpcErr)
			stop()
		}
	}()
	app := platform.NewApp(logger)
	identity.RegisterRoutes(app, service)
	return platform.Run(ctx, app, platform.EnvOr("IDENTITY_ADDR", ":8084"), logger)
}
