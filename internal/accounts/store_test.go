package accounts

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

func openStore(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func registered(t *testing.T, s *Store, id string) Login {
	t.Helper()
	login, err := s.Register(t.Context(), id, "a long password")
	if err != nil {
		t.Fatal(err)
	}
	return login
}

func TestAccountsAndIndependentLoginsSurviveRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data #1", "accounts.sqlite")
	s := openStore(t, path)
	first := registered(t, s, "Player-One")
	second := registered(t, s, "Player_Two")
	if first.User.ID == second.User.ID || !regexp.MustCompile("^[a-f0-9]{32}$").MatchString(first.User.ID) {
		t.Fatal("accounts must have distinct, path-safe server IDs")
	}
	if first.User.Legacy || second.User.Legacy {
		t.Fatal("registration assigned the personal saves")
	}
	if _, err := s.Register(t.Context(), first.User.LoginID, "other password"); !errors.Is(err, ErrIDTaken) {
		t.Fatalf("duplicate registration: %v", err)
	}
	if _, err := s.Login(t.Context(), first.User.LoginID, "wrong password"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("incorrect password: %v", err)
	}
	if _, err := s.Login(t.Context(), "unknown", "wrong password"); !errors.Is(err, ErrIDNotFound) {
		t.Fatalf("unknown login ID: %v", err)
	}
	device, err := s.Login(t.Context(), first.User.LoginID, "a long password")
	if err != nil || device.User != first.User || device.Token == first.Token {
		t.Fatalf("second device login: %v", err)
	}
	if lifetime := time.Until(device.Expires); lifetime > 30*24*time.Hour || lifetime < 30*24*time.Hour-time.Minute {
		t.Fatalf("unexpected login lifetime: %v", lifetime)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openStore(t, path)
	for _, issued := range []Login{first, second, device} {
		got, err := s.Lookup(t.Context(), issued.Token)
		if err != nil || got.User != issued.User || !got.Expires.Equal(issued.Expires) {
			t.Fatalf("lookup after restart: %v", err)
		}
	}
	if err := s.Revoke(t.Context(), first.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Lookup(t.Context(), first.Token); !errors.Is(err, ErrCredentials) {
		t.Fatalf("revoked login: %v", err)
	}
	if _, err := s.Lookup(t.Context(), device.Token); err != nil {
		t.Fatalf("logout revoked the other device: %v", err)
	}
	digest := sha256.Sum256([]byte(device.Token))
	if _, err := s.db.Exec("UPDATE login_sessions SET expires_at = 0 WHERE token_digest = ?", digest[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Lookup(t.Context(), device.Token); !errors.Is(err, ErrCredentials) {
		t.Fatalf("expired login: %v", err)
	}
	if _, err := s.Lookup(t.Context(), second.Token); err != nil {
		t.Fatalf("the second player's login changed: %v", err)
	}
}

func TestPasswordHashesAndLoginTokensAtRest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.sqlite")
	s := openStore(t, path)
	one := registered(t, s, "one")
	two := registered(t, s, "two")
	if !regexp.MustCompile("^[a-f0-9]{64}$").MatchString(one.Token) || one.Token == two.Token {
		t.Fatal("logins must use independent random tokens")
	}
	type stored struct {
		algorithm string
		rounds    int
		salt, key []byte
	}
	read := func(user User) (r stored) {
		t.Helper()
		if err := s.db.QueryRow("SELECT algorithm, iterations, salt, password_hash FROM users WHERE id = ?", user.ID).
			Scan(&r.algorithm, &r.rounds, &r.salt, &r.key); err != nil {
			t.Fatal(err)
		}
		return r
	}
	first, second := read(one.User), read(two.User)
	if first.algorithm != "pbkdf2-sha256" || first.rounds < 600000 || len(first.salt) < 16 || len(first.key) != sha256.Size {
		t.Fatal("incomplete adaptive password hash")
	}
	if bytes.Equal(first.salt, second.salt) || bytes.Equal(first.key, second.key) {
		t.Fatal("the same password shared stored material across accounts")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("a long password")) || bytes.Contains(data, []byte(one.Token)) {
		t.Fatal("database contains a raw credential")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0 {
		t.Fatalf("account file permissions: %o", info.Mode().Perm())
	}
}

func TestPersonalSaveBindingIsExplicitExclusiveAndRepeatable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.sqlite")
	s := openStore(t, path)
	first := registered(t, s, "first")
	owner := registered(t, s, "owner")
	if err := s.BindLegacy(t.Context(), "missing"); err == nil {
		t.Fatal("bound an unknown login")
	}
	if err := s.BindLegacy(t.Context(), owner.User.LoginID); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = openStore(t, path)
	if err := s.BindLegacy(t.Context(), owner.User.LoginID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if err := s.BindLegacy(t.Context(), first.User.LoginID); err == nil {
		t.Fatal("reassigned the personal saves")
	}
	for _, login := range []Login{first, owner} {
		got, err := s.Lookup(t.Context(), login.Token)
		if err != nil || got.User.ID != login.User.ID || got.User.Legacy != (login.User.ID == owner.User.ID) {
			t.Fatalf("wrong legacy ownership for %s: %v", login.User.LoginID, err)
		}
	}
}

func TestRegistrationRollsBackWhenLoginCannotBeCreated(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "accounts.sqlite"))
	if _, err := s.db.Exec("CREATE TRIGGER deny_login BEFORE INSERT ON login_sessions BEGIN SELECT RAISE(ABORT, 'test'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Register(t.Context(), "first", "long password"); err == nil {
		t.Fatal("reported a registration without a login")
	}
	if _, err := s.db.Exec("DROP TRIGGER deny_login"); err != nil {
		t.Fatal(err)
	}
	registered(t, s, "first")
}

func TestOpenRejectsBrokenStore(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "accounts.sqlite")
	if err := os.WriteFile(path, []byte("damaged account database"), 0600); err != nil {
		t.Fatal(err)
	}
	if s, err := Open(path); err == nil {
		s.Close()
		t.Fatal("opened a damaged database")
	}
	if s, err := Open(root); err == nil {
		s.Close()
		t.Fatal("opened a directory as the account file")
	}
}

func TestRegistrationCountsUnicodeCharactersAndPreservesSpacesInPasswords(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "accounts.sqlite"))
	// A Hangul letter and an astral character require different byte and UTF-16 lengths.
	for _, credentials := range []struct{ id, password string }{
		{strings.Repeat("\uac00", 3), strings.Repeat("\U0001f642", 8)},
		{strings.Repeat("\uac00", 20), " " + strings.Repeat("\U0001f642", 30) + " "},
	} {
		login, err := s.Register(t.Context(), credentials.id, credentials.password)
		if err != nil {
			t.Fatal(err)
		}
		again, err := s.Login(t.Context(), credentials.id, credentials.password)
		if err != nil || again.User.ID != login.User.ID {
			t.Fatalf("cannot use registered credentials: %v", err)
		}
		if trimmed := strings.TrimSpace(credentials.password); trimmed != credentials.password {
			if _, err := s.Login(t.Context(), credentials.id, trimmed); !errors.Is(err, ErrWrongPassword) {
				t.Fatalf("trimmed password: %v", err)
			}
		}
	}
}

func TestEarlierCredentialsStillSignInAndBindPersonalSaves(t *testing.T) {
	s := openStore(t, filepath.Join(t.TempDir(), "accounts.sqlite"))
	long := strings.Repeat("x", 128)
	h := passwordHash{name: algorithm, iterations: iterations, salt: make([]byte, saltBytes)}
	var err error
	h.key, err = s.derive(t.Context(), long, h)
	if err != nil {
		t.Fatal(err)
	}
	oldID := "old player " + strings.Repeat("x", 53)
	for _, id := range []string{"a", oldID} {
		user := User{ID: randomHex(16), LoginID: id}
		if _, err := s.issue(t.Context(), user, &h); err != nil {
			t.Fatal(err)
		}
		if again, err := s.Login(t.Context(), id, long); err != nil || again.User.ID != user.ID {
			t.Fatalf("existing login: %v", err)
		}
		if _, err := s.Login(t.Context(), id, long[:32]); !errors.Is(err, ErrWrongPassword) {
			t.Fatalf("truncated password: %v", err)
		}
	}
	if err := s.BindLegacy(t.Context(), oldID); err != nil {
		t.Fatalf("existing owner binding: %v", err)
	}
}

func TestOpenRequiresAWritableAccountFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.sqlite")
	s := openStore(t, path)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0600) })
	if opened, err := Open(path); err == nil {
		opened.Close()
		t.Fatal("opened a read-only account store")
	}
}

func TestOpenRejectsAnIncompleteAccountSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.sqlite")
	s := openStore(t, path)
	if _, err := s.db.Exec("ALTER TABLE users RENAME COLUMN salt TO wrong_column"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if opened, err := Open(path); err == nil {
		opened.Close()
		t.Fatal("startup accepted a store that cannot verify credentials")
	}
}

func TestOpenLeavesAnUnrelatedDatabaseAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "accounts.sqlite")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE existing (data TEXT); INSERT INTO existing VALUES ('keep')"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	oldInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if opened, err := Open(path); err == nil {
		opened.Close()
		t.Fatal("adopted an unrelated database")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("failed startup changed the database: %v", err)
	}
	newInfo, err := os.Stat(path)
	if err != nil || newInfo.Mode().Perm() != oldInfo.Mode().Perm() {
		t.Fatalf("failed startup changed database permissions: %v", err)
	}
}
