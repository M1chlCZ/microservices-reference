// Command invoicing issues invoices and relays outbox events to the
// notifications service.
package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/m1chlcz/fiber-problem"
	"github.com/m1chlcz/microservices-reference/internal/eventrelay"
	"github.com/m1chlcz/microservices-reference/internal/invoicing"
	"github.com/m1chlcz/microservices-reference/internal/platform"
	"github.com/m1chlcz/pgoutbox"
)

const eventDeliveryTimeout = 5 * time.Second

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
	relay, err := eventrelay.NewWorker(queue, workerID(), &http.Client{Timeout: eventDeliveryTimeout}, map[string][]string{
		invoicing.EventInvoiceIssued: {strings.TrimRight(notificationsURL, "/") + "/events"},
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

	app := platform.NewApp(logger)
	invoicing.RegisterRoutes(app, invoicing.NewService(invoicing.NewPostgresStore(pool)))
	return platform.Run(ctx, app, platform.EnvOr("INVOICING_ADDR", ":8088"), logger)
}

func workerID() string {
	host, err := os.Hostname()
	if err != nil {
		host = "invoicing"
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}
