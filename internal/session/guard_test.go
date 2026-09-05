package session

import (
	"errors"
	"strings"
	"testing"
)

// A guest panic on the Host's own goroutine used to leave the session behind:
// nothing past the tick loop ran, so the game was never closed, its guest
// memory and worker goroutines were held for the life of the server, and the
// save directory kept a claim released nowhere. These say the boundary
// converts one instead.

func TestAGuestPanicBecomesTheErrorTheCallerExpected(t *testing.T) {
	session := &Session{}
	err := session.guarded("session tick", func() error {
		panic("an unsupported archive did something")
	})
	if err == nil {
		t.Fatal("a panic answered with no error")
	}
	if !strings.Contains(err.Error(), "an unsupported archive did something") {
		t.Fatalf("the error does not carry what panicked: %v", err)
	}
	if !strings.Contains(err.Error(), "session tick") {
		t.Fatalf("the error does not say where it happened: %v", err)
	}
	if session.Failed() == nil {
		t.Fatal("the session does not remember what ended it")
	}
}

// A recovered panic leaves the guest halfway through whatever it was doing, so
// the session is over whether or not the caller treated the first error as
// fatal. Every later call answers with the same error rather than re-entering.
func TestASessionThatPanickedIsNotEnteredAgain(t *testing.T) {
	session := &Session{}
	first := session.guarded("session tick", func() error { panic("stop") })

	entered := false
	again := session.guarded("session key", func() error {
		entered = true
		return nil
	})
	if entered {
		t.Fatal("a session that panicked was entered again")
	}
	if !errors.Is(again, first) {
		t.Fatalf("the second call answered %v, want the failure %v", again, first)
	}
}

// An error the call returns itself is not a panic, and must not stick: a game
// that refuses one key goes on running.
func TestAnOrdinaryErrorDoesNotEndTheSession(t *testing.T) {
	session := &Session{}
	refused := errors.New("unknown key action")
	if err := session.guarded("session key", func() error { return refused }); !errors.Is(err, refused) {
		t.Fatalf("guarded answered %v, want %v", err, refused)
	}
	if session.Failed() != nil {
		t.Fatal("an ordinary error was recorded as a panic")
	}
	if err := session.guarded("session tick", func() error { return nil }); err != nil {
		t.Fatalf("the next call answered %v, want nil", err)
	}
}

// Closing is the one call a failed session still has to make, because it is
// what lets go of the guest memory and the worker goroutines the panic left
// running. It is also where a second Close must not enter a platform the
// first one already took out.
func TestClosingStillRunsAfterAPanic(t *testing.T) {
	session := &Session{}
	_ = session.guarded("session tick", func() error { panic("stop") })
	session.Close()
	session.Close()
	if session.Running() {
		t.Fatal("a closed session still reports a game")
	}
}
