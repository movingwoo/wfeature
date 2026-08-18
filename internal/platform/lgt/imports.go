package lgt

import (
	"fmt"
	"sort"
)

// A module's imports are the closest thing an LGT title has to a link map.
//
// There is no list of them in the archive: an ELF here carries no dynamic
// symbols, and the only place a platform function is named is the pair of
// numbers the module passes to `get import function` while it is starting. It
// passes every one of them, for everything it might ever call, before it runs
// any of its own code — so the resolutions a startup produces are the whole
// surface the title links against, which is the question a compatibility pass
// asks first and which a run answers one call at a time.
//
// Recording them costs a map entry per distinct pair, which a module makes a
// few hundred of, once. `internal/tools/apiscan` is what reads them back.

// ImportRecord is one platform function a module resolved at startup.
type ImportRecord struct {
	// Category is the SVC category the resolved stub traps into, which is what
	// names the slot: the import table a module asks is not always the table
	// that services the call, since the OEM table's Java entry is serviced
	// with the Java ones.
	Category uint32
	Slot     uint32
	// Name is what this platform calls the slot, or empty when it has no name
	// for it. It is filled in at report time rather than at resolution time,
	// because a Java title's slots are numbers until the class metadata it
	// hands over later says what they stand for.
	Name string
	// Implemented reports whether reaching this slot would be serviced.
	// Resolution answers everything either way — a module resolves what it
	// might use, and refusing here would stop a title over a function it never
	// calls — so this is what separates "resolved" from "answered".
	Implemented bool
}

// recordImport notes one (category, slot) resolution. It is called before the
// stub is built, because building one takes the client lock.
func (client *Client) recordImport(category, slot uint32) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.imports == nil {
		client.imports = map[[2]uint32]bool{}
	}
	client.imports[[2]uint32{category, slot}] = true
}

// ResolvedImports reports every platform function the module has resolved,
// ordered by category and slot. It is meaningful once Start has returned: a
// module resolves everything at startup, so a client that has not started has
// nothing to report and one that is playing has nothing to add.
func (client *Client) ResolvedImports() []ImportRecord {
	client.mu.Lock()
	pairs := make([][2]uint32, 0, len(client.imports))
	for pair := range client.imports {
		pairs = append(pairs, pair)
	}
	client.mu.Unlock()

	sort.Slice(pairs, func(left, right int) bool {
		if pairs[left][0] != pairs[right][0] {
			return pairs[left][0] < pairs[right][0]
		}
		return pairs[left][1] < pairs[right][1]
	})
	records := make([]ImportRecord, 0, len(pairs))
	for _, pair := range pairs {
		category, slot := pair[0], pair[1]
		records = append(records, ImportRecord{
			Category:    category,
			Slot:        slot,
			Name:        client.svcSlotName(category, slot),
			Implemented: client.importImplemented(category, slot),
		})
	}
	return records
}

// Describe names a slot the way a report should print it: the table and the
// entry, and the platform's own name for it when it has one.
//
// A Java auxiliary slot is unpacked back into the table and index the module
// passed, because the number it is carried as in between is this platform's
// packing rather than anything the module ever said. Nothing is invented for a
// slot with no name — an unnamed slot is a finding, and a made-up name would
// hide it.
func (record ImportRecord) Describe() string {
	where := fmt.Sprintf("%s %#x", svcCategoryName(record.Category), record.Slot)
	if table, index, auxiliary := javaAuxiliaryParts(record.Slot); auxiliary {
		where = fmt.Sprintf("%s table %#x index %#x", svcCategoryName(record.Category), table, index)
	}
	if record.Name == "" {
		return where
	}
	return where + " " + record.Name
}

// importImplemented reports whether a call on this slot would be serviced.
// Each table answers it the way its own handler decides: the WIPI C and OEM
// tables keep the predicate their stubs already log against, the C library and
// the Java table are decided by whether the slot has a name at all, and a Java
// member slot is named only once the module's class metadata has arrived.
func (client *Client) importImplemented(category, slot uint32) bool {
	switch category {
	case svcCategoryInit:
		return true
	case svcCategoryWIPIC:
		return knownWIPICSlot(slot) || unknownSlotAccepted(slot)
	case svcCategoryOEM:
		return knownOEMSlot(slot)
	case svcCategoryStdlib:
		return stdlibSlotNames[slot] != ""
	case svcCategoryJava:
		if _, known := javaSVCArguments[slot]; known {
			return true
		}
		return client.javaSlotName(slot) != ""
	}
	return false
}
