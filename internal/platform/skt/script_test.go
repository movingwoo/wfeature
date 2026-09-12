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
