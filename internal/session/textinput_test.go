package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/ktf"
)

func TestTextInputCommitReportsGuestExit(t *testing.T) {
	platform := &ktf.Session{}
	running := &Session{ktf: platform}
	want := fmt.Errorf("notify text listener: %w", ktf.ErrGuestExited)
	called := 0
	err := running.commitTextInput(context.Background(), platform, func(context.Context, string) error {
		called++
		return want
	}, "complete text")
	if !errors.Is(err, ErrExited) {
		t.Fatalf("text commit error = %v, want ErrExited", err)
	}
	if called != 1 {
		t.Fatalf("guest commit calls = %d, want 1", called)
	}
	if running.Running() {
		t.Fatal("session is still running after text listener exited")
	}
	if reason := running.ExitReason(); !strings.Contains(reason, want.Error()) {
		t.Fatalf("exit reason = %q, want %q", reason, want.Error())
	}
}

func TestTextInputCancelUsesTheSameOwnershipGuard(t *testing.T) {
	platform := &ktf.Session{}
	running := &Session{ktf: platform}
	called := 0
	cancel := func(context.Context) error {
		called++
		return nil
	}
	if err := running.cancelTextInput(context.Background(), platform, cancel); err != nil {
		t.Fatal(err)
	}
	running.ktf = &ktf.Session{}
	if err := running.cancelTextInput(context.Background(), platform, cancel); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("stale cancel error = %v", err)
	}
	if called != 1 {
		t.Fatalf("cancel calls = %d, want 1", called)
	}
}
