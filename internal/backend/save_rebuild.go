package backend

import (
	"fmt"
	"strconv"
)

// RebuildReader is the store a quick load reads through while it rebuilds a
// restored runtime's view of its saves.
//
// A load restores execution state only, so every host copy of a save the
// restored runtime holds — an open file's buffer, a record list, a removal
// list — is read again from the store as it is when the load commits. The
// names those reads are made with come from the slot, which is untrusted, and
// the runtime's ordinary lookups read a save whole. This store stands between
// the two:
//
//   - A key is read once. Every object of one name is filled from the same
//     bytes, and a slot that names the largest save a thousand times is one
//     read of it.
//   - The reads share a budget. A key larger than what is left of it is
//     refused before it is read (see ReadSaveLimit), and what is read is taken
//     off it.
//   - Nothing is written. A load writes no save, so a write that reaches this
//     store is a defect in the rebuild: it is refused, and remembered.
//
// The answers are the base store's own slices, kept until the reader is
// dropped. A caller copies what it keeps and changes nothing it was handed.
//
// It is not safe for concurrent use: a rebuild runs on the one goroutine that
// commits the load, with the runtime it fills not yet running.
type RebuildReader struct {
	base      SaveStore
	remaining int64
	read      map[string]rebuildRead
	err       error
}

type rebuildRead struct {
	data    []byte
	present bool
}

// NewRebuildReader wraps the store a load commits over. limit is how many
// bytes the rebuild may read from it in all.
func NewRebuildReader(base SaveStore, limit int64) *RebuildReader {
	return &RebuildReader{base: base, remaining: max(limit, 0), read: make(map[string]rebuildRead)}
}

// Err is the first failure the reader met: a read the base store failed or
// refused as too large, or a write that was attempted. A rebuild that ends
// with one is not adopted.
func (reader *RebuildReader) Err() error {
	if reader == nil {
		return nil
	}
	return reader.err
}

// LoadSave is ReadSave without its error, which Err still holds.
func (reader *RebuildReader) LoadSave(name string) ([]byte, bool) {
	data, present, _ := reader.ReadSave(name)
	return data, present
}

// ReadSave answers one entry of the base store, reading it the first time the
// key is asked for.
func (reader *RebuildReader) ReadSave(name string) ([]byte, bool, error) {
	key, err := NormalizeSaveKey(name)
	if err != nil {
		reader.fail(err)
		return nil, false, err
	}
	if read, seen := reader.read[key]; seen {
		return read.data, read.present, nil
	}
	data, present, err := ReadSaveLimit(reader.base, key, reader.remaining)
	if err != nil {
		reader.fail(err)
		return nil, false, err
	}
	reader.remaining -= int64(len(data))
	reader.read[key] = rebuildRead{data: data, present: present}
	return data, present, nil
}

// StoreSave refuses the write and remembers that one was attempted.
func (reader *RebuildReader) StoreSave(name string, _ []byte) error {
	return reader.refuse(name)
}

// StoreSaves refuses the batch and remembers that one was attempted.
func (reader *RebuildReader) StoreSaves(entries map[string][]byte) error {
	first, named := "", false
	for name := range entries {
		if !named || name < first {
			first, named = name, true
		}
	}
	return reader.refuse(first)
}

func (reader *RebuildReader) refuse(name string) error {
	const limit = 64
	quoted := strconv.Quote(name)
	if len(name) > limit {
		quoted = strconv.Quote(name[:limit]) + "..."
	}
	err := fmt.Errorf("a quick load writes no save, and a write of %s was attempted", quoted)
	reader.fail(err)
	return err
}

func (reader *RebuildReader) fail(err error) {
	if reader.err == nil {
		reader.err = err
	}
}
