package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

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
