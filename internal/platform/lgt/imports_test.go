package lgt

import (
	"context"
	"testing"
)

// What an LGT title links against exists nowhere except the resolutions it
// makes while it starts, so what this pins is that they are kept — and kept
// apart: a slot that would be serviced and a slot that only resolves are both
// answered with a stub, and only the record says which is which.
func TestResolvedImportsSeparateWhatIsAnsweredFromWhatOnlyResolves(t *testing.T) {
	archive, err := Open(fixtureArchive(t))
	if err != nil {
		t.Fatal(err)
	}
	client, err := Load(archive, Options{Width: 16, Height: 8, MaxSteps: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(client.ResolvedImports()) != 0 {
		t.Fatal("a client that has not started has already resolved something")
	}
	if err := client.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	records := map[uint32]ImportRecord{}
	for _, record := range client.ResolvedImports() {
		if record.Category != svcCategoryWIPIC {
			t.Errorf("the fixture resolved %s, which it does not ask for", record.Describe())
			continue
		}
		records[record.Slot] = record
	}
	// The fixture module resolves these four and nothing else. Each one is
	// serviced, so each one is a record that says so, named rather than
	// numbered.
	for _, slot := range []uint32{
		slotCletRegister, slotGetScreenFramebuffer, slotFramebufferPointer, slotFlushLcd,
	} {
		record, ok := records[slot]
		if !ok {
			t.Fatalf("the module resolved slot %#x and it was not recorded: %v", slot, client.ResolvedImports())
		}
		if !record.Implemented {
			t.Errorf("slot %#x is serviced but the record calls it unimplemented", slot)
		}
		if record.Name != wipicSlotNames[slot] || record.Name == "" {
			t.Errorf("slot %#x is named %q, want %q", slot, record.Name, wipicSlotNames[slot])
		}
	}

	// A slot with no implementation resolves the same way — refusing here would
	// stop a title over a function it never calls — and is what the record has
	// to tell apart.
	const unimplemented uint32 = 0x4c9
	if knownWIPICSlot(unimplemented) || unknownSlotAccepted(unimplemented) {
		t.Fatalf("slot %#x is implemented now; pick another for this test", unimplemented)
	}
	if _, err := client.importFunction(importTableWIPIC, unimplemented); err != nil {
		t.Fatalf("resolving an unimplemented slot = %v, want a stub", err)
	}
	var found bool
	for _, record := range client.ResolvedImports() {
		if record.Category == svcCategoryWIPIC && record.Slot == unimplemented {
			found = true
			if record.Implemented {
				t.Errorf("%s is recorded as implemented", record.Describe())
			}
		}
	}
	if !found {
		t.Errorf("resolving slot %#x recorded nothing", unimplemented)
	}
}

// A Java title resolves its imports from tables this platform packs into one
// slot space of its own. A report that printed the packed number would be
// naming an address in this platform rather than anything the module passed.
func TestAJavaAuxiliaryImportIsDescribedAsTheTableTheModuleAsked(t *testing.T) {
	archive, err := Open(fixtureArchive(t))
	if err != nil {
		t.Fatal(err)
	}
	client, err := Load(archive, Options{Width: 16, Height: 8})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.importFunction(0x1fc, 3); err != nil {
		t.Fatalf("resolving an auxiliary Java table = %v, want a stub", err)
	}
	var described string
	for _, record := range client.ResolvedImports() {
		described = record.Describe()
	}
	if described != "java table 0x1fc index 0x3" {
		t.Fatalf("Describe() = %q", described)
	}
}
