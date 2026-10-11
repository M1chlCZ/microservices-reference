package catalog

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore stores products in PostgreSQL.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore returns a PostgresStore.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// Create inserts one product and returns the stored row.
func (store *PostgresStore) Create(ctx context.Context, product Product) (Product, error) {
	var created Product
	err := store.pool.QueryRow(ctx, `
		INSERT INTO products (id, sku, name, price_minor, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, sku, name, price_minor, status, created_at, updated_at
	`, product.ID, product.SKU, product.Name, product.PriceMinor, product.Status,
		product.CreatedAt, product.UpdatedAt).Scan(
		&created.ID,
		&created.SKU,
		&created.Name,
		&created.PriceMinor,
		&created.Status,
		&created.CreatedAt,
		&created.UpdatedAt,
	)
	if isUniqueViolation(err) {
		return Product{}, ErrDuplicateSKU
	}
	if err != nil {
		return Product{}, fmt.Errorf("insert product: %w", err)
	}
	created.CreatedAt = created.CreatedAt.UTC()
	created.UpdatedAt = created.UpdatedAt.UTC()
	return created, nil
}

// Get returns one product.
func (store *PostgresStore) Get(ctx context.Context, id uuid.UUID) (Product, error) {
	var product Product
	err := store.pool.QueryRow(ctx, `
		SELECT id, sku, name, price_minor, status, created_at, updated_at
		FROM products WHERE id = $1
	`, id).Scan(
		&product.ID,
		&product.SKU,
		&product.Name,
		&product.PriceMinor,
		&product.Status,
		&product.CreatedAt,
		&product.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, ErrUnknownProduct
	}
	if err != nil {
		return Product{}, fmt.Errorf("read product: %w", err)
	}
	product.CreatedAt = product.CreatedAt.UTC()
	product.UpdatedAt = product.UpdatedAt.UTC()
	return product, nil
}

// List returns active products ordered by SKU after the cursor.
func (store *PostgresStore) List(ctx context.Context, limit int, cursor string) ([]Product, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT id, sku, name, price_minor, status, created_at, updated_at
		FROM products
		WHERE status = $1 AND ($2 = '' OR sku > $2)
		ORDER BY sku
		LIMIT $3
	`, StatusActive, cursor, limit)
	if err != nil {
		return nil, fmt.Errorf("read products: %w", err)
	}
	defer rows.Close()
	products := make([]Product, 0, limit)
	for rows.Next() {
		var product Product
		if scanErr := rows.Scan(
			&product.ID,
			&product.SKU,
			&product.Name,
			&product.PriceMinor,
			&product.Status,
			&product.CreatedAt,
			&product.UpdatedAt,
		); scanErr != nil {
			return nil, fmt.Errorf("read product: %w", scanErr)
		}
		product.CreatedAt = product.CreatedAt.UTC()
		product.UpdatedAt = product.UpdatedAt.UTC()
		products = append(products, product)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read products: %w", err)
	}
	return products, nil
}

// Update writes the mutable columns of one product and returns the stored
// row.
func (store *PostgresStore) Update(ctx context.Context, product Product) (Product, error) {
	var updated Product
	err := store.pool.QueryRow(ctx, `
		UPDATE products
		SET name = $2, price_minor = $3, status = $4, updated_at = $5
		WHERE id = $1
		RETURNING id, sku, name, price_minor, status, created_at, updated_at
	`, product.ID, product.Name, product.PriceMinor, product.Status, product.UpdatedAt).Scan(
		&updated.ID,
		&updated.SKU,
		&updated.Name,
		&updated.PriceMinor,
		&updated.Status,
		&updated.CreatedAt,
		&updated.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Product{}, ErrUnknownProduct
	}
	if err != nil {
		return Product{}, fmt.Errorf("update product: %w", err)
	}
	updated.CreatedAt = updated.CreatedAt.UTC()
	updated.UpdatedAt = updated.UpdatedAt.UTC()
	return updated, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
