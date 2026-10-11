package notifications

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore stores notifications in PostgreSQL.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore returns a PostgresStore.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// Insert stores one notification.
func (store *PostgresStore) Insert(ctx context.Context, kind string, payload []byte) (Notification, error) {
	var notification Notification
	err := store.pool.QueryRow(ctx, `
		INSERT INTO notifications (kind, payload)
		VALUES ($1, $2)
		RETURNING id, kind, payload, received_at
	`, kind, payload).Scan(
		&notification.ID,
		&notification.Kind,
		&notification.Payload,
		&notification.ReceivedAt,
	)
	if err != nil {
		return Notification{}, fmt.Errorf("insert notification: %w", err)
	}
	notification.ReceivedAt = notification.ReceivedAt.UTC()
	return notification, nil
}

// List returns the most recent notifications.
func (store *PostgresStore) List(ctx context.Context, limit int) ([]Notification, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT id, kind, payload, received_at
		FROM notifications ORDER BY id DESC LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("read notifications: %w", err)
	}
	defer rows.Close()
	notifications := make([]Notification, 0, limit)
	for rows.Next() {
		var notification Notification
		if scanErr := rows.Scan(
			&notification.ID,
			&notification.Kind,
			&notification.Payload,
			&notification.ReceivedAt,
		); scanErr != nil {
			return nil, fmt.Errorf("read notification: %w", scanErr)
		}
		notification.ReceivedAt = notification.ReceivedAt.UTC()
		notifications = append(notifications, notification)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read notifications: %w", err)
	}
	return notifications, nil
}
