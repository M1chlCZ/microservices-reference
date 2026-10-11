package orders

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	eventOrderCreated = "order.created"
	eventOrderPaid    = "order.paid"
)

// orderEvent is the payload of the order outbox events.
type orderEvent struct {
	OrderID     string `json:"orderId"`
	CustomerRef string `json:"customerRef"`
	TotalMinor  int64  `json:"totalMinor"`
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// PostgresStore stores orders in PostgreSQL.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore returns a PostgresStore.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// Create inserts the order, its lines and the order.created outbox event in
// one transaction. The event commits with the order row.
func (store *PostgresStore) Create(ctx context.Context, order Order) error {
	payload, err := json.Marshal(orderEvent{
		OrderID:     order.ID.String(),
		CustomerRef: order.CustomerRef,
		TotalMinor:  order.TotalMinor,
	})
	if err != nil {
		return fmt.Errorf("encode order event: %w", err)
	}
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin order: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `
		INSERT INTO orders (
			id, customer_ref, status, total_minor, payment_intent_id, provider_ref, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, order.ID, order.CustomerRef, order.Status, order.TotalMinor,
		paymentIntentID(order), providerRef(order), order.CreatedAt); err != nil {
		return fmt.Errorf("insert order: %w", err)
	}
	for _, line := range order.Lines {
		if _, err = tx.Exec(ctx, `
			INSERT INTO order_items (order_id, sku, quantity, unit_price_minor)
			VALUES ($1, $2, $3, $4)
		`, order.ID, line.SKU, line.Quantity, line.UnitPriceMinor); err != nil {
			return fmt.Errorf("insert order line: %w", err)
		}
	}
	// The insert mirrors pgoutbox.Enqueue, which cannot run inside this
	// transaction.
	if _, err = tx.Exec(ctx, `
		INSERT INTO outbox_jobs (kind, aggregate_type, aggregate_id, generation, payload)
		VALUES ($1, 'order', $2, 0, $3)
		ON CONFLICT (kind, aggregate_type, aggregate_id, generation) DO NOTHING
	`, eventOrderCreated, order.ID, payload); err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit order: %w", err)
	}
	return nil
}

// Get returns one order with its lines.
func (store *PostgresStore) Get(ctx context.Context, id uuid.UUID) (Order, error) {
	return readOrder(ctx, store.pool, id)
}

// MarkPaid marks a pending order paid and records the order.paid event in
// one transaction. It returns the stored order and a duplicate flag.
func (store *PostgresStore) MarkPaid(ctx context.Context, id uuid.UUID) (Order, bool, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return Order{}, false, fmt.Errorf("begin paid order: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		UPDATE orders SET status = $2 WHERE id = $1 AND status = $3
	`, id, statusPaid, statusPendingPayment)
	if err != nil {
		return Order{}, false, fmt.Errorf("mark order paid: %w", err)
	}
	order, err := readOrder(ctx, tx, id)
	if err != nil {
		return Order{}, false, err
	}
	duplicate := tag.RowsAffected() == 0
	if !duplicate {
		payload, err := json.Marshal(orderEvent{
			OrderID:     order.ID.String(),
			CustomerRef: order.CustomerRef,
			TotalMinor:  order.TotalMinor,
		})
		if err != nil {
			return Order{}, false, fmt.Errorf("encode order event: %w", err)
		}
		if _, err = tx.Exec(ctx, `
			INSERT INTO outbox_jobs (kind, aggregate_type, aggregate_id, generation, payload)
			VALUES ($1, 'order', $2, 0, $3)
			ON CONFLICT (kind, aggregate_type, aggregate_id, generation) DO NOTHING
		`, eventOrderPaid, order.ID, payload); err != nil {
			return Order{}, false, fmt.Errorf("insert outbox event: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return Order{}, false, fmt.Errorf("commit paid order: %w", err)
	}
	return order, duplicate, nil
}

func readOrder(ctx context.Context, database querier, id uuid.UUID) (Order, error) {
	var order Order
	var paymentIntent uuid.UUID
	var providerRefValue *string
	err := database.QueryRow(ctx, `
		SELECT id, customer_ref, status, total_minor, payment_intent_id, provider_ref, created_at
		FROM orders WHERE id = $1
	`, id).Scan(
		&order.ID,
		&order.CustomerRef,
		&order.Status,
		&order.TotalMinor,
		&paymentIntent,
		&providerRefValue,
		&order.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrUnknownOrder
	}
	if err != nil {
		return Order{}, fmt.Errorf("read order: %w", err)
	}
	order.CreatedAt = order.CreatedAt.UTC()
	if paymentIntent != uuid.Nil {
		payment := Payment{IntentID: paymentIntent}
		if providerRefValue != nil {
			payment.ProviderRef = *providerRefValue
		}
		order.Payment = &payment
	}
	rows, err := database.Query(ctx, `
		SELECT sku, quantity, unit_price_minor
		FROM order_items WHERE order_id = $1 ORDER BY sku
	`, id)
	if err != nil {
		return Order{}, fmt.Errorf("read order lines: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var line Line
		if scanErr := rows.Scan(&line.SKU, &line.Quantity, &line.UnitPriceMinor); scanErr != nil {
			return Order{}, fmt.Errorf("read order line: %w", scanErr)
		}
		order.Lines = append(order.Lines, line)
	}
	if err = rows.Err(); err != nil {
		return Order{}, fmt.Errorf("read order lines: %w", err)
	}
	return order, nil
}

func paymentIntentID(order Order) any {
	if order.Payment == nil {
		return nil
	}
	return order.Payment.IntentID
}

func providerRef(order Order) *string {
	if order.Payment == nil || order.Payment.ProviderRef == "" {
		return nil
	}
	value := order.Payment.ProviderRef
	return &value
}
