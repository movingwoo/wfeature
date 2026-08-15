package cheat

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// A cheat is found once and used forever, so what a session learns has to
// outlive it. A table is that record: the frozen values, the addresses being
// watched, and a note per entry, in a format a person can read and edit.
//
// Candidates are deliberately not saved. They are the middle of a search, not
// a result, and a list of ten thousand addresses that were plausible under a
// memory layout that no longer exists is worse than nothing.

// TableEntry is one saved value.
type TableEntry struct {
	// Name is what the entry is for — "gold", "hp" — carried so a table read
	// six months later still means something.
	Name    string `json:"name,omitempty"`
	Address uint32 `json:"address"`
	Value   int64  `json:"value"`
	// Type is the text form of the value type, which is what the console and
	// the browser both speak.
	Type string `json:"type"`
	// Frozen records whether this entry was being held when the table was
	// saved, so loading restores the freezes and not just the addresses.
	Frozen bool `json:"frozen"`
}

// Table is a saved set of cheats.
type Table struct {
	// Game names what the table was made against. Nothing enforces it — an
	// address from another game simply will not mean anything — but a table
	// that does not say is a table nobody can place.
	Game    string       `json:"game,omitempty"`
	Entries []TableEntry `json:"entries"`
	// Watches are the addresses whose writers were being traced.
	Watches []uint32 `json:"watches,omitempty"`
}

// SaveTable captures the session's frozen values and watches.
func (session *Session) SaveTable(game string) Table {
	table := Table{Game: game}
	for _, entry := range session.freezes.Entries() {
		table.Entries = append(table.Entries, TableEntry{
			Name:    entry.Label,
			Address: entry.Address,
			Value:   entry.Value,
			Type:    entry.ValueType.String(),
			Frozen:  true,
		})
	}
	sort.Slice(table.Entries, func(left, right int) bool {
		return table.Entries[left].Address < table.Entries[right].Address
	})
	if watches, err := session.Watches(); err == nil {
		table.Watches = watches
	}
	return table
}

// LoadTable applies a table: every frozen entry is written and held, and every
// watch is re-armed. It replaces what the session was holding, because a table
// describes a complete set rather than an addition to one.
//
// A table naming a platform that cannot watch still loads its values; the
// watches are reported as skipped rather than failing the load.
func (session *Session) LoadTable(table Table) (applied int, err error) {
	session.freezes.Clear()
	_ = session.ClearWatches()

	for index, entry := range table.Entries {
		valueType, ok := ParseValueType(entry.Type)
		if !ok {
			return applied, fmt.Errorf("entry %d (%s): %q is not a value type", index, entry.Name, entry.Type)
		}
		if !entry.Frozen {
			continue
		}
		if writeErr := session.WriteValue(entry.Address, valueType, entry.Value); writeErr != nil {
			return applied, fmt.Errorf("entry %d (%s) at %#x: %w", index, entry.Name, entry.Address, writeErr)
		}
		session.freezes.Insert(FreezeEntry{Address: entry.Address, ValueType: valueType, Value: entry.Value, Label: entry.Name})
		applied++
	}
	for _, address := range table.Watches {
		if watchErr := session.Watch(address); watchErr != nil {
			break // the platform cannot watch; the values still loaded
		}
	}
	return applied, nil
}

// MarshalTable writes a table as indented JSON, which is the form the CLI
// saves and a person edits.
func MarshalTable(table Table) ([]byte, error) {
	return json.MarshalIndent(table, "", "  ")
}

// UnmarshalTable reads a table, checking what JSON cannot: that every entry
// names a type this build understands.
func UnmarshalTable(data []byte) (Table, error) {
	var table Table
	if err := json.Unmarshal(data, &table); err != nil {
		return Table{}, fmt.Errorf("read cheat table: %w", err)
	}
	for index := range table.Entries {
		table.Entries[index].Type = strings.TrimSpace(table.Entries[index].Type)
		if _, ok := ParseValueType(table.Entries[index].Type); !ok {
			return Table{}, fmt.Errorf("entry %d (%s): %q is not a value type",
				index, table.Entries[index].Name, table.Entries[index].Type)
		}
	}
	return table, nil
}
