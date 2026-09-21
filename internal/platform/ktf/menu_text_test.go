package ktf

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

func TestMenuTextFollowsCenteredPanelAndPreservesMachineState(t *testing.T) {
	for _, height := range []int{220, 280, 320, 321, 400} {
		for _, store := range []struct {
			address uint32
			y       uint32
			inPanel int
		}{
			{menuTextTitle, 85, 29},
			{menuTextBody, 115, 59},
			{menuTextSettingsTitle, 85, 29},
			{menuTextToggleOn, 115, 59},
			{menuTextToggleOff, 115, 59},
			{menuTextVolume, 128, 72},
			{menuTextSpeed, 141, 85},
		} {
			client, runtime := newHookRuntime(t)
			client.SetScreen(240, height)
			runtime.menuTextCompatibility = true
			original := bytes.Repeat([]byte{0x5a}, 16)
			if err := client.core.Memory().Write(hookScratch, original); err != nil {
				t.Fatal(err)
			}
			state := armcore.NewContext()
			state.CPSR |= 0xf0000000
			for i := 0; i < 13; i++ {
				state.Registers[i] = uint32(100 + i)
			}
			state.Registers[3], state.Registers[13] = store.y, hookScratch
			thread := armcore.NewThread(state)
			call := armcore.SupervisorCall{Immediate: svcCategoryMenuText, Address: store.address, ResumePC: store.address + 2}
			if err := runtime.handleSupervisorCall(t.Context(), thread, call); err != nil {
				t.Fatal(err)
			}
			if thread.Context() != state {
				t.Fatal("text argument store changed registers or flags")
			}
			// The panel is 168 pixels high. Its title and body retain their
			// artwork-relative positions on both even and odd screen heights.
			wantY := height/2 - 84 + store.inPanel
			want := append([]byte(nil), original...)
			binary.LittleEndian.PutUint32(want[4:], uint32(wantY))
			if got := readScratch(t, client, hookScratch, len(want)); !bytes.Equal(got, want) {
				t.Fatalf("height=%d store=%#x: got %x, want %x", height, store.address, got, want)
			}
		}
	}
}

func TestMenuTextRejectsUnknownImagesCallsAndInvalidStack(t *testing.T) {
	client, runtime := newTestRuntime(t)
	if err := runtime.installMenuTextCompatibility(); err != nil {
		t.Fatal(err)
	}
	if runtime.menuTextCompatibility {
		t.Fatal("unknown image received a menu correction")
	}
	call := armcore.SupervisorCall{Address: menuTextTitle, ResumePC: menuTextTitle + 2}
	if err := runtime.storeMenuTextY(client.thread, call); err == nil {
		t.Fatal("uninstalled compatibility call accepted")
	}
	runtime.menuTextCompatibility = true
	for _, wrong := range []armcore.SupervisorCall{
		{Address: menuTextTitle + 2, ResumePC: menuTextTitle + 4},
		{Address: menuTextTitle, ResumePC: menuTextTitle + 4},
	} {
		if err := runtime.storeMenuTextY(client.thread, wrong); err == nil {
			t.Fatal("unrecognized instruction or resume address accepted")
		}
	}
	for _, stack := range []uint32{0xfffffffc, 0} {
		if err := client.thread.SetRegister(13, stack); err != nil {
			t.Fatal(err)
		}
		if err := runtime.storeMenuTextY(client.thread, call); err == nil {
			t.Fatalf("invalid stack %#x accepted", stack)
		}
	}
}
