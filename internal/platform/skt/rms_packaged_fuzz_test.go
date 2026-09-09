package skt

import (
	"encoding/binary"
	"testing"
)

// A container's record stores are untrusted input: they are files inside an
// archive somebody downloaded, and this parser reads a record table out of one
// of them before anything has said the file is a store at all. Every field it
// reads either indexes the data file or sizes an allocation, which is what
// makes the arithmetic worth fuzzing rather than only reading.
//
// The seeds are the shapes the parser has branches for, so the fuzzer starts
// inside the arithmetic rather than having to discover a header.
func FuzzPackagedRecordStoreNeverPanics(f *testing.F) {
	index, data := packagedStoreFiles("Carried", []byte("first"), []byte("second"))
	f.Add(index, data)
	empty, none := packagedStoreFiles("Empty")
	f.Add(empty, none)
	// A record of no bytes, which is a record rather than a hole.
	hole, holeData := packagedStoreFiles("Hole", []byte("aaaaa"), []byte{}, []byte("bbbbb"))
	f.Add(hole, holeData)
	// The two words that size an allocation, each at its limit.
	wide, wideData := packagedStoreFiles("Wide")
	binary.BigEndian.PutUint32(wide[0:4], packagedStoreMaxRecords)
	f.Add(wide, wideData)
	f.Add([]byte("qtnothing"), []byte{})
	f.Add([]byte{}, []byte{})
	f.Fuzz(func(t *testing.T, header, data []byte) {
		name, store, ok := parsePackagedRecordStore(header, data)
		if !ok {
			return
		}
		// A store that parsed has to be usable rather than merely
		// non-panicking. Everything below is something openStore or the RMS
		// surface would go on to rely on, and each one is a value a crafted
		// header could otherwise carry into them.
		if name == "" {
			t.Fatal("parsed a store with no name")
		}
		if len(store.records) > packagedStoreMaxRecords {
			t.Fatalf("parsed a store of %d slots, over the %d bound", len(store.records), packagedStoreMaxRecords)
		}
		carried := 0
		for id, record := range store.records {
			if record == nil {
				// A hole is an id the store no longer has, which is what the
				// runtime's own tombstone means.
				continue
			}
			if len(record) > rmsMaxRecordBytes {
				t.Fatalf("record %d is %d bytes, over the %d bound", id+1, len(record), rmsMaxRecordBytes)
			}
			carried += len(record)
		}
		// The entries may point anywhere inside the data file, including all
		// at the same place, so what they ask for between them is bounded by
		// the file they point into rather than by their own count.
		if carried > len(data) {
			t.Fatalf("records carry %d bytes out of a %d-byte data file", carried, len(data))
		}
	})
}

// The whole scan is the other half: one archive holds many of these, each
// asking for its slots before a record is read, so a budget across them is as
// much a bound as the one on each.
func FuzzPackagedRecordStoresNeverPanics(f *testing.F) {
	index, data := packagedStoreFiles("Carried", []byte("first"))
	f.Add(index, data, "#Carried")
	f.Add(index, data, "rs/#Carried")
	f.Add([]byte("qtnothing"), []byte{}, "x")
	f.Fuzz(func(t *testing.T, header, data []byte, stem string) {
		if len(stem) > 64 {
			return
		}
		archive := &Archive{Entries: map[string][]byte{
			stem + packagedStoreIndex: header,
			stem + packagedStoreData:  data,
		}}
		slots := 0
		for name, store := range archive.packagedRecordStores() {
			// A name that reached the store list has to be one the runtime can
			// open and delete, or listRecordStores would answer a name nothing
			// else in the surface accepts.
			if !validRecordStoreName(name) {
				t.Fatalf("carried a store named %q, which this runtime will not open", name)
			}
			if _, err := recordStoreKey(name); err != nil {
				t.Fatalf("carried a store named %q, which is not a save key: %v", name, err)
			}
			slots += len(store.records)
		}
		if slots > packagedStoreMaxSlots {
			t.Fatalf("one archive asked for %d slots, over the %d budget", slots, packagedStoreMaxSlots)
		}
	})
}

// A store name is the other untrusted input, and it arrives from two places:
// the guest, and — since the container's stores are read — an archive. What
// this pins is the property the review kept finding holes in one entry point
// at a time: a name this runtime accepts has to survive the list its index is,
// and must not address that list.
func FuzzRecordStoreNameSurvivesTheIndex(f *testing.F) {
	for _, name := range []string{"scores", "save ", ".index", "a\nb", "..", ".", "/", "", "Carried"} {
		f.Add(name)
	}
	f.Fuzz(func(t *testing.T, name string) {
		if !validRecordStoreName(name) {
			return
		}
		key, err := recordStoreKey(name)
		if err != nil {
			// A name the runtime says is valid and the store cannot key is
			// a store the title is told it opened and that never persists.
			t.Fatalf("validRecordStoreName(%q) but no save key: %v", name, err)
		}
		if key == rmsIndexKey {
			t.Fatalf("%q addresses the store index itself", name)
		}
		// The index is names joined by newlines and read back a line at a
		// time, so a name it accepts has to come back as itself.
		if rebuilt := splitStoreIndex(joinStoreIndex([]string{name})); len(rebuilt) != 1 || rebuilt[0] != name {
			t.Fatalf("%q came back from the index as %q", name, rebuilt)
		}
	})
}
