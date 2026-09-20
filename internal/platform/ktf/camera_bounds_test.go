package ktf

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

func TestCameraBoundsStorePreservesRegistersFlagsAndNeighbors(t *testing.T) {
	for _, value := range []int32{-2147483648, -16, -1, 0, 1, 900, 2147483647} {
		client, runtime := newHookRuntime(t)
		original := bytes.Repeat([]byte{0x5a}, 128)
		if err := client.core.Memory().Write(hookScratch, original); err != nil {
			t.Fatal(err)
		}
		runtime.cameraBoundsStore = cameraBoundsStore
		state := armcore.NewContext()
		state.CPSR |= 0xf0000000
		for i := 0; i < 13; i++ {
			state.Registers[i] = uint32(100 + i)
		}
		state.Registers[0], state.Registers[3] = hookScratch, uint32(value)
		thread := armcore.NewThread(state)
		if err := runtime.handleSupervisorCall(t.Context(), thread, armcore.SupervisorCall{Immediate: svcCategoryCameraBounds, Address: cameraBoundsStore, ResumePC: cameraBoundsStore + 2}); err != nil {
			t.Fatal(err)
		}
		if thread.Context() != state {
			t.Fatalf("store of %d changed registers or flags", value)
		}
		want := append([]byte(nil), original...)
		binary.LittleEndian.PutUint32(want[0x74:], uint32(max(value, 0)))
		if got := readScratch(t, client, hookScratch, 128); !bytes.Equal(got, want) {
			t.Fatalf("store of %d changed the wrong bytes", value)
		}
	}
}

// This authored Thumb fixture implements the two-branch clamp that oscillates
// on an undersized map. Only the final store differs in the corrected run.
func TestCameraBoundsStopsOscillationAndPreservesTallMaps(t *testing.T) {
	for _, corrected := range []bool{false, true} {
		for _, height := range []uint32{304, 320, 640} {
			client, runtime := newHookRuntime(t)
			address := uint32(runtime.codeCursor)
			words := []uint16{0x2a00, 0xdb05, 0x6e03, 0x1a5b, 0x429a, 0xdc02, 0x4613, 0xe000, 0x2300, 0x6743, 0x4770}
			if corrected {
				words[9] = 0xdf00 | uint16(svcCategoryCameraBounds)
				runtime.cameraBoundsStore = address + 18
			}
			code := make([]byte, len(words)*2)
			for i, word := range words {
				binary.LittleEndian.PutUint16(code[i*2:], word)
			}
			if err := client.core.Memory().Load(address, code); err != nil {
				t.Fatal(err)
			}
			var word [4]byte
			binary.LittleEndian.PutUint32(word[:], height)
			if err := client.core.Memory().Write(hookScratch+0x60, word[:]); err != nil {
				t.Fatal(err)
			}
			var y int32
			for frame := 0; frame < 400; frame++ {
				candidate := y + 1
				want := candidate
				if candidate < 0 {
					want = 0
				} else if candidate > int32(height)-320 {
					want = int32(height) - 320
				}
				if corrected {
					want = max(want, 0)
				}
				if _, err := client.core.Call(t.Context(), client.thread, address|1, ReturnAddress, []uint32{hookScratch, 320, uint32(candidate)}, runtime.handleSupervisorCall); err != nil {
					t.Fatal(err)
				}
				y = int32(binary.LittleEndian.Uint32(readScratch(t, client, hookScratch+0x74, 4)))
				if y != want {
					t.Fatalf("corrected=%v height=%d frame=%d: y=%d want=%d", corrected, height, frame, y, want)
				}
			}
		}
	}
}

func TestCameraBoundsRejectsUnknownImagesCallsAndInvalidMemory(t *testing.T) {
	client, runtime := newTestRuntime(t)
	if err := runtime.installCameraBoundsCompatibility(); err != nil {
		t.Fatal(err)
	}
	if runtime.cameraBoundsStore != 0 {
		t.Fatal("unknown executable received a compatibility patch")
	}
	call := armcore.SupervisorCall{Address: cameraBoundsStore, ResumePC: cameraBoundsStore + 2}
	if err := runtime.storeCameraBounds(client.thread, call); err == nil {
		t.Fatal("uninstalled compatibility call accepted")
	}
	runtime.cameraBoundsStore = cameraBoundsStore
	wrong := call
	wrong.Address += 2
	if err := runtime.storeCameraBounds(client.thread, wrong); err == nil {
		t.Fatal("call from another instruction accepted")
	}
	for _, base := range []uint32{0xfffffff0, 0} {
		if err := client.thread.SetRegister(0, base); err != nil {
			t.Fatal(err)
		}
		if err := runtime.storeCameraBounds(client.thread, call); err == nil {
			t.Fatalf("invalid camera address %#x accepted", base)
		}
	}
}
