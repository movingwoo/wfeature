package midp

import "github.com/movingwoo/wfeature/internal/jvm"

// The record store methods MIDP defines in terms of other record store calls,
// and the enumeration itself. Record data lives on the platform side, which
// persists it through the Host save boundary; everything here is the guest's
// view of it.

const (
	recordStoreDescriptor = "Ljavax/microedition/rms/RecordStore;"
	recordFilterName      = "javax/microedition/rms/RecordFilter"
	recordComparatorName  = "javax/microedition/rms/RecordComparator"
	recordListenerName    = "javax/microedition/rms/RecordListener"
)

// openRecordStoreAuthorized is the four-argument open. Only one MIDlet suite
// runs at a time, so every store is already private to it and the sharing mode
// changes nothing observable.
func openRecordStoreAuthorized(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	return call.InvokeStatic(RecordStoreClass, "openRecordStore", "("+stringDescriptor+"Z)"+recordStoreDescriptor,
		arguments[0], arguments[1])
}

// openRecordStoreFromSuite is the cross-suite open. There is no other suite to
// read from, so it opens this suite's store without creating one, and a name
// that does not exist fails the way the single-suite open fails.
func openRecordStoreFromSuite(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	return call.InvokeStatic(RecordStoreClass, "openRecordStore", "("+stringDescriptor+"Z)"+recordStoreDescriptor,
		arguments[0], jvm.IntValue(0))
}

// recordStoreSetMode accepts the authorization mode a game sets and only
// checks that the store is open, for the reason openRecordStoreAuthorized
// ignores it.
func recordStoreSetMode(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	store, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	_, err = call.InvokeSpecial(store, RecordStoreClass, "checkOpen", "()V")
	return jvm.VoidValue(), err
}

// recordStoreEnumerate hands back the runtime's enumeration over this store.
func recordStoreEnumerate(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	store, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if _, err := call.InvokeSpecial(store, RecordStoreClass, "checkOpen", "()V"); err != nil {
		return jvm.VoidValue(), err
	}
	set, err := call.NewObject(RecordSetClass,
		"("+recordStoreDescriptor+"L"+recordFilterName+";L"+recordComparatorName+";Z)V",
		jvm.ReferenceValue(store), arguments[1], arguments[2], arguments[3])
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.ReferenceValue(set), nil
}

// recordSetInit builds the enumeration and takes its first selection. The
// filter and the comparator are application objects, and calling them from
// here keeps them ordinary guest calls on the caller's own execution.
func recordSetInit(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	set, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	machine := call.VM()
	if err := machine.SetField(set, RecordSetClass, "store", recordStoreDescriptor, arguments[1]); err != nil {
		return jvm.VoidValue(), err
	}
	if err := machine.SetField(set, RecordSetClass, "filter", "L"+recordFilterName+";", arguments[2]); err != nil {
		return jvm.VoidValue(), err
	}
	if err := machine.SetField(set, RecordSetClass, "comparator", "L"+recordComparatorName+";", arguments[3]); err != nil {
		return jvm.VoidValue(), err
	}
	if err := setRecordIDs(call, set, nil); err != nil {
		return jvm.VoidValue(), err
	}
	if _, err := call.InvokeVirtual(set, "rebuild", "()V"); err != nil {
		return jvm.VoidValue(), err
	}
	keepUpdated, err := arguments[4].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if keepUpdated != 0 {
		if _, err := call.InvokeVirtual(set, "keepUpdated", "(Z)V", jvm.IntValue(1)); err != nil {
			return jvm.VoidValue(), err
		}
	}
	return jvm.VoidValue(), nil
}

// recordSetState reads the cursor: the selected ids and how many of them are
// behind it. The cursor sits between records, so position is the number of
// records before it.
func recordSetState(machine *jvm.VM, set *jvm.Object) (ids []int32, position int32, err error) {
	value, err := machine.Field(set, RecordSetClass, "ids", "[I")
	if err != nil {
		return nil, 0, err
	}
	array, err := value.Reference()
	if err != nil {
		return nil, 0, err
	}
	if array != nil {
		_, values, err := jvm.ArraySnapshot(array)
		if err != nil {
			return nil, 0, err
		}
		ids = make([]int32, 0, len(values))
		for _, element := range values {
			id, err := element.Int32()
			if err != nil {
				return nil, 0, err
			}
			ids = append(ids, id)
		}
	}
	positionValue, err := machine.Field(set, RecordSetClass, "position", "I")
	if err != nil {
		return nil, 0, err
	}
	position, err = positionValue.Int32()
	if err != nil {
		return nil, 0, err
	}
	return ids, position, nil
}

// setRecordIDs replaces the selection and puts the cursor back at the start,
// which is what every rebuild does.
func setRecordIDs(call *jvm.Invocation, set *jvm.Object, ids []int32) error {
	machine := call.VM()
	array, err := machine.NewArray(jvm.Type{Kind: jvm.TypeInt}, int32(len(ids)))
	if err != nil {
		return err
	}
	values := make([]jvm.Value, 0, len(ids))
	for _, id := range ids {
		values = append(values, jvm.IntValue(id))
	}
	if err := jvm.SetArrayRange(array, 0, values); err != nil {
		return err
	}
	if err := machine.SetField(set, RecordSetClass, "ids", "[I", jvm.ReferenceValue(array)); err != nil {
		return err
	}
	return machine.SetField(set, RecordSetClass, "position", "I", jvm.IntValue(0))
}

func recordSetFlag(machine *jvm.VM, set *jvm.Object, name string) (bool, error) {
	value, err := machine.Field(set, RecordSetClass, name, "Z")
	if err != nil {
		return false, err
	}
	flag, err := value.Int32()
	if err != nil {
		return false, err
	}
	return flag != 0, nil
}

func setRecordSetFlag(machine *jvm.VM, set *jvm.Object, name string, flag bool) error {
	value := int32(0)
	if flag {
		value = 1
	}
	return machine.SetField(set, RecordSetClass, name, "Z", jvm.IntValue(value))
}

func recordSetNumRecords(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	set, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	ids, _, err := recordSetState(call.VM(), set)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(int32(len(ids))), nil
}

// recordSetRecord answers with the record the cursor step selects. The id and
// the data are read in two calls so a game that overrides neither still sees
// the store's own exceptions.
func recordSetRecord(step string) jvm.ContextMethod {
	return func(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
		set, err := receiver(arguments)
		if err != nil {
			return jvm.VoidValue(), err
		}
		id, err := call.InvokeVirtual(set, step, "()I")
		if err != nil {
			return jvm.VoidValue(), err
		}
		value, err := call.VM().Field(set, RecordSetClass, "store", recordStoreDescriptor)
		if err != nil {
			return jvm.VoidValue(), err
		}
		store, err := value.Reference()
		if err != nil {
			return jvm.VoidValue(), err
		}
		if store == nil {
			return jvm.VoidValue(), jvm.Throw("java/lang/NullPointerException", "enumeration has no store")
		}
		return call.InvokeVirtual(store, "getRecord", "(I)[B", id)
	}
}

func recordSetNextRecordID(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	set, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if err := checkRecordSetAlive(call.VM(), set); err != nil {
		return jvm.VoidValue(), err
	}
	ids, position, err := recordSetState(call.VM(), set)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if int(position) >= len(ids) {
		return jvm.VoidValue(), jvm.Throw(InvalidRecordIDExceptionClass, "enumeration is past the last record")
	}
	if err := call.VM().SetField(set, RecordSetClass, "position", "I", jvm.IntValue(position+1)); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(ids[position]), nil
}

func recordSetPreviousRecordID(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	set, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if err := checkRecordSetAlive(call.VM(), set); err != nil {
		return jvm.VoidValue(), err
	}
	ids, position, err := recordSetState(call.VM(), set)
	if err != nil {
		return jvm.VoidValue(), err
	}
	if position <= 0 {
		return jvm.VoidValue(), jvm.Throw(InvalidRecordIDExceptionClass, "enumeration is before the first record")
	}
	if err := call.VM().SetField(set, RecordSetClass, "position", "I", jvm.IntValue(position-1)); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.IntValue(ids[position-1]), nil
}

func recordSetHasElement(forward bool) jvm.ContextMethod {
	return func(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
		set, err := receiver(arguments)
		if err != nil {
			return jvm.VoidValue(), err
		}
		destroyed, err := recordSetFlag(call.VM(), set, "destroyed")
		if err != nil {
			return jvm.VoidValue(), err
		}
		ids, position, err := recordSetState(call.VM(), set)
		if err != nil {
			return jvm.VoidValue(), err
		}
		has := !destroyed && position > 0
		if forward {
			has = !destroyed && int(position) < len(ids)
		}
		if has {
			return jvm.IntValue(1), nil
		}
		return jvm.IntValue(0), nil
	}
}

func recordSetReset(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	set, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), call.VM().SetField(set, RecordSetClass, "position", "I", jvm.IntValue(0))
}

// recordSetRebuild reselects from the store. A record that disappears between
// the listing and the read simply is not part of this enumeration, and a store
// that was closed underneath leaves an empty one rather than an error, because
// a game holding an enumeration over a store it closed is not asking anything.
func recordSetRebuild(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	set, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	machine := call.VM()
	destroyed, err := recordSetFlag(machine, set, "destroyed")
	if err != nil || destroyed {
		return jvm.VoidValue(), err
	}
	storeValue, err := machine.Field(set, RecordSetClass, "store", recordStoreDescriptor)
	if err != nil {
		return jvm.VoidValue(), err
	}
	store, err := storeValue.Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if store == nil {
		return jvm.VoidValue(), setRecordIDs(call, set, nil)
	}
	listed, err := call.InvokeVirtual(store, "recordIds", "()[I")
	if err != nil {
		if machine.IsGuestException(err, RecordStoreNotOpenExceptionClass) {
			return jvm.VoidValue(), setRecordIDs(call, set, nil)
		}
		return jvm.VoidValue(), err
	}
	all, err := listed.Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	filterValue, err := machine.Field(set, RecordSetClass, "filter", "L"+recordFilterName+";")
	if err != nil {
		return jvm.VoidValue(), err
	}
	filter, err := filterValue.Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}

	var selected []int32
	var records []jvm.Value
	if all != nil {
		_, values, err := jvm.ArraySnapshot(all)
		if err != nil {
			return jvm.VoidValue(), err
		}
		for _, element := range values {
			id, err := element.Int32()
			if err != nil {
				return jvm.VoidValue(), err
			}
			record, err := call.InvokeVirtual(store, "getRecord", "(I)[B", jvm.IntValue(id))
			if err != nil {
				if machine.IsGuestException(err, RecordStoreExceptionClass) {
					continue
				}
				return jvm.VoidValue(), err
			}
			if filter != nil {
				matched, err := call.InvokeVirtual(filter, "matches", "([B)Z", record)
				if err != nil {
					return jvm.VoidValue(), err
				}
				keep, err := matched.Int32()
				if err != nil {
					return jvm.VoidValue(), err
				}
				if keep == 0 {
					continue
				}
			}
			selected = append(selected, id)
			records = append(records, record)
		}
	}

	comparatorValue, err := machine.Field(set, RecordSetClass, "comparator", "L"+recordComparatorName+";")
	if err != nil {
		return jvm.VoidValue(), err
	}
	comparator, err := comparatorValue.Reference()
	if err != nil {
		return jvm.VoidValue(), err
	}
	if comparator != nil {
		if err := sortRecords(call, comparator, selected, records); err != nil {
			return jvm.VoidValue(), err
		}
	}
	return jvm.VoidValue(), setRecordIDs(call, set, selected)
}

// recordComparatorFollows is RecordComparator.FOLLOWS: the first record sorts
// after the second.
const recordComparatorFollows int32 = 1

// sortRecords orders the selection with the application's comparator.
// Insertion sort keeps equal records in the order the store listed them, which
// is the order a game that sorts by one field expects for records sharing it.
func sortRecords(call *jvm.Invocation, comparator *jvm.Object, ids []int32, records []jvm.Value) error {
	for index := 1; index < len(ids); index++ {
		id := ids[index]
		record := records[index]
		scan := index - 1
		for scan >= 0 {
			result, err := call.InvokeVirtual(comparator, "compare", "([B[B)I", records[scan], record)
			if err != nil {
				return err
			}
			order, err := result.Int32()
			if err != nil {
				return err
			}
			if order != recordComparatorFollows {
				break
			}
			ids[scan+1] = ids[scan]
			records[scan+1] = records[scan]
			scan--
		}
		ids[scan+1] = id
		records[scan+1] = record
	}
	return nil
}

// recordSetKeepUpdated subscribes the enumeration to the store, or drops the
// subscription. A game that asks for the state it already has gets nothing
// done, which is what keeps a repeated call from registering twice.
func recordSetKeepUpdated(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	set, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	machine := call.VM()
	wanted, err := arguments[1].Int32()
	if err != nil {
		return jvm.VoidValue(), err
	}
	destroyed, err := recordSetFlag(machine, set, "destroyed")
	if err != nil {
		return jvm.VoidValue(), err
	}
	updated, err := recordSetFlag(machine, set, "updated")
	if err != nil {
		return jvm.VoidValue(), err
	}
	if destroyed || updated == (wanted != 0) {
		return jvm.VoidValue(), nil
	}
	if err := setRecordSetFlag(machine, set, "updated", wanted != 0); err != nil {
		return jvm.VoidValue(), err
	}
	store, err := recordSetStore(machine, set)
	if err != nil || store == nil {
		return jvm.VoidValue(), err
	}
	if wanted != 0 {
		if _, err := call.InvokeVirtual(store, "addRecordListener", "(L"+recordListenerName+";)V", jvm.ReferenceValue(set)); err != nil {
			return jvm.VoidValue(), err
		}
		_, err = call.InvokeVirtual(set, "rebuild", "()V")
		return jvm.VoidValue(), err
	}
	_, err = call.InvokeVirtual(store, "removeRecordListener", "(L"+recordListenerName+";)V", jvm.ReferenceValue(set))
	return jvm.VoidValue(), err
}

func recordSetStore(machine *jvm.VM, set *jvm.Object) (*jvm.Object, error) {
	value, err := machine.Field(set, RecordSetClass, "store", recordStoreDescriptor)
	if err != nil {
		return nil, err
	}
	return value.Reference()
}

func recordSetIsKeptUpdated(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	set, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	updated, err := recordSetFlag(call.VM(), set, "updated")
	if err != nil {
		return jvm.VoidValue(), err
	}
	if updated {
		return jvm.IntValue(1), nil
	}
	return jvm.IntValue(0), nil
}

func recordSetDestroy(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	set, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	machine := call.VM()
	destroyed, err := recordSetFlag(machine, set, "destroyed")
	if err != nil || destroyed {
		return jvm.VoidValue(), err
	}
	updated, err := recordSetFlag(machine, set, "updated")
	if err != nil {
		return jvm.VoidValue(), err
	}
	if updated {
		store, err := recordSetStore(machine, set)
		if err != nil {
			return jvm.VoidValue(), err
		}
		if store != nil {
			if _, err := call.InvokeVirtual(store, "removeRecordListener", "(L"+recordListenerName+";)V", jvm.ReferenceValue(set)); err != nil {
				return jvm.VoidValue(), err
			}
		}
		if err := setRecordSetFlag(machine, set, "updated", false); err != nil {
			return jvm.VoidValue(), err
		}
	}
	if err := setRecordSetFlag(machine, set, "destroyed", true); err != nil {
		return jvm.VoidValue(), err
	}
	return jvm.VoidValue(), setRecordIDs(call, set, nil)
}

// recordSetChanged is what the store calls when its records move under a
// subscribed enumeration. Every change reselects, because a filter or a
// comparator can make one added record change the whole selection.
func recordSetChanged(call *jvm.Invocation, arguments []jvm.Value) (jvm.Value, error) {
	set, err := receiver(arguments)
	if err != nil {
		return jvm.VoidValue(), err
	}
	_, err = call.InvokeVirtual(set, "rebuild", "()V")
	return jvm.VoidValue(), err
}

func checkRecordSetAlive(machine *jvm.VM, set *jvm.Object) error {
	destroyed, err := recordSetFlag(machine, set, "destroyed")
	if err != nil {
		return err
	}
	if destroyed {
		return jvm.Throw(InvalidRecordIDExceptionClass, "enumeration is destroyed")
	}
	return nil
}
