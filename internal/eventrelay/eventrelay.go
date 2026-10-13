// Package eventrelay turns outbox jobs into gRPC event deliveries.
package eventrelay

import (
	"context"
	"fmt"
	"time"

	eventsv1 "github.com/m1chlcz/microservices-reference/api/events/v1"
	"github.com/m1chlcz/microservices-reference/internal/platform"
	"github.com/m1chlcz/pgoutbox"
)

const deliveryTimeout = 5 * time.Second

// NewWorker returns a pgoutbox worker that delivers every event to the gRPC
// targets of its kind. The worker dials the targets with tlsFiles, so the
// internal calls verify their peers when the configuration enables mTLS. A
// delivery succeeds only when every target answers OK, so a failure leaves the
// job pending for a retry. Consumers must treat a repeated delivery as a
// duplicate.
func NewWorker(
	queue *pgoutbox.Queue,
	workerID string,
	routes map[string][]string,
	tlsFiles platform.TLSFiles,
) (*pgoutbox.Worker, error) {
	kinds := make([]string, 0, len(routes))
	clients := make(map[string]eventsv1.EventSinkClient)
	for kind, targets := range routes {
		kinds = append(kinds, kind)
		for _, target := range targets {
			if _, ok := clients[target]; ok {
				continue
			}
			connection, err := platform.Dial(target, tlsFiles)
			if err != nil {
				return nil, fmt.Errorf("dial %s: %w", target, err)
			}
			clients[target] = eventsv1.NewEventSinkClient(connection)
		}
	}
	return pgoutbox.NewWorker(pgoutbox.WorkerConfig{
		Queue:    queue,
		Kinds:    kinds,
		WorkerID: workerID,
		Handler: func(ctx context.Context, job pgoutbox.Job) error {
			return deliver(ctx, clients, routes[job.Kind], job.Kind, job.Payload)
		},
	})
}

func deliver(
	ctx context.Context,
	clients map[string]eventsv1.EventSinkClient,
	targets []string,
	kind string,
	data []byte,
) error {
	deliveryCtx, cancel := context.WithTimeout(ctx, deliveryTimeout)
	defer cancel()
	for _, target := range targets {
		_, err := clients[target].Deliver(deliveryCtx, &eventsv1.Event{Kind: kind, Data: data})
		if err != nil {
			return fmt.Errorf("deliver to %s: %w", target, err)
		}
	}
	return nil
}
