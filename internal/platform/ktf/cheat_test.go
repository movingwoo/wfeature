package ktf

import (
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/cheat"
)

func TestSessionCheatScansAndFreezesGuestMemory(t *testing.T) {
	client := syntheticLifecycleClient(t)
	session := &Session{Client: client}

	// A distinctive value inside the mapped client image, where game state
	// would live in a real title.
	const address = ImageBase + 0x1f0
	var word [4]byte
	binary.LittleEndian.PutUint32(word[:], 123456)
	if err := client.Core().Memory().Write(address, word[:]); err != nil {
		t.Fatal(err)
	}

	engine := session.Cheat()
	regions := engine.Regions()
	if len(regions) == 0 {
		t.Fatal("cheat session reports no committed regions")
	}
	foundClient := false
	for _, region := range regions {
		if region.Label == "client" {
			foundClient = true
		}
	}
	if !foundClient {
		t.Fatalf("no region labeled client: %+v", regions)
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
	if err := client.Core().Memory().Write(address, word[:]); err != nil {
		t.Fatal(err)
	}
	if err := session.serviceCheat(); err != nil {
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
