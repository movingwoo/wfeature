package webhost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/movingwoo/wfeature/internal/accounts"
)

const accountCookie = "__Host-wfeature_login"

// Expand the signup limits once when the text catalog loads, so the form and
// API errors both describe the policy enforced by Register.
var registrationLimits = []string{
	"idMin", strconv.Itoa(accounts.MinIDLength), "idMax", strconv.Itoa(accounts.MaxIDLength),
	"passwordMin", strconv.Itoa(accounts.MinPasswordLength), "passwordMax", strconv.Itoa(accounts.MaxPasswordLength),
}

func loginToken(r *http.Request) string {
	cookie, err := r.Cookie(accountCookie)
	if err != nil {
		return ""
	}
	return cookie.Value
}

func setLogin(w http.ResponseWriter, token string, expires time.Time) {
	cookie := &http.Cookie{Name: accountCookie, Path: "/", Value: token, Expires: expires,
		HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode}
	if token == "" {
		cookie.MaxAge = -1
	}
	http.SetCookie(w, cookie)
}

var accountErrors = []struct {
	cause  error
	status int
	key    string
}{
	{accounts.ErrCredentials, http.StatusUnauthorized, "account.login-required"},
	{accounts.ErrIDNotFound, http.StatusUnauthorized, "account.id-not-found"},
	{accounts.ErrWrongPassword, http.StatusUnauthorized, "account.wrong-password"},
	{accounts.ErrIDTaken, http.StatusConflict, "account.id-taken"},
	{accounts.ErrIDRequired, http.StatusBadRequest, "account.id-required"},
	{accounts.ErrIDTooShort, http.StatusBadRequest, "account.id-too-short"},
	{accounts.ErrIDTooLong, http.StatusBadRequest, "account.id-too-long"},
	{accounts.ErrIDWhitespace, http.StatusBadRequest, "account.id-whitespace"},
	{accounts.ErrIDCharacters, http.StatusBadRequest, "account.id-characters"},
	{accounts.ErrPasswordRequired, http.StatusBadRequest, "account.password-required"},
	{accounts.ErrPasswordTooShort, http.StatusBadRequest, "account.password-too-short"},
	{accounts.ErrPasswordTooLong, http.StatusBadRequest, "account.password-too-long"},
	{accounts.ErrPasswordBlank, http.StatusBadRequest, "account.password-blank"},
	{accounts.ErrInvalidInput, http.StatusBadRequest, "account.invalid-input"},
}

func (s *Server) accountError(w http.ResponseWriter, err error) {
	for _, problem := range accountErrors {
		if errors.Is(err, problem.cause) {
			writeError(w, problem.status, s.phrase(problem.key))
			return
		}
	}
	s.logger.Error("account store request failed", "error", err)
	writeError(w, http.StatusInternalServerError, s.phrase("account.unavailable"))
}

func writeAccount(w http.ResponseWriter, user accounts.User) {
	type publicUser struct {
		ID    string `json:"id"`
		Login string `json:"login"`
	}
	var player *publicUser
	if user.ID != "" {
		player = &publicUser{ID: user.ID, Login: user.LoginID}
	}
	data, _ := json.Marshal(struct {
		User *publicUser `json:"user"`
	}{player})
	writeJSON(w, http.StatusOK, data)
}

func (s *Server) serveAccount(w http.ResponseWriter, r *http.Request) {
	if s.accounts == nil {
		writeError(w, http.StatusNotFound, "Not Found")
		return
	}
	ctx := r.Context()
	token := loginToken(r)
	if r.URL.Path == "/api/account" {
		if r.Method != http.MethodGet {
			writeError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
			return
		}
		login, err := s.accounts.Lookup(ctx, token)
		if err != nil && !errors.Is(err, accounts.ErrCredentials) {
			s.accountError(w, err)
			return
		}
		writeAccount(w, login.User)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "Method Not Allowed")
		return
	}
	if r.URL.Path == "/api/logout" {
		if token != "" {
			if err := s.revokeLogin(ctx, token); err != nil {
				s.accountError(w, err)
				return
			}
		}
		setLogin(w, "", time.Unix(1, 0))
		writeAccount(w, accounts.User{})
		return
	}
	if r.TLS == nil && !(localPeer(r) && r.Header.Get("X-Forwarded-Proto") == "https") {
		writeError(w, http.StatusForbidden, s.phrase("account.https"))
		return
	}
	body, err := readBody(r, 4096)
	var credentials *struct {
		ID       string `json:"id"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err != nil || decoder.Decode(&credentials) != nil || credentials == nil || decoder.Decode(&struct{}{}) != io.EOF {
		s.accountError(w, accounts.ErrInvalidInput)
		return
	}
	action := s.accounts.Login
	if r.URL.Path == "/api/register" {
		action = s.accounts.Register
	}
	login, err := action(ctx, credentials.ID, credentials.Password)
	if err != nil {
		s.accountError(w, err)
		return
	}
	// Replacing a cookie on this browser also retires sockets using its old login.
	if token != "" && token != login.Token {
		if err := s.revokeLogin(ctx, token); err != nil {
			s.accountError(w, err)
			return
		}
	}
	setLogin(w, login.Token, login.Expires)
	writeAccount(w, login.User)
}

type loginSocket struct{ cancel context.CancelFunc }
type loginSockets struct {
	mu      sync.Mutex
	byToken map[string]map[*loginSocket]struct{}
}

// Register before the final lookup. A racing logout either cancels this
// context or has already revoked the row when Lookup reads it.
func (s *Server) watchLogin(ctx context.Context, login accounts.Login) (context.Context, func(), error) {
	live, cancel := context.WithDeadline(ctx, login.Expires)
	socket := &loginSocket{cancel}
	s.logins.mu.Lock()
	if s.logins.byToken == nil {
		s.logins.byToken = make(map[string]map[*loginSocket]struct{})
	}
	group := s.logins.byToken[login.Token]
	if group == nil {
		group = make(map[*loginSocket]struct{})
		s.logins.byToken[login.Token] = group
	}
	group[socket] = struct{}{}
	s.logins.mu.Unlock()
	release := func() {
		cancel()
		s.logins.mu.Lock()
		delete(group, socket)
		if len(group) == 0 {
			delete(s.logins.byToken, login.Token)
		}
		s.logins.mu.Unlock()
	}
	if _, err := s.accounts.Lookup(ctx, login.Token); err != nil {
		release()
		return nil, nil, err
	}
	return live, release, nil
}

func (s *Server) revokeLogin(ctx context.Context, token string) error {
	if err := s.accounts.Revoke(ctx, token); err != nil && !errors.Is(err, accounts.ErrCredentials) {
		return err
	}
	s.logins.mu.Lock()
	defer s.logins.mu.Unlock()
	for socket := range s.logins.byToken[token] {
		socket.cancel()
	}
	return nil
}
