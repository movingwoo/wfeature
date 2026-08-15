package skt

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// MIDP record ids start at 1 and are never reused, so a deleted record leaves
// a hole rather than shifting the ids after it. records is indexed by
// id - 1 and a deleted slot holds nil, which is exactly what the save
// encoding's tombstone represents.
const (
	rmsFirstRecordID = 1
	// rmsCapacity is what getSizeAvailable reports. Real handsets answered
	// with the free space of a small flash partition; games use it to decide
	// whether a save will fit, so an honest fixed budget is better than
	// claiming an unbounded store.
	rmsCapacity = 512 * 1024
	// rmsMaxRecordBytes bounds one record so a runaway guest cannot make the
	// runtime allocate without limit.
	rmsMaxRecordBytes = 1 << 20
	// rmsSaveScope is the save key prefix RMS owns. KTF uses "db/", "jdb/"
	// and "fs/" under the same owner directory; RMS uses this.
	rmsSaveScope = "rms/"
	// rmsIndexKey lists the store names that exist, because
	// listRecordStores must name stores nobody has opened this session and a
	// SaveStore only answers about keys it is asked for.
	rmsIndexKey = rmsSaveScope + ".index"
)

// recordStore is one open or previously opened RMS store. It stays in the
// runtime's map after closing so a reopen sees the same records without a
// round trip through the Host.
type recordStore struct {
	mu        sync.Mutex
	name      string
	records   [][]byte
	version   int32
	modified  int64
	open      int32
	listeners []*jvm.Object
}

// rmsState is the runtime's whole record-store world.
type rmsState struct {
	mu     sync.Mutex
	stores map[string]*recordStore
	names  []string
	loaded bool
}

// AttachSaveStore supplies the persistence boundary RMS uses. Without one,
// record stores live only as long as the session.
func (runtime *Runtime) AttachSaveStore(store backend.SaveStore) {
	if runtime == nil {
		return
	}
	runtime.saveMu.Lock()
	runtime.saveStore = store
	runtime.saveMu.Unlock()
}

func (runtime *Runtime) saveStoreBoundary() backend.SaveStore {
	runtime.saveMu.RLock()
	defer runtime.saveMu.RUnlock()
	return runtime.saveStore
}

// SaveOwner names the directory a title's saves live in. MIDlet-Name is what
// a user recognizes and what stays stable across rebuilds of the same JAR;
// the main class is the fallback for a JAR that omits it.
func jarSaveOwner(descriptor Descriptor) string {
	name := strings.TrimSpace(descriptor.Name)
	if name == "" {
		name = strings.ReplaceAll(descriptor.MainClass, "/", ".")
	}
	sanitized := make([]rune, 0, len(name))
	for _, symbol := range name {
		switch {
		case symbol == '/' || symbol == '\\' || symbol == 0:
			sanitized = append(sanitized, '_')
		case symbol < 0x20:
			sanitized = append(sanitized, '_')
		default:
			sanitized = append(sanitized, symbol)
		}
	}
	owner := strings.TrimSpace(string(sanitized))
	if owner == "" || owner == "." || owner == ".." {
		return "unnamed"
	}
	return owner
}

func (runtime *Runtime) rms() *rmsState {
	runtime.rmsOnce.Do(func() {
		runtime.rmsState = &rmsState{stores: make(map[string]*recordStore)}
	})
	return runtime.rmsState
}

// loadIndex seeds the known store names from the Host once per session.
func (state *rmsState) loadIndex(store backend.SaveStore) {
	if state.loaded {
		return
	}
	state.loaded = true
	if store == nil {
		return
	}
	data, ok := store.LoadSave(rmsIndexKey)
	if !ok {
		return
	}
	for _, name := range strings.Split(string(data), "\n") {
		name = strings.TrimSpace(name)
		if name == "" || state.contains(name) {
			continue
		}
		state.names = append(state.names, name)
	}
}

func (state *rmsState) contains(name string) bool {
	for _, existing := range state.names {
		if existing == name {
			return true
		}
	}
	return false
}

// storeIndex writes the store name list back so a later session lists stores
// it has not opened.
func (runtime *Runtime) storeIndex(state *rmsState) {
	boundary := runtime.saveStoreBoundary()
	if boundary == nil {
		return
	}
	if err := boundary.StoreSave(rmsIndexKey, []byte(strings.Join(state.names, "\n"))); err != nil && runtime.logger != nil {
		runtime.logger.Debug("RMS index store failed", "error", err)
	}
}

// recordStoreKey is the save key one store's records live under. RMS store
// names are chosen by the game, so they go through the same normalization
// every other save key does.
func recordStoreKey(name string) (string, error) {
	key, err := backend.NormalizeSaveKey(rmsSaveScope + name)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(key, rmsSaveScope) {
		return "", fmt.Errorf("record store name %q is not a save key", name)
	}
	return key, nil
}

// validRecordStoreName applies the MIDP rule: at most 32 Unicode characters,
// case sensitive, non-empty.
func validRecordStoreName(name string) bool {
	if name == "" {
		return false
	}
	count := 0
	for range name {
		count++
		if count > 32 {
			return false
		}
	}
	return !strings.ContainsAny(name, "/\\\x00")
}

// openStore finds or loads a store, creating it when asked.
func (runtime *Runtime) openStore(name string, create bool) (*recordStore, error) {
	if !validRecordStoreName(name) {
		return nil, newGuestException("java/lang/IllegalArgumentException", "invalid record store name "+name)
	}
	state := runtime.rms()
	state.mu.Lock()
	defer state.mu.Unlock()
	boundary := runtime.saveStoreBoundary()
	state.loadIndex(boundary)

	if store, ok := state.stores[name]; ok {
		store.mu.Lock()
		store.open++
		store.mu.Unlock()
		return store, nil
	}

	key, err := recordStoreKey(name)
	if err != nil {
		return nil, newGuestException("java/lang/IllegalArgumentException", err.Error())
	}
	// The index — not the presence of a data file — decides whether a store
	// exists. SaveStore has no remove, so a deleted store's file stays behind
	// holding an empty record list; without this the next openRecordStore
	// would find that file and report a store the game just deleted.
	found := state.contains(name)
	if !found && !create {
		return nil, newGuestException(midp.RecordStoreNotFoundExceptionClass, "no record store named "+name)
	}
	var records [][]byte
	if found && boundary != nil {
		if data, ok := boundary.LoadSave(key); ok {
			decoded, decodeErr := backend.DecodeSaveRecords(data)
			if decodeErr != nil {
				// A save this runtime cannot read is reported rather than
				// silently replaced: a game told the store exists would
				// otherwise overwrite a save that a later fix could read.
				return nil, newGuestException(midp.RecordStoreExceptionClass,
					fmt.Sprintf("record store %s is unreadable: %v", name, decodeErr))
			}
			records = decoded
		}
	}
	store := &recordStore{name: name, records: records, open: 1, modified: runtime.nowMillis()}
	state.stores[name] = store
	if !found {
		state.names = append(state.names, name)
		runtime.storeIndex(state)
		runtime.persist(store)
	}
	return store, nil
}

// persist writes one store's records through the Host boundary.
func (runtime *Runtime) persist(store *recordStore) {
	boundary := runtime.saveStoreBoundary()
	if boundary == nil {
		return
	}
	key, err := recordStoreKey(store.name)
	if err != nil {
		return
	}
	if err := boundary.StoreSave(key, backend.EncodeSaveRecords(store.records)); err != nil && runtime.logger != nil {
		runtime.logger.Debug("RMS store failed", "name", store.name, "error", err)
	}
}

// recordStoreArgument resolves the receiver of an instance method.
func recordStoreArgument(arguments []jvm.Value, index int) (*recordStore, error) {
	object, err := referenceArgument(arguments, index)
	if err != nil {
		return nil, err
	}
	if object == nil {
		return nil, newGuestException("java/lang/NullPointerException", "RecordStore is null")
	}
	store, ok := object.Native.(*recordStore)
	if object.ClassName != midp.RecordStoreClass || !ok || store == nil {
		return nil, fmt.Errorf("argument %d is not a RecordStore", index)
	}
	return store, nil
}

// openRecordStoreArgument is recordStoreArgument plus the open check every
// operation but closeRecordStore shares.
func openRecordStoreArgument(arguments []jvm.Value, index int) (*recordStore, error) {
	store, err := recordStoreArgument(arguments, index)
	if err != nil {
		return nil, err
	}
	store.mu.Lock()
	open := store.open > 0
	store.mu.Unlock()
	if !open {
		return nil, newGuestException(midp.RecordStoreNotOpenExceptionClass, "record store "+store.name+" is closed")
	}
	return store, nil
}

func (runtime *Runtime) nowMillis() int64 {
	return runtime.clockMillis()
}

// --- native methods ---

func (runtime *Runtime) rmsOpenRecordStore(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	name, err := stringArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	create, err := booleanArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	store, err := runtime.openStore(name, create)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(&jvm.Object{
		ClassName: midp.RecordStoreClass,
		Fields:    make(map[string]jvm.Value),
		Native:    store,
	}), nil
}

func (runtime *Runtime) rmsListRecordStores(vm *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	state := runtime.rms()
	state.mu.Lock()
	state.loadIndex(runtime.saveStoreBoundary())
	names := append([]string(nil), state.names...)
	state.mu.Unlock()
	if len(names) == 0 {
		// MIDP specifies null, not an empty array, when no store exists.
		return jvm.ReferenceValue(nil), nil
	}
	sort.Strings(names)
	array, err := vm.NewArray(jvm.Type{Kind: jvm.TypeReference, ClassName: jvm.StringClass}, int32(len(names)))
	if err != nil {
		return jvm.VoidValue(), err
	}
	values := make([]jvm.Value, len(names))
	for index, name := range names {
		values[index] = jvm.ReferenceValue(vm.NewString(name))
	}
	if err := jvm.SetArrayRange(array, 0, values); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(array), nil
}

func (runtime *Runtime) rmsDeleteRecordStore(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	name, err := stringArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	state := runtime.rms()
	state.mu.Lock()
	defer state.mu.Unlock()
	state.loadIndex(runtime.saveStoreBoundary())
	store, known := state.stores[name]
	if known {
		store.mu.Lock()
		open := store.open > 0
		store.mu.Unlock()
		if open {
			return jvm.VoidValue(), newGuestException(midp.RecordStoreExceptionClass,
				"record store "+name+" is open")
		}
	}
	if !known && !state.contains(name) {
		return jvm.VoidValue(), newGuestException(midp.RecordStoreNotFoundExceptionClass, "no record store named "+name)
	}
	delete(state.stores, name)
	remaining := state.names[:0]
	for _, existing := range state.names {
		if existing != name {
			remaining = append(remaining, existing)
		}
	}
	state.names = remaining
	runtime.storeIndex(state)
	// An emptied store is how deletion is represented at the save boundary:
	// SaveStore has no remove, and an empty record list reads back as a store
	// with no records, which the index no longer names.
	if boundary := runtime.saveStoreBoundary(); boundary != nil {
		if key, keyErr := recordStoreKey(name); keyErr == nil {
			_ = boundary.StoreSave(key, backend.EncodeSaveRecords(nil))
		}
	}
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) rmsCloseRecordStore(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := openRecordStoreArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	store.mu.Lock()
	store.open--
	closed := store.open == 0
	if closed {
		store.listeners = nil
	}
	store.mu.Unlock()
	if closed {
		runtime.persist(store)
	}
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) rmsCheckOpen(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if _, err := openRecordStoreArgument(arguments, 0); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) rmsGetName(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := openRecordStoreArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(vm.NewString(store.name)), nil
}

func (runtime *Runtime) rmsGetVersion(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := openRecordStoreArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return jvm.IntValue(store.version), nil
}

func (runtime *Runtime) rmsGetNumRecords(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := openRecordStoreArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	count := int32(0)
	for _, record := range store.records {
		if record != nil {
			count++
		}
	}
	return jvm.IntValue(count), nil
}

func (runtime *Runtime) rmsGetSize(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := openRecordStoreArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return jvm.IntValue(int32(storeBytes(store))), nil
}

func (runtime *Runtime) rmsGetSizeAvailable(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := openRecordStoreArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	available := rmsCapacity - storeBytes(store)
	if available < 0 {
		available = 0
	}
	return jvm.IntValue(int32(available)), nil
}

// storeBytes is the record payload the store holds. The caller holds the lock.
func storeBytes(store *recordStore) int {
	total := 0
	for _, record := range store.records {
		total += len(record)
	}
	return total
}

func (runtime *Runtime) rmsGetLastModified(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := openRecordStoreArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return jvm.LongValue(store.modified), nil
}

func (runtime *Runtime) rmsGetNextRecordID(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := openRecordStoreArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	return jvm.IntValue(int32(len(store.records) + rmsFirstRecordID)), nil
}

func (runtime *Runtime) rmsAddRecord(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := openRecordStoreArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data, err := recordBytesArgument(arguments, 1, 2, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	store.mu.Lock()
	if storeBytes(store)+len(data) > rmsCapacity {
		store.mu.Unlock()
		return jvm.VoidValue(), newGuestException(midp.RecordStoreFullExceptionClass,
			fmt.Sprintf("record store %s has no room for %d bytes", store.name, len(data)))
	}
	store.records = append(store.records, data)
	id := int32(len(store.records) - 1 + rmsFirstRecordID)
	store.version++
	store.modified = runtime.nowMillis()
	store.mu.Unlock()
	runtime.persist(store)
	runtime.notifyRecordListeners(store, "recordAdded", id)
	return jvm.IntValue(id), nil
}

func (runtime *Runtime) rmsSetRecord(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := openRecordStoreArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	id, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	data, err := recordBytesArgument(arguments, 2, 3, 4)
	if err != nil {
		return jvm.VoidValue(), err
	}
	store.mu.Lock()
	index, err := recordIndex(store, id)
	if err != nil {
		store.mu.Unlock()
		return jvm.VoidValue(), err
	}
	if storeBytes(store)-len(store.records[index])+len(data) > rmsCapacity {
		store.mu.Unlock()
		return jvm.VoidValue(), newGuestException(midp.RecordStoreFullExceptionClass,
			fmt.Sprintf("record store %s has no room for %d bytes", store.name, len(data)))
	}
	store.records[index] = data
	store.version++
	store.modified = runtime.nowMillis()
	store.mu.Unlock()
	runtime.persist(store)
	runtime.notifyRecordListeners(store, "recordChanged", id)
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) rmsDeleteRecord(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := openRecordStoreArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	id, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	store.mu.Lock()
	index, err := recordIndex(store, id)
	if err != nil {
		store.mu.Unlock()
		return jvm.VoidValue(), err
	}
	store.records[index] = nil
	store.version++
	store.modified = runtime.nowMillis()
	store.mu.Unlock()
	runtime.persist(store)
	runtime.notifyRecordListeners(store, "recordDeleted", id)
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) rmsGetRecordSize(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := openRecordStoreArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	id, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	index, err := recordIndex(store, id)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(len(store.records[index]))), nil
}

func (runtime *Runtime) rmsGetRecordBytes(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := openRecordStoreArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	id, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	store.mu.Lock()
	index, err := recordIndex(store, id)
	if err != nil {
		store.mu.Unlock()
		return jvm.VoidValue(), err
	}
	record := append([]byte(nil), store.records[index]...)
	store.mu.Unlock()
	if len(record) == 0 {
		// MIDP returns null for a zero-length record.
		return jvm.ReferenceValue(nil), nil
	}
	array, err := newByteArray(vm, record)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(array), nil
}

func (runtime *Runtime) rmsGetRecordInto(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := openRecordStoreArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	id, err := intArgument(arguments, 1)
	if err != nil {
		return jvm.VoidValue(), err
	}
	buffer, values, err := primitiveArrayArgument(arguments, 2, jvm.TypeByte)
	if err != nil {
		return jvm.VoidValue(), err
	}
	offset, err := intArgument(arguments, 3)
	if err != nil {
		return jvm.VoidValue(), err
	}
	store.mu.Lock()
	index, err := recordIndex(store, id)
	if err != nil {
		store.mu.Unlock()
		return jvm.VoidValue(), err
	}
	record := append([]byte(nil), store.records[index]...)
	store.mu.Unlock()
	if offset < 0 || int64(offset)+int64(len(record)) > int64(len(values)) {
		return jvm.VoidValue(), newGuestException("java/lang/ArrayIndexOutOfBoundsException",
			fmt.Sprintf("record %d does not fit at offset %d", id, offset))
	}
	copied := make([]jvm.Value, len(record))
	for index, symbol := range record {
		copied[index] = jvm.IntValue(int32(int8(symbol)))
	}
	if err := jvm.SetArrayRange(buffer, int(offset), copied); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(len(record))), nil
}

func (runtime *Runtime) rmsRecordIDs(vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := openRecordStoreArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	store.mu.Lock()
	ids := make([]jvm.Value, 0, len(store.records))
	for index, record := range store.records {
		if record != nil {
			ids = append(ids, jvm.IntValue(int32(index+rmsFirstRecordID)))
		}
	}
	store.mu.Unlock()
	array, err := vm.NewArray(jvm.Type{Kind: jvm.TypeInt}, int32(len(ids)))
	if err != nil {
		return jvm.VoidValue(), err
	}
	if err := jvm.SetArrayRange(array, 0, ids); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(array), nil
}

func (runtime *Runtime) rmsAddRecordListener(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := openRecordStoreArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	listener, err := referenceArgument(arguments, 1)
	if err != nil || listener == nil {
		return jvm.VoidValue(), err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, existing := range store.listeners {
		if existing == listener {
			return jvm.VoidValue(), nil
		}
	}
	store.listeners = append(store.listeners, listener)
	return jvm.VoidValue(), nil
}

func (runtime *Runtime) rmsRemoveRecordListener(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := recordStoreArgument(arguments, 0)
	if err != nil {
		return jvm.VoidValue(), err
	}
	listener, err := referenceArgument(arguments, 1)
	if err != nil || listener == nil {
		return jvm.VoidValue(), err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	remaining := store.listeners[:0]
	for _, existing := range store.listeners {
		if existing != listener {
			remaining = append(remaining, existing)
		}
	}
	store.listeners = remaining
	return jvm.VoidValue(), nil
}

// notifyRecordListeners calls the guest listeners after a change. A listener
// that throws does not undo the change that already happened — the store is
// authoritative and the exception is reported, matching how the MIDP event
// callbacks elsewhere in this runtime treat guest failures.
func (runtime *Runtime) notifyRecordListeners(store *recordStore, callback string, id int32) {
	store.mu.Lock()
	listeners := append([]*jvm.Object(nil), store.listeners...)
	store.mu.Unlock()
	if len(listeners) == 0 {
		return
	}
	receiver := &jvm.Object{
		ClassName: midp.RecordStoreClass,
		Fields:    make(map[string]jvm.Value),
		Native:    store,
	}
	for _, listener := range listeners {
		_, err := runtime.VM.InvokeVirtual(listener, callback,
			"(Ljavax/microedition/rms/RecordStore;I)V",
			jvm.ReferenceValue(receiver), jvm.IntValue(id))
		if err != nil && runtime.logger != nil {
			runtime.logger.Debug("RMS listener failed", "callback", callback, "error", err)
		}
	}
}

// recordIndex maps a guest record id onto a live slot. The caller holds the
// store lock.
func recordIndex(store *recordStore, id int32) (int, error) {
	index := int(id) - rmsFirstRecordID
	if index < 0 || index >= len(store.records) || store.records[index] == nil {
		return 0, newGuestException(midp.InvalidRecordIDExceptionClass,
			fmt.Sprintf("record %d is not in %s", id, store.name))
	}
	return index, nil
}

// recordBytesArgument reads a (byte[], offset, length) triple. MIDP allows a
// null array, which writes an empty record.
func recordBytesArgument(arguments []jvm.Value, arrayIndex, offsetIndex, lengthIndex int) ([]byte, error) {
	object, err := referenceArgument(arguments, arrayIndex)
	if err != nil {
		return nil, err
	}
	offset, err := intArgument(arguments, offsetIndex)
	if err != nil {
		return nil, err
	}
	length, err := intArgument(arguments, lengthIndex)
	if err != nil {
		return nil, err
	}
	if object == nil {
		if length != 0 {
			return nil, newGuestException("java/lang/NullPointerException", "record data is null")
		}
		return []byte{}, nil
	}
	component, values, err := jvm.ArraySnapshot(object)
	if err != nil {
		return nil, err
	}
	if component.Kind != jvm.TypeByte {
		return nil, fmt.Errorf("record data is not a byte array")
	}
	if offset < 0 || length < 0 || int64(offset)+int64(length) > int64(len(values)) {
		return nil, newGuestException("java/lang/ArrayIndexOutOfBoundsException",
			fmt.Sprintf("record range %d..%d of %d", offset, offset+length, len(values)))
	}
	if length > rmsMaxRecordBytes {
		return nil, newGuestException(midp.RecordStoreFullExceptionClass,
			fmt.Sprintf("record length %d exceeds %d", length, rmsMaxRecordBytes))
	}
	data := make([]byte, length)
	for index := range data {
		raw, valueErr := values[int(offset)+index].Int32()
		if valueErr != nil {
			return nil, fmt.Errorf("record byte %d: %w", index, valueErr)
		}
		data[index] = byte(raw)
	}
	return data, nil
}

// newByteArray builds a guest byte[] holding data.
func newByteArray(vm *jvm.VM, data []byte) (*jvm.Object, error) {
	array, err := vm.NewArray(jvm.Type{Kind: jvm.TypeByte}, int32(len(data)))
	if err != nil {
		return nil, err
	}
	values := make([]jvm.Value, len(data))
	for index, symbol := range data {
		values[index] = jvm.IntValue(int32(int8(symbol)))
	}
	if err := jvm.SetArrayRange(array, 0, values); err != nil {
		return nil, err
	}
	return array, nil
}

// registerRecordStoreNatives connects the RMS class surface to this runtime.
func (runtime *Runtime) registerRecordStoreNatives() error {
	registrations := []struct {
		class      string
		name       string
		descriptor string
		method     jvm.NativeMethod
	}{
		{midp.RecordStoreClass, "listRecordStores", "()[Ljava/lang/String;", runtime.rmsListRecordStores},
		{midp.RecordStoreClass, "openRecordStore", "(Ljava/lang/String;Z)Ljavax/microedition/rms/RecordStore;", runtime.rmsOpenRecordStore},
		{midp.RecordStoreClass, "deleteRecordStore", "(Ljava/lang/String;)V", runtime.rmsDeleteRecordStore},
		{midp.RecordStoreClass, "closeRecordStore", "()V", runtime.rmsCloseRecordStore},
		{midp.RecordStoreClass, "checkOpen", "()V", runtime.rmsCheckOpen},
		{midp.RecordStoreClass, "getName", "()Ljava/lang/String;", runtime.rmsGetName},
		{midp.RecordStoreClass, "getVersion", "()I", runtime.rmsGetVersion},
		{midp.RecordStoreClass, "getNumRecords", "()I", runtime.rmsGetNumRecords},
		{midp.RecordStoreClass, "getSize", "()I", runtime.rmsGetSize},
		{midp.RecordStoreClass, "getSizeAvailable", "()I", runtime.rmsGetSizeAvailable},
		{midp.RecordStoreClass, "getLastModified", "()J", runtime.rmsGetLastModified},
		{midp.RecordStoreClass, "getNextRecordID", "()I", runtime.rmsGetNextRecordID},
		{midp.RecordStoreClass, "addRecord", "([BII)I", runtime.rmsAddRecord},
		{midp.RecordStoreClass, "deleteRecord", "(I)V", runtime.rmsDeleteRecord},
		{midp.RecordStoreClass, "getRecordSize", "(I)I", runtime.rmsGetRecordSize},
		{midp.RecordStoreClass, "getRecord", "(I[BI)I", runtime.rmsGetRecordInto},
		{midp.RecordStoreClass, "getRecord", "(I)[B", runtime.rmsGetRecordBytes},
		{midp.RecordStoreClass, "setRecord", "(I[BII)V", runtime.rmsSetRecord},
		{midp.RecordStoreClass, "addRecordListener", "(Ljavax/microedition/rms/RecordListener;)V", runtime.rmsAddRecordListener},
		{midp.RecordStoreClass, "removeRecordListener", "(Ljavax/microedition/rms/RecordListener;)V", runtime.rmsRemoveRecordListener},
		{midp.RecordStoreClass, "recordIds", "()[I", runtime.rmsRecordIDs},
	}
	for _, registration := range registrations {
		if err := runtime.registerNative(registration.class, registration.name, registration.descriptor, registration.method); err != nil {
			return fmt.Errorf("register %s.%s: %w", registration.class, registration.name, err)
		}
	}
	return nil
}
