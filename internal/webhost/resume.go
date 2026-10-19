package webhost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
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
	parkedAt      time.Time
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
	owner.admittedAs = parked.label
	if claim := s.claims[parked.saveDirectory]; claim != nil {
		claim.parked = false
	}
	return parked, true
}

// StopDeadline bounds stopping a server: how long its drain waits for the
// games it holds to close. A game with its page attached first finishes the
// round it is in, which can be waiting on the clock for as long as a title's
// call may (backend.WaitBound), and then runs its title's own ending — an SKT
// destroyApp, an LGT destroyClet, a KTF destroyApp — for as long as every
// platform lets it (backend.EndingBound), and stores what it wrote after
// that. A drain that gave up first left a title behind that was still ending,
// with whatever it had not yet stored: at the ending's bound plus five
// seconds, a round that had waited past those five did. So this is past the
// two together. A Host that waits for a stopping server waits longer still:
// the launcher's stop, and the Android app before it starts the next server.
const StopDeadline = backend.WaitBound + backend.EndingBound + 5*time.Second

// CloseSessions ends every game the server holds, and is what stopping the
// server has to call. The HTTP server's own shutdown neither closes nor waits
// for a hijacked connection, and a session socket is one: a game left running
// behind it ends with the process, and so does whatever its title had written
// and not yet handed to the save store — a file it opened and did not close,
// keys kept in memory until a frame ends.
//
// From here on nothing starts and nothing parks. A parked game is closed on
// a goroutine of its own, because no goroutine is inside it; its save
// directory stays claimed until its close has returned. A game with a page
// attached is not closed from here: guest code is not re-entrant and its
// runner may be in the middle of a tick, so the runner is asked, finishes the
// round it is in and closes the game on its own goroutine, the way it closes
// one its page stopped. A socket with no game behind it is left alone.
//
// ctx bounds the wait for both. A close that has not ended by then is named
// in the log and left behind, so that a guest call which never returns cannot
// keep the process from ending. A parked game used to be closed here, one
// after another under the lock, before the wait began, so a title's
// destroyApp that never returned kept the process from ending and an app
// reopened behind it waiting on that process.
func (s *Server) CloseSessions(ctx context.Context) {
	type holder struct {
		released <-chan struct{}
		label    string
	}
	s.parkedMu.Lock()
	s.sessionsClosed = true
	var holders []holder
	for token, parked := range s.parked {
		delete(s.parked, token)
		closed := make(chan struct{})
		holders = append(holders, holder{released: closed, label: parked.label})
		go func() {
			defer close(closed)
			parked.game.Close()
			s.parkedMu.Lock()
			s.releaseParkedLocked(parked, "server stopping")
			s.parkedMu.Unlock()
		}()
	}
	// Only an admitted runner has a game or is starting one, and the flag
	// above admits no more: a start that has reserved its token and nothing
	// else is refused by it, and nothing is left parked to resume.
	for _, runner := range s.attached {
		// A runner only has the two channels once serveSession has made it;
		// one without them has no loop to ask and nothing to wait for. A stop
		// is the last place to find that out by closing a nil channel: the
		// panic would leave every other game unclosed.
		if !runner.admitted || runner.stop == nil || runner.released == nil {
			continue
		}
		// This is the only place the channel is closed, and it is under the
		// mutex, so a second stop finds it closed already.
		select {
		case <-runner.stop:
		default:
			close(runner.stop)
		}
		holders = append(holders, holder{released: runner.released, label: runner.admittedAs})
	}
	s.parkedMu.Unlock()

	for _, held := range holders {
		select {
		case <-held.released:
			continue
		case <-ctx.Done():
		}
		// The deadline has passed. A game that let go in the same moment is
		// not late, so it is asked once more before it is reported.
		select {
		case <-held.released:
		default:
			s.logger.Warn("session did not close before the shutdown deadline", "game", held.label)
		}
	}
}

// dropLocked closes one parked game. The caller holds the mutex; a close is
// bounded (see session.Session.Close).
func (s *Server) dropLocked(token string, parked *parkedSession, reason string) {
	parked.game.Close()
	delete(s.parked, token)
	s.releaseParkedLocked(parked, reason)
}

// releaseParkedLocked lets go of what a parked game held once it is closed.
// The caller holds the mutex.
func (s *Server) releaseParkedLocked(parked *parkedSession, reason string) {
	s.releaseSaveDirectoryLocked(parked.saveDirectory)
	if parked.cancel != nil {
		parked.cancel()
	}
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
