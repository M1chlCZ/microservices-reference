// Command orders serves the order API and relays outbox events to the
// notifications service.
package main

import (
	"fmt"
	"os"

	"github.com/m1chlcz/fiber-problem"
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
	inventoryURL, err := platform.RequireEnv("INVENTORY_URL")
	if err != nil {
		return err
	}
	notificationsURL, err := platform.RequireEnv("NOTIFICATIONS_URL")
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
	relay, err := orders.NewRelay(queue, notificationsURL, workerID())
	if err != nil {
		return err
	}
	go func() {
		if relayErr := relay.Run(ctx); relayErr != nil && ctx.Err() == nil {
			logger.Error("relay stopped", "error", relayErr)
			stop()
		}
	}()

	service := orders.NewService(orders.NewPostgresStore(pool), orders.NewHTTPInventoryClient(inventoryURL))
	app := platform.NewApp(logger)
	orders.RegisterRoutes(app, service)
	return platform.Run(ctx, app, platform.EnvOr("ORDERS_ADDR", ":8081"), logger)
}

func workerID() string {
	host, err := os.Hostname()
	if err != nil {
		host = "orders"
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}
