// Command invoicing issues invoices and relays outbox events to the
// notifications service.
package main

import (
	"fmt"
	"os"

	"github.com/m1chlcz/fiber-problem"
	"github.com/m1chlcz/microservices-reference/internal/eventrelay"
	"github.com/m1chlcz/microservices-reference/internal/invoicing"
	"github.com/m1chlcz/microservices-reference/internal/platform"
	"github.com/m1chlcz/pgoutbox"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "invoicing:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := platform.SignalContext()
	defer stop()
	logger := fiberproblem.NewJSONLogger(os.Stdout)
	databaseURL, err := platform.RequireEnv("INVOICING_DATABASE_URL")
	if err != nil {
		return err
	}
	notificationsTarget, err := platform.RequireEnv("NOTIFICATIONS_GRPC")
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
	relay, err := eventrelay.NewWorker(queue, platform.WorkerID("invoicing"), map[string][]string{
		invoicing.EventInvoiceIssued: {notificationsTarget},
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

	service := invoicing.NewService(invoicing.NewPostgresStore(pool))
	grpcServer := platform.NewGRPCServer(logger)
	invoicing.RegisterGRPC(grpcServer, service)
	go func() {
		if grpcErr := platform.RunGRPC(ctx, grpcServer, platform.EnvOr("INVOICING_GRPC_ADDR", ":9088"), logger); grpcErr != nil && ctx.Err() == nil {
			logger.Error("grpc stopped", "error", grpcErr)
			stop()
		}
	}()

	app := platform.NewApp(logger)
	invoicing.RegisterRoutes(app, service)
	return platform.Run(ctx, app, platform.EnvOr("INVOICING_ADDR", ":8088"), logger)
}
