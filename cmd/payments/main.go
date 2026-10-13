// Command payments serves the payment API and relays captured payment events
// to the orders service.
package main

import (
	"fmt"
	"os"

	"github.com/m1chlcz/fiber-problem"
	"github.com/m1chlcz/microservices-reference/internal/eventrelay"
	"github.com/m1chlcz/microservices-reference/internal/payments"
	"github.com/m1chlcz/microservices-reference/internal/platform"
	"github.com/m1chlcz/pgoutbox"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "payments:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := platform.SignalContext()
	defer stop()
	logger := fiberproblem.NewJSONLogger(os.Stdout)
	shutdown, err := platform.SetupTracing(ctx, "payments")
	if err != nil {
		return err
	}
	defer shutdown()
	serverTLS, err := platform.ServerTLSFromEnv()
	if err != nil {
		return err
	}
	clientTLS, err := platform.ClientTLSFromEnv()
	if err != nil {
		return err
	}
	databaseURL, err := platform.RequireEnv("PAYMENTS_DATABASE_URL")
	if err != nil {
		return err
	}
	ordersTarget, err := platform.RequireEnv("ORDERS_GRPC")
	if err != nil {
		return err
	}
	pool, err := platform.ConnectDatabase(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	queue, err := pgoutbox.New(pool)
	if err != nil {
		return err
	}
	relay, err := eventrelay.NewWorker(queue, platform.WorkerID("payments"), map[string][]string{
		"payment.captured": {ordersTarget},
	}, clientTLS)
	if err != nil {
		return err
	}
	go func() {
		if relayErr := relay.Run(ctx); relayErr != nil && ctx.Err() == nil {
			logger.Error("relay stopped", "error", relayErr)
			stop()
		}
	}()

	service := payments.NewService(payments.NewPostgresStore(pool))
	grpcServer, err := platform.NewGRPCServer(logger, serverTLS)
	if err != nil {
		return err
	}
	payments.RegisterGRPC(grpcServer, service)
	go func() {
		if grpcErr := platform.RunGRPC(ctx, grpcServer, platform.EnvOr("PAYMENTS_GRPC_ADDR", ":9086"), logger); grpcErr != nil && ctx.Err() == nil {
			logger.Error("grpc server stopped", "error", grpcErr)
			stop()
		}
	}()

	app := platform.NewApp(logger)
	payments.RegisterRoutes(app, service)
	return platform.Run(ctx, app, platform.EnvOr("PAYMENTS_ADDR", ":8086"), logger)
}
