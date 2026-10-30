package clet

import "testing"

// The SKT root table and its divergences from the KTF function numbering are
// the whole of what is SKT-specific in the dispatch; see clet_tables.go and
// docs/skt-native.md. These bound them against regression.

func TestSKTFilesystemFunctionSwapsExistAndTell(t *testing.T) {
	// SKT's filesystem table is the specification's order with MC_fsIsExist and
	// MC_fsTell at 15 and 16 exchanged from the KTF table.
	if got := sktFilesystemFunction(15); got != wipicFileExists {
		t.Errorf("SKT fs 15 = %d, want MC_fsIsExist (%d)", got, wipicFileExists)
	}
	if got := sktFilesystemFunction(16); got != wipicFileTell {
		t.Errorf("SKT fs 16 = %d, want MC_fsTell (%d)", got, wipicFileTell)
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
		0:  wipicMediaClipCreate,
		1:  wipicMediaClipFree,
		4:  wipicMediaClipPutData,
		10: wipicMediaClipClearData,
		13: wipicMediaClipSetVolume,
		14: wipicMediaPlay,
		17: wipicMediaStop,
		21: wipicMediaVibrator,
		24: wipicMediaSetMuteState,
		25: wipicMediaGetMuteState,
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
