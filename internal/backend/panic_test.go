package backend

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestGuestPanicReportsWhereAndWhat(t *testing.T) {
	var log bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelError}))

	var err error
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = GuestPanic(logger, "a guest thread", recovered)
			}
		}()
		var pixels []byte
		_ = pixels[7]
	}()

	if err == nil {
		t.Fatal("a recovered panic produced no error")
	}
	if !strings.Contains(err.Error(), "a guest thread panicked") {
		t.Errorf("error = %q, want it to name where it happened", err)
	}
	if !strings.Contains(err.Error(), "index out of range") {
		t.Errorf("error = %q, want it to carry the panic", err)
	}
	// The stack is the whole point of converting rather than crashing: without
	// it the log says a game failed and nothing about where.
	written := log.String()
	if !strings.Contains(written, "TestGuestPanicReportsWhereAndWhat") {
		t.Errorf("the log carries no stack:\n%s", written)
	}
}

func TestGuestPanicWithoutALogger(t *testing.T) {
	// A Host that never attached a logger still gets the error; it is the only
	// thing between an unsupported archive and a dead process.
	err := GuestPanic(nil, "a guest thread", "boom")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error = %v, want it to carry the panic", err)
	}
}
