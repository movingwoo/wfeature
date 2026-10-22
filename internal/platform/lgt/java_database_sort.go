package lgt

import (
	"context"
	"fmt"
	"slices"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// `DataBase.sortRecord(DataFilter filter, DataComparator comparator)`, which
// answers the ids of the records `filter` accepts — all of them when it is
// null — in id order when `comparator` is null, and otherwise in the order the
// comparator gives: a positive answer places its first record after the
// second (FOLLOWS 1, EQUIVALENT 0, PRECEDES -1), and records it calls equal
// keep their id order. No record answers null. That is the specification's
// account and the common contract's (docs/wipi-storage.md).
//
// **An exception either of them throws is the title's**: it reaches the
// try region around the title's sortRecord call (rethrowFromCallback).
//
// **The filter and the comparator are the title's own objects**, so their one
// method is a call into the title, made the way a Runnable's `run` is reached
// (callJavaRunnable): by the member its class record names, or, for a class
// the compiler laid out itself, through the slot its interface table gives
// the interface. Each record is handed over as a byte array of its own, as
// selectRecord would answer it, and the records are taken as they are when
// the call starts: a callback that writes the database does not change what
// is being sorted.
func javaSortRecord(
	client *Client, ctx context.Context, thread *armcore.Thread, arguments []uint32,
) (uint32, error) {
	database, err := client.javaDatabaseOf(arguments[0])
	if err != nil {
		return 0, err
	}
	filter, comparator := arguments[1], arguments[2]
	records := make(map[uint32][]byte)
	ids := make([]uint32, 0, len(database.records))
	for index, record := range database.records {
		if !database.deleted[index] {
			ids = append(ids, uint32(index))
			records[uint32(index)] = slices.Clone(record)
		}
	}
	arrays := make(map[uint32]uint32, len(ids))
	array := func(id uint32) (uint32, error) {
		if object, made := arrays[id]; made {
			return object, nil
		}
		object, err := client.newJavaByteArray(records[id])
		if err != nil {
			return 0, err
		}
		arrays[id] = object
		return object, nil
	}
	if filter != 0 {
		body, err := client.javaCallbackBody(filter, javaDataFilterInterface, "filter")
		if err != nil {
			return 0, client.throwJavaPlatform(thread, javaDataBaseExceptionClass, ": "+err.Error())
		}
		kept := ids[:0]
		for _, id := range ids {
			data, err := array(id)
			if err != nil {
				return 0, err
			}
			answer, err := client.callOn(ctx, thread, body, []uint32{filter, data})
			if err != nil {
				return 0, client.rethrowFromCallback(thread, fmt.Errorf("run %s.filter at %#x: %w", javaDataFilterInterface, body, err))
			}
			if answer != 0 {
				kept = append(kept, id)
			}
		}
		ids = kept
	}
	if comparator != 0 && len(ids) > 1 {
		body, err := client.javaCallbackBody(comparator, javaDataComparatorInterface, "compare")
		if err != nil {
			return 0, client.throwJavaPlatform(thread, javaDataBaseExceptionClass, ": "+err.Error())
		}
		ids, err = stableMergeSort(ids, func(first, second uint32) (int32, error) {
			left, err := array(first)
			if err != nil {
				return 0, err
			}
			right, err := array(second)
			if err != nil {
				return 0, err
			}
			answer, err := client.callOn(ctx, thread, body, []uint32{comparator, left, right})
			if err != nil {
				return 0, fmt.Errorf("run %s.compare at %#x: %w", javaDataComparatorInterface, body, err)
			}
			return int32(answer), nil
		})
		if err != nil {
			return 0, client.rethrowFromCallback(thread, err)
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}
	return client.newJavaIntArray(ids)
}

// stableMergeSort orders ids by compare, a positive answer placing its first
// argument after its second, and keeps the given order of ids it calls equal.
// It is a merge sort rather than the library's because the comparison is a
// call into the title that can fail, and the failure has to stop the sort
// and be answered rather than be swallowed by a less function.
func stableMergeSort(ids []uint32, compare func(first, second uint32) (int32, error)) ([]uint32, error) {
	source := slices.Clone(ids)
	target := make([]uint32, len(source))
	for width := 1; width < len(source); width *= 2 {
		for low := 0; low < len(source); low += 2 * width {
			middle, high := min(low+width, len(source)), min(low+2*width, len(source))
			left, right, at := low, middle, low
			for left < middle && right < high {
				answer, err := compare(source[left], source[right])
				if err != nil {
					return nil, err
				}
				if answer > 0 {
					target[at] = source[right]
					right++
				} else {
					target[at] = source[left]
					left++
				}
				at++
			}
			at += copy(target[at:], source[left:middle])
			copy(target[at:], source[right:high])
		}
		source, target = target, source
	}
	return source, nil
}

// javaCallbackBody answers where the title's object implements the one method
// of an interface it was handed as: the member its class record names, or the
// entry its interface table gives the interface — in the object's own
// dispatch table when the entry holds a slot, and the entry's own first method
// when it holds the method addresses itself (see javaInterfaceTable). A class
// above the object's that implements the interface counts.
func (client *Client) javaCallbackBody(object uint32, interfaceName, method string) (uint32, error) {
	class, known := client.javaClassOfObject(object)
	if !known {
		return 0, fmt.Errorf("the %s at %#x is not an object issued here", interfaceName, object)
	}
	if member, _, ok := client.findJavaMethod(class.Record, method); ok && member.Body != 0 {
		return member.Body, nil
	}
	for owner := class; owner != nil; owner = owner.Super {
		for _, implemented := range owner.Record.Interfaces {
			if implemented.Name != interfaceName {
				continue
			}
			var body uint32
			var err error
			switch {
			case implemented.Slot < class.Slots && class.VTable != 0:
				body, err = client.readWord(class.VTable + 4 + implemented.Slot*4)
			case implemented.Entry != 0:
				body, err = client.readWord(implemented.Entry + 4)
			}
			if err == nil && body != 0 {
				return body, nil
			}
		}
	}
	return 0, fmt.Errorf("%s implements no %s.%s", class.Name, interfaceName, method)
}
