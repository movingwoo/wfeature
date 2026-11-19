package clet

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

// The SKT root table and its divergences from the KTF function numbering are
// the whole of what is SKT-specific in the dispatch; see clet_tables.go and
// docs/skt-native.md. These bound them against regression.

func TestSKTFilesystemFunctionSwapsExistAndTell(t *testing.T) {
	// SKT's filesystem table is the specification's order with MC_fsIsExist and
	// MC_fsTell at 15 and 16 exchanged from the KTF table.
	if got := sktFilesystemFunction(15); got != wipicrt.WIPICFileExists {
		t.Errorf("SKT fs 15 = %d, want MC_fsIsExist (%d)", got, wipicrt.WIPICFileExists)
	}
	if got := sktFilesystemFunction(16); got != wipicrt.WIPICFileTell {
		t.Errorf("SKT fs 16 = %d, want MC_fsTell (%d)", got, wipicrt.WIPICFileTell)
	}
	// Every other slot is the specification's own number.
	for _, function := range []uint32{0, 1, 2, 3, 4, 5, 6, 8, 12} {
		if got := sktFilesystemFunction(function); got != function {
			t.Errorf("SKT fs %d = %d, want unchanged", function, got)
		}
	}
}

func TestSKTMediaFunctionsMapToRuntime(t *testing.T) {
	want := map[uint32]uint32{
		0:  wipicrt.WIPICMediaClipCreate,
		1:  wipicrt.WIPICMediaClipFree,
		2:  wipicrt.WIPICMediaSetWaterMark,
		4:  wipicrt.WIPICMediaClipPutData,
		10: wipicrt.WIPICMediaClipClearData,
		12: wipicrt.WIPICMediaClipGetVolume,
		13: wipicrt.WIPICMediaClipSetVolume,
		14: wipicrt.WIPICMediaPlay,
		17: wipicrt.WIPICMediaStop,
		21: wipicrt.WIPICMediaVibrator,
		24: wipicrt.WIPICMediaSetMuteState,
		25: wipicrt.WIPICMediaGetMuteState,
	}
	for slot, expected := range want {
		if got, ok := sktMediaFunctions[slot]; !ok || got != expected {
			t.Errorf("SKT media %d = (%d, %v), want %d", slot, got, ok, expected)
		}
	}
}

func TestSKTRootSlotOrder(t *testing.T) {
	// The SKT root order read off both local titles (docs/skt-native.md).
	cases := []struct {
		name string
		slot int
		want int
	}{
		{"kernel", sktRootKernel, 0}, {"graphics", sktRootGraphics, 1},
		{"filesystem", sktRootFilesystem, 3}, {"misc", sktRootMisc, 4},
		{"net", sktRootNet, 5}, {"util", sktRootUtil, 6}, {"media", sktRootMedia, 7},
		{"serial", sktRootSerial, 9}, {"phone", sktRootPhone, 10},
		{"terminal", sktRootTerminal, 11}, {"clibrary", sktRootCLibrary, 14},
	}
	for _, c := range cases {
		if c.slot != c.want {
			t.Errorf("SKT root %s = %d, want %d", c.name, c.slot, c.want)
		}
	}
}

// TestSKTHandsetCallsTitlesFallBackOn pins the handset's own slots three
// titles call: the default volume by category name, and two the titles carry
// on without — a text the kernel is asked for and the font faces.
func TestSKTHandsetCallsTitlesFallBackOn(t *testing.T) {
	store, _ := backend.NewMemorySaveStore(nil)
	session := fixtureSession(t, fixtureProgram(t, testfixture.SKTCletOptions{}), store)
	defer session.Close()
	runtime := session.Client.runtime
	call := func(slot, function uint32, registers ...uint32) uint32 {
		t.Helper()
		thread := armcore.NewThread(armcore.NewContext())
		for index, value := range registers {
			if err := thread.SetRegister(index, value); err != nil {
				t.Fatal(err)
			}
		}
		result, err := runtime.handleCletCall(thread, slot<<16|function)
		if err != nil {
			t.Fatalf("root %d function %d: %v", slot, function, err)
		}
		return result
	}
	general, err := runtime.AllocateBytes([]byte("GENERAL\x00"))
	if err != nil {
		t.Fatal(err)
	}
	ring, err := runtime.AllocateBytes([]byte("RING\x00"))
	if err != nil {
		t.Fatal(err)
	}
	buffer, err := runtime.AllocateBytes(make([]byte, 1024))
	if err != nil {
		t.Fatal(err)
	}
	session.Client.Audio.SetVolume(60)
	if got := call(sktRootMedia, sktMediaDefaultVolume, general); got != 60 {
		t.Errorf("default volume of GENERAL = %d, want the device's 60", got)
	}
	if got := call(sktRootMedia, sktMediaDefaultVolume, ring); got != wipicrt.WIPICErrorInvalid {
		t.Errorf("default volume of RING = %#x, want M_E_INVALID", got)
	}
	if got := call(sktRootKernel, sktKernelVersionText, buffer, 150); got != wipicrt.WIPICErrorInvalid {
		t.Errorf("kernel 63 = %#x, want M_E_INVALID", got)
	}
	if got := call(sktRootGraphics, sktGraphicsFontFaces, buffer, 1024); got != wipicrt.WIPICErrorInvalid {
		t.Errorf("graphics 54 = %#x, want M_E_INVALID", got)
	}
}
