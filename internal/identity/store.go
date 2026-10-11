package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const pgUniqueViolation = "23505"

// PostgresStore stores accounts and sessions in PostgreSQL.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore returns a PostgresStore.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

// InsertAccount inserts one account. A duplicate email reports ErrEmailTaken.
func (store *PostgresStore) InsertAccount(ctx context.Context, account Account, passwordHash string) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO accounts (id, email, password_hash, created_at)
		VALUES ($1, $2, $3, $4)
	`, account.ID, account.Email, passwordHash, account.CreatedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return ErrEmailTaken
	}
	if err != nil {
		return fmt.Errorf("insert account: %w", err)
	}
	return nil
}

// AccountByEmail returns one account with its private fields.
func (store *PostgresStore) AccountByEmail(ctx context.Context, email string) (Record, error) {
	var record Record
	var totpSecret *string
	err := store.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, totp_secret, totp_confirmed, created_at
		FROM accounts WHERE email = $1
	`, email).Scan(&record.ID, &record.Email, &record.PasswordHash, &totpSecret,
		&record.TOTPConfirmed, &record.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, ErrUnknownAccount
	}
	if err != nil {
		return Record{}, fmt.Errorf("read account: %w", err)
	}
	if totpSecret != nil {
		record.TOTPSecret = *totpSecret
	}
	record.CreatedAt = record.CreatedAt.UTC()
	return record, nil
}

// AccountByID returns one account with its private fields.
func (store *PostgresStore) AccountByID(ctx context.Context, id uuid.UUID) (Record, error) {
	var record Record
	var totpSecret *string
	err := store.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, totp_secret, totp_confirmed, created_at
		FROM accounts WHERE id = $1
	`, id).Scan(&record.ID, &record.Email, &record.PasswordHash, &totpSecret,
		&record.TOTPConfirmed, &record.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, ErrUnknownAccount
	}
	if err != nil {
		return Record{}, fmt.Errorf("read account: %w", err)
	}
	if totpSecret != nil {
		record.TOTPSecret = *totpSecret
	}
	record.CreatedAt = record.CreatedAt.UTC()
	return record, nil
}

// InsertSession inserts one session token hash.
func (store *PostgresStore) InsertSession(ctx context.Context, tokenHash []byte, accountID uuid.UUID, createdAt, expiresAt time.Time) error {
	if _, err := store.pool.Exec(ctx, `
		INSERT INTO sessions (token_hash, account_id, created_at, expires_at)
		VALUES ($1, $2, $3, $4)
	`, tokenHash, accountID, createdAt, expiresAt); err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

// AccountBySession returns the account of one unexpired session token hash.
func (store *PostgresStore) AccountBySession(ctx context.Context, tokenHash []byte, at time.Time) (Account, error) {
	var account Account
	err := store.pool.QueryRow(ctx, `
		SELECT a.id, a.email, a.created_at
		FROM sessions s
		JOIN accounts a ON a.id = s.account_id
		WHERE s.token_hash = $1 AND s.expires_at > $2
	`, tokenHash, at).Scan(&account.ID, &account.Email, &account.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Account{}, ErrInvalidSession
	}
	if err != nil {
		return Account{}, fmt.Errorf("read session: %w", err)
	}
	account.CreatedAt = account.CreatedAt.UTC()
	return account, nil
}

// DeleteSession removes one session token hash. An unknown hash is a no-op.
func (store *PostgresStore) DeleteSession(ctx context.Context, tokenHash []byte) error {
	if _, err := store.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// SetTOTPSecret stores one unconfirmed TOTP secret.
func (store *PostgresStore) SetTOTPSecret(ctx context.Context, accountID uuid.UUID, secret string) error {
	tag, err := store.pool.Exec(ctx, `
		UPDATE accounts SET totp_secret = $2, totp_confirmed = false WHERE id = $1
	`, accountID, secret)
	if err != nil {
		return fmt.Errorf("store totp secret: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUnknownAccount
	}
	return nil
}

// ConfirmTOTP marks one TOTP secret as confirmed.
func (store *PostgresStore) ConfirmTOTP(ctx context.Context, accountID uuid.UUID) error {
	tag, err := store.pool.Exec(ctx, `
		UPDATE accounts SET totp_confirmed = true WHERE id = $1 AND totp_secret IS NOT NULL
	`, accountID)
	if err != nil {
		return fmt.Errorf("confirm totp: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrUnknownAccount
	}
	return nil
}
