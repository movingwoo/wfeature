package ktf

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"golang.org/x/text/encoding/korean"
)

func cInputCall(t *testing.T, runtime *initializationRuntime, function uint32, args ...uint32) (uint32, error) {
	t.Helper()
	thread := armcore.NewThread(armcore.NewContext())
	for i, v := range args {
		if i < 4 {
			if err := thread.SetRegister(i, v); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(args) > 4 {
		stack, err := runtime.allocateWords(args[4:])
		if err != nil {
			t.Fatal(err)
		}
		if err := thread.SetRegister(armcore.RegisterSP, stack); err != nil {
			t.Fatal(err)
		}
	}
	return runtime.handleWIPICTableCall(thread, wipicTableInputMethod, function)
}

func TestCInputModesAndNumericBuffers(t *testing.T) {
	_, runtime := newTestRuntime(t)
	if got, err := cInputCall(t, runtime, wipicIMSetCurrentMode, 3); err != nil || got != 1 {
		t.Fatalf("set mode: %d %v", got, err)
	}
	if got, err := cInputCall(t, runtime, wipicIMGetCurrentMode); err != nil || got != 3 {
		t.Fatalf("get mode: %d %v", got, err)
	}
	if got, err := cInputCall(t, runtime, wipicIMSetCurrentMode, 99); err != nil || got != 0 || runtime.cInput.mode != 3 {
		t.Fatalf("invalid mode: %d %v", got, err)
	}
	completed, _ := runtime.allocateBytes([]byte("stale!"))
	composing, _ := runtime.allocateBytes([]byte("stale!!!"))
	size1, _ := runtime.allocateWords([]uint32{6})
	size2, _ := runtime.allocateWords([]uint32{8})
	for _, test := range []struct {
		key, kind uint32
		text      string
	}{{'4', 2, "4"}, {'4', 3, ""}, {uint32(cInputFlush), 2, ""}} {
		writeWord(t, runtime, size1, 6)
		writeWord(t, runtime, size2, 8)
		got, err := cInputCall(t, runtime, wipicIMHandleInput, test.key, test.kind, completed, size1, composing, size2)
		if err != nil {
			t.Fatal(err)
		}
		if (got != 0) != (test.text != "") {
			t.Fatalf("handled=%d", got)
		}
		words, err := runtime.readAOTWords(size1, 1, "test size")
		if err != nil || words[0] != uint32(len(test.text)) {
			t.Fatalf("size=%v %v", words, err)
		}
		b := make([]byte, len(test.text)+1)
		if err := runtime.client.core.Memory().Read(completed, b); err != nil {
			t.Fatal(err)
		}
		if string(b) != test.text+"\x00" {
			t.Fatalf("completed=%q", b)
		}
		b = make([]byte, 1)
		runtime.client.core.Memory().Read(composing, b)
		if b[0] != 0 {
			t.Fatal("stale composition")
		}
	}
	if _, err := cInputCall(t, runtime, wipicIMHandleInput, '0', 2, completed, 0xfffffff0, composing, size2); err == nil {
		t.Fatal("invalid size pointer accepted")
	}
}

// The authored card forwards pressed digits through the same C table and
// stack arguments as a C-backed Java card. Its accumulated bytes represent
// the value owned by the guest, not a Host-side shadow of that value.
func cInputFixture(t *testing.T) (*Session, *[]byte, *bool) {
	t.Helper()
	client, runtime := newTestRuntime(t)
	completed, _ := runtime.allocateBytes(make([]byte, 6))
	composing, _ := runtime.allocateBytes(make([]byte, 8))
	size1, _ := runtime.allocateWords([]uint32{6})
	size2, _ := runtime.allocateWords([]uint32{8})
	text := []byte{}
	visible := true
	if err := client.vm.RegisterNative("test/CInputCard", "keyNotify", "(II)Z", func(_ *jvm.VM, args []jvm.Value) (jvm.Value, error) {
		kind, _ := args[1].Int32()
		key, _ := args[2].Int32()
		if !visible || kind != KeyPressed || key < '0' || key > '9' {
			return jvm.IntValue(0), nil
		}
		writeWord(t, runtime, size1, 6)
		writeWord(t, runtime, size2, 8)
		result, err := cInputCall(t, runtime, wipicIMHandleInput, uint32(key), 2, completed, size1, composing, size2)
		if err != nil {
			return jvm.VoidValue(), err
		}
		if result != 0 {
			words, err := runtime.readAOTWords(size1, 1, "test length")
			if err != nil {
				return jvm.VoidValue(), err
			}
			b := make([]byte, words[0])
			if err := client.core.Memory().Read(completed, b); err != nil {
				return jvm.VoidValue(), err
			}
			text = append(text, b...)
		} else {
			// An unhandled key causes this widget to flush. A capacity rejection
			// must consume the carrier without inserting anything or flushing.
			_, err = cInputCall(t, runtime, wipicIMHandleInput, uint32(cInputFlush), 2, completed, size1, composing, size2)
		}
		return jvm.IntValue(0), err
	}); err != nil {
		t.Fatal(err)
	}
	runtime.displayCards = append(runtime.displayCards, newWidget("test/CInputCard"))
	if _, err := cInputCall(t, runtime, wipicIMSetCurrentMode, 3); err != nil {
		t.Fatal(err)
	}
	return &Session{Client: client}, &text, &visible
}

func TestCInputHostCommitAndCapacityRetry(t *testing.T) {
	s, text, _ := cInputFixture(t)
	edit, err := s.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !edit.Append || edit.InputMode != "text" {
		t.Fatalf("edit=%+v", edit)
	}
	for _, bad := range []string{"\uD55C\uAE00\uC785", "\x00", "\n", "\U0001f600", strings.Repeat("a", 65)} {
		if err := edit.Commit(t.Context(), bad); !errors.Is(err, backend.ErrInvalidTextInput) {
			t.Fatalf("invalid commit: %v", err)
		}
		if len(*text) != 0 {
			t.Fatal("rejected input changed guest value")
		}
	}
	// A keypad mode is not a field constraint. The OS may compose Korean even
	// while the handset's keypad automaton has numeric mode selected.
	if err := edit.Commit(t.Context(), "\uD55C\uAE00"); err != nil {
		t.Fatal(err)
	}
	want, _ := korean.EUCKR.NewEncoder().Bytes([]byte("\uD55C\uAE00"))
	if !bytes.Equal(*text, want) {
		t.Fatalf("guest bytes=%x want=%x", *text, want)
	}
	if err := edit.Commit(t.Context(), "a"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("reused edit: %v", err)
	}
	edit, err = s.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := edit.Commit(t.Context(), "A"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(*text, append(want, 'A')) {
		t.Fatalf("append bytes=%x", *text)
	}
}

func TestCInputHostRejectsStaleTargets(t *testing.T) {
	for _, name := range []string{"key", "mode", "card", "closed", "queued", "flush", "ignored", "focus", "runtime"} {
		t.Run(name, func(t *testing.T) {
			s, text, visible := cInputFixture(t)
			runtime := s.Client.runtime
			edit, err := s.TextInput(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			switch name {
			case "key":
				err = s.SendKey(t.Context(), KeyPressed, KeyRight)
			case "mode":
				_, err = cInputCall(t, runtime, wipicIMSetCurrentMode, 2)
			case "card":
				runtime.displayCards[0] = newWidget("test/OtherCard")
			case "runtime":
				s.Client.runtime = nil
			case "closed":
				s.Client.workersStopped = true
			case "queued":
				runtime.guestEventLoop = true
			case "flush":
				_, err = cInputCall(t, runtime, wipicIMHandleInput, uint32(cInputFlush), 2, 0, 0, 0, 0)
			case "focus":
				runtime.runtimeObjects["lwc:focus"] = newWidget(runtimeTextFieldComponentClass)
			case "ignored":
				*visible = false
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := edit.Commit(t.Context(), "A"); !errors.Is(err, backend.ErrTextInputChanged) {
				t.Fatalf("stale commit: %v", err)
			}
			if len(*text) != 0 || len(runtime.cInput.pending) != 0 {
				t.Fatal("stale input escaped")
			}
		})
	}
}

func TestCInputHostCancellationAndInactiveEditor(t *testing.T) {
	s, text, _ := cInputFixture(t)
	edit, err := s.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := edit.Commit(ctx, "a"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if len(*text) != 0 {
		t.Fatal("cancelled input changed value")
	}
	s.Client.runtime.cInput.active = false
	if _, err := s.TextInput(t.Context()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatal(err)
	}
}

func TestCInputNewCardDoesNotInheritEditor(t *testing.T) {
	s, _, _ := cInputFixture(t)
	s.Client.runtime.displayCards = append(s.Client.runtime.displayCards, newWidget("test/Overlay"))
	if _, err := s.TextInput(t.Context()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("overlay input: %v", err)
	}
}
