package lgt

import (
	"context"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

func TestNumericInputMethodReturnsCompletedDigits(t *testing.T) {
	client := fixtureClient(t)
	completed, _ := client.allocateBytes([]byte("stale\x00"))
	composing, _ := client.allocateBytes([]byte("stale\x00"))
	sizes, _ := client.allocateWords([]uint32{6, 6})
	frame, _ := client.allocateWords([]uint32{composing, sizes + 4})
	for _, mode := range []uint32{0, 1, 2, 3} {
		callSlot(t, client, slotIMSetCurrentMode, mode)
		for _, kind := range []uint32{EventKeyPressed, EventKeyReleased, EventKeyRepeated, 0xffffffff} {
			for _, key := range []uint32{'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', '*', '#', imaFlushKey} {
				if err := client.writeWord(sizes, 6); err != nil {
					t.Fatal(err)
				}
				if err := client.writeWord(sizes+4, 6); err != nil {
					t.Fatal(err)
				}
				thread := armcore.NewThread(armcore.NewContext())
				for index, value := range []uint32{key, kind, completed, sizes} {
					if err := thread.SetRegister(index, value); err != nil {
						t.Fatal(err)
					}
				}
				if err := thread.SetRegister(armcore.RegisterSP, frame); err != nil {
					t.Fatal(err)
				}
				if err := client.handleWIPICSVC(context.Background(), thread, slotIMHandleInput); err != nil {
					t.Fatal(err)
				}
				want := ""
				if mode == 3 && (kind == EventKeyPressed || kind == EventKeyRepeated) && key >= '0' && key <= '9' {
					want = string(rune(key))
				}
				got, err := client.readCString(completed)
				if err != nil || got != want {
					t.Fatalf("mode %d event %d key %d: completed %q, want %q: %v", mode, kind, key, got, want, err)
				}
				result, _ := thread.Register(0)
				if (result == 1) != (want != "") {
					t.Fatalf("handled = %d for %q", result, want)
				}
				pending, err := client.readCString(composing)
				if err != nil || pending != "" {
					t.Fatalf("composing = %q: %v", pending, err)
				}
				completedLength, err := client.readWord(sizes)
				if err != nil || completedLength != uint32(len(want)) {
					t.Fatalf("completed length = %d, want %d: %v", completedLength, len(want), err)
				}
				composingLength, err := client.readWord(sizes + 4)
				if err != nil || composingLength != 0 {
					t.Fatalf("composing length = %d, want 0: %v", composingLength, err)
				}
			}
		}
	}
}

func TestNumericInputMethodRespectsCompletionCapacity(t *testing.T) {
	client := fixtureClient(t)
	callSlot(t, client, slotIMSetCurrentMode, 3)
	for _, capacity := range []uint32{0, 1, 2} {
		buffer, _ := client.allocateBytes([]byte("xyz\x00"))
		size, _ := client.allocateWords([]uint32{capacity})
		result := callSlot(t, client, slotIMHandleInput, '2', EventKeyPressed, buffer, size)
		got := make([]byte, 4)
		if err := client.core.Memory().Read(buffer, got); err != nil {
			t.Fatal(err)
		}
		want := []string{"xyz\x00", "\x00yz\x00", "2\x00z\x00"}[capacity]
		if string(got) != want || (result == 1) != (capacity >= 2) {
			t.Fatalf("capacity %d: %q, handled %d", capacity, got, result)
		}
		length, err := client.readWord(size)
		wantLength := uint32(0)
		if capacity >= 2 {
			wantLength = 1
		}
		if err != nil || length != wantLength {
			t.Fatalf("capacity %d: output length %d: %v", capacity, length, err)
		}
	}
	if result := callSlot(t, client, slotIMHandleInput, '2', EventKeyPressed, 0, 0); result != 0 {
		t.Fatalf("missing completion buffer handled key: %d", result)
	}
}

// The authored caller uses two five-byte local strings and output length
// words. Matching the code alone is insufficient: its live arguments must
// identify those exact stack objects before capacity can be supplied.
func TestOutputOnlyInputBuffersRequireCallerAndStackAgreement(t *testing.T) {
	client := fixtureClient(t)
	instructions := []uint16{0x2600, 0xaa4a, 0xab48, 0xa947, 0x7016, 0x701e,
		0x0638, 0x9100, 0xab05, 0x21fb, 0x9301, 0xaa49, 0x9649,
		0x9647, 0x0e00, 0x0049, 0xab06, 0x4c00, 0xf000, 0xf800}
	code := installThumb(t, client, instructions...)
	stack, err := client.allocateBytes(make([]byte, 0x130))
	if err != nil {
		t.Fatal(err)
	}
	complete := inputBuffer{address: stack + 0x124, size: stack + 0x18}
	composing := inputBuffer{address: stack + 0x11c, size: stack + 0x14}
	thread := armcore.NewThread(armcore.NewContext())
	for index, value := range []uint32{hostTextInputCarrier, EventKeyPressed, complete.address, complete.size} {
		if err := thread.SetRegister(index, value); err != nil {
			t.Fatal(err)
		}
	}
	thread.SetRegister(armcore.RegisterSP, stack)
	thread.SetRegister(armcore.RegisterLR, code+40)
	if err := client.writeWord(stack, composing.address); err != nil {
		t.Fatal(err)
	}
	if err := client.writeWord(stack+4, composing.size); err != nil {
		t.Fatal(err)
	}
	if !client.outputOnlyInputBuffers(thread, complete, composing) {
		t.Fatal("SDK caller was not recognized")
	}
	wrong := complete
	wrong.address++
	if client.outputOnlyInputBuffers(thread, wrong, composing) {
		t.Fatal("mismatched stack accepted")
	}
	thread.SetRegister(armcore.RegisterLR, code+39)
	if client.outputOnlyInputBuffers(thread, complete, composing) {
		t.Fatal("ARM return address accepted")
	}
	thread.SetRegister(armcore.RegisterLR, code+40)
	for i, word := range instructions {
		if err := client.writeHalfword((code&^1)+uint32(i*2), word^0x8000); err != nil {
			t.Fatal(err)
		}
		if client.outputOnlyInputBuffers(thread, complete, composing) {
			t.Fatalf("changed caller word %d accepted", i)
		}
		if err := client.writeHalfword((code&^1)+uint32(i*2), word); err != nil {
			t.Fatal(err)
		}
	}
	// Every size word contains residue, including an unsafe large value.
	// The recognized caller's five-byte bound overrides it on every call.
	encoded, err := validateCTextInput("A한별B")
	if err != nil {
		t.Fatal(err)
	}
	client.cTextInput.pending = encoded
	for _, want := range []string{"A한", "별B"} {
		client.writeWord(complete.size, 0xffffffff)
		client.writeWord(composing.size, 0xffffffff)
		client.core.Memory().Write(complete.address+5, []byte{0x7e})
		thread.SetRegister(0, hostTextInputCarrier)
		if err := client.handleInputKey(thread); err != nil {
			t.Fatal(err)
		}
		got, err := client.readCText(complete.address)
		if err != nil || got != want {
			t.Fatalf("completed %q, want %q: %v", got, want, err)
		}
		var guard [1]byte
		if err := client.core.Memory().Read(complete.address+5, guard[:]); err != nil || guard[0] != 0x7e {
			t.Fatalf("buffer guard = %x: %v", guard, err)
		}
	}
	if len(client.cTextInput.pending) != 0 {
		t.Fatal("pending text remains")
	}
}
