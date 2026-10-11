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

const eventOrderCreated = "order.created"

// orderEvent is the payload of the order.created outbox event.
type orderEvent struct {
	OrderID     string `json:"orderId"`
	CustomerRef string `json:"customerRef"`
	TotalMinor  int64  `json:"totalMinor"`
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
		INSERT INTO orders (id, customer_ref, total_minor, created_at)
		VALUES ($1, $2, $3, $4)
	`, order.ID, order.CustomerRef, order.TotalMinor, order.CreatedAt); err != nil {
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
	var order Order
	err := store.pool.QueryRow(ctx, `
		SELECT id, customer_ref, total_minor, created_at FROM orders WHERE id = $1
	`, id).Scan(&order.ID, &order.CustomerRef, &order.TotalMinor, &order.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Order{}, ErrUnknownOrder
	}
	if err != nil {
		return Order{}, fmt.Errorf("read order: %w", err)
	}
	order.CreatedAt = order.CreatedAt.UTC()
	rows, err := store.pool.Query(ctx, `
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
