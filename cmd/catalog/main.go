// Command catalog serves the product catalog over HTTP.
package main

import (
	"fmt"
	"os"

	"github.com/m1chlcz/fiber-problem"
	"github.com/m1chlcz/microservices-reference/internal/catalog"
	"github.com/m1chlcz/microservices-reference/internal/platform"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "catalog:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := platform.SignalContext()
	defer stop()
	logger := fiberproblem.NewJSONLogger(os.Stdout)
	databaseURL, err := platform.RequireEnv("CATALOG_DATABASE_URL")
	if err != nil {
		return err
	}
	pool, err := platform.ConnectDatabase(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	app := platform.NewApp(logger)
	catalog.RegisterRoutes(app, catalog.NewService(catalog.NewPostgresStore(pool)))
	return platform.Run(ctx, app, platform.EnvOr("CATALOG_ADDR", ":8085"), logger)
}
