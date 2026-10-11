// Command shipping creates one shipment per paid order and relays shipment
// events to the notifications service.
package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/m1chlcz/fiber-problem"
	"github.com/m1chlcz/microservices-reference/internal/eventrelay"
	"github.com/m1chlcz/microservices-reference/internal/platform"
	"github.com/m1chlcz/microservices-reference/internal/shipping"
	"github.com/m1chlcz/pgoutbox"
)

const eventDeliveryTimeout = 5 * time.Second

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
	notificationsURL, err := platform.RequireEnv("NOTIFICATIONS_URL")
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
	relay, err := eventrelay.NewWorker(queue, workerID(), &http.Client{Timeout: eventDeliveryTimeout},
		map[string][]string{"shipment.created": {strings.TrimRight(notificationsURL, "/") + "/events"}})
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
	app := platform.NewApp(logger)
	shipping.RegisterRoutes(app, service)
	return platform.Run(ctx, app, platform.EnvOr("SHIPPING_ADDR", ":8087"), logger)
}

func workerID() string {
	host, err := os.Hostname()
	if err != nil {
		host = "shipping"
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}
