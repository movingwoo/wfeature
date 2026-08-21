package webhost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/movingwoo/wfeature/internal/session"
)

// A game outlives its socket for a few minutes.
//
// One connection was one session, and closing it ended the game. That rule is
// right for a desktop, where a page stays open, and wrong for the phone this
// is built for: switching to another app suspends the page, the browser drops
// the socket a moment later, and coming back used to mean starting the game
// over. The phone did nothing unusual — it backgrounded a tab — and the game
// it was playing is exactly the state worth keeping.
//
// So a session whose page goes away is **parked** rather than closed: it keeps
// its guest memory, its save handle, its diagnostics and the picture it had,
// and it waits under a token the page was told when the game started. A page
// that comes back within the window sends the token and gets its game back. A
// page that does not is why the window exists at all — nothing can be left
// running for a player who is not coming back.
//
// **A parked game does not tick.** Nobody is watching it, and a game driven
// with no one there is a game that can be killed while its player is away;
// freezing it is also what a handset does when a call suspends an application.
// The guest's clock is the wall clock, so a resumed game sees the time it was
// away as one long wait — the same jump a suspended handset produces, and the
// reason the window is minutes rather than hours.
const (
	// resumeWindow is how long a parked game waits for its page. It is the
	// span a person spends looking at something else on their phone, not a
	// pause button: a game left longer than this is one nobody came back to.
	resumeWindow = 5 * time.Minute

	// maxParkedSessions bounds what a server holds for pages that are gone.
	// Each one costs a running game's guest memory — a KTF arena alone is
	// mapped at 64 MiB — so this is a memory limit rather than a policy: past
	// it the oldest is closed to make room, because the newest page to
	// disappear is the likeliest to return. Four is generous for the machine
	// this runs on, which is one household's server.
	maxParkedSessions = 4
)

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
	parkedAt      time.Time
	timer         *time.Timer
}

// parkSession takes a running game off a runner that is losing its socket and
// holds it under a fresh token. The token is the one the page was given when
// the game started, so a page that reconnects already knows it.
func (s *Server) parkSession(token string, parked *parkedSession) {
	s.parkedMu.Lock()
	defer s.parkedMu.Unlock()
	if s.parked == nil {
		s.parked = make(map[string]*parkedSession)
	}
	// Replacing a token's own earlier session closes it: the token is one
	// page's, and that page cannot be playing two games.
	if previous, ok := s.parked[token]; ok {
		s.dropLocked(token, previous, "replaced")
	}
	for len(s.parked) >= maxParkedSessions {
		oldest, oldestToken := (*parkedSession)(nil), ""
		for candidate, held := range s.parked {
			if oldest == nil || held.parkedAt.Before(oldest.parkedAt) {
				oldest, oldestToken = held, candidate
			}
		}
		if oldest == nil {
			break
		}
		s.dropLocked(oldestToken, oldest, "too many parked sessions")
	}
	parked.parkedAt = time.Now()
	parked.timer = time.AfterFunc(resumeWindow, func() { s.expireSession(token) })
	s.parked[token] = parked
	s.logger.Info("session parked", "game", parked.label, "window", resumeWindow)
}

// resumeSession hands a parked game back, or reports that there is none under
// that token. A token is spent by the page that uses it: two pages racing on
// one token cannot both get the game.
func (s *Server) resumeSession(token string) (*parkedSession, bool) {
	s.parkedMu.Lock()
	defer s.parkedMu.Unlock()
	parked, ok := s.parked[token]
	if !ok {
		return nil, false
	}
	parked.timer.Stop()
	delete(s.parked, token)
	return parked, true
}

// expireSession closes a game nobody came back for.
func (s *Server) expireSession(token string) {
	s.parkedMu.Lock()
	defer s.parkedMu.Unlock()
	parked, ok := s.parked[token]
	if !ok {
		return
	}
	s.dropLocked(token, parked, "resume window elapsed")
}

// CloseParkedSessions releases every game still waiting for a page. A server
// that is stopping has no window left to honour, and the guest goroutines are
// what would otherwise outlive it.
func (s *Server) CloseParkedSessions() {
	s.parkedMu.Lock()
	defer s.parkedMu.Unlock()
	for token, parked := range s.parked {
		s.dropLocked(token, parked, "server stopping")
	}
}

// dropLocked closes one parked game. The caller holds the mutex.
func (s *Server) dropLocked(token string, parked *parkedSession, reason string) {
	parked.timer.Stop()
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
