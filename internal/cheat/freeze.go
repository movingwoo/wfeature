package cheat

// FreezeEntry is one value rewritten into the guest after every tick.
type FreezeEntry struct {
	Address   uint32
	ValueType ValueType
	Value     int64
	Label     string
}

// FreezeList holds the values rewritten into the guest after every emulator
// tick. Rewriting once per frame rather than trapping every store is enough
// for these games and costs nothing while no values are frozen.
type FreezeList struct {
	entries []FreezeEntry
}

// Entries exposes the frozen values in insertion order.
func (freezes *FreezeList) Entries() []FreezeEntry { return freezes.entries }

func (freezes *FreezeList) Len() int { return len(freezes.entries) }

// Insert adds an entry, replacing any existing freeze on the same address.
// It reports whether an existing entry was replaced.
func (freezes *FreezeList) Insert(entry FreezeEntry) bool {
	for index := range freezes.entries {
		if freezes.entries[index].Address == entry.Address {
			freezes.entries[index] = entry
			return true
		}
	}
	freezes.entries = append(freezes.entries, entry)
	return false
}

// Remove drops the freeze at address, reporting whether one existed.
func (freezes *FreezeList) Remove(address uint32) bool {
	kept := freezes.entries[:0]
	for _, entry := range freezes.entries {
		if entry.Address != address {
			kept = append(kept, entry)
		}
	}
	removed := len(kept) != len(freezes.entries)
	freezes.entries = kept
	return removed
}

// Clear drops every freeze.
func (freezes *FreezeList) Clear() {
	freezes.entries = nil
}

// Apply rewrites every frozen value. Entries whose write fails are reported
// rather than dropped, since a transiently unmapped address should not
// silently disable a cheat.
func (freezes *FreezeList) Apply(target MemoryTarget) []uint32 {
	var failed []uint32
	for _, entry := range freezes.entries {
		bytes, err := entry.ValueType.Encode(entry.Value)
		if err != nil {
			failed = append(failed, entry.Address)
			continue
		}
		if target.WriteMemory(entry.Address, bytes) != nil {
			failed = append(failed, entry.Address)
		}
	}
	return failed
}
