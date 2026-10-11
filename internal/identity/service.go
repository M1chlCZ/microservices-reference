// Package identity implements accounts, sessions and TOTP factors.
package identity

import (
	"context"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ErrInvalidAccount reports a malformed email or password.
var ErrInvalidAccount = errors.New("invalid account")

// ErrEmailTaken reports that the email already has an account.
var ErrEmailTaken = errors.New("email taken")

// ErrInvalidCredentials reports a failed login.
var ErrInvalidCredentials = errors.New("invalid credentials")

// ErrInvalidSession reports a missing, unknown or expired session token.
var ErrInvalidSession = errors.New("invalid session")

// ErrUnknownAccount reports a lookup for an account that does not exist.
var ErrUnknownAccount = errors.New("unknown account")

// ErrTOTPConfirmed reports that the account already confirmed TOTP.
var ErrTOTPConfirmed = errors.New("totp already confirmed")

// ErrInvalidCode reports a TOTP code that does not match.
var ErrInvalidCode = errors.New("invalid totp code")

const (
	defaultPasswordIterations = 600000
	passwordSaltBytes         = 16
	passwordKeyBytes          = 32
	minPasswordLength  = 10
	maxPasswordLength  = 200
	sessionTokenBytes  = 32
	sessionDuration    = 24 * time.Hour
	totpSecretBytes    = 20
	totpDigits         = 6
	totpPeriodSeconds  = 30
	issuerName         = "microservices-reference"
)

// Account is one registered identity.
type Account struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"createdAt"`
}

// Record is one account with its private authentication fields.
type Record struct {
	Account
	PasswordHash  string
	TOTPSecret    string
	TOTPConfirmed bool
}

// Session is one issued session token.
type Session struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// TOTPProvisioning is one new TOTP secret with its otpauth URL.
type TOTPProvisioning struct {
	Secret     string `json:"secret"`
	OtpauthURL string `json:"otpauthUrl"`
}

// Store is the persistence boundary of the service.
type Store interface {
	InsertAccount(ctx context.Context, account Account, passwordHash string) error
	AccountByEmail(ctx context.Context, email string) (Record, error)
	AccountByID(ctx context.Context, id uuid.UUID) (Record, error)
	InsertSession(ctx context.Context, tokenHash []byte, accountID uuid.UUID, createdAt, expiresAt time.Time) error
	AccountBySession(ctx context.Context, tokenHash []byte, at time.Time) (Account, error)
	DeleteSession(ctx context.Context, tokenHash []byte) error
	SetTOTPSecret(ctx context.Context, accountID uuid.UUID, secret string) error
	ConfirmTOTP(ctx context.Context, accountID uuid.UUID) error
}

// Service implements the identity use cases.
type Service struct {
	store              Store
	now                func() time.Time
	passwordIterations int
}

// NewService returns a Service.
func NewService(store Store) *Service {
	return &Service{store: store, now: time.Now, passwordIterations: defaultPasswordIterations}
}

// Register creates one account and hashes the password before storage.
func (service *Service) Register(ctx context.Context, email, password string) (Account, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if !validEmail(email) || !validPassword(password) {
		return Account{}, ErrInvalidAccount
	}
	passwordHash, err := hashPassword(password, service.passwordIterations)
	if err != nil {
		return Account{}, err
	}
	account := Account{ID: uuid.New(), Email: email, CreatedAt: service.now().UTC()}
	if err = service.store.InsertAccount(ctx, account, passwordHash); err != nil {
		return Account{}, err
	}
	return account, nil
}

// Login checks the credentials and creates one session. It returns the raw
// token. An unknown email and a wrong password produce the same error.
func (service *Service) Login(ctx context.Context, email, password string) (Session, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	record, err := service.store.AccountByEmail(ctx, email)
	if errors.Is(err, ErrUnknownAccount) {
		return Session{}, ErrInvalidCredentials
	}
	if err != nil {
		return Session{}, err
	}
	if !verifyPassword(record.PasswordHash, password) {
		return Session{}, ErrInvalidCredentials
	}
	token, tokenHash, err := newSessionToken()
	if err != nil {
		return Session{}, err
	}
	createdAt := service.now().UTC()
	expiresAt := createdAt.Add(sessionDuration)
	if err = service.store.InsertSession(ctx, tokenHash, record.ID, createdAt, expiresAt); err != nil {
		return Session{}, err
	}
	return Session{Token: token, ExpiresAt: expiresAt}, nil
}

// Authenticate returns the account of one session token.
func (service *Service) Authenticate(ctx context.Context, token string) (Account, error) {
	tokenHash, ok := hashToken(token)
	if !ok {
		return Account{}, ErrInvalidSession
	}
	account, err := service.store.AccountBySession(ctx, tokenHash, service.now().UTC())
	if errors.Is(err, ErrUnknownAccount) {
		return Account{}, ErrInvalidSession
	}
	if err != nil {
		return Account{}, err
	}
	return account, nil
}

// Logout removes one session. An unknown token is a no-op.
func (service *Service) Logout(ctx context.Context, token string) error {
	tokenHash, ok := hashToken(token)
	if !ok {
		return nil
	}
	return service.store.DeleteSession(ctx, tokenHash)
}

// ProvisionTOTP creates one TOTP secret for the account and returns its
// otpauth URL.
func (service *Service) ProvisionTOTP(ctx context.Context, accountID uuid.UUID) (TOTPProvisioning, error) {
	record, err := service.store.AccountByID(ctx, accountID)
	if errors.Is(err, ErrUnknownAccount) {
		return TOTPProvisioning{}, ErrInvalidSession
	}
	if err != nil {
		return TOTPProvisioning{}, err
	}
	if record.TOTPConfirmed {
		return TOTPProvisioning{}, ErrTOTPConfirmed
	}
	raw := make([]byte, totpSecretBytes)
	if _, err = rand.Read(raw); err != nil {
		return TOTPProvisioning{}, fmt.Errorf("generate totp secret: %w", err)
	}
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
	if err = service.store.SetTOTPSecret(ctx, accountID, secret); err != nil {
		return TOTPProvisioning{}, err
	}
	return TOTPProvisioning{Secret: secret, OtpauthURL: otpauthURL(record.Email, secret)}, nil
}

// ConfirmTOTP checks one TOTP code and marks the secret as confirmed.
func (service *Service) ConfirmTOTP(ctx context.Context, accountID uuid.UUID, code string) error {
	record, err := service.store.AccountByID(ctx, accountID)
	if errors.Is(err, ErrUnknownAccount) {
		return ErrInvalidSession
	}
	if err != nil {
		return err
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(record.TOTPSecret)
	if err != nil || len(secret) == 0 {
		return ErrInvalidCode
	}
	if !verifyTOTP(secret, code, service.now().UTC()) {
		return ErrInvalidCode
	}
	return service.store.ConfirmTOTP(ctx, accountID)
}

func validEmail(email string) bool {
	return email != "" && strings.IndexByte(email, '@') > 0
}

func validPassword(password string) bool {
	return len(password) >= minPasswordLength && len(password) <= maxPasswordLength
}

func hashPassword(password string, iterations int) (string, error) {
	salt := make([]byte, passwordSaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, iterations, passwordKeyBytes)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", iterations,
		base64.StdEncoding.EncodeToString(salt),
		base64.StdEncoding.EncodeToString(key)), nil
}

func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations <= 0 {
		return false
	}
	salt, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	want, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iterations, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

func newSessionToken() (string, []byte, error) {
	raw := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate session token: %w", err)
	}
	hash := sha256.Sum256(raw)
	return base64.RawURLEncoding.EncodeToString(raw), hash[:], nil
}

func hashToken(token string) ([]byte, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != sessionTokenBytes {
		return nil, false
	}
	hash := sha256.Sum256(raw)
	return hash[:], true
}

func verifyTOTP(secret []byte, code string, at time.Time) bool {
	if len(code) != totpDigits {
		return false
	}
	for offset := -1; offset <= 1; offset++ {
		window := at.Add(time.Duration(offset) * totpPeriodSeconds * time.Second)
		if subtle.ConstantTimeCompare([]byte(totpCodeAt(secret, window, totpDigits)), []byte(code)) == 1 {
			return true
		}
	}
	return false
}

func totpCodeAt(secret []byte, at time.Time, digits int) string {
	return totpCode(secret, uint64(at.Unix()/totpPeriodSeconds), digits)
}

func totpCode(secret []byte, counter uint64, digits int) string {
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], counter)
	mac := hmac.New(sha1.New, secret)
	mac.Write(message[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	modulo := uint32(1)
	for index := 0; index < digits; index++ {
		modulo *= 10
	}
	return fmt.Sprintf("%0*d", digits, value%modulo)
}

func otpauthURL(email, secret string) string {
	label := url.PathEscape(issuerName + ":" + email)
	return "otpauth://totp/" + label + "?secret=" + secret + "&issuer=" + url.QueryEscape(issuerName)
}
