package session

import (
	"github.com/movingwoo/wfeature/internal/backend"
)

// A panic raised by guest code must not reach the Host's goroutine.
//
// The platforms already convert one on the goroutines they own — a KTF or LGT
// guest thread, a JVM guest thread, an asynchronous session start — for the
// reason `backend.GuestPanic` gives: a panic is one archive doing something
// this emulator does not implement yet, and taking the process down with it
// destroys the report that would have said which. **The Host's own goroutine
// had no such boundary**, and that is the one that runs most guest code:
// `Tick` enters the guest on every platform, and so does a key, a touch, and
// each half of the pause pair.
//
// What that cost was worth naming, because it is not "the process exits". The
// server runs a session on the goroutine `net/http` gave the connection, and
// `net/http` recovers a panic there, logs it and closes the socket — so the
// process survives and the session does not clean up. Nothing in
// `sessionRunner.run` past the tick loop runs: the game is never closed, so
// its guest memory and worker goroutines are held for as long as the server
// lives; the encoder goroutine is left blocked on a channel nobody closes;
// and the save directory keeps a claim that is released nowhere, which makes
// that one game unstartable until the server is restarted. One unsupported
// archive quietly took a game away from a server that was still serving every
// other one.
//
// So the boundary belongs here, in the layer both Hosts drive the game
// through, rather than in either of them. A converted panic arrives as the
// error the caller already handles — the server ends the game, writes its
// post-mortem and tells the page, and the CLI reports it and stops.

// guarded runs one call into guest code and converts a panic it raises into an
// error.
//
// The failure is remembered. A recovered panic leaves the guest halfway
// through whatever it was doing — a frame partly drawn, a lock a native was
// holding, an ARM thread parked inside a Go call that will never be granted
// another slice — so the session is over whether or not the caller treats the
// first error as fatal. Remembering it means a Host that keeps asking is told
// the same thing every time instead of being let back into a machine that is
// no longer in a state to answer.
func (s *Session) guarded(where string, run func() error) (err error) {
	if s.failed != nil {
		return s.failed
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = backend.GuestPanic(s.options.Logger, where, recovered)
			s.failed = err
		}
	}()
	return run()
}

// Failed reports the panic that ended this session, if one did. A Host that
// writes a report asks, because by then the error has already been handled and
// the session is the only thing left that knows why it ended.
func (s *Session) Failed() error { return s.failed }
