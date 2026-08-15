package cheat

import (
	"testing"
)

// watchableTarget is a memory target that also records writes, which is what a
// real emulated address space provides.
type watchableTarget struct {
	*testMemory
	watches map[uint32]struct{}
	hits    []WatchHit
}

func newWatchableTarget(target *testMemory) *watchableTarget {
	return &watchableTarget{testMemory: target, watches: map[uint32]struct{}{}}
}

func (target *watchableTarget) Watch(address uint32)   { target.watches[address] = struct{}{} }
func (target *watchableTarget) Unwatch(address uint32) { delete(target.watches, address) }
func (target *watchableTarget) ClearWatches()          { target.watches = map[uint32]struct{}{} }
func (target *watchableTarget) Watches() []uint32 {
	addresses := make([]uint32, 0, len(target.watches))
	for address := range target.watches {
		addresses = append(addresses, address)
	}
	return addresses
}
func (target *watchableTarget) WatchHits() []WatchHit     { return target.hits }
func (target *watchableTarget) WatchHitsOverflowed() bool { return false }

func TestWatchCallsReportAPlatformThatCannotWatch(t *testing.T) {
	session := NewSession(newTestMemory())
	if session.CanWatch() {
		t.Fatal("a plain memory target claimed it can watch writes")
	}
	if err := session.Watch(0x1000); err != ErrWatchUnsupported {
		t.Fatalf("Watch on an uninstrumented target = %v, want ErrWatchUnsupported", err)
	}
	if _, _, err := session.WatchHits(); err != ErrWatchUnsupported {
		t.Fatalf("WatchHits on an uninstrumented target = %v, want ErrWatchUnsupported", err)
	}
	// A table still saves and loads; it simply carries no watches.
	table := session.SaveTable("game")
	if len(table.Watches) != 0 {
		t.Fatalf("table carries %d watches from a target that cannot watch", len(table.Watches))
	}
}

func TestTableRoundTripsFrozenValuesAndWatches(t *testing.T) {
	target := newWatchableTarget(newTestMemory())
	session := NewSession(target)

	valueType := ValueType{Kind: KindU32, Endian: Little}
	if _, err := session.Freeze(0x1004, valueType, 1234, "gold"); err != nil {
		t.Fatal(err)
	}
	if err := session.Watch(0x1004); err != nil {
		t.Fatal(err)
	}

	data, err := MarshalTable(session.SaveTable("테스트게임"))
	if err != nil {
		t.Fatal(err)
	}

	// A fresh session over the same memory has nothing held until the table
	// is loaded, which is the case a saved table exists for.
	restored := NewSession(newWatchableTarget(newTestMemory()))
	table, err := UnmarshalTable(data)
	if err != nil {
		t.Fatal(err)
	}
	if table.Game != "테스트게임" {
		t.Errorf("table names game %q, want 테스트게임", table.Game)
	}
	applied, err := restored.LoadTable(table)
	if err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("applied %d entries, want 1", applied)
	}

	entries := restored.Freezes().Entries()
	if len(entries) != 1 {
		t.Fatalf("restored %d frozen values, want 1", len(entries))
	}
	if entries[0].Address != 0x1004 || entries[0].Value != 1234 || entries[0].Label != "gold" {
		t.Errorf("restored entry is %+v, want gold=1234 at 0x1004", entries[0])
	}
	// Loading writes the value, not just the record: a table is applied.
	value, err := restored.ReadValue(0x1004, valueType)
	if err != nil {
		t.Fatal(err)
	}
	if value != 1234 {
		t.Errorf("memory holds %d after loading, want 1234", value)
	}
	watches, err := restored.Watches()
	if err != nil {
		t.Fatal(err)
	}
	if len(watches) != 1 || watches[0] != 0x1004 {
		t.Errorf("restored watches = %#x, want [0x1004]", watches)
	}
}

// Loading replaces what a session held rather than adding to it, because a
// table describes a complete set.
func TestLoadTableReplacesWhatWasHeld(t *testing.T) {
	session := NewSession(newWatchableTarget(newTestMemory()))
	valueType := ValueType{Kind: KindU32, Endian: Little}
	if _, err := session.Freeze(0x1000, valueType, 7, "old"); err != nil {
		t.Fatal(err)
	}
	if _, err := session.LoadTable(Table{Entries: []TableEntry{
		{Address: 0x1008, Value: 42, Type: "u32", Frozen: true, Name: "new"},
	}}); err != nil {
		t.Fatal(err)
	}
	entries := session.Freezes().Entries()
	if len(entries) != 1 || entries[0].Address != 0x1008 {
		t.Fatalf("after loading, frozen values are %+v; want only the table's", entries)
	}
}

func TestUnmarshalTableRejectsAnUnknownType(t *testing.T) {
	if _, err := UnmarshalTable([]byte(`{"entries":[{"address":16,"value":1,"type":"u128"}]}`)); err == nil {
		t.Fatal("a table naming an unknown value type was accepted")
	}
	if _, err := UnmarshalTable([]byte(`not json`)); err == nil {
		t.Fatal("a table that is not JSON was accepted")
	}
}
