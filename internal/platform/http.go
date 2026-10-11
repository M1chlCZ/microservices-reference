package platform

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/m1chlcz/fiber-problem"
)

const (
	shutdownTimeout = 10 * time.Second
	bodyLimitBytes  = 256 * 1024
)

// NewApp returns a Fiber app with the shared middleware stack: request IDs,
// access logs, panic recovery, a body limit and RFC 9457 problem responses.
func NewApp(logger *slog.Logger) *fiber.App {
	app := fiber.New(fiber.Config{
		ErrorHandler: fiberproblem.ErrorHandler,
	})
	app.Hooks().OnPreStartupMessage(func(data *fiber.PreStartupMessageData) error {
		data.PreventDefault = true
		return nil
	})
	app.Use(fiberproblem.RequestID())
	app.Use(fiberproblem.AccessLog(logger, nil))
	app.Use(fiberproblem.Recovery(logger))
	app.Use(fiberproblem.BodyLimit(bodyLimitBytes, nil))
	app.Get("/healthz", func(c fiber.Ctx) error {
		return c.SendString("ok\n")
	})
	return app
}

// Run serves the app until the context is canceled. Then it shuts the server
// down and waits for in-flight requests.
func Run(ctx context.Context, app *fiber.App, addr string, logger *slog.Logger) error {
	serveErr := make(chan error, 1)
	go func() { serveErr <- app.Listen(addr) }()
	logger.Info("listening", "addr", addr)
	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := app.ShutdownWithContext(shutdownCtx); err != nil {
		return err
	}
	err := <-serveErr
	if err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// SignalContext returns a context that cancels on SIGINT or SIGTERM.
func SignalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
