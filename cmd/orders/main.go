// Command orders serves the order API over HTTP and gRPC and relays outbox
// events to the notifications, shipping and invoicing services.
package main

import (
	"fmt"
	"os"

	"github.com/m1chlcz/fiber-problem"
	"github.com/m1chlcz/microservices-reference/internal/eventrelay"
	"github.com/m1chlcz/microservices-reference/internal/orders"
	"github.com/m1chlcz/microservices-reference/internal/platform"
	"github.com/m1chlcz/pgoutbox"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "orders:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := platform.SignalContext()
	defer stop()
	logger := fiberproblem.NewJSONLogger(os.Stdout)
	databaseURL, err := platform.RequireEnv("ORDERS_DATABASE_URL")
	if err != nil {
		return err
	}
	inventoryTarget, err := platform.RequireEnv("INVENTORY_GRPC")
	if err != nil {
		return err
	}
	paymentsTarget, err := platform.RequireEnv("PAYMENTS_GRPC")
	if err != nil {
		return err
	}
	notificationsTarget, err := platform.RequireEnv("NOTIFICATIONS_GRPC")
	if err != nil {
		return err
	}
	shippingTarget, err := platform.RequireEnv("SHIPPING_GRPC")
	if err != nil {
		return err
	}
	invoicingTarget, err := platform.RequireEnv("INVOICING_GRPC")
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
	relay, err := eventrelay.NewWorker(queue, platform.WorkerID("orders"),
		orders.RelayRoutes(notificationsTarget, shippingTarget, invoicingTarget))
	if err != nil {
		return err
	}
	go func() {
		if relayErr := relay.Run(ctx); relayErr != nil && ctx.Err() == nil {
			logger.Error("relay stopped", "error", relayErr)
			stop()
		}
	}()

	inventoryConnection, err := platform.Dial(inventoryTarget)
	if err != nil {
		return err
	}
	defer func() { _ = inventoryConnection.Close() }()
	paymentsConnection, err := platform.Dial(paymentsTarget)
	if err != nil {
		return err
	}
	defer func() { _ = paymentsConnection.Close() }()

	service := orders.NewService(
		orders.NewPostgresStore(pool),
		orders.NewGRPCInventoryClient(inventoryConnection),
		orders.NewGRPCPaymentsClient(paymentsConnection),
	)
	grpcServer := platform.NewGRPCServer(logger)
	orders.RegisterGRPC(grpcServer, service)
	go func() {
		grpcAddr := platform.EnvOr("ORDERS_GRPC_ADDR", ":9081")
		if grpcErr := platform.RunGRPC(ctx, grpcServer, grpcAddr, logger); grpcErr != nil && ctx.Err() == nil {
			logger.Error("grpc server stopped", "error", grpcErr)
			stop()
		}
	}()

	app := platform.NewApp(logger)
	orders.RegisterRoutes(app, service)
	return platform.Run(ctx, app, platform.EnvOr("ORDERS_ADDR", ":8081"), logger)
}
