// Package accounts keeps the shared host's users and revocable logins in SQLite.
package accounts

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/driver"
)

var (
	ErrCredentials      = errors.New("missing or expired login session")
	ErrInvalidInput     = errors.New("invalid account input")
	ErrIDRequired       = errors.New("enter a login ID")
	ErrIDTooShort       = errors.New("login ID too short")
	ErrIDTooLong        = errors.New("login ID too long")
	ErrIDWhitespace     = errors.New("login ID contains whitespace")
	ErrIDCharacters     = errors.New("login ID contains unsupported characters")
	ErrIDTaken          = errors.New("login ID already registered")
	ErrIDNotFound       = errors.New("no account with that login ID")
	ErrPasswordRequired = errors.New("enter a password")
	ErrPasswordTooShort = errors.New("password too short")
	ErrPasswordTooLong  = errors.New("password too long")
	ErrPasswordBlank    = errors.New("password contains only whitespace")
	ErrWrongPassword    = errors.New("incorrect password")
)

// Registration limits count Unicode code points. Existing logins keep the
// credentials accepted when their account was created.
const (
	MinIDLength       = 3
	MaxIDLength       = 20
	MinPasswordLength = 8
	MaxPasswordLength = 32
)

const (
	algorithm  = "pbkdf2-sha256"
	iterations = 600_000
	saltBytes  = 16
	tokenBytes = 32
	loginLife  = 30 * 24 * time.Hour
)

type User struct {
	ID      string // Immutable server ID used for save ownership.
	LoginID string
	Legacy  bool
}

type Login struct {
	User    User
	Token   string
	Expires time.Time
}

type Store struct {
	db        *sql.DB
	hashSlots chan struct{}
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
	id TEXT PRIMARY KEY CHECK (length(id) = 32 AND id NOT GLOB '*[^0-9a-f]*'),
	login_id TEXT NOT NULL UNIQUE,
	algorithm TEXT NOT NULL,
	iterations INTEGER NOT NULL,
	salt BLOB NOT NULL,
	password_hash BLOB NOT NULL,
	legacy_owner INTEGER NOT NULL DEFAULT 0 CHECK (legacy_owner IN (0, 1))
) STRICT;
CREATE UNIQUE INDEX IF NOT EXISTS users_one_legacy_owner ON users(legacy_owner) WHERE legacy_owner = 1;
CREATE TABLE IF NOT EXISTS login_sessions (
	token_digest BLOB PRIMARY KEY CHECK (length(token_digest) = 32),
	user_id TEXT NOT NULL REFERENCES users(id),
	expires_at INTEGER NOT NULL
) STRICT;
CREATE INDEX IF NOT EXISTS login_expiry ON login_sessions(expires_at);
PRAGMA user_version = 1;`

const credentialsQuery = `SELECT id, algorithm, iterations, salt, password_hash
	FROM users WHERE login_id = ?`
const sessionQuery = `SELECT users.id, login_id, legacy_owner, expires_at
	FROM login_sessions JOIN users ON users.id = login_sessions.user_id WHERE token_digest = ?`

// Open verifies a writable database before the shared host accepts requests.
func Open(path string) (*Store, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	// Respect a read-only installation even when the process may override
	// file permissions.
	if !info.Mode().IsRegular() || info.Mode().Perm()&0600 != 0600 {
		return nil, errors.New("account store requires a readable, writable regular file")
	}
	db, err := driver.Open(path, func(conn *sqlite3.Conn) error {
		return conn.Exec("PRAGMA busy_timeout = 5000; PRAGMA foreign_keys = ON")
	})
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := initialize(db, file); err != nil {
		db.Close()
		return nil, fmt.Errorf("open account database: %w", err)
	}
	return &Store{db: db, hashSlots: make(chan struct{}, 4)}, nil
}

func initialize(db *sql.DB, file *os.File) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err := tx.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	switch version {
	case 0:
		var nonempty bool
		if err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM sqlite_schema)").Scan(&nonempty); err != nil {
			return err
		}
		if nonempty {
			return errors.New("existing database is not an account store")
		}
	case 1:
		// quick_check validates SQLite pages. Preparing the queries also
		// checks that the expected account and login columns are present.
		for _, query := range []string{credentialsQuery, sessionQuery} {
			stmt, err := tx.Prepare(query)
			if err != nil {
				return err
			}
			if err := stmt.Close(); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unsupported account database version %d", version)
	}
	var check string
	if err := tx.QueryRow("PRAGMA quick_check(1)").Scan(&check); err != nil {
		return err
	}
	if check != "ok" {
		return errors.New("account database failed its integrity check")
	}
	// Only a recognized or empty database reaches this point. Limit access
	// before the first write, as SQLite derives journal permissions from it.
	if err := file.Chmod(0600); err != nil {
		return err
	}
	if _, err := tx.Exec(schema); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Close() error { return s.db.Close() }

// Older accounts could use a 1–64 character ID, including internal spaces.
func validStoredID(id string) bool {
	return len(id) > 0 && len(id) <= 64*utf8.UTFMax && utf8.ValidString(id) &&
		utf8.RuneCountInString(id) <= 64 && id == strings.TrimSpace(id) &&
		strings.IndexFunc(id, unicode.IsControl) < 0
}

func validateRegistration(id, password string) error {
	idLength := utf8.RuneCountInString(id)
	switch {
	case strings.TrimSpace(id) == "":
		return ErrIDRequired
	case idLength < MinIDLength:
		return ErrIDTooShort
	case idLength > MaxIDLength:
		return ErrIDTooLong
	}
	for _, c := range id {
		if unicode.IsSpace(c) {
			return ErrIDWhitespace
		}
		if !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '_' && c != '-' {
			return ErrIDCharacters
		}
	}
	passwordLength := utf8.RuneCountInString(password)
	switch {
	case password == "":
		return ErrPasswordRequired
	case !utf8.ValidString(password):
		return ErrInvalidInput
	case strings.TrimSpace(password) == "":
		return ErrPasswordBlank
	case passwordLength < MinPasswordLength:
		return ErrPasswordTooShort
	case passwordLength > MaxPasswordLength:
		return ErrPasswordTooLong
	}
	return nil
}

type passwordHash struct {
	name       string
	iterations int
	salt, key  []byte
}

func (s *Store) derive(ctx context.Context, password string, h passwordHash) ([]byte, error) {
	if h.name != algorithm || h.iterations < 1 || h.iterations > 4*iterations || len(h.salt) != saltBytes {
		return nil, errors.New("unsupported stored password hash")
	}
	// Password verification costs CPU. Wait for it outside every database
	// transaction so checks on existing logins and logout can always proceed.
	select {
	case s.hashSlots <- struct{}{}:
		defer func() { <-s.hashSlots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return pbkdf2.Key(sha256.New, password, h.salt, h.iterations, sha256.Size)
}

func randomHex(n int) string {
	raw := make([]byte, n)
	rand.Read(raw)
	return hex.EncodeToString(raw)
}

// Register creates an ordinary user and its first login in the same transaction.
func (s *Store) Register(ctx context.Context, id, password string) (Login, error) {
	if err := validateRegistration(id, password); err != nil {
		return Login{}, err
	}
	h := passwordHash{name: algorithm, iterations: iterations, salt: make([]byte, saltBytes)}
	rand.Read(h.salt)
	var err error
	h.key, err = s.derive(ctx, password, h)
	if err != nil {
		return Login{}, err
	}
	return s.issue(ctx, User{ID: randomHex(16), LoginID: id}, &h)
}

func (s *Store) Login(ctx context.Context, id, password string) (Login, error) {
	switch {
	case strings.TrimSpace(id) == "":
		return Login{}, ErrIDRequired
	case password == "":
		return Login{}, ErrPasswordRequired
	case !validStoredID(id):
		return Login{}, ErrIDNotFound
	case len(password) > 128*utf8.UTFMax || !utf8.ValidString(password):
		return Login{}, ErrWrongPassword
	}
	user := User{LoginID: id}
	var h passwordHash
	err := s.db.QueryRowContext(ctx, credentialsQuery, id).
		Scan(&user.ID, &h.name, &h.iterations, &h.salt, &h.key)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// An unknown login incurs the same password derivation as a wrong one.
		h = passwordHash{name: algorithm, iterations: iterations, salt: make([]byte, saltBytes), key: make([]byte, sha256.Size)}
	case err != nil:
		return Login{}, err
	case len(h.key) != sha256.Size:
		return Login{}, errors.New("invalid stored password hash length")
	}
	key, err := s.derive(ctx, password, h)
	if err != nil {
		return Login{}, err
	}
	match := subtle.ConstantTimeCompare(key, h.key)
	if user.ID == "" {
		return Login{}, ErrIDNotFound
	}
	if match != 1 {
		return Login{}, ErrWrongPassword
	}
	return s.issue(ctx, user, nil)
}

func (s *Store) issue(ctx context.Context, user User, newPassword *passwordHash) (Login, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Login{}, err
	}
	defer tx.Rollback()
	if h := newPassword; h != nil {
		_, err = tx.ExecContext(ctx, `INSERT INTO users (id, login_id, algorithm, iterations, salt, password_hash)
			VALUES (?, ?, ?, ?, ?, ?)`, user.ID, user.LoginID, h.name, h.iterations, h.salt, h.key)
		if errors.Is(err, sqlite3.CONSTRAINT_UNIQUE) {
			return Login{}, ErrIDTaken
		}
		if err != nil {
			return Login{}, err
		}
	} else if err := tx.QueryRowContext(ctx, "SELECT legacy_owner FROM users WHERE id = ?", user.ID).Scan(&user.Legacy); err != nil {
		return Login{}, err
	}
	now := time.Now().UTC().Truncate(time.Second)
	login := Login{User: user, Token: randomHex(tokenBytes), Expires: now.Add(loginLife)}
	digest := sha256.Sum256([]byte(login.Token))
	if _, err := tx.ExecContext(ctx, "DELETE FROM login_sessions WHERE expires_at <= ?", now.Unix()); err != nil {
		return Login{}, err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO login_sessions (token_digest, user_id, expires_at) VALUES (?, ?, ?)",
		digest[:], user.ID, login.Expires.Unix()); err != nil {
		return Login{}, err
	}
	if err := tx.Commit(); err != nil {
		return Login{}, err
	}
	return login, nil
}

func (s *Store) Lookup(ctx context.Context, token string) (Login, error) {
	if len(token) != hex.EncodedLen(tokenBytes) {
		return Login{}, ErrCredentials
	}
	digest := sha256.Sum256([]byte(token))
	var user User
	var expires int64
	err := s.db.QueryRowContext(ctx, sessionQuery, digest[:]).
		Scan(&user.ID, &user.LoginID, &user.Legacy, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return Login{}, ErrCredentials
	}
	if err != nil {
		return Login{}, err
	}
	if expires <= time.Now().Unix() {
		return Login{}, ErrCredentials
	}
	return Login{User: user, Token: token, Expires: time.Unix(expires, 0).UTC()}, nil
}

func (s *Store) Revoke(ctx context.Context, token string) error {
	if len(token) != hex.EncodedLen(tokenBytes) {
		return nil
	}
	digest := sha256.Sum256([]byte(token))
	_, err := s.db.ExecContext(ctx, "DELETE FROM login_sessions WHERE token_digest = ?", digest[:])
	return err
}

// BindLegacy gives one existing account the personal save tree. The partial
// unique index enforces the choice across concurrent setup commands.
func (s *Store) BindLegacy(ctx context.Context, loginID string) error {
	if !validStoredID(loginID) {
		return ErrIDNotFound
	}
	result, err := s.db.ExecContext(ctx, "UPDATE users SET legacy_owner = 1 WHERE login_id = ?", loginID)
	if errors.Is(err, sqlite3.CONSTRAINT_UNIQUE) {
		return errors.New("the personal saves already belong to another account")
	}
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count == 0 {
		return ErrIDNotFound
	}
	return err
}
