package ktf

import (
	"fmt"
	"sort"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// The remaining WIPI Java classes: database exceptions and the sort/filter
// callbacks, the media listener and vibrator, and the network entry point.
// A game reaches these through save handling, sound, and online features, so
// they exist as real classes even where the Host has nothing behind them yet.

const (
	runtimeDataBaseClass          = "org/kwis/msp/db/DataBase"
	runtimeDataBaseExceptionClass = "org/kwis/msp/db/DataBaseException"
	runtimeDataBaseRecordClass    = "org/kwis/msp/db/DataBaseRecordException"
	runtimeDataComparatorClass    = "org/kwis/msp/db/DataComparator"
	runtimeDataFilterClass        = "org/kwis/msp/db/DataFilter"
	runtimePlayListenerClass      = "org/kwis/msp/media/PlayListener"
	runtimeVibratorClass          = "org/kwis/msp/media/Vibrator"
	runtimeNetworkClass           = "org/kwis/msf/io/Network"
	runtimeSchemeExceptionClass   = "org/kwis/msf/io/SchemeNotFoundException"
)

// runtimeExceptionClass declares a runtime-owned exception with the two CLDC
// constructor forms. The message is kept on the object so a guest catch that
// reads it observes what failed.
func runtimeExceptionClass(name, superName string) runtimeJavaClass {
	return runtimeJavaClass{
		name:        name,
		superName:   superName,
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: name, name: "<init>", descriptor: "()V", accessFlags: 0x0001, implementation: runtimeThrowableConstructor},
			{class: name, name: "<init>", descriptor: "(Ljava/lang/String;)V", accessFlags: 0x0001, implementation: runtimeThrowableConstructor},
		},
	}
}

func runtimeThrowableConstructor(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) < 1 || len(arguments) > 2 {
		return jvm.VoidValue(), fmt.Errorf("exception constructor expected a receiver and optional message, got %d arguments", len(arguments))
	}
	receiver, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if receiver == nil {
		return jvm.VoidValue(), fmt.Errorf("exception constructor receiver is null")
	}
	if len(arguments) == 2 {
		message, err := arguments[1].Reference()
		if err != nil {
			return jvm.VoidValue(), err
		}
		if text, ok := jvm.StringText(message); ok {
			receiver.Native = text
		}
	}
	return jvm.VoidValue(), nil
}

func runtimeVibratorClassDefinition() runtimeJavaClass {
	const class = runtimeVibratorClass
	return runtimeJavaClass{
		name:        class,
		superName:   "java/lang/Object",
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: class, name: "on", descriptor: "(II)V", accessFlags: 0x0009, implementation: runtimeVibratorOn},
			{class: class, name: "off", descriptor: "()V", accessFlags: 0x0009, implementation: runtimeVibratorOff},
		},
	}
}

// runtimeVibratorOn records the requested vibration. Browsers and the CLI have
// no shared vibration boundary yet, so the request is counted as a diagnostic
// rather than silently discarded.
func runtimeVibratorOn(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) != 2 {
		return jvm.VoidValue(), fmt.Errorf("Vibrator.on expected level and duration, got %d arguments", len(arguments))
	}
	level, err := arguments[0].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	duration, err := arguments[1].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	runtime.countDiagnostic(fmt.Sprintf("vibrate level=%d ms=%d", level, duration))
	return jvm.VoidValue(), nil
}

func runtimeVibratorOff(runtime *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	runtime.countDiagnostic("vibrate off")
	return jvm.VoidValue(), nil
}

func runtimeNetworkClassDefinition() runtimeJavaClass {
	const class = runtimeNetworkClass
	return runtimeJavaClass{
		name:        class,
		superName:   "java/lang/Object",
		accessFlags: 0x0021,
		methods: []runtimeJavaMethod{
			{class: class, name: "connect", descriptor: "()I", accessFlags: 0x0009, implementation: runtimeNetworkConnect},
			{class: class, name: "disconnect", descriptor: "()V", accessFlags: 0x0009, implementation: runtimeNetworkDisconnect},
		},
	}
}

// runtimeNetworkConnect reports a failed connection. The emulator has no
// network boundary, and a game that is told the network is unavailable takes
// its documented offline path; a fake success would strand it waiting for data
// that never arrives.
func runtimeNetworkConnect(runtime *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	runtime.countDiagnostic("network connect refused")
	return jvm.IntValue(-1), nil
}

func runtimeNetworkDisconnect(_ *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.VoidValue(), nil
}

// runtimeDataBaseName answers the name the database was opened with.
func runtimeDataBaseName(_ *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := runtimeDataBaseState(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(vm.NewString(store.name)), nil
}

// runtimeDataBaseSize reports the bytes the stored records occupy.
func runtimeDataBaseSize(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := runtimeDataBaseState(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	total := 0
	for _, record := range store.records {
		total += len(record)
	}
	return jvm.IntValue(int32(total)), nil
}

// runtimeDataBaseRecordSize reports the fixed record size the database was
// opened with, which the original runtime keeps per store.
func runtimeDataBaseRecordSize(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) < 1 {
		return jvm.VoidValue(), fmt.Errorf("DataBase.getRecordSize expected receiver")
	}
	receiver, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if receiver == nil {
		return jvm.VoidValue(), fmt.Errorf("DataBase.getRecordSize receiver is null")
	}
	if value, ok := receiver.Fields["recordSize:I"]; ok {
		return value, nil
	}
	return jvm.IntValue(0), nil
}

// runtimeDataBaseSizeAvailable reports the room left for new records. Saves
// live in Host storage without a device quota, so the answer is the same
// generous constant the filesystem reports.
func runtimeDataBaseSizeAvailable(_ *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(maxGuestFileBytes), nil
}

// runtimeDataBaseListNames answers every database this session has opened.
func runtimeDataBaseListNames(runtime *initializationRuntime, vm *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	names := make([]string, 0, len(runtime.databases))
	for name := range runtime.databases {
		names = append(names, name)
	}
	sort.Strings(names)
	array, err := vm.NewArray(jvm.Type{Kind: jvm.TypeReference, ClassName: "java/lang/String"}, int32(len(names)))
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

// runtimeDataBaseDelete drops a database and its persisted save content.
func runtimeDataBaseDeleteStore(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if len(arguments) < 1 {
		return jvm.VoidValue(), fmt.Errorf("DataBase.deleteDataBase expected a name")
	}
	nameObject, err := arguments[0].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	name, ok := jvm.StringText(nameObject)
	if !ok {
		return jvm.VoidValue(), fmt.Errorf("DataBase.deleteDataBase name is not a string")
	}
	store := runtime.databases[name]
	if store == nil {
		message := "database not found: " + name
		return jvm.VoidValue(), &jvm.GuestException{
			Object:  &jvm.Object{ClassName: runtimeDataBaseExceptionClass, Native: message},
			Message: message,
		}
	}
	store.records = nil
	store.persist(runtime)
	delete(runtime.databases, name)
	return jvm.VoidValue(), nil
}

// runtimeDataBaseAccessMode reports read/write access. Every database the
// emulator opens is writable, which is mode 1 in the WIPI access model.
func runtimeDataBaseAccessMode(_ *initializationRuntime, _ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
	return jvm.IntValue(1), nil
}

// runtimeDataBaseLastModified reports the guest-visible modification time of
// the store, which advances with the same virtual clock the rest of the
// runtime uses.
func runtimeDataBaseLastModified(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	if _, err := runtimeDataBaseState(arguments); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.LongValue(runtime.guestMillis()), nil
}

// runtimeDataBaseSelectInto copies one record into a caller-owned buffer and
// reports nothing; the original runtime throws when the record is missing.
func runtimeDataBaseSelectInto(_ *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := runtimeDataBaseState(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) != 4 {
		return jvm.VoidValue(), fmt.Errorf("DataBase.selectRecord expected identifier, buffer, and offset, got %d arguments", len(arguments))
	}
	index, err := runtimeDataBaseRecordIndex(store, arguments[1:])
	if err != nil {
		return jvm.VoidValue(), runtimeDataBaseRecordException(err)
	}
	buffer, err := arguments[2].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if buffer == nil {
		return jvm.VoidValue(), fmt.Errorf("DataBase.selectRecord buffer is null")
	}
	offset, err := arguments[3].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	record := store.records[index]
	_, length, ok := jvm.ArrayComponent(buffer)
	if !ok {
		return jvm.VoidValue(), fmt.Errorf("DataBase.selectRecord buffer is not an array")
	}
	if offset < 0 || int64(offset)+int64(len(record)) > int64(length) {
		message := fmt.Sprintf("record %d does not fit at offset %d", index, offset)
		return jvm.VoidValue(), &jvm.GuestException{
			Object:  &jvm.Object{ClassName: "java/lang/ArrayIndexOutOfBoundsException", Native: message},
			Message: message,
		}
	}
	values := make([]jvm.Value, len(record))
	for position, value := range record {
		values[position] = jvm.IntValue(int32(int8(value)))
	}
	return jvm.VoidValue(), jvm.SetArrayRange(buffer, int(offset), values)
}

// runtimeDataBaseUpdateRange replaces a record with part of a caller buffer.
func runtimeDataBaseUpdateRange(runtime *initializationRuntime, _ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := runtimeDataBaseState(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) != 5 {
		return jvm.VoidValue(), fmt.Errorf("DataBase.updateRecord expected identifier, data, offset, and length, got %d arguments", len(arguments))
	}
	index, err := runtimeDataBaseRecordIndex(store, arguments[1:])
	if err != nil {
		return jvm.VoidValue(), runtimeDataBaseRecordException(err)
	}
	data, err := runtimeDataBaseBytes(arguments[2:])
	if err != nil {
		return jvm.VoidValue(), err
	}
	store.records[index] = data
	store.persist(runtime)
	return jvm.VoidValue(), nil
}

func runtimeDataBaseRecordException(err error) error {
	return &jvm.GuestException{
		Object:  &jvm.Object{ClassName: runtimeDataBaseRecordClass, Native: err.Error()},
		Message: err.Error(),
	}
}

// runtimeDataBaseSortRecord answers the record identifiers that pass the
// guest's filter, ordered by the guest's comparator. Both callbacks are guest
// objects, so this walks back into guest code for every comparison; a nil
// callback means "keep everything" and "keep the stored order" respectively.
func runtimeDataBaseSortRecord(_ *initializationRuntime, vm *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
	store, err := runtimeDataBaseState(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if len(arguments) != 3 {
		return jvm.VoidValue(), fmt.Errorf("DataBase.sortRecord expected a filter and comparator, got %d arguments", len(arguments))
	}
	filter, err := arguments[1].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	comparator, err := arguments[2].Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	var identifiers []int32
	for index, record := range store.records {
		if record == nil {
			continue
		}
		if filter != nil {
			keep, err := vm.InvokeVirtual(filter, "filter", "([B)Z", jvm.ReferenceValue(jvm.NewByteArray(record)))
			if err != nil {
				return jvm.VoidValue(), fmt.Errorf("apply KTF data filter %s: %w", filter.ClassName, err)
			}
			accepted, err := keep.Int32()
			if err != nil {
				return jvm.VoidValue(), err
			}
			if accepted == 0 {
				continue
			}
		}
		identifiers = append(identifiers, int32(index))
	}
	if comparator != nil {
		if err := sortRecordIdentifiers(vm, comparator, store, identifiers); err != nil {
			return jvm.VoidValue(), err
		}
	}
	array, err := vm.NewArray(jvm.Type{Kind: jvm.TypeInt}, int32(len(identifiers)))
	if err != nil {
		return jvm.VoidValue(), err
	}
	values := make([]jvm.Value, len(identifiers))
	for index, identifier := range identifiers {
		values[index] = jvm.IntValue(identifier)
	}
	if err := jvm.SetArrayRange(array, 0, values); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(array), nil
}

// sortRecordIdentifiers is an insertion sort rather than sort.Slice because
// every comparison can fail inside guest code, and a failed comparison has to
// surface instead of being swallowed by a panic-free comparator contract.
func sortRecordIdentifiers(vm *jvm.VM, comparator *jvm.Object, store *runtimeDataBaseStore, identifiers []int32) error {
	for index := 1; index < len(identifiers); index++ {
		current := identifiers[index]
		position := index - 1
		for position >= 0 {
			order, err := compareRecords(vm, comparator, store.records[identifiers[position]], store.records[current])
			if err != nil {
				return err
			}
			if order <= 0 {
				break
			}
			identifiers[position+1] = identifiers[position]
			position--
		}
		identifiers[position+1] = current
	}
	return nil
}

func compareRecords(vm *jvm.VM, comparator *jvm.Object, left, right []byte) (int32, error) {
	result, err := vm.InvokeVirtual(comparator, "compare", "([B[B)I",
		jvm.ReferenceValue(jvm.NewByteArray(left)), jvm.ReferenceValue(jvm.NewByteArray(right)))
	if err != nil {
		return 0, fmt.Errorf("apply KTF data comparator %s: %w", comparator.ClassName, err)
	}
	return result.Int32()
}
