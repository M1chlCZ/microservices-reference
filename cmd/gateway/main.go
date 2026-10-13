// Command gateway exposes a REST endpoint that reserves stock through the
// internal inventory gRPC service. It is the gateway example of the
// reference: REST on the edge, mTLS and trace context on the internal call.
package main

import (
	"fmt"
	"os"

	"github.com/m1chlcz/fiber-problem"
	"github.com/m1chlcz/microservices-reference/internal/gateway"
	"github.com/m1chlcz/microservices-reference/internal/platform"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gateway:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := platform.SignalContext()
	defer stop()
	logger := fiberproblem.NewJSONLogger(os.Stdout)
	shutdown, err := platform.SetupTracing(ctx, "gateway")
	if err != nil {
		return err
	}
	defer shutdown()
	inventoryTarget, err := platform.RequireEnv("INVENTORY_GRPC")
	if err != nil {
		return err
	}
	clientTLS, err := platform.ClientTLSFromEnv()
	if err != nil {
		return err
	}
	connection, err := platform.Dial(inventoryTarget, clientTLS)
	if err != nil {
		return err
	}
	defer func() { _ = connection.Close() }()

	service := gateway.NewService(gateway.NewGRPCInventoryClient(connection))
	app := platform.NewApp(logger)
	gateway.RegisterRoutes(app, service)
	return platform.Run(ctx, app, platform.EnvOr("GATEWAY_ADDR", ":8090"), logger)
}
