// Command shipping creates one shipment per paid order and relays shipment
// events to the notifications service.
package main

import (
	"fmt"
	"os"

	"github.com/m1chlcz/fiber-problem"
	"github.com/m1chlcz/microservices-reference/internal/eventrelay"
	"github.com/m1chlcz/microservices-reference/internal/platform"
	"github.com/m1chlcz/microservices-reference/internal/shipping"
	"github.com/m1chlcz/pgoutbox"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "shipping:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := platform.SignalContext()
	defer stop()
	logger := fiberproblem.NewJSONLogger(os.Stdout)
	databaseURL, err := platform.RequireEnv("SHIPPING_DATABASE_URL")
	if err != nil {
		return err
	}
	notificationsTarget, err := platform.RequireEnv("NOTIFICATIONS_GRPC")
	if err != nil {
		return err
	}
	carrier := platform.EnvOr("SHIPPING_CARRIER", "generic")
	pool, err := platform.ConnectDatabase(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	queue, err := pgoutbox.New(pool)
	if err != nil {
		return err
	}
	relay, err := eventrelay.NewWorker(queue, platform.WorkerID("shipping"), map[string][]string{
		"shipment.created": {notificationsTarget},
	})
	if err != nil {
		return err
	}
	go func() {
		if relayErr := relay.Run(ctx); relayErr != nil && ctx.Err() == nil {
			logger.Error("relay stopped", "error", relayErr)
			stop()
		}
	}()

	service := shipping.NewService(shipping.NewPostgresStore(pool), carrier)
	grpcServer := platform.NewGRPCServer(logger)
	shipping.RegisterGRPC(grpcServer, service)
	go func() {
		addr := platform.EnvOr("SHIPPING_GRPC_ADDR", ":9087")
		if grpcErr := platform.RunGRPC(ctx, grpcServer, addr, logger); grpcErr != nil && ctx.Err() == nil {
			logger.Error("grpc stopped", "error", grpcErr)
			stop()
		}
	}()

	app := platform.NewApp(logger)
	shipping.RegisterRoutes(app, service)
	return platform.Run(ctx, app, platform.EnvOr("SHIPPING_ADDR", ":8087"), logger)
}
