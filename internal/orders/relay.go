package orders

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/m1chlcz/pgoutbox"
)

const eventDeliveryTimeout = 5 * time.Second

type eventEnvelope struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

// NewRelay returns the outbox worker that delivers order.created events to
// the notifications service. A failed delivery returns an error, so pgoutbox
// retries it with backoff.
func NewRelay(queue *pgoutbox.Queue, notificationsURL string, workerID string) (*pgoutbox.Worker, error) {
	client := &http.Client{Timeout: eventDeliveryTimeout}
	target := strings.TrimRight(notificationsURL, "/") + "/events"
	return pgoutbox.NewWorker(pgoutbox.WorkerConfig{
		Queue:    queue,
		Kinds:    []string{eventOrderCreated},
		WorkerID: workerID,
		Handler: func(ctx context.Context, job pgoutbox.Job) error {
			body, err := json.Marshal(eventEnvelope{Kind: job.Kind, Data: job.Payload})
			if err != nil {
				return fmt.Errorf("encode event: %w", err)
			}
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
			if err != nil {
				return fmt.Errorf("build event request: %w", err)
			}
			request.Header.Set("Content-Type", "application/json")
			response, err := client.Do(request)
			if err != nil {
				return fmt.Errorf("deliver event: %w", err)
			}
			defer response.Body.Close()
			if response.StatusCode < 200 || response.StatusCode > 299 {
				return fmt.Errorf("notifications answered with status %d", response.StatusCode)
			}
			return nil
		},
	})
}
