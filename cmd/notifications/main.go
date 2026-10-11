// Command notifications records events that other services deliver.
package main

import (
	"fmt"
	"os"

	"github.com/m1chlcz/fiber-problem"
	"github.com/m1chlcz/microservices-reference/internal/notifications"
	"github.com/m1chlcz/microservices-reference/internal/platform"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "notifications:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := platform.SignalContext()
	defer stop()
	logger := fiberproblem.NewJSONLogger(os.Stdout)
	databaseURL, err := platform.RequireEnv("NOTIFICATIONS_DATABASE_URL")
	if err != nil {
		return err
	}
	pool, err := platform.ConnectDatabase(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	app := platform.NewApp(logger)
	notifications.RegisterRoutes(app, notifications.NewService(notifications.NewPostgresStore(pool)))
	return platform.Run(ctx, app, platform.EnvOr("NOTIFICATIONS_ADDR", ":8083"), logger)
}
