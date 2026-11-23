package ktf

import (
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/compatibility"
)

const saveFileTimeClient = "fb39fb636a1311480f71123b411e9641a124486401a99faef0a5989a0a0b210b"

// saveFileTimeRun executes the recognized comparison with a save that records
// stored and a file whose time is now, and reports whether it took the delete
// path. The table r10 addresses, the save, the literals after the window and
// the delete routine (which only returns) are the test's own; the delete path
// marks a flag word the way the title does.
func saveFileTimeRun(t *testing.T, patched bool, stored uint64, now uint32) bool {
	t.Helper()
	memory := armcore.NewMemory()
	for _, page := range []uint32{0x149000, 0x163000, 0x200000, 0x201000, 0x210000} {
		if err := memory.Map(page, 0x1000, armcore.PermissionReadWriteExecute); err != nil {
			t.Fatal(err)
		}
	}
	for _, rewrite := range saveFileTimeRewrites[saveFileTimeClient] {
		if err := memory.Load(rewrite.at, rewrite.expect); err != nil {
			t.Fatal(err)
		}
	}
	word := func(value uint32) []byte { return binary.LittleEndian.AppendUint32(nil, value) }
	for address, data := range map[uint32][]byte{
		0x14982c: word(0x40),                                    // table slot of the loaded save
		0x149830: word(0x44),                                    // table slot of the flag word
		0x149834: word(5000),                                    // the threshold, in seconds
		0x200040: word(0x201000),                                // the loaded save
		0x200044: word(0x200800),                                // the flag word
		0x2015e0: binary.LittleEndian.AppendUint64(nil, stored), // the time the save records
		0x163788: {0x70, 0x47},                                  // delete: bx lr
		0x210800: word(0x48),                                    // [sp]: the table slot of the name
		0x2108c8: word(now),                                     // [sp+0xc8]: the file's time now
	} {
		if err := memory.Load(address, data); err != nil {
			t.Fatal(err)
		}
	}
	if patched {
		if err := applySaveFileTime(memory, saveFileTimeClient); err != nil {
			t.Fatal(err)
		}
	}
	state := armcore.NewContext()
	state.Registers[8] = 0x4c
	state.Registers[10] = 0x200000
	state.Registers[13] = 0x210800
	if err := state.SetPC(0x149771); err != nil {
		t.Fatal(err)
	}
	run, err := (armcore.Engine{}).Run(&state, memory, 0x149536, 128)
	if err != nil {
		t.Fatal(err)
	}
	if run.Reason != armcore.StopEnd {
		t.Fatalf("stopped at %#x before the comparison's exit", state.PC())
	}
	var flag [4]byte
	if err := memory.Read(0x200800, flag[:]); err != nil {
		t.Fatal(err)
	}
	return binary.LittleEndian.Uint32(flag[:]) == 1
}

// As packaged, a save whose recorded time is more than 5000 seconds from the
// file's is deleted on either side; with the fix none is.
func TestSaveFileTimeAcceptsTheRecordedTime(t *testing.T) {
	for _, tc := range []struct {
		name    string
		stored  uint64
		now     uint32
		deleted bool
	}{
		{"both zero, as this platform writes", 0, 0, false},
		{"within the threshold", 1000, 3000, false},
		{"a handset's time against zero", 1300000000, 0, true},
		{"the file newer than the save", 100, 6000, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := saveFileTimeRun(t, false, tc.stored, tc.now); got != tc.deleted {
				t.Fatalf("packaged: deleted = %v, want %v", got, tc.deleted)
			}
			if saveFileTimeRun(t, true, tc.stored, tc.now) {
				t.Fatal("with the fix the save was deleted")
			}
		})
	}
}

func TestSaveFileTimeRejectsUnexpectedCode(t *testing.T) {
	memory := armcore.NewMemory()
	if err := applySaveFileTime(memory, saveFileTimeClient); err == nil {
		t.Fatal("unmapped code accepted")
	}
	if err := memory.Map(0x149000, 0x1000, armcore.PermissionReadWriteExecute); err != nil {
		t.Fatal(err)
	}
	rewrite := saveFileTimeRewrites[saveFileTimeClient][0]
	changed := append([]byte(nil), rewrite.expect...)
	changed[rewrite.offset] ^= 0xff
	if err := memory.Load(rewrite.at, changed); err != nil {
		t.Fatal(err)
	}
	if err := applySaveFileTime(memory, saveFileTimeClient); err == nil {
		t.Fatal("changed comparison accepted")
	}
	if err := applySaveFileTime(memory, "unknown"); err == nil {
		t.Fatal("client without recorded code accepted")
	}
	if compatibility.IdentifyNative("ktf", []byte("unrecognized client")).Has(compatibility.KTFSaveFileTime) {
		t.Fatal("unknown revision matched")
	}
}
