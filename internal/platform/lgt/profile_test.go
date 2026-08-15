package lgt

import "testing"

// A Clet has no method names, so what the profile can say about an address is
// where it is. Executable code is deliberately left unnamed: the shared region
// grouping only groups frames without a symbol, and naming every address in
// the module `.text` would collapse the whole ranking into one row — the
// opposite of what a profile of a nameless image is for.
func TestAddressPlacerLeavesCodeUnnamedAndNamesEverythingElse(t *testing.T) {
	client := &Client{module: &Module{Sections: []Section{
		{Name: ".text", Address: 0x1000, Size: 0x2000, Executable: true},
		{Name: ".data", Address: 0x3000, Size: 0x1000},
	}}}
	placer := client.newAddressPlacer()

	if frame := placer.place(0x1800); frame.Symbol != "" {
		t.Fatalf("an executable address was named %q, so its loop cannot group", frame.Symbol)
	}
	if frame := placer.place(0x3100); frame.Symbol != ".data" || frame.Offset != 0x100 {
		t.Fatalf("data address = %q+%#x, want .data+0x100", frame.Symbol, frame.Offset)
	}
	// The platform's own regions matter for the same reason: a sample in the
	// stub area is a platform call in flight, and a bare address cannot say so.
	if frame := placer.place(heapBase + 0x40); frame.Symbol != "[heap]" {
		t.Fatalf("heap address = %q, want [heap]", frame.Symbol)
	}
	if frame := placer.place(platformCodeBase + 4); frame.Symbol != "[platform stubs]" {
		t.Fatalf("stub address = %q, want [platform stubs]", frame.Symbol)
	}
	// An address in nothing known stays unnamed rather than being attached to
	// whichever span happens to sort next to it.
	if frame := placer.place(0x9000); frame.Symbol != "" {
		t.Fatalf("unmapped address was named %q", frame.Symbol)
	}
}

// A module with no sections still has to answer, because the platform regions
// are known whatever the module turned out to be.
func TestAddressPlacerWithoutAModuleStillPlacesThePlatform(t *testing.T) {
	placer := (&Client{}).newAddressPlacer()
	if frame := placer.place(stackBase + 8); frame.Symbol != "[stack]" {
		t.Fatalf("stack address = %q, want [stack]", frame.Symbol)
	}
	if frame := placer.place(0x1000); frame.Symbol != "" {
		t.Fatalf("address outside every region was named %q", frame.Symbol)
	}
}
