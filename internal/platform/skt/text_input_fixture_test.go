package skt

import (
	"context"
	_ "embed"
	"errors"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

//go:embed testdata/text-input.jar
var textInputJAR []byte

func TestPackagedTextInputMIDletSupportsHostComposition(t *testing.T) {
	archive, err := Open(textInputJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	runtime, err := Start(archive, Options{Framebuffer: newTestFramebuffer(t, 32, 24)})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { _ = runtime.Destroy(true) })
	if err := runtime.RunPending(); err != nil {
		t.Fatalf("RunPending() error = %v", err)
	}

	stale, err := runtime.TextInput(context.Background())
	if err != nil {
		t.Fatalf("TextInput() error = %v", err)
	}
	if stale.Text != "" || stale.MaxLength != 16 || !stale.Multiline || stale.InputMode != "text" {
		t.Fatalf("first TextInput() = %+v", stale)
	}
	if err := stale.Commit(context.Background(), "한글 입력"); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
	reopened, err := runtime.TextInput(context.Background())
	if err != nil {
		t.Fatalf("reopen TextInput() error = %v", err)
	}
	if reopened.Text != "한글 입력" {
		t.Fatalf("reopened Text = %q", reopened.Text)
	}
	if err := runtime.Pause(); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}
	if err := runtime.Resume(); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	resumed, err := runtime.TextInput(context.Background())
	if err != nil {
		t.Fatalf("TextInput() after resume error = %v", err)
	}
	if resumed.Text != "한글 입력" {
		t.Fatalf("resumed Text = %q", resumed.Text)
	}

	if err := runtime.SendKey(KeyPressed, KeyCodeSoft1); err != nil {
		t.Fatalf("SendKey(soft 1) error = %v", err)
	}
	if err := resumed.Commit(context.Background(), "stale"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("Commit() after command switch error = %v", err)
	}
	second, err := runtime.TextInput(context.Background())
	if err != nil {
		t.Fatalf("second TextInput() error = %v", err)
	}
	if second.Text != "" || second.MaxLength != 4 {
		t.Fatalf("second TextInput() = %+v", second)
	}
	if err := second.Commit(context.Background(), "12345"); !errors.Is(err, backend.ErrInvalidTextInput) {
		t.Fatalf("Commit() over second maxSize error = %v", err)
	}
}
