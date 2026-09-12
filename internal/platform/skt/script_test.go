package skt

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func newScriptTest(t *testing.T, code []byte, store backend.SaveStore) *ScriptSession {
	t.Helper()
	p := &sgsvm.Program{Data: append([]byte{0}, code...), Entries: [8]uint16{1}, Variables: make([]sgsvm.Variable, 17)}
	for i := range p.Variables {
		p.Variables[i] = sgsvm.Variable{Mutable: true, Offset: i, Values: []int16{0}}
	}
	p.Variables[16].Values = make([]int16, 33)
	fb, _ := backend.NewMemoryFramebuffer(16, 16)
	s, err := StartScript(context.Background(), &Archive{Script: p}, ScriptOptions{Framebuffer: fb, SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestScriptSavePreservesHandsetSizedData(t *testing.T) {
	store := backend.NewDirectorySaveStore(t.TempDir())
	data := make([]byte, 66)
	binary.LittleEndian.PutUint16(data[64:], 1234)
	if err := store.StoreSave("nv/data", data); err != nil {
		t.Fatal(err)
	}
	s := newScriptTest(t, []byte{5, 16, 5, 33, 0x98, 5, 16, 5, 33, 0x99, 0xff}, store)
	if s.vm.Value(16, 32) != 1234 {
		t.Fatal("last word of handset save was lost")
	}
	saved, _ := store.LoadSave("nv/data")
	if len(saved) != 66 || binary.LittleEndian.Uint16(saved[64:]) != 1234 {
		t.Fatal("save was truncated")
	}
}

func TestScriptMalformedServiceHasNoSideEffect(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	before := s.graphics.bank
	if err := s.Call(0x59, s.vm); err == nil {
		t.Fatal("missing palette argument accepted")
	}
	if s.graphics.bank != before {
		t.Fatal("invalid call changed palette")
	}
}

func TestScriptAbsoluteValueWrapsAtWordBoundary(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	for _, pair := range [][2]int16{{0, 0}, {-123, 123}, {32767, 32767}, {-32768, -32768}} {
		s.vm.Push(pair[0])
		if err := s.Call(0xa3, s.vm); err != nil {
			t.Fatal(err)
		}
		if got := s.vm.Pop(); got != pair[1] {
			t.Fatalf("abs(%d) = %d, want %d", pair[0], got, pair[1])
		}
	}
}

func TestScriptStandaloneRuntimeMode(t *testing.T) {
	// The initialization callback copies system variable 0 into a scratch
	// variable before returning. Standalone startup exposes runtime mode 2
	// through that variable.
	s := newScriptTest(t, []byte{4, 0, 10, 16, 0xff}, nil)
	if got := s.vm.Value(16, 0); got != 2 {
		t.Fatalf("initial runtime mode = %d, want 2", got)
	}

	if err := s.SendKey(context.Background(), "press", KeyCodeFire); err != nil {
		t.Fatal(err)
	}
	if got := s.vm.Value(0, 0); got != 20 {
		t.Fatalf("key event parameter = %d, want 20", got)
	}
	s.vm.Push(1234)
	if err := s.Call(0xd2, s.vm); err != nil {
		t.Fatal(err)
	}
	if got := s.vm.Pop(); got != 2 {
		t.Fatalf("queried runtime mode = %d, want 2", got)
	}
	if got := s.vm.Pop(); got != 1234 {
		t.Fatalf("runtime mode query changed caller stack: got %d", got)
	}
}

func TestScriptModeGatedQueryOutsideModeThree(t *testing.T) {
	// The script stores the query result, then writes a second value. Reaching
	// the second store proves that the service does not yield the invocation.
	s := newScriptTest(t, []byte{5, 7, 5, 9, 0xbe, 10, 16, 5, 1, 10, 15, 0xff}, nil)
	if got := s.vm.Value(16, 0); got != 0 {
		t.Fatalf("initial mode query = %d, want 0", got)
	}
	if got := s.vm.Value(15, 0); got != 1 {
		t.Fatalf("instruction after mode query did not run: got %d", got)
	}

	for _, mode := range []byte{0, 1, 2, 4, 255} {
		s.runtimeMode = mode
		s.vm.Push(1234)
		s.vm.Push(-123)
		s.vm.Push(321)
		if err := s.Call(0xbe, s.vm); err != nil {
			t.Fatalf("mode %d: %v", mode, err)
		}
		if got := s.vm.Pop(); got != 0 {
			t.Fatalf("mode %d result = %d, want 0", mode, got)
		}
		if got := s.vm.Pop(); got != 1234 {
			t.Fatalf("mode %d changed caller stack: got %d", mode, got)
		}
	}
}

func TestScriptModeThreeQueryIsUnsupportedAtomically(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	s.runtimeMode = 3
	s.vm.Push(1234)
	s.vm.Push(-123)
	s.vm.Push(321)
	if err := s.Call(0xbe, s.vm); err == nil || err.Error() != "unsupported SGS service 0xbe in runtime mode 3" {
		t.Fatalf("mode-three query: %v", err)
	}
	for i, want := range []int16{321, -123, 1234} {
		if got := s.vm.Pop(); got != want {
			t.Fatalf("stack word %d after rejected query = %d, want %d", i, got, want)
		}
	}

	// Operand registration rejects underflow before the service mutates state.
	s.runtimeMode = scriptStandaloneRuntimeMode
	s.vm.Push(1234)
	if err := s.Call(0xbe, s.vm); err == nil || err.Error() != "operand stack underflow" {
		t.Fatalf("one-operand query: %v", err)
	}
	if got := s.vm.Pop(); got != 1234 {
		t.Fatalf("underflow changed caller stack: got %d", got)
	}
}

func TestScriptModeRequestStatusQuery(t *testing.T) {
	// Reaching the second assignment proves that the local status query does
	// not yield the current invocation.
	s := newScriptTest(t, []byte{5, 0xff, 0xbf, 10, 16, 5, 7, 10, 15, 0xff}, nil)
	if got := s.vm.Value(16, 0); got != 1 {
		t.Fatalf("initial request status = %d, want 1", got)
	}
	if got := s.vm.Value(15, 0); got != 7 {
		t.Fatalf("instruction after request status query did not run: got %d", got)
	}

	for _, test := range []struct {
		query, want int16
	}{
		{query: -1, want: 1},
		{query: 0, want: 4},
		{query: 32767, want: 4},
	} {
		s.vm.Push(1234)
		s.vm.Push(test.query)
		if err := s.Call(0xbf, s.vm); err != nil {
			t.Fatal(err)
		}
		if got := s.vm.Pop(); got != test.want {
			t.Fatalf("request %d status = %d, want %d", test.query, got, test.want)
		}
		if got := s.vm.Pop(); got != 1234 {
			t.Fatalf("request %d changed caller stack: got %d", test.query, got)
		}
	}

	s.requestID = -123
	for _, state := range []int16{-32768, -1, 0, 1, 2, 3, 32767} {
		s.requestStatus = state
		s.vm.Push(-123)
		if err := s.Call(0xbf, s.vm); err != nil {
			t.Fatal(err)
		}
		if got := s.vm.Pop(); got != state {
			t.Fatalf("signed request state %d became %d", state, got)
		}

		s.vm.Push(321)
		if err := s.Call(0xbf, s.vm); err != nil {
			t.Fatal(err)
		}
		if got := s.vm.Pop(); got != 4 {
			t.Fatalf("mismatched request with state %d = %d, want 4", state, got)
		}
	}

	// The non-mode-three 0xbe path does not enter the request helper or change
	// its local status.
	s.runtimeMode = scriptStandaloneRuntimeMode
	s.requestStatus = 2
	s.vm.Push(-123)
	s.vm.Push(321)
	if err := s.Call(0xbe, s.vm); err != nil {
		t.Fatal(err)
	}
	if got := s.vm.Pop(); got != 0 {
		t.Fatalf("mode-gated request result = %d, want 0", got)
	}
	s.vm.Push(-123)
	if err := s.Call(0xbf, s.vm); err != nil {
		t.Fatal(err)
	}
	if got := s.vm.Pop(); got != 2 {
		t.Fatalf("mode-gated request changed status to %d, want 2", got)
	}
}

func TestScriptModeRequestStatusIsSessionOwnedAndUnderflowIsAtomic(t *testing.T) {
	first := newScriptTest(t, []byte{0xff}, nil)
	second := newScriptTest(t, []byte{0xff}, nil)
	first.requestID = 123
	first.requestStatus = 2

	second.vm.Push(-1)
	if err := second.Call(0xbf, second.vm); err != nil {
		t.Fatal(err)
	}
	if got := second.vm.Pop(); got != 1 {
		t.Fatalf("second session inherited first session state: got %d", got)
	}

	beforeID, beforeState := second.requestID, second.requestStatus
	if err := second.Call(0xbf, second.vm); err == nil || err.Error() != "operand stack underflow" {
		t.Fatalf("empty request status query: %v", err)
	}
	if second.requestID != beforeID || second.requestStatus != beforeState {
		t.Fatal("underflow changed request status state")
	}
}

func TestScriptExitStopsFutureTimersAndKeys(t *testing.T) {
	s := newScriptTest(t, []byte{0x46}, nil)
	if !s.Exited() {
		t.Fatal("exit instruction not propagated")
	}
	before := s.vm.Value(0, 0)
	if err := s.SendKey(context.Background(), "press", KeyCodeFire); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Advance(context.Background(), 16*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if s.vm.Value(0, 0) != before {
		t.Fatal("exited script received input")
	}
}

func TestScriptHostActionTwoYieldsOnlyCurrentInvocation(t *testing.T) {
	// The assignment after 0xc5 belongs to the same invocation and must not
	// run. Unlike the bytecode exit instruction above, the service does not
	// prevent a later Host event from entering a fresh callback.
	s := newScriptTest(t, []byte{0xc5, 5, 9, 0x0a, 16, 0xff}, nil)
	if s.Exited() {
		t.Fatal("host action 2 became a script exit")
	}
	if got := s.vm.Value(16, 0); got != 0 {
		t.Fatalf("yielded initialization continued: scratch = %d", got)
	}

	s.vm.Push(1234)
	if err := s.Call(0xc5, s.vm); err != nil {
		t.Fatal(err)
	}
	if got := s.vm.Pop(); got != 1234 {
		t.Fatalf("host action 2 changed caller stack: got %d", got)
	}

	entry := len(s.vm.Program.Data)
	s.vm.Program.Data = append(s.vm.Program.Data, 0x3a, 16, 1, 0xff)
	s.vm.Program.Entries[3] = uint16(entry)
	if err := s.SendKey(context.Background(), "press", KeyCodeFire); err != nil {
		t.Fatal(err)
	}
	if got := s.vm.Value(16, 0); got != 1 {
		t.Fatalf("later key callback did not run: scratch = %d", got)
	}
	if s.Exited() {
		t.Fatal("script exited after its later callback")
	}
}

func TestScriptHostActionTwoLeavesScheduledTimerRunning(t *testing.T) {
	// Schedule a one-shot timer before yielding. Its later callback increments
	// scratch, proving 0xc5 did not merely leave an active bit behind while
	// preventing the timer from entering guest code.
	s := newScriptTest(t, []byte{5, 10, 5, 0, 0x9a, 0xc5, 5, 9, 0x0a, 16, 0xff}, nil)
	entry := len(s.vm.Program.Data)
	s.vm.Program.Data = append(s.vm.Program.Data, 0x3a, 16, 1, 0xff)
	s.vm.Program.Entries[2] = uint16(entry)
	if _, err := s.Advance(context.Background(), 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if got := s.vm.Value(16, 0); got != 1 {
		t.Fatalf("scheduled timer callback did not run: scratch = %d", got)
	}
	if s.Exited() {
		t.Fatal("script exited after its scheduled timer callback")
	}
}

func TestScriptTextAndEllipse(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	vm := s.vm
	if err := s.Call(0x55, vm); err != nil {
		t.Fatal(err)
	}
	vm.Resources = []sgsvm.Resource{{Data: []byte("A\x00")}}
	for _, v := range []int16{0, 0, 0} {
		vm.Push(v)
	}
	if err := s.Call(0x6a, vm); err != nil {
		t.Fatal(err)
	}
	for _, v := range []int16{8, 8, 3, 3} {
		vm.Push(v)
	}
	if err := s.Call(0x65, vm); err != nil {
		t.Fatal(err)
	}
	if s.graphics.pixels[8*16+8] != 0 || s.graphics.pixels[15*16+15] != 255 {
		t.Fatal("ellipse center not filled black")
	}
}

func TestScriptExternalNavigationIsExplicitlyUnsupported(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	s.vm.Resources = []sgsvm.Resource{{Data: []byte("https://example.invalid/download\x00")}}
	s.vm.Push(0)
	err := s.Call(0xc4, s.vm)
	if err == nil || err.Error() != "SGS external URL launch is unsupported by this host" {
		t.Fatalf("external navigation: %v", err)
	}
}

func TestScriptResourceByteServiceUsesSignedGuestRegionRead(t *testing.T) {
	s := newScriptTest(t, []byte{0xff}, nil)
	vm := sgsvm.New(&sgsvm.Program{Resources: []sgsvm.Resource{{Data: []byte{1}}, {Data: []byte{0x80}}}}, s)
	vm.Push(0)
	vm.Push(1)
	if err := s.Call(0x81, vm); err != nil {
		t.Fatal(err)
	}
	if vm.Pop() != -128 {
		t.Fatal("byte read lost signed result or adjacent guest region")
	}
}
