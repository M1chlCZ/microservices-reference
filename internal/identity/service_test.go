package identity

import (
	"context"
	"encoding/base32"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

const testPassword = "long-enough-password"

type fakeStore struct {
	accounts map[string]Record
	byID     map[uuid.UUID]Record
	sessions map[string]uuid.UUID
	expires  map[string]time.Time
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		accounts: map[string]Record{},
		byID:     map[uuid.UUID]Record{},
		sessions: map[string]uuid.UUID{},
		expires:  map[string]time.Time{},
	}
}

func (store *fakeStore) InsertAccount(_ context.Context, account Account, passwordHash string) error {
	if _, ok := store.accounts[account.Email]; ok {
		return ErrEmailTaken
	}
	record := Record{Account: account, PasswordHash: passwordHash}
	store.accounts[account.Email] = record
	store.byID[account.ID] = record
	return nil
}

func (store *fakeStore) AccountByEmail(_ context.Context, email string) (Record, error) {
	record, ok := store.accounts[email]
	if !ok {
		return Record{}, ErrUnknownAccount
	}
	return record, nil
}

func (store *fakeStore) AccountByID(_ context.Context, id uuid.UUID) (Record, error) {
	record, ok := store.byID[id]
	if !ok {
		return Record{}, ErrUnknownAccount
	}
	return record, nil
}

func (store *fakeStore) InsertSession(_ context.Context, tokenHash []byte, accountID uuid.UUID, _ time.Time, expiresAt time.Time) error {
	key := string(tokenHash)
	store.sessions[key] = accountID
	store.expires[key] = expiresAt
	return nil
}

func (store *fakeStore) AccountBySession(_ context.Context, tokenHash []byte, at time.Time) (Account, error) {
	key := string(tokenHash)
	accountID, ok := store.sessions[key]
	if !ok || !store.expires[key].After(at) {
		return Account{}, ErrInvalidSession
	}
	record, ok := store.byID[accountID]
	if !ok {
		return Account{}, ErrInvalidSession
	}
	return record.Account, nil
}

func (store *fakeStore) DeleteSession(_ context.Context, tokenHash []byte) error {
	key := string(tokenHash)
	delete(store.sessions, key)
	delete(store.expires, key)
	return nil
}

func (store *fakeStore) SetTOTPSecret(_ context.Context, accountID uuid.UUID, secret string) error {
	record, ok := store.byID[accountID]
	if !ok {
		return ErrUnknownAccount
	}
	record.TOTPSecret = secret
	store.byID[accountID] = record
	store.accounts[record.Email] = record
	return nil
}

func (store *fakeStore) ConfirmTOTP(_ context.Context, accountID uuid.UUID) error {
	record, ok := store.byID[accountID]
	if !ok {
		return ErrUnknownAccount
	}
	record.TOTPConfirmed = true
	store.byID[accountID] = record
	store.accounts[record.Email] = record
	return nil
}

const testPasswordIterations = 1000

func newTestApp(store *fakeStore) (*fiber.App, *Service) {
	service := NewService(store)
	service.passwordIterations = testPasswordIterations
	app := fiber.New()
	RegisterRoutes(app, service)
	return app, service
}

func doRequest(t *testing.T, app *fiber.App, method, path, body string) (*http.Response, string) {
	return doAuthRequest(t, app, method, path, body, "")
}

func doAuthRequest(t *testing.T, app *fiber.App, method, path, body, token string) (*http.Response, string) {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response, string(payload)
}

func createSession(t *testing.T, app *fiber.App, email, password string) string {
	t.Helper()
	response, body := doRequest(t, app, http.MethodPost, "/accounts",
		`{"email":"`+email+`","password":"`+password+`"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create account: %d %s", response.StatusCode, body)
	}
	response, body = doRequest(t, app, http.MethodPost, "/sessions",
		`{"email":"`+email+`","password":"`+password+`"}`)
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("login: %d %s", response.StatusCode, body)
	}
	var session Session
	if err := json.Unmarshal([]byte(body), &session); err != nil {
		t.Fatal(err)
	}
	if session.Token == "" {
		t.Fatal("login returned no token")
	}
	return session.Token
}

func TestPasswordHashRoundTrip(t *testing.T) {
	t.Parallel()
	encoded, err := hashPassword(testPassword, testPasswordIterations)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encoded, "pbkdf2-sha256$1000$") {
		t.Fatalf("encoded form: %s", encoded)
	}
	if !verifyPassword(encoded, testPassword) {
		t.Fatal("correct password rejected")
	}
	if verifyPassword(encoded, "a-different-password") {
		t.Fatal("wrong password accepted")
	}
}

func TestTOTPMatchesRFC6238(t *testing.T) {
	t.Parallel()
	// RFC 6238 defines this test vector with an ASCII secret, not base32.
	secret := []byte("12345678901234567890")
	at := time.Unix(59, 0).UTC()
	if code := totpCodeAt(secret, at, 8); code != "94287082" {
		t.Fatalf("rfc 6238 code: %s", code)
	}
	if code := totpCodeAt(secret, at, totpDigits); code != "287082" {
		t.Fatalf("six digit code: %s", code)
	}
}

func TestVerifyTOTPWindow(t *testing.T) {
	t.Parallel()
	secret := []byte("12345678901234567890")
	at := time.Unix(6000, 0).UTC()
	for _, offset := range []int{-1, 0, 1} {
		window := at.Add(time.Duration(offset) * totpPeriodSeconds * time.Second)
		if !verifyTOTP(secret, totpCodeAt(secret, window, totpDigits), at) {
			t.Fatalf("offset %d rejected", offset)
		}
	}
	stale := totpCodeAt(secret, at.Add(-2*totpPeriodSeconds*time.Second), totpDigits)
	if verifyTOTP(secret, stale, at) {
		t.Fatal("stale code accepted")
	}
	if verifyTOTP(secret, "12345", at) {
		t.Fatal("short code accepted")
	}
}

func TestAccountSessionFlow(t *testing.T) {
	t.Parallel()
	app, _ := newTestApp(newFakeStore())
	token := createSession(t, app, "ada@example.com", testPassword)

	response, body := doAuthRequest(t, app, http.MethodGet, "/sessions/me", "", token)
	if response.StatusCode != http.StatusOK || !strings.Contains(body, `"email":"ada@example.com"`) {
		t.Fatalf("me: %d %s", response.StatusCode, body)
	}
	response, _ = doAuthRequest(t, app, http.MethodDelete, "/sessions", "", token)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("logout: %d", response.StatusCode)
	}
	response, _ = doAuthRequest(t, app, http.MethodGet, "/sessions/me", "", token)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("me after logout: %d", response.StatusCode)
	}
}

func TestCreateAccountRejections(t *testing.T) {
	t.Parallel()
	app, _ := newTestApp(newFakeStore())
	doRequest(t, app, http.MethodPost, "/accounts",
		`{"email":"ada@example.com","password":"`+testPassword+`"}`)

	longPassword := strings.Repeat("a", 201)
	cases := []struct {
		name string
		body string
		want int
	}{
		{"duplicate email", `{"email":"ada@example.com","password":"` + testPassword + `"}`, http.StatusConflict},
		{"malformed email", `{"email":"not-an-email","password":"` + testPassword + `"}`, http.StatusUnprocessableEntity},
		{"short password", `{"email":"b@example.com","password":"short"}`, http.StatusUnprocessableEntity},
		{"long password", `{"email":"c@example.com","password":"` + longPassword + `"}`, http.StatusUnprocessableEntity},
		{"invalid body", `{"email":`, http.StatusUnprocessableEntity},
	}
	for _, testCase := range cases {
		response, body := doRequest(t, app, http.MethodPost, "/accounts", testCase.body)
		if response.StatusCode != testCase.want {
			t.Fatalf("%s: %d %s", testCase.name, response.StatusCode, body)
		}
	}
}

func TestLoginRejections(t *testing.T) {
	t.Parallel()
	app, _ := newTestApp(newFakeStore())
	doRequest(t, app, http.MethodPost, "/accounts",
		`{"email":"ada@example.com","password":"`+testPassword+`"}`)

	cases := []struct {
		name string
		body string
		want int
	}{
		{"unknown email", `{"email":"nobody@example.com","password":"` + testPassword + `"}`, http.StatusUnauthorized},
		{"wrong password", `{"email":"ada@example.com","password":"a-different-password"}`, http.StatusUnauthorized},
	}
	for _, testCase := range cases {
		response, body := doRequest(t, app, http.MethodPost, "/sessions", testCase.body)
		if response.StatusCode != testCase.want {
			t.Fatalf("%s: %d %s", testCase.name, response.StatusCode, body)
		}
	}
}

func TestSessionUnauthorizedPaths(t *testing.T) {
	t.Parallel()
	app, service := newTestApp(newFakeStore())
	token := createSession(t, app, "ada@example.com", testPassword)

	cases := []struct {
		name   string
		method string
		path   string
		token  string
		want   int
	}{
		{"me without token", http.MethodGet, "/sessions/me", "", http.StatusUnauthorized},
		{"me unknown token", http.MethodGet, "/sessions/me", "unknown-token", http.StatusUnauthorized},
		{"logout unknown token", http.MethodDelete, "/sessions", "unknown-token", http.StatusNoContent},
		{"totp without token", http.MethodPost, "/accounts/totp", "", http.StatusUnauthorized},
		{"verify without token", http.MethodPost, "/accounts/totp/verify", "", http.StatusUnauthorized},
	}
	for _, testCase := range cases {
		response, body := doAuthRequest(t, app, testCase.method, testCase.path, "{}", testCase.token)
		if response.StatusCode != testCase.want {
			t.Fatalf("%s: %d %s", testCase.name, response.StatusCode, body)
		}
	}

	service.now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	response, body := doAuthRequest(t, app, http.MethodGet, "/sessions/me", "", token)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("me expired token: %d %s", response.StatusCode, body)
	}
}

func TestTOTPFlow(t *testing.T) {
	t.Parallel()
	app, service := newTestApp(newFakeStore())
	service.now = func() time.Time { return time.Unix(59, 0).UTC() }
	token := createSession(t, app, "ada@example.com", testPassword)

	response, body := doAuthRequest(t, app, http.MethodPost, "/accounts/totp", "", token)
	if response.StatusCode != http.StatusCreated || !strings.Contains(body, "otpauth://totp/") {
		t.Fatalf("provision: %d %s", response.StatusCode, body)
	}
	var provisioning TOTPProvisioning
	if err := json.Unmarshal([]byte(body), &provisioning); err != nil {
		t.Fatal(err)
	}

	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(provisioning.Secret)
	if err != nil {
		t.Fatal(err)
	}
	validCode := totpCodeAt(secret, service.now(), totpDigits)
	wrongCode := []byte(validCode)
	if wrongCode[0] == '0' {
		wrongCode[0] = '1'
	} else {
		wrongCode[0] = '0'
	}
	response, body = doAuthRequest(t, app, http.MethodPost, "/accounts/totp/verify",
		`{"code":"`+string(wrongCode)+`"}`, token)
	if response.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("wrong code: %d %s", response.StatusCode, body)
	}

	response, body = doAuthRequest(t, app, http.MethodPost, "/accounts/totp/verify",
		`{"code":"`+validCode+`"}`, token)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("verify: %d %s", response.StatusCode, body)
	}
	response, body = doAuthRequest(t, app, http.MethodPost, "/accounts/totp", "", token)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("provision confirmed: %d %s", response.StatusCode, body)
	}
}
