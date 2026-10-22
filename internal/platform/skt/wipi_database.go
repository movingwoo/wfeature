package skt

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/movingwoo/wfeature/internal/api/wipi"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// WIPI's record database, org.kwis.msp.db.DataBase
//
// Every answer here is the WIPI storage contract's, docs/wipi-storage.md,
// which KTF and LGT give too; this file is where SKT keeps what it asks for. A
// database is a named list of records, which is where a Jlet keeps its
// progress when it does not write a file. It lives behind the same save
// boundary as RMS and the files:
//
//	jdb/<name>   the records, in backend.EncodeSaveRecords' form: a record's
//	             id is its index, and a deleted record keeps its slot as a
//	             tombstone so the ids above it do not move
//	jdb/.index   the record size of each database, one per line: the size in
//	             decimal, a space, the name, in name order
//
// `jdb/` is the key KTF gives the same class, in the same record form, and it
// is apart from everything else this platform stores. A name is its key's path
// below the scope, so the names a key spells alike — "save", "/save",
// "./save" — are one database.
//
// **A database exists when the index names it, or when its record list holds
// something.** The index is where the record size a database was made with is
// written down, which the record form has no room for. A record list the index
// does not name is the title's database all the same, and the first open of
// it gives it that open's record size, which the index then keeps. The save
// boundary has no delete, so a deleted database leaves its name off the index
// and an empty record list under its key, and an empty list is no database.
//
// **Every change is stored when it is made, and a change the store refuses is
// not made.** A creation or a deletion writes the records and the index in one
// batch. A refusal is the DataBaseException the specification gives a record
// that cannot be stored, and the session's records stay as the store has
// them, so a title that retries is not left with a record twice; nothing of a
// database waits for a close, a quick save or the end of the session.
//
// **Every object open on one name is one database.** The records belong to the
// session and each object is a handle on them. Deleting the database empties
// them and takes the name off the index; an object still open on it reads it
// empty, and a write through it makes it again with its record size.

const (
	wipiDatabaseScope     = "jdb/"
	wipiDatabaseIndexName = ".index"
	wipiDatabaseIndexKey  = wipiDatabaseScope + wipiDatabaseIndexName
	// wipiDatabaseMaxBytes bounds one database's records together, the
	// contract's bound on untrusted input: a write past it is
	// DataBaseException, and a record list beyond it is not read.
	wipiDatabaseMaxBytes = 4 << 20
)

// wipiDatabase is one database's state in the session, shared by every
// DataBase object on the name. wipiDatabaseMu guards it.
type wipiDatabase struct {
	name string
	// recordSize is getRecordSize: the size the database was made with, or
	// the size of the first open of a record list the index did not name. A
	// deleted database keeps it, for a write that makes it again. sized says
	// it is known.
	recordSize int32
	sized      bool
	// records holds a record per id and nil for a deleted one.
	records [][]byte
	// exists says the index names the database or its record list holds
	// something.
	exists bool
	// loaded says the state above was read from the store this session.
	loaded bool
	// modified is getLastModified, in the guest clock's milliseconds: this
	// session's first open of the database, then each write. The store
	// keeps no time.
	modified int64
}

func (runtime *Runtime) wipiDatabaseRegistrations() []nativeRegistration {
	const text = "Ljava/lang/String;"
	return []nativeRegistration{
		{wipi.DataBaseClass, "openDataBase", "(" + text + "IZ)Lorg/kwis/msp/db/DataBase;", runtime.wipiOpenDataBase},
		{wipi.DataBaseClass, "openDataBase", "(" + text + "IZI)Lorg/kwis/msp/db/DataBase;", runtime.wipiOpenDataBase},
		{wipi.DataBaseClass, "closeDataBase", "()V", wipiCloseDataBase},
		{wipi.DataBaseClass, "deleteDataBase", "(" + text + ")V", runtime.wipiDeleteDataBase},
		{wipi.DataBaseClass, "deleteDataBase", "(" + text + "I)V", runtime.wipiDeleteDataBase},
		{wipi.DataBaseClass, "listDataBases", "()[" + text, runtime.wipiListDataBases},
		{wipi.DataBaseClass, "getAccessMode", "(" + text + ")I", runtime.wipiDataBaseAccessMode},
		{wipi.DataBaseClass, "getDataBaseName", "()" + text, wipiDataBaseName},
		{wipi.DataBaseClass, "getSizeAvailable", "()I", runtime.wipiDataBaseSizeAvailable},
		{wipi.DataBaseClass, "getDataBaseSize", "()I", runtime.wipiDataBaseAnswer(func(db *wipiDatabase) jvm.Value {
			return jvm.IntValue(int32(db.recordBytes()))
		})},
		{wipi.DataBaseClass, "getLastModified", "()J", runtime.wipiDataBaseAnswer(func(db *wipiDatabase) jvm.Value {
			return jvm.LongValue(db.modified)
		})},
		{wipi.DataBaseClass, "getNumberOfRecords", "()I", runtime.wipiDataBaseAnswer(func(db *wipiDatabase) jvm.Value {
			count := 0
			for _, record := range db.records {
				if record != nil {
					count++
				}
			}
			return jvm.IntValue(int32(count))
		})},
		{wipi.DataBaseClass, "getRecordSize", "()I", runtime.wipiDataBaseAnswer(func(db *wipiDatabase) jvm.Value {
			return jvm.IntValue(db.recordSize)
		})},
		{wipi.DataBaseClass, "insertRecord", "([B)I", runtime.wipiInsertRecord},
		{wipi.DataBaseClass, "insertRecord", "([BII)I", runtime.wipiInsertRecord},
		{wipi.DataBaseClass, "selectRecord", "(I)[B", runtime.wipiSelectRecord},
		{wipi.DataBaseClass, "selectRecord", "(I[BI)V", runtime.wipiSelectRecordInto},
		{wipi.DataBaseClass, "updateRecord", "(I[B)V", runtime.wipiUpdateRecord},
		{wipi.DataBaseClass, "updateRecord", "(I[BII)V", runtime.wipiUpdateRecord},
		{wipi.DataBaseClass, "deleteRecord", "(I)V", runtime.wipiDeleteRecord},
	}
}

// wipiDatabaseKeyName answers the name a database is kept under, its key's
// path below the scope, and whether the store can keep one of that name. A
// database name is read as a file's is (wipiName), so a backslash separates
// segments. A name that normalizes to nothing or climbs out with "..", one
// carrying what a key cannot (a NUL) or a line break the index could not read
// back, and the index's own name cannot be kept.
func wipiDatabaseKeyName(name string) (string, bool) {
	name = wipiName(name)
	if strings.ContainsAny(name, "\n\r") {
		return "", false
	}
	key, err := backend.NormalizeSaveKey(wipiDatabaseScope + name)
	if err != nil {
		return "", false
	}
	rest, under := strings.CutPrefix(key, wipiDatabaseScope)
	first, _, _ := strings.Cut(rest, "/")
	if !under || rest == "" || first == wipiDatabaseIndexName {
		return "", false
	}
	return rest, true
}

func wipiDatabaseException(format string, arguments ...any) error {
	return newGuestException(wipi.DataBaseExceptionClass, fmt.Sprintf(format, arguments...))
}

func wipiRecordException(format string, arguments ...any) error {
	return newGuestException(wipi.DataBaseRecordExceptionClass, fmt.Sprintf(format, arguments...))
}

// encodeWIPIDatabaseIndex writes the index; decodeWIPIDatabaseIndex reads it
// back exactly. An index this runtime did not write is refused whole rather
// than read in part: a database left out of it would lose its record size, and
// one listed twice has two.
func encodeWIPIDatabaseIndex(index map[string]int32) []byte {
	lines := make([]string, 0, len(index))
	for _, name := range slices.Sorted(maps.Keys(index)) {
		lines = append(lines, strconv.Itoa(int(index[name]))+" "+name)
	}
	return []byte(strings.Join(lines, "\n"))
}

func decodeWIPIDatabaseIndex(data []byte) (map[string]int32, error) {
	index := make(map[string]int32)
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		size, name, ok := strings.Cut(line, " ")
		value, err := strconv.ParseInt(size, 10, 32)
		if !ok || err != nil {
			return nil, fmt.Errorf("line %q has no record size", line)
		}
		if kept, valid := wipiDatabaseKeyName(name); !valid || kept != name {
			return nil, fmt.Errorf("line %q has no database name", line)
		}
		if _, twice := index[name]; twice {
			return nil, fmt.Errorf("database %q is listed twice", name)
		}
		index[name] = int32(value)
	}
	return index, nil
}

// wipiEmptiedRecords says a record list is the one a deletion leaves, or an
// empty entry: neither is a database the index does not name.
func wipiEmptiedRecords(data []byte) bool {
	return len(data) == 0 || bytes.Equal(data, backend.EncodeSaveRecords(nil))
}

// wipiRecordBytes is what records hold together, the figure the contract
// bounds and getDataBaseSize answers.
func wipiRecordBytes(records [][]byte) int {
	total := 0
	for _, record := range records {
		total += len(record)
	}
	return total
}

// recordBytes is getDataBaseSize: the sum of the records' bytes, which is
// nothing for a deleted database.
func (db *wipiDatabase) recordBytes() int {
	if !db.exists {
		return 0
	}
	return wipiRecordBytes(db.records)
}

// wipiDatabaseIndexLocked answers the index, read from the store once per
// session. charge, when there is one, is told the bytes read. The caller holds
// wipiDatabaseMu.
func (runtime *Runtime) wipiDatabaseIndexLocked(charge func(int64) error) (map[string]int32, error) {
	if runtime.wipiDatabaseIndex != nil {
		return runtime.wipiDatabaseIndex, nil
	}
	index := make(map[string]int32)
	data, found, err := backend.ReadSave(runtime.saveStoreBoundary(), wipiDatabaseIndexKey)
	if err != nil {
		return nil, wipiDatabaseException("the database index cannot be read: %v", err)
	}
	if found {
		if charge != nil {
			if err := charge(int64(len(data))); err != nil {
				return nil, err
			}
		}
		if index, err = decodeWIPIDatabaseIndex(data); err != nil {
			return nil, wipiDatabaseException("the database index is unreadable: %v", err)
		}
	}
	runtime.wipiDatabaseIndex = index
	return index, nil
}

// knownWIPIDatabaseLocked answers a database, read, for a call that names it.
// A name with no database is answered without being kept, unless the call may
// make it: a title that probes generated names does not grow the session's
// table by one for each. The caller holds wipiDatabaseMu.
func (runtime *Runtime) knownWIPIDatabaseLocked(name string, create bool) (*wipiDatabase, error) {
	if db := runtime.wipiDatabases[name]; db != nil {
		return db, runtime.loadWIPIDatabaseLocked(db, nil)
	}
	db := &wipiDatabase{name: name}
	if err := runtime.loadWIPIDatabaseLocked(db, nil); err != nil {
		return nil, err
	}
	if db.exists || create {
		if runtime.wipiDatabases == nil {
			runtime.wipiDatabases = make(map[string]*wipiDatabase)
		}
		runtime.wipiDatabases[name] = db
	}
	return db, nil
}

// loadWIPIDatabaseLocked reads a database from the store the first time the
// session uses it. A database the index names whose record list was never
// written holds no record. A record list that does not decode, or holds more
// than the bound, is refused rather than read in part: a title told its save
// is empty writes over it. charge, when there is one, is told what is read.
// The caller holds wipiDatabaseMu.
func (runtime *Runtime) loadWIPIDatabaseLocked(db *wipiDatabase, charge func(int64) error) error {
	if db.loaded {
		return nil
	}
	index, err := runtime.wipiDatabaseIndexLocked(charge)
	if err != nil {
		return err
	}
	data, found, err := backend.ReadSave(runtime.saveStoreBoundary(), wipiDatabaseScope+db.name)
	if err != nil {
		return wipiDatabaseException("database %s cannot be read: %v", db.name, err)
	}
	size, listed := index[db.name]
	db.records, db.exists = nil, false
	if listed || found && !wipiEmptiedRecords(data) {
		if found {
			if charge != nil {
				if err := charge(int64(len(data))); err != nil {
					return err
				}
			}
			records, err := backend.DecodeSaveRecords(data)
			if err != nil {
				return wipiDatabaseException("database %s is unreadable: %v", db.name, err)
			}
			if total := wipiRecordBytes(records); total > wipiDatabaseMaxBytes {
				return wipiDatabaseException("database %s holds %d bytes of records, more than %d", db.name, total, wipiDatabaseMaxBytes)
			}
			db.records = records
		}
		db.exists = true
		if listed {
			db.recordSize, db.sized = size, true
		}
	}
	db.loaded = true
	return nil
}

// commitWIPIDatabaseLocked stores a database as a change leaves it — its
// record list, and the index when the change makes or deletes it or gives it a
// record size — and makes the change in the session only once the store has
// it. Without a store the session is all there is. The caller holds
// wipiDatabaseMu.
func (runtime *Runtime) commitWIPIDatabaseLocked(db *wipiDatabase, recordSize int32, records [][]byte, exists bool) error {
	index, err := runtime.wipiDatabaseIndexLocked(nil)
	if err != nil {
		return err
	}
	entries := map[string][]byte{wipiDatabaseScope + db.name: backend.EncodeSaveRecords(records)}
	var staged map[string]int32
	if size, listed := index[db.name]; listed != exists || exists && size != recordSize {
		staged = maps.Clone(index)
		if exists {
			staged[db.name] = recordSize
		} else {
			delete(staged, db.name)
		}
		entries[wipiDatabaseIndexKey] = encodeWIPIDatabaseIndex(staged)
	}
	if store := runtime.saveStoreBoundary(); store != nil {
		// Every write through the save boundary takes this turn, as the
		// files and the record stores do.
		runtime.savePendingMu.Lock()
		err = backend.StoreSaves(store, entries)
		runtime.savePendingMu.Unlock()
		if err != nil {
			return wipiDatabaseException("database %s cannot be stored: %v", db.name, err)
		}
	}
	if staged != nil {
		runtime.wipiDatabaseIndex = staged
	}
	db.recordSize, db.records, db.exists = recordSize, records, exists
	db.sized = db.sized || exists
	db.modified = runtime.clockMillis()
	return nil
}

// adoptWIPIDatabaseSizeLocked gives a record list the index does not name the
// record size of the open that found it, and writes that size into the index:
// the contract's database "takes the size of the open that finds it and keeps
// it from then on". The open is not a write, so a store that refuses the index
// leaves the size for the session alone, and the database's next write stores
// it. The caller holds wipiDatabaseMu.
func (runtime *Runtime) adoptWIPIDatabaseSizeLocked(db *wipiDatabase, recordSize int32) {
	db.recordSize, db.sized = recordSize, true
	index, err := runtime.wipiDatabaseIndexLocked(nil)
	if err != nil {
		return
	}
	staged := maps.Clone(index)
	staged[db.name] = recordSize
	if store := runtime.saveStoreBoundary(); store != nil {
		runtime.savePendingMu.Lock()
		err = store.StoreSave(wipiDatabaseIndexKey, encodeWIPIDatabaseIndex(staged))
		runtime.savePendingMu.Unlock()
		if err != nil {
			if runtime.logger != nil {
				runtime.logger.Debug("WIPI database record size not stored", "name", db.name, "error", err)
			}
			return
		}
	}
	runtime.wipiDatabaseIndex = staged
}

// wipiDatabaseNamesLocked answers every database the title has, in name order:
// the index's, the session's, and every record list the index does not name
// that holds something. A store that cannot list leaves the first two. The
// caller holds wipiDatabaseMu.
func (runtime *Runtime) wipiDatabaseNamesLocked() ([]string, error) {
	index, err := runtime.wipiDatabaseIndexLocked(nil)
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(index))
	for name := range index {
		names[name] = true
	}
	store := runtime.saveStoreBoundary()
	keys, _, err := backend.ListSaves(store, strings.TrimSuffix(wipiDatabaseScope, "/"))
	for _, key := range keys {
		name := strings.TrimPrefix(key, wipiDatabaseScope)
		if db := runtime.wipiDatabases[name]; names[name] || db != nil && db.loaded {
			continue
		}
		if kept, valid := wipiDatabaseKeyName(name); !valid || kept != name {
			continue
		}
		if data, found, readErr := backend.ReadSave(store, key); readErr == nil && found && !wipiEmptiedRecords(data) {
			names[name] = true
		}
	}
	for name, db := range runtime.wipiDatabases {
		if db.loaded {
			if db.exists {
				names[name] = true
			} else {
				delete(names, name)
			}
		}
	}
	return slices.Sorted(maps.Keys(names)), err
}

// wipiDatabaseBytesLocked answers what the title's databases take, for the
// space figure: each one's records, less the archive's file of the same name
// as the contract counts every file and database, though this platform ships
// no database. A database that cannot be read is left out, as the figure
// declares no exception. The caller holds wipiDatabaseMu.
func (runtime *Runtime) wipiDatabaseBytesLocked() int {
	names, err := runtime.wipiDatabaseNamesLocked()
	if err != nil {
		runtime.logStorageFigure(err)
	}
	used := 0
	for _, name := range names {
		db, err := runtime.knownWIPIDatabaseLocked(name, false)
		if err != nil {
			runtime.logStorageFigure(err)
			continue
		}
		shipped := 0
		if packaged, ok := runtime.packagedFile(name); ok {
			shipped = len(packaged)
		}
		used += max(db.recordBytes()-shipped, 0)
	}
	return used
}

// recordLocked answers a record by id, or the DataBaseRecordException the
// specification gives an id with no record. The caller holds wipiDatabaseMu.
func (db *wipiDatabase) recordLocked(id int32) ([]byte, error) {
	if id < 0 || int(id) >= len(db.records) || db.records[id] == nil {
		return nil, wipiRecordException("database %s has no record %d", db.name, id)
	}
	return db.records[id], nil
}

// wipiDatabaseNameArgument reads a database name argument. A null name, and
// one the store cannot keep, are DataBaseException: the specification gives
// getAccessMode that answer for a null name, and openDataBase and
// deleteDataBase answer a name they cannot find or make the same way.
func wipiDatabaseNameArgument(arguments []jvm.Value, index int) (string, error) {
	object, err := referenceArgument(arguments, index)
	if err != nil {
		return "", err
	}
	if object == nil {
		return "", wipiDatabaseException("database name is null")
	}
	name, ok := object.Native.(string)
	if object.ClassName != jvm.StringClass || !ok {
		return "", fmt.Errorf("argument %d is not a String", index)
	}
	kept, valid := wipiDatabaseKeyName(name)
	if !valid {
		return "", wipiDatabaseException("%q cannot name a database", name)
	}
	return kept, nil
}

// wipiDatabaseArgument resolves the DataBase an instance method is called on.
func wipiDatabaseArgument(arguments []jvm.Value) (*wipiDatabase, error) {
	object, err := referenceArgument(arguments, 0)
	if err != nil {
		return nil, err
	}
	if object == nil {
		return nil, newGuestException("java/lang/NullPointerException", "DataBase is null")
	}
	db, ok := object.Native.(*wipiDatabase)
	if !ok || db == nil {
		return nil, fmt.Errorf("receiver is not a DataBase")
	}
	return db, nil
}

// wipiRecordArgument copies the record an insert or an update is handed: the
// whole array, or the window the four-argument forms name. A window outside
// the array is the specification's IllegalArgumentException.
func wipiRecordArgument(arguments []jvm.Value, arrayIndex int, windowed bool) ([]byte, error) {
	object, err := referenceArgument(arguments, arrayIndex)
	if err != nil {
		return nil, err
	}
	if object == nil {
		return nil, newGuestException("java/lang/NullPointerException", "record data is null")
	}
	data, err := jvm.ByteArraySnapshot(object)
	if err != nil {
		return nil, err
	}
	if !windowed {
		return data, nil
	}
	offset, err := intArgument(arguments, arrayIndex+1)
	if err != nil {
		return nil, err
	}
	length, err := intArgument(arguments, arrayIndex+2)
	if err != nil {
		return nil, err
	}
	if offset < 0 || length < 0 || int64(offset)+int64(length) > int64(len(data)) {
		return nil, newGuestException("java/lang/IllegalArgumentException",
			fmt.Sprintf("%d bytes from %d are not in a %d-byte array", length, offset, len(data)))
	}
	// A record of no bytes is still a record, and nil is a deleted one.
	window := make([]byte, length)
	copy(window, data[offset:])
	return window, nil
}

// wipiOpenDataBase is openDataBase, with or without the access flag, which
// picks the one directory this title has. create with a record size below one
// is the specification's IllegalArgumentException. A database that is not
// there is DataBaseException without create — the throw is how a title learns
// it is running for the first time — and is made at once, empty and listed,
// with it. One that exists keeps its record size.
func (runtime *Runtime) wipiOpenDataBase(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	name, err := wipiDatabaseNameArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	recordSize, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	create, err := booleanArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if create && recordSize <= 0 {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException", fmt.Sprintf("record size %d", recordSize))
	}
	runtime.wipiDatabaseMu.Lock()
	defer runtime.wipiDatabaseMu.Unlock()
	db, err := runtime.knownWIPIDatabaseLocked(name, create)
	if err != nil {
		return jvm.VoidValue(), err
	}
	switch {
	case !db.exists && !create:
		return jvm.VoidValue(), wipiDatabaseException("no database named %s", name)
	case !db.exists:
		if err := runtime.commitWIPIDatabaseLocked(db, recordSize, nil, true); err != nil {
			return jvm.VoidValue(), err
		}
	case !db.sized:
		runtime.adoptWIPIDatabaseSizeLocked(db, recordSize)
	}
	if db.modified == 0 {
		db.modified = runtime.clockMillis()
	}
	return jvm.ReferenceValue(&jvm.Object{ClassName: wipi.DataBaseClass, Fields: make(map[string]jvm.Value), Native: db}), nil
}

// wipiCloseDataBase is closeDataBase. Every change was stored when it was
// made, so a close has nothing to write, closing twice is ignored, and the
// object stays usable.
func wipiCloseDataBase(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	_, err := wipiDatabaseArgument(arguments)
	return jvm.VoidValue(), err
}

// wipiDeleteDataBase is deleteDataBase, with or without the access flag. A
// database that is not there is the specification's DataBaseException.
func (runtime *Runtime) wipiDeleteDataBase(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	name, err := wipiDatabaseNameArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.wipiDatabaseMu.Lock()
	defer runtime.wipiDatabaseMu.Unlock()
	db, err := runtime.knownWIPIDatabaseLocked(name, false)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if !db.exists {
		return jvm.VoidValue(), wipiDatabaseException("no database named %s", name)
	}
	return jvm.VoidValue(), runtime.commitWIPIDatabaseLocked(db, db.recordSize, nil, false)
}

// wipiListDataBases answers every database the title has, in name order, or
// null when there is none, as the specification says. The method declares no
// exception, so what cannot be read is not listed.
func (runtime *Runtime) wipiListDataBases(vm *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	runtime.wipiDatabaseMu.Lock()
	names, err := runtime.wipiDatabaseNamesLocked()
	runtime.wipiDatabaseMu.Unlock()
	if err != nil && runtime.logger != nil {
		runtime.logger.Debug("WIPI database list left out what it could not read", "error", err)
	}
	if len(names) == 0 {
		return jvm.ReferenceValue(nil), nil
	}
	return stringArray(vm, names)
}

// wipiDataBaseAccessMode is getAccessMode: PRIVATE_ACCESS for a database that
// exists, since every database here is in the title's own directory, and the
// specification's DataBaseException for one that does not or a null name.
func (runtime *Runtime) wipiDataBaseAccessMode(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	name, err := wipiDatabaseNameArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.wipiDatabaseMu.Lock()
	defer runtime.wipiDatabaseMu.Unlock()
	db, err := runtime.knownWIPIDatabaseLocked(name, false)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if !db.exists {
		return jvm.VoidValue(), wipiDatabaseException("no database named %s", name)
	}
	return jvm.IntValue(wipi.PrivateAccess), nil
}

func wipiDataBaseName(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	db, err := wipiDatabaseArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(vm.NewString(db.name)), nil
}

// wipiDataBaseSizeAvailable is getSizeAvailable: the title's figure, the one
// FileSystem.available answers, not one database's.
func (runtime *Runtime) wipiDataBaseSizeAvailable(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if _, err := wipiDatabaseArgument(arguments); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(runtime.wipiAvailable()), nil
}

// wipiDataBaseAnswer is a question about a database's records.
func (runtime *Runtime) wipiDataBaseAnswer(answer func(*wipiDatabase) jvm.Value) jvm.NativeMethod {
	return func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		db, err := wipiDatabaseArgument(arguments)
		if err != nil {
			return jvm.VoidValue(), err
		}
		runtime.wipiDatabaseMu.Lock()
		defer runtime.wipiDatabaseMu.Unlock()
		if err := runtime.loadWIPIDatabaseLocked(db, nil); err != nil {
			return jvm.VoidValue(), err
		}
		return answer(db), nil
	}
}

// wipiInsertRecord is insertRecord. The id is the lowest a deleted record left
// free, or the next past the end: ids start at zero. The record size is not
// enforced, so a longer record is stored whole. A write that would take the
// database past its bound, or that the store refuses, is DataBaseException.
func (runtime *Runtime) wipiInsertRecord(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	db, err := wipiDatabaseArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	record, err := wipiRecordArgument(arguments, 1, len(arguments) > 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.wipiDatabaseMu.Lock()
	defer runtime.wipiDatabaseMu.Unlock()
	if err := runtime.loadWIPIDatabaseLocked(db, nil); err != nil {
		return jvm.VoidValue(), err
	}
	records := slices.Clone(db.records)
	id := slices.IndexFunc(records, func(slot []byte) bool { return slot == nil })
	if id < 0 {
		// A list longer than the decoder reads back would make the save
		// unreadable, and empty records take a slot without a byte.
		if len(records) >= backend.MaxSaveRecords {
			return jvm.VoidValue(), wipiDatabaseException("database %s has no record id left", db.name)
		}
		id = len(records)
		records = append(records, record)
	} else {
		records[id] = record
	}
	if err := wipiFitDatabase(db, records); err != nil {
		return jvm.VoidValue(), err
	}
	if err := runtime.commitWIPIDatabaseLocked(db, db.recordSize, records, true); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(id)), nil
}

// wipiFitDatabase refuses records past the bound on one database.
func wipiFitDatabase(db *wipiDatabase, records [][]byte) error {
	if total := wipiRecordBytes(records); total > wipiDatabaseMaxBytes {
		return wipiDatabaseException("database %s would hold %d bytes of records, more than %d", db.name, total, wipiDatabaseMaxBytes)
	}
	return nil
}

// wipiUpdateRecord is updateRecord, whole or from a window of the array. An id
// with no record is DataBaseRecordException; a longer record is stored whole.
func (runtime *Runtime) wipiUpdateRecord(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	db, err := wipiDatabaseArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	id, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	record, err := wipiRecordArgument(arguments, 2, len(arguments) > 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.wipiDatabaseMu.Lock()
	defer runtime.wipiDatabaseMu.Unlock()
	if err := runtime.loadWIPIDatabaseLocked(db, nil); err != nil {
		return jvm.VoidValue(), err
	}
	if _, err := db.recordLocked(id); err != nil {
		return jvm.VoidValue(), err
	}
	records := slices.Clone(db.records)
	records[id] = record
	if err := wipiFitDatabase(db, records); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), runtime.commitWIPIDatabaseLocked(db, db.recordSize, records, true)
}

// wipiDeleteRecord is deleteRecord. The slot stays, as a tombstone, so the ids
// above it do not move, and the next insert reuses it.
func (runtime *Runtime) wipiDeleteRecord(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	db, err := wipiDatabaseArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	id, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.wipiDatabaseMu.Lock()
	defer runtime.wipiDatabaseMu.Unlock()
	if err := runtime.loadWIPIDatabaseLocked(db, nil); err != nil {
		return jvm.VoidValue(), err
	}
	if _, err := db.recordLocked(id); err != nil {
		return jvm.VoidValue(), err
	}
	records := slices.Clone(db.records)
	records[id] = nil
	return jvm.VoidValue(), runtime.commitWIPIDatabaseLocked(db, db.recordSize, records, true)
}

// wipiSelectRecord is selectRecord(int): a copy of the bytes stored, as long
// as they are.
func (runtime *Runtime) wipiSelectRecord(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	record, err := runtime.wipiReadRecord(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	array, err := newByteArray(vm, record)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(array), nil
}

// wipiSelectRecordInto is selectRecord(int, byte[], int): the stored bytes
// copied into the title's buffer at offset. A buffer they do not fit there is
// the specification's IllegalArgumentException.
func (runtime *Runtime) wipiSelectRecordInto(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	buffer, values, err := primitiveArrayArgument(arguments, 2, jvm.TypeByte)
	if err != nil {
		return jvm.VoidValue(), err
	}
	offset, err := intArgument(arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	record, err := runtime.wipiReadRecord(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if offset < 0 || int64(offset)+int64(len(record)) > int64(len(values)) {
		return jvm.VoidValue(), newGuestException("java/lang/IllegalArgumentException",
			fmt.Sprintf("a %d-byte record does not fit at %d in a %d-byte buffer", len(record), offset, len(values)))
	}
	copied := make([]jvm.Value, len(record))
	for index, symbol := range record {
		copied[index] = jvm.IntValue(int32(int8(symbol)))
	}
	return jvm.VoidValue(), jvm.SetArrayRange(buffer, int(offset), copied)
}

// wipiReadRecord answers a copy of the record the receiver and the id
// argument name.
func (runtime *Runtime) wipiReadRecord(arguments []jvm.Value) ([]byte, error) {
	db, err := wipiDatabaseArgument(arguments)
	if err != nil {
		return nil, err
	}
	id, err := intArgument(arguments, 1)
	if err != nil {
		return nil, err
	}
	runtime.wipiDatabaseMu.Lock()
	defer runtime.wipiDatabaseMu.Unlock()
	if err := runtime.loadWIPIDatabaseLocked(db, nil); err != nil {
		return nil, err
	}
	record, err := db.recordLocked(id)
	if err != nil {
		return nil, err
	}
	return slices.Clone(record), nil
}

// wipiDataBaseSortRecord is sortRecord: the ids of the records the title's
// filter keeps, in the order its comparator gives them, or null when no
// record is left, as the specification says. A null filter keeps every
// record and a null comparator keeps id order.
//
// The records are read under the lock and the title's code is called without
// it, because a filter that reads the database would otherwise wait on
// itself. Each record is handed over as selectRecord answers it. A
// comparator's answer counts by its sign — above zero is FOLLOWS, the first
// record after the second — so one that returns a difference sorts as one
// that returns the constants.
func (runtime *Runtime) wipiDataBaseSortRecord(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	db, err := wipiDatabaseArgument(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	filter, err := referenceArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	comparator, err := referenceArgument(arguments, 2)
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.wipiDatabaseMu.Lock()
	err = runtime.loadWIPIDatabaseLocked(db, nil)
	var ids []int32
	var reads [][]byte
	for id, record := range db.records {
		if record != nil {
			ids = append(ids, int32(id))
			reads = append(reads, slices.Clone(record))
		}
	}
	runtime.wipiDatabaseMu.Unlock()
	if err != nil {
		return jvm.VoidValue(), err
	}
	vm := call.VM()
	kept := ids[:0]
	records := make([]jvm.Value, 0, len(ids))
	for index, read := range reads {
		array, err := newByteArray(vm, read)
		if err != nil {
			return jvm.VoidValue(), err
		}
		record := jvm.ReferenceValue(array)
		if filter != nil {
			answer, err := call.InvokeVirtual(filter, "filter", "([B)Z", record)
			if err != nil {
				return jvm.VoidValue(), err
			}
			keep, err := answer.Int32()
			if err != nil {
				return jvm.VoidValue(), err
			}
			if keep == 0 {
				continue
			}
		}
		kept = append(kept, ids[index])
		records = append(records, record)
	}
	if comparator != nil {
		if err := sortWIPIRecords(call, comparator, kept, records); err != nil {
			return jvm.VoidValue(), err
		}
	}
	if len(kept) == 0 {
		return jvm.ReferenceValue(nil), nil
	}
	array, err := vm.NewArray(jvm.Type{Kind: jvm.TypeInt}, int32(len(kept)))
	if err != nil {
		return jvm.VoidValue(), err
	}
	values := make([]jvm.Value, len(kept))
	for index, id := range kept {
		values[index] = jvm.IntValue(id)
	}
	if err := jvm.SetArrayRange(array, 0, values); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(array), nil
}

// sortWIPIRecords orders ids, and the records beside them, by the title's
// comparator: a merge sort, so a database of thousands of records asks the
// title's code n log n times rather than n squared, and a stable one — a
// record moves behind another only when the comparator says it follows it, so
// records it calls equal keep their id order. A comparison that throws ends
// the sort with its exception.
func sortWIPIRecords(call *jvm.Invocation, comparator *jvm.Object, ids []int32, records []jvm.Value) error {
	follows := func(first, second jvm.Value) (bool, error) {
		answer, err := call.InvokeVirtual(comparator, "compare", "([B[B)I", first, second)
		if err != nil {
			return false, err
		}
		order, err := answer.Int32()
		return order > 0, err
	}
	count := len(ids)
	fromIDs, fromRecords := ids, records
	toIDs, toRecords := make([]int32, count), make([]jvm.Value, count)
	for width := 1; width < count; width *= 2 {
		for low := 0; low < count; low += 2 * width {
			middle, high := min(low+width, count), min(low+2*width, count)
			left, right, out := low, middle, low
			for left < middle && right < high {
				after, err := follows(fromRecords[left], fromRecords[right])
				if err != nil {
					return err
				}
				if after {
					toIDs[out], toRecords[out] = fromIDs[right], fromRecords[right]
					right++
				} else {
					toIDs[out], toRecords[out] = fromIDs[left], fromRecords[left]
					left++
				}
				out++
			}
			for ; left < middle; left, out = left+1, out+1 {
				toIDs[out], toRecords[out] = fromIDs[left], fromRecords[left]
			}
			for ; right < high; right, out = right+1, out+1 {
				toIDs[out], toRecords[out] = fromIDs[right], fromRecords[right]
			}
		}
		fromIDs, toIDs = toIDs, fromIDs
		fromRecords, toRecords = toRecords, fromRecords
	}
	copy(ids, fromIDs)
	copy(records, fromRecords)
	return nil
}
