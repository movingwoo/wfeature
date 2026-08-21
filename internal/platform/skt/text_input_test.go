package skt

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// The vendor blit ignores two of its arguments, and the only thing that makes
// that recoverable from a screenshot is the line it writes when a title passes
// one. It is written once a run: the call is on a draw, and a title that makes
// it once makes it every frame.
func TestDrawImageExReportsTheArgumentsItIgnores(t *testing.T) {
	var log bytes.Buffer
	runtime := &Runtime{logger: slog.New(slog.NewTextHandler(&log, &slog.HandlerOptions{Level: slog.LevelWarn}))}

	// The arguments every local call site passes say nothing.
	runtime.reportDrawImageExArguments(nil, 0)
	if log.Len() != 0 {
		t.Fatalf("an ordinary call reported %q", log.String())
	}

	runtime.reportDrawImageExArguments(nil, 2)
	first := log.String()
	if !strings.Contains(first, "drawImageEx") || !strings.Contains(first, "mode=2") {
		t.Fatalf("report = %q, want the ignored mode named", first)
	}
	if lines := strings.Count(strings.TrimSpace(first), "\n"); lines != 0 {
		t.Fatalf("one call wrote %d lines", lines+1)
	}

	runtime.reportDrawImageExArguments(nil, 4)
	if log.String() != first {
		t.Fatalf("a second call wrote again: %q", log.String())
	}
}
