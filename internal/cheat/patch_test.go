package cheat

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// failingMemory writes fine until the write that starts at refuseAt, which is
// how a patch that fails part of the way through is reached without inventing
// a guest that does it.
type failingMemory struct {
	*testMemory
	refuseAt uint32
}

func (memory *failingMemory) WriteMemory(address uint32, data []byte) error {
	if memory.refuseAt != 0 && address == memory.refuseAt {
		return fmt.Errorf("write refused at 0x%08x", address)
	}
	return memory.testMemory.WriteMemory(address, data)
}

func span(bytes ...byte) HexBytes { return HexBytes(bytes) }

func TestApplyPatchRefusesWhenMemoryDoesNotHoldTheDeclaredBytes(t *testing.T) {
	memory := newTestMemory()
	session := NewSession(memory)
	if err := memory.WriteMemory(0x1000, []byte{0x01, 0x02, 0x03, 0x04}); err != nil {
		t.Fatal(err)
	}

	err := session.ApplyPatch(PatchEntry{
		Name:    "skip the check",
		Patches: []Patch{{Address: 0x1000, Expect: span(0xde, 0xad), Replace: span(0x00, 0x00)}},
	})
	if err == nil {
		t.Fatal("a patch whose declared bytes are not there was applied")
	}
	if !strings.Contains(err.Error(), "memory holds 0102") {
		t.Fatalf("the refusal does not say what was there instead: %v", err)
	}
	// Nothing may have been written: the point of declaring the bytes is that
	// a patch aimed at another layout does not land in this one.
	read := make([]byte, 2)
	if err := memory.ReadMemory(0x1000, read); err != nil {
		t.Fatal(err)
	}
	if read[0] != 0x01 || read[1] != 0x02 {
		t.Fatalf("the refused patch still wrote: %x", read)
	}
	if len(session.Patches()) != 0 {
		t.Fatalf("a refused patch was recorded: %+v", session.Patches())
	}
}

func TestApplyPatchIsAllOrNothingAcrossItsSpans(t *testing.T) {
	inner := newTestMemory()
	for _, address := range []uint32{0x1000, 0x1100} {
		if err := inner.WriteMemory(address, []byte{0xaa, 0xbb}); err != nil {
			t.Fatal(err)
		}
	}
	memory := &failingMemory{testMemory: inner, refuseAt: 0x1100}
	session := NewSession(memory)

	err := session.ApplyPatch(PatchEntry{
		Name: "branch and constant",
		Patches: []Patch{
			{Address: 0x1000, Expect: span(0xaa, 0xbb), Replace: span(0x01, 0x02)},
			{Address: 0x1100, Expect: span(0xaa, 0xbb), Replace: span(0x03, 0x04)},
		},
	})
	if err == nil {
		t.Fatal("a patch whose second span could not be written reported success")
	}
	// Half an entry is a guest state neither the game nor the patch describes,
	// so the first span has to have been put back.
	read := make([]byte, 2)
	if err := inner.ReadMemory(0x1000, read); err != nil {
		t.Fatal(err)
	}
	if read[0] != 0xaa || read[1] != 0xbb {
		t.Fatalf("the first span was left patched after the second failed: %x", read)
	}
	if len(session.Patches()) != 0 {
		t.Fatalf("a failed patch was recorded: %+v", session.Patches())
	}
}

func TestApplyAndRevertPatchRoundTrips(t *testing.T) {
	memory := newTestMemory()
	session := NewSession(memory)
	if err := memory.WriteMemory(0x1000, []byte{0x0a, 0x00, 0x00, 0xea}); err != nil {
		t.Fatal(err)
	}

	entry := PatchEntry{
		Name: "gate",
		Note: "turn the branch around",
		Patches: []Patch{
			{Address: 0x1000, Expect: span(0x0a, 0x00, 0x00, 0xea), Replace: span(0x00, 0x00, 0x00, 0xea)},
		},
	}
	if err := session.ApplyPatch(entry); err != nil {
		t.Fatalf("apply: %v", err)
	}
	read := make([]byte, 4)
	if err := memory.ReadMemory(0x1000, read); err != nil {
		t.Fatal(err)
	}
	if read[0] != 0x00 {
		t.Fatalf("the patch did not land: %x", read)
	}
	if !session.PatchApplied("gate") {
		t.Fatal("the applied patch is not listed")
	}
	if err := session.ApplyPatch(entry); err == nil {
		t.Fatal("the same entry applied twice")
	}

	if err := session.RevertPatch("gate"); err != nil {
		t.Fatalf("revert: %v", err)
	}
	if err := memory.ReadMemory(0x1000, read); err != nil {
		t.Fatal(err)
	}
	if read[0] != 0x0a {
		t.Fatalf("reverting did not put back what was there: %x", read)
	}
	if session.PatchApplied("gate") {
		t.Fatal("a reverted patch is still listed")
	}
}

func TestRevertRefusesWhenSomethingElseOwnsTheBytesNow(t *testing.T) {
	memory := newTestMemory()
	session := NewSession(memory)
	if err := memory.WriteMemory(0x1000, []byte{0x11, 0x22}); err != nil {
		t.Fatal(err)
	}
	if err := session.ApplyPatch(PatchEntry{
		Name:    "gate",
		Patches: []Patch{{Address: 0x1000, Expect: span(0x11, 0x22), Replace: span(0x33, 0x44)}},
	}); err != nil {
		t.Fatal(err)
	}
	// The guest has since put something of its own there. Restoring over it
	// would destroy state that has nothing to do with the patch.
	if err := memory.WriteMemory(0x1000, []byte{0x99, 0x99}); err != nil {
		t.Fatal(err)
	}
	if err := session.RevertPatch("gate"); err == nil {
		t.Fatal("a patch whose bytes had been taken over was reverted anyway")
	}
	read := make([]byte, 2)
	if err := memory.ReadMemory(0x1000, read); err != nil {
		t.Fatal(err)
	}
	if read[0] != 0x99 {
		t.Fatalf("the refused revert wrote anyway: %x", read)
	}
	if !session.ForgetPatch("gate") {
		t.Fatal("the record could not be dropped")
	}
	if session.PatchApplied("gate") {
		t.Fatal("the forgotten patch is still listed")
	}
}

func TestApplyPatchRefusesOverlappingSpans(t *testing.T) {
	memory := newTestMemory()
	session := NewSession(memory)

	err := session.ApplyPatch(PatchEntry{
		Name: "overlapping",
		Patches: []Patch{
			{Address: 0x1000, Expect: span(0, 0, 0, 0), Replace: span(1, 1, 1, 1)},
			{Address: 0x1002, Expect: span(0, 0), Replace: span(2, 2)},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "overlap") {
		t.Fatalf("overlapping spans in one entry were accepted: %v", err)
	}

	if err := session.ApplyPatch(PatchEntry{
		Name:    "first",
		Patches: []Patch{{Address: 0x1000, Expect: span(0, 0, 0, 0), Replace: span(1, 1, 1, 1)}},
	}); err != nil {
		t.Fatal(err)
	}
	err = session.ApplyPatch(PatchEntry{
		Name:    "second",
		Patches: []Patch{{Address: 0x1002, Expect: span(1, 1), Replace: span(2, 2)}},
	})
	if err == nil || !strings.Contains(err.Error(), "already applied") {
		t.Fatalf("a span overlapping an applied patch was accepted: %v", err)
	}
}

func TestApplyPatchRefusesASpanThatChangesLength(t *testing.T) {
	session := NewSession(newTestMemory())
	err := session.ApplyPatch(PatchEntry{
		Name:    "resize",
		Patches: []Patch{{Address: 0x1000, Expect: span(0, 0), Replace: span(1)}},
	})
	if err == nil || !strings.Contains(err.Error(), "change the length") {
		t.Fatalf("a patch that resizes its span was accepted: %v", err)
	}
}

func TestPatchJSONReadsHexAddressesAndSpacedBytes(t *testing.T) {
	var entry PatchEntry
	text := `{"name":"gate","note":"why","patches":[{"address":"0x0001f004","expect":"de ad be ef","replace":"00000000"}]}`
	if err := json.Unmarshal([]byte(text), &entry); err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(entry.Patches) != 1 {
		t.Fatalf("read %d span(s)", len(entry.Patches))
	}
	patch := entry.Patches[0]
	if patch.Address != 0x1f004 {
		t.Fatalf("address = %#x", uint32(patch.Address))
	}
	if fmt.Sprintf("%x", []byte(patch.Expect)) != "deadbeef" {
		t.Fatalf("expect = %x", []byte(patch.Expect))
	}
	// A plain number is a guest address too, so a table written either way
	// reads.
	var plain Patch
	if err := json.Unmarshal([]byte(`{"address":4096,"expect":"00","replace":"01"}`), &plain); err != nil {
		t.Fatalf("read a numeric address: %v", err)
	}
	if plain.Address != 0x1000 {
		t.Fatalf("numeric address = %#x", uint32(plain.Address))
	}
	// And it writes back as hex, which is how a patch address is read.
	written, err := json.Marshal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), `"0x00001000"`) {
		t.Fatalf("written as %s", written)
	}
}

func TestSweepSkipsCodeRegions(t *testing.T) {
	memory := &codeRegionMemory{testMemory: newTestMemory()}
	// The same word in both halves; only the data half may be found.
	memory.writeU32(0x1000, 0x1234)
	memory.writeU32(0x1200, 0x1234)
	session := NewSession(memory)

	if _, err := session.Scan(ScanFilter{Op: FilterEq, A: 0x1234}); err != nil {
		t.Fatal(err)
	}
	candidates := session.Candidates()
	if len(candidates) != 1 {
		t.Fatalf("scan found %d candidate(s), want the data one only: %+v", len(candidates), candidates)
	}
	if candidates[0].Address != 0x1000 {
		t.Fatalf("the surviving candidate is 0x%08x, which is in the code region", candidates[0].Address)
	}
	// Code stays readable and writable: a byte patch has to reach it.
	if err := session.ApplyPatch(PatchEntry{
		Name:    "into code",
		Patches: []Patch{{Address: 0x1200, Expect: span(0x34, 0x12, 0x00, 0x00), Replace: span(0, 0, 0, 0)}},
	}); err != nil {
		t.Fatalf("a patch into a code region was refused: %v", err)
	}
}

// codeRegionMemory reports its upper half as code, which is what a platform
// does for an arena that holds veneers rather than state.
type codeRegionMemory struct{ *testMemory }

func (memory *codeRegionMemory) Regions() []Region {
	return []Region{
		{Base: memory.base, Size: 0x200, Label: "data"},
		{Base: memory.base + 0x200, Size: 0x200, Label: "stubs", Code: true},
	}
}
