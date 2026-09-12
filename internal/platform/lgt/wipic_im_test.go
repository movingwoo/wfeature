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
				for _, address := range []uint32{sizes, sizes + 4} {
					capacity, err := client.readWord(address)
					if err != nil || capacity != 6 {
						t.Fatalf("capacity changed: %d, %v", capacity, err)
					}
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
	}
	if result := callSlot(t, client, slotIMHandleInput, '2', EventKeyPressed, 0, 0); result != 0 {
		t.Fatalf("missing completion buffer handled key: %d", result)
	}
}
