package shipping

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const eventShipmentCreated = "shipment.created"

// shipmentEvent is the payload of the shipment.created outbox event.
type shipmentEvent struct {
	ShipmentID   string `json:"shipmentId"`
	OrderID      string `json:"orderId"`
	Carrier      string `json:"carrier"`
	TrackingCode string `json:"trackingCode"`
}

// PostgresStore stores shipments in PostgreSQL.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore returns a PostgresStore.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// Create inserts the shipment and the shipment.created outbox event in one
// transaction. When the order already has a shipment, it returns the stored
// shipment with a duplicate flag.
func (store *PostgresStore) Create(ctx context.Context, shipment Shipment) (Shipment, bool, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return Shipment{}, false, fmt.Errorf("begin shipment: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		INSERT INTO shipments (id, order_id, carrier, tracking_code, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (order_id) DO NOTHING
	`, shipment.ID, shipment.OrderID, shipment.Carrier, shipment.TrackingCode,
		shipment.Status, shipment.CreatedAt, shipment.UpdatedAt)
	if err != nil {
		return Shipment{}, false, fmt.Errorf("insert shipment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		existing, readErr := scanShipment(tx.QueryRow(ctx, `
			SELECT id, order_id, carrier, tracking_code, status, created_at, updated_at
			FROM shipments WHERE order_id = $1
		`, shipment.OrderID))
		if readErr != nil {
			return Shipment{}, false, readErr
		}
		return existing, true, nil
	}
	payload, err := json.Marshal(shipmentEvent{
		ShipmentID:   shipment.ID.String(),
		OrderID:      shipment.OrderID.String(),
		Carrier:      shipment.Carrier,
		TrackingCode: shipment.TrackingCode,
	})
	if err != nil {
		return Shipment{}, false, fmt.Errorf("encode shipment event: %w", err)
	}
	// The insert mirrors pgoutbox.Enqueue and cannot call it inside this transaction.
	if _, err = tx.Exec(ctx, `
		INSERT INTO outbox_jobs (kind, aggregate_type, aggregate_id, generation, payload)
		VALUES ($1, 'shipment', $2, 0, $3)
		ON CONFLICT (kind, aggregate_type, aggregate_id, generation) DO NOTHING
	`, eventShipmentCreated, shipment.ID, payload); err != nil {
		return Shipment{}, false, fmt.Errorf("insert outbox event: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return Shipment{}, false, fmt.Errorf("commit shipment: %w", err)
	}
	return shipment, false, nil
}

// UpdateStatus applies one status report in a transaction. The same status is
// a no-op. A backwards or unknown transition returns ErrInvalidState.
func (store *PostgresStore) UpdateStatus(ctx context.Context, trackingCode, status string) (Shipment, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return Shipment{}, fmt.Errorf("begin status: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	shipment, err := scanShipment(tx.QueryRow(ctx, `
		SELECT id, order_id, carrier, tracking_code, status, created_at, updated_at
		FROM shipments WHERE tracking_code = $1 FOR UPDATE
	`, trackingCode))
	if err != nil {
		return Shipment{}, err
	}
	if shipment.Status == status {
		return shipment, nil
	}
	if !canTransition(shipment.Status, status) {
		return Shipment{}, ErrInvalidState
	}
	shipment.Status = status
	shipment.UpdatedAt = time.Now().UTC()
	if _, err = tx.Exec(ctx, `
		UPDATE shipments SET status = $2, updated_at = $3 WHERE id = $1
	`, shipment.ID, shipment.Status, shipment.UpdatedAt); err != nil {
		return Shipment{}, fmt.Errorf("update shipment: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return Shipment{}, fmt.Errorf("commit status: %w", err)
	}
	return shipment, nil
}

// ShipmentForOrder returns the shipment of one order.
func (store *PostgresStore) ShipmentForOrder(ctx context.Context, orderID uuid.UUID) (Shipment, error) {
	return scanShipment(store.pool.QueryRow(ctx, `
		SELECT id, order_id, carrier, tracking_code, status, created_at, updated_at
		FROM shipments WHERE order_id = $1
	`, orderID))
}

// scanShipment reads one shipment row. It returns ErrUnknownShipment when the
// row does not exist.
func scanShipment(row pgx.Row) (Shipment, error) {
	var shipment Shipment
	err := row.Scan(&shipment.ID, &shipment.OrderID, &shipment.Carrier,
		&shipment.TrackingCode, &shipment.Status, &shipment.CreatedAt, &shipment.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Shipment{}, ErrUnknownShipment
	}
	if err != nil {
		return Shipment{}, fmt.Errorf("read shipment: %w", err)
	}
	shipment.CreatedAt = shipment.CreatedAt.UTC()
	shipment.UpdatedAt = shipment.UpdatedAt.UTC()
	return shipment, nil
}
