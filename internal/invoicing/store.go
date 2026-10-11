package invoicing

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

// EventInvoiceIssued is the outbox event kind of an issued invoice.
const EventInvoiceIssued = "invoice.issued"

// invoiceEvent is the payload of the invoice.issued outbox event.
type invoiceEvent struct {
	InvoiceID  string `json:"invoiceId"`
	Number     string `json:"number"`
	OrderID    string `json:"orderId"`
	TotalMinor int64  `json:"totalMinor"`
}

// PostgresStore stores invoices in PostgreSQL.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore returns a PostgresStore.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// Issue inserts one invoice and its invoice.issued outbox event in one
// transaction. A repeated call returns the stored invoice.
func (store *PostgresStore) Issue(ctx context.Context, orderID uuid.UUID, totalMinor int64) (Invoice, bool, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return Invoice{}, false, fmt.Errorf("begin invoice: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	stored, err := readInvoiceForOrder(ctx, tx, orderID)
	if err == nil {
		return stored, true, nil
	}
	if !errors.Is(err, ErrUnknownInvoice) {
		return Invoice{}, false, err
	}
	issuedAt := time.Now().UTC()
	invoice := Invoice{
		ID:         uuid.New(),
		OrderID:    orderID,
		TotalMinor: totalMinor,
		IssuedAt:   issuedAt,
	}
	var counter int
	if err = tx.QueryRow(ctx, `
		INSERT INTO invoice_sequences (year, next_number) VALUES ($1, 1)
		ON CONFLICT (year) DO UPDATE SET next_number = invoice_sequences.next_number + 1
		RETURNING next_number
	`, issuedAt.Year()).Scan(&counter); err != nil {
		return Invoice{}, false, fmt.Errorf("allocate invoice number: %w", err)
	}
	invoice.Number = formatInvoiceNumber(issuedAt.Year(), counter)
	payload, err := json.Marshal(invoiceEvent{
		InvoiceID:  invoice.ID.String(),
		Number:     invoice.Number,
		OrderID:    invoice.OrderID.String(),
		TotalMinor: invoice.TotalMinor,
	})
	if err != nil {
		return Invoice{}, false, fmt.Errorf("encode invoice event: %w", err)
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO invoices (id, number, order_id, total_minor, issued_at)
		VALUES ($1, $2, $3, $4, $5)
	`, invoice.ID, invoice.Number, invoice.OrderID, invoice.TotalMinor, invoice.IssuedAt); err != nil {
		if isUniqueViolation(err) {
			// A concurrent call inserted the invoice first; return the stored row.
			_ = tx.Rollback(ctx)
			stored, readErr := store.InvoiceForOrder(ctx, orderID)
			if readErr != nil {
				return Invoice{}, false, readErr
			}
			return stored, true, nil
		}
		return Invoice{}, false, fmt.Errorf("insert invoice: %w", err)
	}
	// The insert mirrors pgoutbox.Enqueue, which cannot run inside this transaction.
	if _, err = tx.Exec(ctx, `
		INSERT INTO outbox_jobs (kind, aggregate_type, aggregate_id, generation, payload)
		VALUES ($1, 'invoice', $2, 0, $3)
		ON CONFLICT (kind, aggregate_type, aggregate_id, generation) DO NOTHING
	`, EventInvoiceIssued, invoice.ID, payload); err != nil {
		return Invoice{}, false, fmt.Errorf("insert outbox event: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return Invoice{}, false, fmt.Errorf("commit invoice: %w", err)
	}
	return invoice, false, nil
}

// InvoiceForOrder returns the invoice of one order.
func (store *PostgresStore) InvoiceForOrder(ctx context.Context, orderID uuid.UUID) (Invoice, error) {
	return readInvoiceForOrder(ctx, store.pool, orderID)
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func readInvoiceForOrder(ctx context.Context, q querier, orderID uuid.UUID) (Invoice, error) {
	var invoice Invoice
	err := q.QueryRow(ctx, `
		SELECT id, number, order_id, total_minor, issued_at FROM invoices WHERE order_id = $1
	`, orderID).Scan(&invoice.ID, &invoice.Number, &invoice.OrderID, &invoice.TotalMinor, &invoice.IssuedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Invoice{}, ErrUnknownInvoice
	}
	if err != nil {
		return Invoice{}, fmt.Errorf("read invoice: %w", err)
	}
	invoice.IssuedAt = invoice.IssuedAt.UTC()
	return invoice, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func formatInvoiceNumber(year, counter int) string {
	return fmt.Sprintf("%d-%06d", year, counter)
}
