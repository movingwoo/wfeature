package ktf

import (
	"encoding/binary"
	"strings"
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

// This platform writes into the guest's address space constantly — a Java
// object's fields are guest words the runtime keeps in step — and a watch that
// saw only guest instructions reported no writers for exactly those addresses.
// The engine's own freeze rewrites are the one kind that must stay out: they
// land every tick, and counting them would make the cheat the loudest writer of
// every address it holds.
func TestWatchSeesPlatformWritesButNotTheCheatsOwn(t *testing.T) {
	client := syntheticLifecycleClient(t)
	session := &Session{Client: client}
	engine := session.Cheat()

	const address = ImageBase + 0x1f0
	if err := engine.Watch(address); err != nil {
		t.Fatal(err)
	}

	var word [4]byte
	binary.LittleEndian.PutUint32(word[:], 4242)
	if err := client.Core().Memory().Write(address, word[:]); err != nil {
		t.Fatal(err)
	}

	hits, _, err := engine.WatchHits()
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Origin != cheat.OriginHost || hits[0].Value != 4242 {
		t.Fatalf("platform write recorded as %+v, want one host write of 4242", hits)
	}
	// A reader has to be told, or the PC beside a host write reads as an
	// address to go and disassemble when it is only where the guest stopped.
	if output := session.CheatConsole().Execute("hits"); !strings.Contains(output, "host, last pc") {
		t.Fatalf("hits did not mark the host write: %q", output)
	}

	valueType, _ := cheat.ParseValueType("u32")
	if _, err := engine.Freeze(address, valueType, 9999, "hp"); err != nil {
		t.Fatal(err)
	}
	for range 5 {
		if err := session.serviceCheat(); err != nil {
			t.Fatal(err)
		}
	}
	after, _, err := engine.WatchHits()
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || after[0].Count != hits[0].Count {
		t.Fatalf("the cheat's own freeze was recorded as a writer: %+v", after)
	}
}
