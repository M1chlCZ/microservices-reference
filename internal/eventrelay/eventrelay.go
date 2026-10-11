// Package eventrelay turns outbox jobs into HTTP deliveries.
package eventrelay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/m1chlcz/pgoutbox"
)

// Envelope is the wire shape of one delivered event.
type Envelope struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

// NewWorker returns a pgoutbox worker that posts every event to the endpoints
// that its kind maps to. The handler reports success only when every endpoint
// accepts the event, so a partial failure retries the whole delivery.
// Consumers must treat a repeated delivery as a duplicate and answer with
// success.
func NewWorker(
	queue *pgoutbox.Queue,
	workerID string,
	client *http.Client,
	routes map[string][]string,
) (*pgoutbox.Worker, error) {
	kinds := make([]string, 0, len(routes))
	for kind := range routes {
		kinds = append(kinds, kind)
	}
	return pgoutbox.NewWorker(pgoutbox.WorkerConfig{
		Queue:    queue,
		Kinds:    kinds,
		WorkerID: workerID,
		Handler: func(ctx context.Context, job pgoutbox.Job) error {
			return deliver(ctx, client, routes[job.Kind], Envelope{
				Kind: job.Kind,
				Data: job.Payload,
			})
		},
	})
}

func deliver(ctx context.Context, client *http.Client, targets []string, envelope Envelope) error {
	body, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}
	for _, target := range targets {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("build event request: %w", err)
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := client.Do(request)
		if err != nil {
			return fmt.Errorf("deliver event: %w", err)
		}
		response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode > 299 {
			return fmt.Errorf("target answered with status %d", response.StatusCode)
		}
	}
	return nil
}
