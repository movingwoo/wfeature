package ktf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
)

// maxHeapStorageBytes bounds what the storage objects of one checkpoint may
// hold between them: where it is taken, by what the objects hold then, and
// where it is loaded, by what the store holds for them.
const maxHeapStorageBytes = 128 << 20

// A database travels as its name. Names are bytes because JSON strings replace
// invalid UTF-8.
type heapDatabaseState struct {
	Name []byte
}

type heapDatabaseBinding struct {
	Name     []byte
	Database uint32
}

func (context *heapNativeContext) chargeStorage(size uint64) error {
	if size > maxHeapStorageBytes || context.storageBytes > maxHeapStorageBytes-size {
		return fmt.Errorf("KTF checkpoint storage holds more than the %d bytes a load restores", maxHeapStorageBytes)
	}
	context.storageBytes += size
	return nil
}

func validHeapDatabaseName(name string) bool {
	return len(name) <= 1<<20 && storableName(javaDatabaseScope, name)
}

func cloneHeapRecords(records [][]byte) [][]byte {
	if records == nil {
		return nil
	}
	copyRecords := make([][]byte, len(records))
	for i, record := range records {
		copyRecords[i] = bytes.Clone(record)
	}
	return copyRecords
}

func (context *heapNativeContext) captureDatabase(store *runtimeDataBaseStore) (uint32, error) {
	if store == nil {
		return 0, fmt.Errorf("KTF heap database is nil")
	}
	if id := context.databaseIDs[store]; id != 0 {
		return id, nil
	}
	if len(context.databases) >= 1<<16 {
		return 0, fmt.Errorf("KTF heap database count exceeds limit")
	}
	if !validHeapDatabaseName(store.name) || len(store.records) > maxDataBaseRecords {
		return 0, fmt.Errorf("KTF heap database name or record count is invalid")
	}
	// What the store holds is not recorded, and is charged all the same: a
	// load reads it back, on the same budget.
	if err := context.chargeStorage(uint64(len(store.name)) + recordListBytes(store.records) + 128); err != nil {
		return 0, err
	}
	if context.databaseIDs == nil {
		context.databaseIDs = make(map[*runtimeDataBaseStore]uint32)
	}
	id := uint32(len(context.databases) + 1)
	context.databaseIDs[store] = id
	context.databases = append(context.databases, heapDatabaseState{Name: []byte(store.name)})
	return id, nil
}

func (context *heapNativeContext) captureDatabaseBindings() ([]heapDatabaseBinding, error) {
	if len(context.runtime.databases) > 1<<16 {
		return nil, fmt.Errorf("KTF heap database catalog exceeds limit")
	}
	names := make([]string, 0, len(context.runtime.databases))
	for name := range context.runtime.databases {
		names = append(names, name)
	}
	slices.Sort(names)
	bindings := make([]heapDatabaseBinding, 0, len(names))
	for _, name := range names {
		store := context.runtime.databases[name]
		if store == nil || store.name != name {
			return nil, fmt.Errorf("KTF heap database catalog name differs from its store")
		}
		id, err := context.captureDatabase(store)
		if err != nil {
			return nil, err
		}
		bindings = append(bindings, heapDatabaseBinding{Name: []byte(name), Database: id})
	}
	return bindings, nil
}

// restoreDatabases builds the named, empty stores a record lists and answers
// the catalog over them. One name is one store: a record can name a database
// twice — the catalog's, and one an object kept after the title deleted it —
// and both are the same database once they are read back from the store.
func (context *heapNativeContext) restoreDatabases(records []heapDatabaseState, bindings []heapDatabaseBinding) (map[string]*runtimeDataBaseStore, error) {
	if len(records) > 1<<16 || len(bindings) > 1<<16 {
		return nil, fmt.Errorf("KTF heap database records exceed limit")
	}
	for _, record := range records {
		if !validHeapDatabaseName(string(record.Name)) {
			return nil, fmt.Errorf("KTF heap database name is invalid")
		}
	}
	context.heldDBs = make([]bool, len(records)+1)
	for i, binding := range bindings {
		if binding.Database == 0 || uint64(binding.Database) > uint64(len(records)) || !bytes.Equal(binding.Name, records[binding.Database-1].Name) || i > 0 && bytes.Compare(bindings[i-1].Name, binding.Name) >= 0 {
			return nil, fmt.Errorf("KTF heap database catalog binding is invalid")
		}
		context.heldDBs[binding.Database] = true
	}
	named := make(map[string]*runtimeDataBaseStore, len(records))
	context.restoredDBs = make([]*runtimeDataBaseStore, len(records)+1)
	context.distinctDBs = nil
	for i, record := range records {
		store := named[string(record.Name)]
		if store == nil {
			store = &runtimeDataBaseStore{name: string(record.Name)}
			named[store.name] = store
			context.distinctDBs = append(context.distinctDBs, store)
		}
		context.restoredDBs[i+1] = store
	}
	databases := make(map[string]*runtimeDataBaseStore, len(bindings))
	for _, binding := range bindings {
		databases[string(binding.Name)] = context.restoredDBs[binding.Database]
	}
	return databases, nil
}

// unheldDatabase reports a database of a record that neither the catalog nor
// an object holds. A store is in a record because something holds it; one that
// nothing holds was put there, and every store is a read at load.
func (context *heapNativeContext) unheldDatabase() bool {
	for index := 1; index < len(context.heldDBs); index++ {
		if !context.heldDBs[index] {
			return true
		}
	}
	return false
}

// The File payload, by byte offset. It carries the object's name and cursor
// and nothing of what the file holds.
const (
	heapFileNameLength = 0
	heapFilePosition   = 4
	heapFileFlags      = 8
	heapFileLength     = 12
	heapFileHeader     = 16

	// heapFileTruncated marks an object whose open asked for an empty file,
	// and the length beside it is how many bytes it held when the checkpoint
	// was taken. See runtimeGuestFile.truncated.
	heapFileTruncated = 1
)

func validHeapFile(name string, position, length uint64, truncated bool) bool {
	return len(name) <= 1<<20 && storableName(guestFileScope, strings.TrimPrefix(name, "/")) &&
		position <= maxHeapStorageBytes && length <= maxHeapStorageBytes && (truncated || length == 0)
}

func (context *heapNativeContext) captureHeapFile(file *runtimeGuestFile) ([]byte, error) {
	if file == nil || file.position < 0 {
		return nil, fmt.Errorf("KTF heap file name or cursor is invalid")
	}
	length := uint64(0)
	if file.truncated {
		length = uint64(len(file.data))
	}
	if !validHeapFile(file.name, uint64(file.position), length, file.truncated) {
		return nil, fmt.Errorf("KTF heap file name or cursor is invalid")
	}
	if err := context.chargeStorage(uint64(len(file.name)) + uint64(len(file.data)) + 64); err != nil {
		return nil, err
	}
	data := make([]byte, heapFileHeader+len(file.name))
	binary.LittleEndian.PutUint32(data[heapFileNameLength:], uint32(len(file.name)))
	binary.LittleEndian.PutUint32(data[heapFilePosition:], uint32(file.position))
	if file.truncated {
		binary.LittleEndian.PutUint32(data[heapFileFlags:], heapFileTruncated)
	}
	binary.LittleEndian.PutUint32(data[heapFileLength:], uint32(length))
	copy(data[heapFileHeader:], file.name)
	return data, nil
}

func (context *heapNativeContext) restoreHeapFile(data []byte) (*runtimeGuestFile, error) {
	if len(data) < heapFileHeader || len(data) > (1<<20)+heapFileHeader {
		return nil, fmt.Errorf("KTF heap file payload size is invalid")
	}
	nameLength := uint64(binary.LittleEndian.Uint32(data[heapFileNameLength:]))
	position := uint64(binary.LittleEndian.Uint32(data[heapFilePosition:]))
	flags := binary.LittleEndian.Uint32(data[heapFileFlags:])
	length := uint64(binary.LittleEndian.Uint32(data[heapFileLength:]))
	if nameLength != uint64(len(data)-heapFileHeader) || flags&^heapFileTruncated != 0 {
		return nil, fmt.Errorf("KTF heap file header is invalid")
	}
	name, truncated := string(data[heapFileHeader:]), flags&heapFileTruncated != 0
	if !validHeapFile(name, position, length, truncated) {
		return nil, fmt.Errorf("KTF heap file name or cursor is invalid")
	}
	file := &runtimeGuestFile{name: name, position: int(position), truncated: truncated}
	context.restoredFiles = append(context.restoredFiles, restoredGuestFile{state: file, length: int(length)})
	return file, nil
}
