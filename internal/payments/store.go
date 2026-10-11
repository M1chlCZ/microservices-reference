package payments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const eventPaymentCaptured = "payment.captured"

const intentColumns = `id, order_id, amount_minor, currency, status, provider_ref, idempotency_key, created_at, updated_at`

// capturedEvent is the payload of the payment.captured outbox event.
type capturedEvent struct {
	PaymentIntentID string `json:"paymentIntentId"`
	OrderID         string `json:"orderId"`
	AmountMinor     int64  `json:"amountMinor"`
}

type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// PostgresStore stores payment intents in PostgreSQL.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore returns a PostgresStore.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// CreateIntent stores the intent and returns the stored row. It returns the
// stored intent and a duplicate flag when the idempotency key or the order
// already has an intent. A reused key with a different request returns
// ErrIdempotencyConflict.
func (store *PostgresStore) CreateIntent(ctx context.Context, intent Intent) (Intent, bool, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return Intent{}, false, fmt.Errorf("begin payment intent: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	stored, duplicate, err := store.resolveExisting(ctx, tx, intent)
	switch {
	case err == nil:
		return stored, duplicate, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return Intent{}, false, err
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO payment_intents (
			id, order_id, amount_minor, currency, status, provider_ref,
			idempotency_key, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`, intent.ID, intent.OrderID, intent.AmountMinor, intent.Currency, intent.Status,
		intent.ProviderRef, intent.IdempotencyKey, intent.CreatedAt, intent.UpdatedAt); err != nil {
		if isUniqueViolation(err) {
			// A concurrent request stored the same key or order first.
			_ = tx.Rollback(ctx)
			return store.resolveExisting(ctx, store.pool, intent)
		}
		return Intent{}, false, fmt.Errorf("insert payment intent: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return Intent{}, false, fmt.Errorf("commit payment intent: %w", err)
	}
	return intent, false, nil
}

// Capture marks the intent captured and writes the payment.captured outbox
// event in one transaction. It returns the captured intent. A repeated
// capture returns the intent and a duplicate flag. An unknown provider
// reference returns ErrUnknownIntent. A refunded intent returns
// ErrInvalidState.
func (store *PostgresStore) Capture(ctx context.Context, providerRef string) (Intent, bool, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return Intent{}, false, fmt.Errorf("begin capture: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	intent, err := scanIntent(tx.QueryRow(ctx, `
		SELECT `+intentColumns+`
		FROM payment_intents WHERE provider_ref = $1 FOR UPDATE
	`, providerRef))
	if errors.Is(err, pgx.ErrNoRows) {
		return Intent{}, false, ErrUnknownIntent
	}
	if err != nil {
		return Intent{}, false, fmt.Errorf("read payment intent: %w", err)
	}
	if intent.Status == statusCaptured {
		return intent, true, nil
	}
	if intent.Status != statusRequiresPayment {
		return Intent{}, false, ErrInvalidState
	}
	payload, err := json.Marshal(capturedEvent{
		PaymentIntentID: intent.ID.String(),
		OrderID:         intent.OrderID.String(),
		AmountMinor:     intent.AmountMinor,
	})
	if err != nil {
		return Intent{}, false, fmt.Errorf("encode payment event: %w", err)
	}
	now := time.Now().UTC()
	if _, err = tx.Exec(ctx, `
		UPDATE payment_intents SET status = $2, updated_at = $3 WHERE id = $1
	`, intent.ID, statusCaptured, now); err != nil {
		return Intent{}, false, fmt.Errorf("update payment intent: %w", err)
	}
	// The insert mirrors pgoutbox.Enqueue, which cannot run inside this
	// transaction.
	if _, err = tx.Exec(ctx, `
		INSERT INTO outbox_jobs (kind, aggregate_type, aggregate_id, generation, payload)
		VALUES ($1, 'payment', $2, 0, $3)
		ON CONFLICT (kind, aggregate_type, aggregate_id, generation) DO NOTHING
	`, eventPaymentCaptured, intent.ID, payload); err != nil {
		return Intent{}, false, fmt.Errorf("insert outbox event: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return Intent{}, false, fmt.Errorf("commit capture: %w", err)
	}
	intent.Status = statusCaptured
	intent.UpdatedAt = now
	return intent, false, nil
}

// Intent returns one payment intent.
func (store *PostgresStore) Intent(ctx context.Context, id uuid.UUID) (Intent, error) {
	intent, err := scanIntent(store.pool.QueryRow(ctx, `
		SELECT `+intentColumns+`
		FROM payment_intents WHERE id = $1
	`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Intent{}, ErrUnknownIntent
	}
	if err != nil {
		return Intent{}, fmt.Errorf("read payment intent: %w", err)
	}
	return intent, nil
}

func (store *PostgresStore) resolveExisting(ctx context.Context, db queryRower, intent Intent) (Intent, bool, error) {
	stored, err := scanIntent(db.QueryRow(ctx, `
		SELECT `+intentColumns+`
		FROM payment_intents WHERE idempotency_key = $1
	`, intent.IdempotencyKey))
	if err == nil {
		if stored.OrderID == intent.OrderID && stored.AmountMinor == intent.AmountMinor &&
			stored.Currency == intent.Currency {
			return stored, true, nil
		}
		return Intent{}, false, ErrIdempotencyConflict
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Intent{}, false, fmt.Errorf("read payment intent: %w", err)
	}
	stored, err = scanIntent(db.QueryRow(ctx, `
		SELECT `+intentColumns+`
		FROM payment_intents WHERE order_id = $1
	`, intent.OrderID))
	if err == nil {
		return stored, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Intent{}, false, fmt.Errorf("read payment intent: %w", err)
	}
	return Intent{}, false, pgx.ErrNoRows
}

func scanIntent(row pgx.Row) (Intent, error) {
	var intent Intent
	err := row.Scan(&intent.ID, &intent.OrderID, &intent.AmountMinor, &intent.Currency,
		&intent.Status, &intent.ProviderRef, &intent.IdempotencyKey,
		&intent.CreatedAt, &intent.UpdatedAt)
	if err != nil {
		return Intent{}, err
	}
	intent.CreatedAt = intent.CreatedAt.UTC()
	intent.UpdatedAt = intent.UpdatedAt.UTC()
	return intent, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
