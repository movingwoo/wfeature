package ktf

import (
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
)

// Author a position-independent Thumb controller, initializer and mode wrapper
// with fresh code/data addresses. The stack models the two saved return frames
// at the native mode call, rather than importing a runtime or archive fixture.
func inputOwnerCaller(t *testing.T, runtime *initializationRuntime) (*armcore.Thread, uint32, uint32) {
	t.Helper()
	base, err := runtime.allocateBytes(make([]byte, 512))
	if err != nil {
		t.Fatal(err)
	}
	state, err := runtime.allocateWords([]uint32{7})
	if err != nil {
		t.Fatal(err)
	}
	put := func(address uint32, words ...uint16) {
		t.Helper()
		data := make([]byte, len(words)*2)
		for i, word := range words {
			binary.LittleEndian.PutUint16(data[i*2:], word)
		}
		if err := runtime.client.core.Memory().Write(address, data); err != nil {
			t.Fatal(err)
		}
	}
	call := func(address, target uint32) {
		delta := int32(target) - int32(address) - 4
		put(address, 0xf000|uint16(uint32(delta)>>12)&0x7ff, 0xf800|uint16(uint32(delta)>>1)&0x7ff)
	}
	caller, initializer, wrapper, helper := base, base+64, base+128, base+480
	staticBase := base + 400
	// Load a controller pointer through the static base; set state 7; call
	// initialization helpers. The two calls are relocated to this fixture.
	put(caller, 0x4b4f, 0x4453, 0x681a, 0x2307, 0x6013)
	call(caller+10, helper)
	call(caller+14, initializer)
	writeWord(t, runtime, base+320, 0xfffffff0) // signed offset -16
	writeWord(t, runtime, staticBase-16, state)
	// Save LR, initialize capacity and text, then select the mode.
	put(initializer, 0xb500, 0x2080, 0x0040)
	call(initializer+6, helper)
	put(initializer+10, 0x2000)
	call(initializer+12, helper)
	put(initializer+16, 0x2000)
	call(initializer+18, wrapper)
	call(initializer+22, helper)
	put(initializer+26, 0xbd00)
	// The wrapper saves LR/static base and resolves input-method table slot 1.
	put(wrapper, 0xb500, 0x469c, 0x4653, 0xb408, 0x4663, 0x4b30, 0x469a,
		0x4b31, 0x44fa, 0x4453, 0x681b, 0x6018, 0x2804, 0xd010,
		0x4b32, 0x4453, 0x681b, 0x6819, 0x4b33, 0x4453, 0x681a,
		0x0083, 0x5898, 0x684b)
	call(wrapper+48, helper)
	call(wrapper+52, helper)
	put(wrapper+56, 0xbc08, 0x469a, 0xbd00)
	put(helper, 0x4770) // bx lr
	stack, err := runtime.allocateWords([]uint32{staticBase, initializer + 22 | 1, caller + 18 | 1})
	if err != nil {
		t.Fatal(err)
	}
	thread := armcore.NewThread(armcore.NewContext())
	for register, value := range map[int]uint32{0: 2, 10: staticBase, armcore.RegisterLR: wrapper + 52 | 1, armcore.RegisterSP: stack} {
		if err := thread.SetRegister(register, value); err != nil {
			t.Fatal(err)
		}
	}
	return thread, state, base
}

func TestCInputOwnerSurvivesRejectedConfirmation(t *testing.T) {
	fixture := newDeferredCInputFixture(t)
	s := fixture.session
	runtime := s.Client.runtime
	thread, state, _ := inputOwnerCaller(t, runtime)
	runtime.rememberCInputOwner(thread)
	if runtime.cInput.owner.address != state {
		t.Fatal("authored controller was not recognized")
	}
	before, err := s.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SendKey(t.Context(), KeyPressed, KeyFire); err != nil {
		t.Fatal(err)
	}
	s.Client.clock.(*ManualClock).Advance(50 * time.Millisecond)
	if _, err := s.Client.ServiceTimers(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if err := before.Commit(t.Context(), "old"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("ignored confirmation revived old snapshot: %v", err)
	}
	edit, err := s.TextInput(t.Context())
	if err != nil {
		t.Fatalf("visible controller became unavailable: %v", err)
	}
	if err := edit.Commit(t.Context(), "new"); err != nil || string(*fixture.text) != "new" {
		t.Fatalf("fresh edit after ignored confirmation: text=%q err=%v", *fixture.text, err)
	}
	edit, err = s.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// A later timer accepts the name or dismisses the editor on the same card.
	writeWord(t, runtime, state, 8)
	if err := edit.Commit(t.Context(), "wrong"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("closed controller accepted input: %v", err)
	}
	if _, err := s.TextInput(t.Context()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("closed controller offered input: %v", err)
	}
	writeWord(t, runtime, state, 7)
	if _, err := s.TextInput(t.Context()); err != nil {
		t.Fatalf("controller restored after validation message: %v", err)
	}
	if err := edit.Commit(t.Context(), "old"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("restored controller revived old snapshot: %v", err)
	}
}

func TestCInputOwnerRejectsUnprovenCallers(t *testing.T) {
	for _, change := range []string{"wrapper", "initializer", "call target", "state store", "state value", "static base", "stack", "literal", "state pointer"} {
		t.Run(change, func(t *testing.T) {
			_, runtime := newTestRuntime(t)
			thread, state, base := inputOwnerCaller(t, runtime)
			stack, _ := thread.Register(armcore.RegisterSP)
			switch change {
			case "wrapper":
				writeWord(t, runtime, base+128+44, 0)
			case "initializer":
				writeWord(t, runtime, base+64, 0)
			case "call target":
				writeWord(t, runtime, base+64+18, 0xf800f000)
			case "state store":
				writeWord(t, runtime, base+8, 0)
			case "state value":
				writeWord(t, runtime, state, 8)
			case "static base":
				writeWord(t, runtime, stack, base+404)
			case "stack":
				thread.SetRegister(armcore.RegisterSP, 0xfffffffc)
			case "literal":
				writeWord(t, runtime, base+320, 0x7fffffff)
			case "state pointer":
				writeWord(t, runtime, base+384, 0xfffffff0)
			}
			if _, ok := runtime.cInputOwnerFromCaller(thread); ok {
				t.Fatal("unproven controller was accepted")
			}
		})
	}
}

func TestCInputOwnerChangeDuringDeliveryRejectsPendingText(t *testing.T) {
	fixture := newDeferredCInputFixture(t)
	runtime := fixture.session.Client.runtime
	thread, state, _ := inputOwnerCaller(t, runtime)
	runtime.rememberCInputOwner(thread)
	edit, err := fixture.session.TextInput(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fixture.beforeHandle = func() { writeWord(t, runtime, state, 8) }
	if err := edit.Commit(t.Context(), "wrong"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("controller changed inside callback: %v", err)
	}
	if len(*fixture.text) != 0 || *fixture.queued != 0 {
		t.Fatalf("closed controller received text=%q queued=%d", *fixture.text, *fixture.queued)
	}
}

func TestCInputModeTracksOnlyTheLiveController(t *testing.T) {
	s, _, _ := cInputFixture(t)
	runtime := s.Client.runtime
	thread, state, _ := inputOwnerCaller(t, runtime)
	if _, err := runtime.handleWIPICInputMethodCall(thread, wipicIMSetCurrentMode); err != nil {
		t.Fatal(err)
	}
	if runtime.cInput.owner.address != state || !runtime.cInput.active {
		t.Fatal("mode activation did not record the controller")
	}
	if _, err := cInputCall(t, runtime, wipicIMSetCurrentMode, 1); err != nil {
		t.Fatal(err)
	}
	if runtime.cInput.owner.address != state {
		t.Fatal("changing keypad mode dropped the live controller")
	}
	writeWord(t, runtime, state, 8)
	if _, err := cInputCall(t, runtime, wipicIMSetCurrentMode, 2); err != nil {
		t.Fatal(err)
	}
	if runtime.cInput.owner.address != 0 {
		t.Fatal("another mode activation retained an inactive controller")
	}
}
