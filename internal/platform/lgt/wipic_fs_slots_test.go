package lgt

import (
	"strings"
	"testing"
)

// The file block runs in the specification's order from MC_fsOpen at 0x190, and
// MC_fsSetMode and MC_fsGetCounts are the two entries it puts between
// MC_fsAvailable and MC_fsTell, both of which titles call. What they answer is
// the storage contract's, tested in storage_contract_c_test.go.
func TestSetModeAndGetCountsSitBetweenAvailableAndTell(t *testing.T) {
	if slotFsAvailable+1 != slotFsSetMode || slotFsSetMode+1 != slotFsGetCounts || slotFsGetCounts+1 != slotFsTell {
		t.Fatalf("available %#x, setMode %#x, getCounts %#x, tell %#x", slotFsAvailable, slotFsSetMode, slotFsGetCounts, slotFsTell)
	}
	for _, slot := range []uint32{slotFsSetMode, slotFsGetCounts} {
		if !knownWIPICSlot(slot) || wipicSlotNames[slot] == "" {
			t.Errorf("slot %#x is not served under a name", slot)
		}
	}
}

// Every storage call reads a name as one path: a backslash separates as a
// slash does, and leading separators, empty and "." segments go. A name that
// is a path already is answered as it is; ".." stays for the store to refuse.
func TestGuestPathReadsEverySpellingAlike(t *testing.T) {
	for name, want := range map[string]string{
		"": "", "/": "", ".": "", "./": "", "\\": "", "//.\\": "",
		"save.dat": "save.dat", "/save.dat": "save.dat", "//save.dat": "save.dat", "./save.dat": "save.dat",
		".\\save.dat": "save.dat", "data\\save.dat": "data/save.dat", "data/./save.dat/": "data/save.dat",
		"data//save.dat": "data/save.dat", "..": "..", "a/../b": "a/../b", "가/나.dat": "가/나.dat",
	} {
		if got := guestPath(name); got != want {
			t.Errorf("guestPath(%q) = %q, want %q", name, got, want)
		}
	}
}

// The longest name the contract allows is one the save store keeps a file
// under, so the 255-byte bound is the only one a title meets.
func TestTheLongestFileNameHasASaveKey(t *testing.T) {
	longest := make([]byte, maxFileNameBytes)
	for index := range longest {
		longest[index] = 'n'
	}
	if !hasSaveKey(string(longest)) || !hasSaveKey("/"+string(longest)) {
		t.Fatalf("a name of %d bytes has no save key", maxFileNameBytes)
	}
	// A spelling longer than the store takes a key to be is still the path it
	// spells, and has that path's key.
	spelled := strings.Repeat("./", 300) + string(longest)
	if key, err := fileSaveKey(spelled); err != nil || key != fileSaveScope+string(longest) {
		t.Fatalf("a %d-byte spelling has the key %q (%v)", len(spelled), key, err)
	}
	if root, code := fileNameCode(spelled); root || code != wipiSuccess {
		t.Fatalf("a %d-byte spelling of a %d-byte path answered %v, %d", len(spelled), maxFileNameBytes, root, code)
	}
	if root, code := fileNameCode(string(longest) + "n"); root || code != wipiLongName {
		t.Fatalf("a name of %d bytes answered %v, %d", maxFileNameBytes+1, root, code)
	}
}
