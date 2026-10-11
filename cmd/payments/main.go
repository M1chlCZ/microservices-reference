// Command payments serves the payment API and relays captured payment events
// to the orders service.
package main

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/m1chlcz/fiber-problem"
	"github.com/m1chlcz/microservices-reference/internal/eventrelay"
	"github.com/m1chlcz/microservices-reference/internal/payments"
	"github.com/m1chlcz/microservices-reference/internal/platform"
	"github.com/m1chlcz/pgoutbox"
)

const eventDeliveryTimeout = 5 * time.Second

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
	databaseURL, err := platform.RequireEnv("PAYMENTS_DATABASE_URL")
	if err != nil {
		return err
	}
	ordersURL, err := platform.RequireEnv("ORDERS_URL")
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
	relay, err := eventrelay.NewWorker(queue, workerID(),
		&http.Client{Timeout: eventDeliveryTimeout},
		map[string][]string{"payment.captured": {ordersURL + "/events"}})
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
	payments.RegisterRoutes(app, payments.NewService(payments.NewPostgresStore(pool)))
	return platform.Run(ctx, app, platform.EnvOr("PAYMENTS_ADDR", ":8086"), logger)
}

func workerID() string {
	host, err := os.Hostname()
	if err != nil {
		host = "payments"
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}
