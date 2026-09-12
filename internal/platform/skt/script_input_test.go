package skt

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/sgsvm"
	"golang.org/x/text/encoding/korean"
)

func beginScriptTextInput(t *testing.T, prompt, value []byte) (*ScriptSession, *backend.TextInput) {
	t.Helper()
	s := newScriptTest(t, []byte{0xff}, nil)
	s.vm.Resources = []sgsvm.Resource{
		{Data: append(bytes.Clone(prompt), 0)},
		{Mutable: true, Data: append(bytes.Clone(value), 0)},
	}
	for index := range s.timers {
		s.timers[index] = scriptTimer{active: true, due: time.Second}
	}
	s.vm.Push(1234)
	s.vm.Push(0)
	s.vm.Push(1)
	if err := s.Call(0x8f, s.vm); err != nil {
		t.Fatal(err)
	}
	if got := s.vm.Pop(); got != 1234 {
		t.Fatalf("input call changed caller stack: got %d", got)
	}
	for index, timer := range s.timers {
		if timer.active {
			t.Fatalf("timer %d remains active", index)
		}
	}
	edit, err := s.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return s, edit
}

func TestScriptTextInputCommitUsesEUCKRBytesAndCompletesOnce(t *testing.T) {
	prompt, err := korean.EUCKR.NewEncoder().Bytes([]byte("이름"))
	if err != nil {
		t.Fatal(err)
	}
	initial, err := korean.EUCKR.NewEncoder().Bytes([]byte("처음"))
	if err != nil {
		t.Fatal(err)
	}
	s, edit := beginScriptTextInput(t, prompt, initial)
	if edit.Prompt != "이름" || edit.Text != "처음" || edit.MaxBytes != 32 || edit.MaxLength != 0 {
		t.Fatalf("edit = %+v", edit)
	}
	request := s.TextInputRequest()
	if request == 0 {
		t.Fatal("pending input has no request number")
	}

	before := bytes.Clone(s.vm.Resources[1].Data)
	for _, invalid := range []string{"입력🙂", "left\x00right", strings.Repeat("a", 33)} {
		if err := edit.Commit(context.Background(), invalid); !errors.Is(err, backend.ErrInvalidTextInput) {
			t.Fatalf("commit %q error = %v", invalid, err)
		}
		if !bytes.Equal(s.vm.Resources[1].Data, before) || s.TextInputRequest() != request {
			t.Fatal("invalid commit changed the destination or consumed the dialog")
		}
	}

	value := strings.Repeat("가", 16)
	encoded, err := korean.EUCKR.NewEncoder().Bytes([]byte(value))
	if err != nil || len(encoded) != 32 {
		t.Fatalf("fixture encoding length = %d, error = %v", len(encoded), err)
	}
	if err := edit.Commit(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	if got := s.vm.Resources[1].Data[:33]; !bytes.Equal(got, append(encoded, 0)) {
		t.Fatalf("destination bytes = %x", got)
	}
	if s.vm.Value(0, 0) != 2 || s.TextInputRequest() != 0 {
		t.Fatal("commit did not deliver system callback 6 with parameter 2")
	}
	if err := edit.Commit(context.Background(), "again"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("duplicate commit error = %v", err)
	}
	if err := edit.Cancel(context.Background()); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("commit-then-cancel error = %v", err)
	}

	cleared, empty := beginScriptTextInput(t, []byte("Prompt"), []byte("old"))
	if err := empty.Commit(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if got := cleared.vm.Resources[1].Data; len(got) == 0 || got[0] != 0 {
		t.Fatalf("empty commit did not clear resource: %x", got)
	}
}

func TestScriptTextInputCancelPreservesBytesAndBlocksTimersAndKeys(t *testing.T) {
	s, edit := beginScriptTextInput(t, []byte("Prompt"), []byte("unchanged"))
	before := bytes.Clone(s.vm.Resources[1].Data)
	beforeClock := s.clock
	beforeEvent := s.vm.Value(0, 0)
	if _, err := s.Advance(context.Background(), 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := s.SendKey(context.Background(), "press", KeyCodeFire); err != nil {
		t.Fatal(err)
	}
	if s.clock != beforeClock || s.vm.Value(0, 0) != beforeEvent {
		t.Fatal("pending modal allowed clock, timer, or key progress")
	}
	if err := edit.Cancel(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(s.vm.Resources[1].Data, before) {
		t.Fatal("cancel changed the initial resource bytes")
	}
	if s.vm.Value(0, 0) != 2 || s.TextInputRequest() != 0 {
		t.Fatal("cancel did not complete callback 6 exactly once")
	}
	if err := edit.Cancel(context.Background()); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("duplicate cancel error = %v", err)
	}
}

func TestScriptTextInputCallbackCanOpenNextDialog(t *testing.T) {
	s, first := beginScriptTextInput(t, []byte("First"), []byte("one"))
	s.vm.Resources = append(s.vm.Resources,
		sgsvm.Resource{Data: []byte("Second\x00")},
		sgsvm.Resource{Mutable: true, Data: []byte("two\x00")},
	)
	entry := len(s.vm.Program.Data)
	s.vm.Program.Data = append(s.vm.Program.Data, 5, 2, 5, 3, 0x8f, 5, 9, 0x0a, 16, 0xff)
	s.vm.Program.Entries[6] = uint16(entry)

	if err := first.Commit(context.Background(), "done"); err != nil {
		t.Fatal(err)
	}
	secondRequest := s.TextInputRequest()
	if secondRequest == 0 {
		t.Fatal("callback did not install its next dialog")
	}
	second, err := s.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.Prompt != "Second" || second.Text != "two" {
		t.Fatalf("second edit = %+v", second)
	}
	if s.vm.Value(16, 0) != 0 {
		t.Fatal("callback continued after the second dialog yielded")
	}
	if err := first.Cancel(context.Background()); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("old dialog invalidated the next one: %v", err)
	}
	if s.TextInputRequest() != secondRequest {
		t.Fatal("stale completion consumed the next dialog")
	}
}

func TestScriptTextInputCallbackCanExit(t *testing.T) {
	s, edit := beginScriptTextInput(t, []byte("Prompt"), []byte("value"))
	entry := len(s.vm.Program.Data)
	s.vm.Program.Data = append(s.vm.Program.Data, 0x46)
	s.vm.Program.Entries[6] = uint16(entry)
	if err := edit.Cancel(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !s.Exited() || s.TextInputRequest() != 0 {
		t.Fatal("callback exit was not retained")
	}
	if err := edit.Cancel(context.Background()); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("exited callback was completed twice: %v", err)
	}
}

func TestScriptTextInputRejectsMalformedResourcesBeforeSideEffects(t *testing.T) {
	for _, prompt := range [][]byte{[]byte("unterminated"), {0x81, 0}} {
		s := newScriptTest(t, []byte{0xff}, nil)
		s.vm.Resources = []sgsvm.Resource{{Data: prompt}, {Mutable: true, Data: []byte("value\x00")}}
		s.timers[0].active = true
		s.vm.Push(0)
		s.vm.Push(1)
		if err := s.Call(0x8f, s.vm); err == nil {
			t.Fatalf("malformed prompt %x accepted", prompt)
		}
		if !s.timers[0].active || s.textInput != nil {
			t.Fatal("malformed dialog stopped timers or installed pending input")
		}
	}
}
