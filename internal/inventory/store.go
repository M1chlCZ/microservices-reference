package inventory

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore stores stock positions and reservations in PostgreSQL.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore returns a PostgresStore.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// Item returns one stock position.
func (store *PostgresStore) Item(ctx context.Context, sku string) (Item, error) {
	var item Item
	err := store.pool.QueryRow(ctx, `
		SELECT sku, available FROM stock WHERE sku = $1
	`, sku).Scan(&item.SKU, &item.Available)
	if errors.Is(err, pgx.ErrNoRows) {
		return Item{}, ErrUnknownSKU
	}
	if err != nil {
		return Item{}, fmt.Errorf("read stock: %w", err)
	}
	return item, nil
}

// Reserve decreases the stock of every item in one transaction.
func (store *PostgresStore) Reserve(ctx context.Context, items []ReservationItem) (uuid.UUID, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("begin reservation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, item := range items {
		tag, execErr := tx.Exec(ctx, `
			UPDATE stock
			SET available = available - $2
			WHERE sku = $1 AND available >= $2
		`, item.SKU, item.Quantity)
		if execErr != nil {
			return uuid.Nil, fmt.Errorf("reserve stock: %w", execErr)
		}
		if tag.RowsAffected() == 1 {
			continue
		}
		var exists bool
		if scanErr := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM stock WHERE sku = $1)
		`, item.SKU).Scan(&exists); scanErr != nil {
			return uuid.Nil, fmt.Errorf("check stock: %w", scanErr)
		}
		if !exists {
			return uuid.Nil, ErrUnknownSKU
		}
		return uuid.Nil, ErrInsufficientStock
	}
	reservationID := uuid.New()
	if _, err = tx.Exec(ctx, `INSERT INTO reservations (id) VALUES ($1)`, reservationID); err != nil {
		return uuid.Nil, fmt.Errorf("record reservation: %w", err)
	}
	for _, item := range items {
		if _, err = tx.Exec(ctx, `
			INSERT INTO reservation_items (reservation_id, sku, quantity)
			VALUES ($1, $2, $3)
		`, reservationID, item.SKU, item.Quantity); err != nil {
			return uuid.Nil, fmt.Errorf("record reservation item: %w", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return uuid.Nil, fmt.Errorf("commit reservation: %w", err)
	}
	return reservationID, nil
}

// Release returns the reserved quantities to stock and removes the
// reservation.
func (store *PostgresStore) Release(ctx context.Context, reservationID uuid.UUID) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin release: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		SELECT sku, quantity FROM reservation_items WHERE reservation_id = $1
	`, reservationID)
	if err != nil {
		return fmt.Errorf("read reservation items: %w", err)
	}
	var items []ReservationItem
	for rows.Next() {
		var item ReservationItem
		if scanErr := rows.Scan(&item.SKU, &item.Quantity); scanErr != nil {
			rows.Close()
			return fmt.Errorf("read reservation item: %w", scanErr)
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("read reservation items: %w", err)
	}
	rows.Close()
	for _, item := range items {
		if _, err = tx.Exec(ctx, `
			UPDATE stock SET available = available + $2 WHERE sku = $1
		`, item.SKU, item.Quantity); err != nil {
			return fmt.Errorf("release stock: %w", err)
		}
	}
	if _, err = tx.Exec(ctx, `DELETE FROM reservation_items WHERE reservation_id = $1`, reservationID); err != nil {
		return fmt.Errorf("delete reservation items: %w", err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM reservations WHERE id = $1`, reservationID); err != nil {
		return fmt.Errorf("delete reservation: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit release: %w", err)
	}
	return nil
}
