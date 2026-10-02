package ktf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
)

const maxHeapStorageBytes = 128 << 20

// Names are bytes because JSON strings replace invalid UTF-8. Deleted record
// slots are nil; an allocated empty record must remain distinct from one.
type heapDatabaseState struct {
	Name    []byte
	Records [][]byte
}

type heapDatabaseBinding struct {
	Name     []byte
	Database uint32
}

func (context *heapNativeContext) chargeStorage(size uint64) error {
	if size > maxHeapStorageBytes || context.storageBytes > maxHeapStorageBytes-size {
		return fmt.Errorf("KTF heap database data exceeds limit")
	}
	context.storageBytes += size
	return nil
}

func (context *heapNativeContext) validateDatabase(name string, records [][]byte) error {
	if len(name) > 1<<20 || !storableName(javaDatabaseScope, name) || len(records) > maxDataBaseRecords {
		return fmt.Errorf("KTF heap database name or record count is invalid")
	}
	if err := context.chargeStorage(uint64(len(name)) + uint64(len(records))*24 + 128); err != nil {
		return err
	}
	for _, record := range records {
		if err := context.chargeStorage(uint64(len(record))); err != nil {
			return err
		}
	}
	return nil
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
	if err := context.validateDatabase(store.name, store.records); err != nil {
		return 0, err
	}
	if context.databaseIDs == nil {
		context.databaseIDs = make(map[*runtimeDataBaseStore]uint32)
	}
	id := uint32(len(context.databases) + 1)
	context.databaseIDs[store] = id
	context.databases = append(context.databases, heapDatabaseState{Name: []byte(store.name), Records: cloneHeapRecords(store.records)})
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
		if err := context.chargeStorage(uint64(len(name)) + 32); err != nil {
			return nil, err
		}
		bindings = append(bindings, heapDatabaseBinding{Name: []byte(name), Database: id})
	}
	return bindings, nil
}

func (context *heapNativeContext) restoreDatabases(records []heapDatabaseState, bindings []heapDatabaseBinding) (map[string]*runtimeDataBaseStore, error) {
	if len(records) > 1<<16 || len(bindings) > 1<<16 {
		return nil, fmt.Errorf("KTF heap database records exceed limit")
	}
	for _, record := range records {
		if err := context.validateDatabase(string(record.Name), record.Records); err != nil {
			return nil, err
		}
	}
	for i, binding := range bindings {
		if binding.Database == 0 || uint64(binding.Database) > uint64(len(records)) || !bytes.Equal(binding.Name, records[binding.Database-1].Name) || i > 0 && bytes.Compare(bindings[i-1].Name, binding.Name) >= 0 {
			return nil, fmt.Errorf("KTF heap database catalog binding is invalid")
		}
		if err := context.chargeStorage(uint64(len(binding.Name)) + 32); err != nil {
			return nil, err
		}
	}
	context.restoredDBs = make([]*runtimeDataBaseStore, len(records)+1)
	for i, record := range records {
		context.restoredDBs[i+1] = &runtimeDataBaseStore{name: string(record.Name), records: cloneHeapRecords(record.Records)}
	}
	databases := make(map[string]*runtimeDataBaseStore, len(bindings))
	for _, binding := range bindings {
		databases[string(binding.Name)] = context.restoredDBs[binding.Database]
	}
	return databases, nil
}

func validHeapFile(name string, size, position int) bool {
	return len(name) <= 1<<20 && storableName(guestFileScope, strings.TrimPrefix(name, "/")) && size <= maxHeapStorageBytes && position >= 0 && position <= size
}

func captureHeapFile(file *runtimeGuestFile) ([]byte, error) {
	if file == nil || !validHeapFile(file.name, len(file.data), file.position) {
		return nil, fmt.Errorf("KTF heap file name, size, or cursor is invalid")
	}
	data := make([]byte, 12+len(file.name)+len(file.data))
	binary.LittleEndian.PutUint32(data, uint32(len(file.name)))
	binary.LittleEndian.PutUint32(data[4:], uint32(file.position))
	if file.data != nil {
		binary.LittleEndian.PutUint32(data[8:], 1)
	}
	copy(data[12:], file.name)
	copy(data[12+len(file.name):], file.data)
	return data, nil
}

func restoreHeapFile(data []byte) (*runtimeGuestFile, error) {
	if len(data) < 12 || len(data) > maxHeapStorageBytes+(1<<20)+12 {
		return nil, fmt.Errorf("KTF heap file payload size is invalid")
	}
	nameLength := uint64(binary.LittleEndian.Uint32(data))
	position := uint64(binary.LittleEndian.Uint32(data[4:]))
	present := binary.LittleEndian.Uint32(data[8:])
	if nameLength > uint64(len(data)-12) || present > 1 {
		return nil, fmt.Errorf("KTF heap file header is invalid")
	}
	name, content := string(data[12:12+nameLength]), data[12+nameLength:]
	if position > uint64(len(content)) || !validHeapFile(name, len(content), int(position)) || present == 0 && len(content) != 0 {
		return nil, fmt.Errorf("KTF heap file name, size, or cursor is invalid")
	}
	file := &runtimeGuestFile{name: name, position: int(position)}
	if present != 0 {
		file.data = make([]byte, len(content))
		copy(file.data, content)
	}
	return file, nil
}
