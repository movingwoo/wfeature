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

// TableKey identifies what a table was made against.
//
// A name cannot carry a patch. The same title arrives as several archives —
// repacked, renamed, one container swapped for another around the same
// executable — and a byte patch is true of the image rather than of whatever
// the file was called. So the key is a hash: the loaded executable image
// first, and the file it was read from as the fallback for a platform that has
// no single image to hash.
type TableKey struct {
	// Image is the SHA-256 of the loaded executable image, in lower-case hex.
	Image string `json:"image,omitempty"`
	// File is the SHA-256 of the archive the image was read from.
	File string `json:"file,omitempty"`
}

// KeyMatch says how confidently a table belongs to the session reading it.
type KeyMatch uint8

const (
	// MatchNone: the table carries a key and neither half is this session's.
	MatchNone KeyMatch = iota
	// MatchUnkeyed: the table carries no key, or the session has none to
	// compare it with. Nothing can be said, which is not a mismatch.
	MatchUnkeyed
	// MatchFile: the archive file is the same one.
	MatchFile
	// MatchImage: the executable image is the same one, whatever container it
	// arrived in.
	MatchImage
)

// Table is a saved set of cheats.
type Table struct {
	TableKey
	// Game is what the table was made against in a person's words. It is a
	// label rather than the key: a table written before the key existed
	// carries only this, and it still loads.
	Game    string       `json:"game,omitempty"`
	Entries []TableEntry `json:"entries"`
	// Watches are the addresses whose writers were being traced.
	Watches []uint32 `json:"watches,omitempty"`
	// Patches are the byte patches the table applies, and they are why the key
	// is a hash. A frozen address that has moved reads as a wrong number and a
	// person notices; a patch applied to the wrong image would corrupt a
	// running guest. Its declared bytes are the guard against that, and the
	// key is what says whether it should have been offered at all.
	Patches []PatchEntry `json:"patches,omitempty"`
}

// Match reports how the table's key compares with the session's, checking the
// image first because that is the identity a repackaged archive keeps.
func (table Table) Match(key TableKey) KeyMatch {
	switch {
	case table.Image != "" && key.Image != "" && table.Image == key.Image:
		return MatchImage
	case table.File != "" && key.File != "" && table.File == key.File:
		return MatchFile
	case table.Image == "" && table.File == "":
		return MatchUnkeyed
	case key.Image == "" && key.File == "":
		return MatchUnkeyed
	default:
		return MatchNone
	}
}

// SaveTable captures the session's frozen values, watches and byte patches,
// keyed by what the session is running.
func (session *Session) SaveTable(game string) Table {
	table := Table{TableKey: session.key, Game: game, Patches: session.Patches()}
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

// LoadTable applies a table: its byte patches go in first, then every frozen
// entry is written and held, then every watch is re-armed. It replaces what
// the session was holding, because a table describes a complete set rather
// than an addition to one.
//
// Patches go first because a patch is usually what makes the rest reachable,
// and a refused patch stops the load before any value is written. The count
// returned is of frozen values, as it always was; Patches() reports what went
// in.
//
// A table naming a platform that cannot watch still loads its values; the
// watches are reported as skipped rather than failing the load.
func (session *Session) LoadTable(table Table) (applied int, err error) {
	if _, revertErr := session.RevertAllPatches(); revertErr != nil {
		return 0, revertErr
	}
	session.freezes.Clear()
	_ = session.ClearWatches()

	for index, entry := range table.Patches {
		if patchErr := session.ApplyPatch(entry); patchErr != nil {
			return 0, fmt.Errorf("table patch %d: %w", index+1, patchErr)
		}
	}
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
	// A patch entry is checked against guest memory when it is applied, but
	// what a table can be read for on its own — that every entry is named and
	// says something — is worth answering before a session exists.
	for index := range table.Patches {
		table.Patches[index].Name = strings.TrimSpace(table.Patches[index].Name)
		if table.Patches[index].Name == "" {
			return Table{}, fmt.Errorf("patch %d has no name", index+1)
		}
		if len(table.Patches[index].Patches) == 0 {
			return Table{}, fmt.Errorf("patch %d (%s) has no spans", index+1, table.Patches[index].Name)
		}
	}
	table.Image = strings.ToLower(strings.TrimSpace(table.Image))
	table.File = strings.ToLower(strings.TrimSpace(table.File))
	return table, nil
}
