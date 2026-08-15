package lgt

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/cheat"
)

func TestSessionCheatScansAndFreezesGuestMemory(t *testing.T) {
	session, err := StartSession(context.Background(), fixtureArchive(t), SessionOptions{
		Width: 16, Height: 8, MaxSteps: 1 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}

	// A distinctive value inside the module's own data, where game state lives
	// in a real title.
	const address = fixtureDataBase + 0xf0
	var word [4]byte
	binary.LittleEndian.PutUint32(word[:], 123456)
	if err := session.client.core.Memory().Write(address, word[:]); err != nil {
		t.Fatal(err)
	}

	engine := session.Cheat()
	regions := engine.Regions()
	if len(regions) == 0 {
		t.Fatal("cheat session reports no committed regions")
	}
	// The module is not at a fixed base here — an ELF keeps whatever addresses
	// it names — so the label has to come from the loaded module's own span.
	foundModule := false
	for _, region := range regions {
		if region.Label == "module" {
			foundModule = true
		}
	}
	if !foundModule {
		t.Fatalf("no region labeled module: %+v", regions)
	}

	count, err := engine.Scan(cheat.ScanFilter{Op: cheat.FilterEq, A: 123456})
	if err != nil || count != 1 {
		t.Fatalf("scan = %d/%v, want exactly 1", count, err)
	}
	if engine.Candidates()[0].Address != address {
		t.Fatalf("candidate = %#x, want %#x", engine.Candidates()[0].Address, address)
	}

	valueType, _ := cheat.ParseValueType("u32")
	if _, err := engine.Freeze(address, valueType, 9999, "hp"); err != nil {
		t.Fatal(err)
	}
	// The game overwrites the value; the per-tick service puts it back.
	binary.LittleEndian.PutUint32(word[:], 3)
	if err := session.client.core.Memory().Write(address, word[:]); err != nil {
		t.Fatal(err)
	}
	if err := session.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	value, err := engine.ReadValue(address, valueType)
	if err != nil || value != 9999 {
		t.Fatalf("frozen value = %d/%v, want 9999", value, err)
	}

	console := session.CheatConsole()
	if output := console.Execute("frozen"); output == "nothing frozen" {
		t.Fatalf("console does not share the session: %q", output)
	}
}
