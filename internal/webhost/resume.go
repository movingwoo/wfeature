package webhost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/movingwoo/wfeature/internal/session"
)

// A browser's game outlives its connection until it is stopped, replaced,
// evicted by the retention count, or the server shuts down. Tokens are random
// bearer capabilities shared by tabs in one browser profile, not user accounts.
// The save layout stays game-scoped and is protected by the existing claims.
// Admission reserves room for active games too: losing a socket never evicts another game.
const maxRetainedSessions = 4

// parkedSession is a game waiting for its page to come back. Everything here
// is what the runner would have lost when its socket closed.
type parkedSession struct {
	game *session.Session
	// context and cancel are the game's own lifetime, which outlived the
	// socket that started it; see sessionRunner.gameCtx for why a game cannot
	// be ticked under its page's context.
	context  context.Context
	cancel   context.CancelFunc
	label    string
	platform string
	// saveDirectory is the claim this game still holds while it waits; see
	// saveclaim.go.
	saveDirectory string
	audio         *audioCollector
	started       startedMessage
	postMortem    string
	presented     uint64

	externalLaunch *externalLaunchMailbox
	parkedAt       time.Time
}

// parkSession retains a game under its existing browser token. Ownership and
// the save claim move together under parkedMu.
func (s *Server) parkSession(token string, parked *parkedSession) {
	s.parkedMu.Lock()
	defer s.parkedMu.Unlock()
	if s.parked == nil {
		s.parked = make(map[string]*parkedSession)
	}
	// Replacing a token's own earlier session closes it: the token is one
	// browser's, and that browser cannot be playing two games.
	if previous, ok := s.parked[token]; ok {
		s.dropLocked(token, previous, "replaced")
	}
	if s.sessionsClosed {
		if owner := s.attached[token]; owner != nil {
			owner.admitted = false
		}
		delete(s.attached, token)
		s.dropLocked(token, parked, "server stopping")
		return
	}
	parked.parkedAt = time.Now()
	if owner := s.attached[token]; owner != nil {
		owner.admitted = false
	}
	delete(s.attached, token)
	if claim := s.claims[parked.saveDirectory]; claim != nil {
		claim.parked = true
	}
	s.parked[token] = parked
	s.logger.Info("session parked", "game", parked.label, "retained", len(s.parked))
}

// resumeSession hands a parked game back, or reports that there is none under
// that token. Removing the parked entry and assigning control are atomic:
// two pages racing on one token cannot both get the game.
func (s *Server) resumeSession(token string, owner *sessionRunner) (*parkedSession, bool) {
	s.parkedMu.Lock()
	defer s.parkedMu.Unlock()
	parked, ok := s.parked[token]
	if !ok {
		return nil, false
	}
	delete(s.parked, token)
	if s.attached == nil {
		s.attached = make(map[string]*sessionRunner)
	}
	s.attached[token] = owner
	owner.admitted = true
	if claim := s.claims[parked.saveDirectory]; claim != nil {
		claim.parked = false
	}
	return parked, true
}

// CloseParkedSessions releases retained games and rejects any late parking
// from connections that close after shutdown has begun.
func (s *Server) CloseParkedSessions() {
	s.parkedMu.Lock()
	defer s.parkedMu.Unlock()
	s.sessionsClosed = true
	for token, parked := range s.parked {
		s.dropLocked(token, parked, "server stopping")
	}
}

// dropLocked closes one parked game. The caller holds the mutex.
func (s *Server) dropLocked(token string, parked *parkedSession, reason string) {
	parked.game.Close()
	s.releaseSaveDirectoryLocked(parked.saveDirectory)
	if parked.cancel != nil {
		parked.cancel()
	}
	delete(s.parked, token)
	s.logger.Info("parked session closed", "game", parked.label, "reason", reason,
		"parked_for", time.Since(parked.parkedAt).Round(time.Second))
}

// parkedCount reports how many games are waiting, for tests and for the status
// a server can be asked about.
func (s *Server) parkedCount() int {
	s.parkedMu.Lock()
	defer s.parkedMu.Unlock()
	return len(s.parked)
}

// parkedGame answers the game waiting under a token, for a test that needs to
// ask the game itself rather than the bookkeeping around it.
func (s *Server) parkedGame(token string) *session.Session {
	s.parkedMu.Lock()
	defer s.parkedMu.Unlock()
	if parked, ok := s.parked[token]; ok {
		return parked.game
	}
	return nil
}

// newResumeToken is the name a parked game waits under. It is a secret in the
// only sense that matters here: it is the one thing that hands a running game
// to a socket, so it is random rather than a counter something else could
// guess its way through.
func newResumeToken() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

// reserveSession prevents simultaneous starts in tabs sharing a browser token.
func (s *Server) reserveSession(token string, owner *sessionRunner) bool {
	s.parkedMu.Lock()
	defer s.parkedMu.Unlock()
	if s.attached[token] != nil || s.parked[token] != nil {
		return false
	}
	if s.attached == nil {
		s.attached = make(map[string]*sessionRunner)
	}
	s.attached[token] = owner
	return true
}

func (s *Server) attachedSession(token string) *sessionRunner {
	s.parkedMu.Lock()
	defer s.parkedMu.Unlock()
	return s.attached[token]
}

func (s *Server) releaseSession(token string, owner *sessionRunner) {
	s.parkedMu.Lock()
	defer s.parkedMu.Unlock()
	if s.attached[token] == owner {
		delete(s.attached, token)
		owner.admitted = false
	}
}

func validResumeToken(token string) bool {
	if len(token) != 32 {
		return false
	}
	_, err := hex.DecodeString(token)
	return err == nil
}
