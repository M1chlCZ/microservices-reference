// Command migrate applies the database migrations of every service with
// pgmig. Each service database has its own least-privilege migrator role.
package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"

	"github.com/m1chlcz/microservices-reference/internal/migrations"
	"github.com/m1chlcz/microservices-reference/internal/platform"
	"github.com/m1chlcz/pgmig"
)

type target struct {
	name     string
	urlEnv   string
	roleEnv  string
	fallback string
	files    fs.FS
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	targets := []target{
		{
			name:     "orders",
			urlEnv:   "ORDERS_MIGRATOR_URL",
			roleEnv:  "ORDERS_OWNER_ROLE",
			fallback: "orders_owner",
			files:    migrations.Orders(),
		},
		{
			name:     "inventory",
			urlEnv:   "INVENTORY_MIGRATOR_URL",
			roleEnv:  "INVENTORY_OWNER_ROLE",
			fallback: "inventory_owner",
			files:    migrations.Inventory(),
		},
		{
			name:     "notifications",
			urlEnv:   "NOTIFICATIONS_MIGRATOR_URL",
			roleEnv:  "NOTIFICATIONS_OWNER_ROLE",
			fallback: "notifications_owner",
			files:    migrations.Notifications(),
		},
	}
	for _, current := range targets {
		databaseURL, err := platform.RequireEnv(current.urlEnv)
		if err != nil {
			return err
		}
		config := pgmig.Config{
			Role:  platform.EnvOr(current.roleEnv, current.fallback),
			Files: current.files,
		}
		if err := pgmig.Run(ctx, databaseURL, config); err != nil {
			return fmt.Errorf("%s: %w", current.name, err)
		}
		fmt.Printf("%s: migrations applied\n", current.name)
	}
	return nil
}
