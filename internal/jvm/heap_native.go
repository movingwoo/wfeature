package jvm

import (
	"encoding/binary"
	"fmt"
	"reflect"
	"slices"
	"time"
)

type heapListKey struct {
	pointer          uintptr
	length, capacity int
}
type heapListRange struct{ start, end uintptr }

func (capture *heapCapture) validateListRanges() error {
	slices.SortFunc(capture.lists, func(a, b heapListRange) int {
		if a.start < b.start {
			return -1
		}
		if a.start > b.start {
			return 1
		}
		return 0
	})
	for i := 1; i < len(capture.lists); i++ {
		if capture.lists[i].start < capture.lists[i-1].end {
			return fmt.Errorf("JVM native object lists have overlapping slice views")
		}
	}
	return nil
}

func (capture *heapCapture) native(native any) (uint32, error) {
	if native == nil {
		return 0, nil
	}
	var key any
	value := reflect.ValueOf(native)
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return 0, fmt.Errorf("JVM native payload is a typed nil pointer")
		}
		key = native
	}
	if list, ok := native.([]*Object); ok && cap(list) != 0 {
		key = heapListKey{pointer: value.Pointer(), length: len(list), capacity: cap(list)}
	}
	if key != nil {
		if id := capture.payloadIDs[key]; id != 0 {
			return id, nil
		}
	}
	if len(capture.saved.Payloads) >= heapStateObjects {
		return 0, fmt.Errorf("JVM native payload count exceeds limit")
	}
	if err := capture.budget.add(1, 96); err != nil {
		return 0, err
	}
	record := HeapPayloadState{}
	copyBytes := func(data []byte) error {
		if err := capture.budget.add(len(data), 1); err != nil {
			return err
		}
		record.Data = append([]byte(nil), data...)
		return nil
	}
	switch data := native.(type) {
	case string:
		record.Kind = "string"
		if err := capture.budget.add(len(data), 1); err != nil {
			return 0, err
		}
		record.Data = []byte(data)
	case int32:
		record.Kind, record.Data = "int32", make([]byte, 4)
		binary.LittleEndian.PutUint32(record.Data, uint32(data))
	case int64:
		record.Kind, record.Data = "int64", make([]byte, 8)
		binary.LittleEndian.PutUint64(record.Data, uint64(data))
	case []*Object:
		record.Kind, record.Data = "objects", make([]byte, 4)
		binary.LittleEndian.PutUint32(record.Data, uint32(len(data)))
		record.References = capture.references(data[:cap(data)])
		if k, ok := key.(heapListKey); ok {
			capture.lists = append(capture.lists, heapListRange{start: k.pointer, end: k.pointer + uintptr(k.capacity)*reflect.TypeOf((*Object)(nil)).Size()})
		}
	case *stringBufferData:
		record.Kind = "buffer"
		data.mu.Lock()
		defer data.mu.Unlock()
		if err := capture.budget.add(len(data.units), 2); err != nil {
			return 0, err
		}
		record.Data = make([]byte, len(data.units)*2)
		for i, unit := range data.units {
			binary.LittleEndian.PutUint16(record.Data[2*i:], unit)
		}
	case *timeZoneData:
		record.Kind = "timezone"
		if err := capture.budget.add(len(data.id), 1); err != nil {
			return 0, err
		}
		record.Data = make([]byte, 4+len(data.id))
		binary.LittleEndian.PutUint32(record.Data, uint32(data.offset))
		copy(record.Data[4:], data.id)
	case *calendarData:
		record.Kind = "calendar-rules-v1"
		encoded, err := captureHeapCalendar(data.time)
		if err != nil {
			return 0, err
		}
		if err := copyBytes(encoded); err != nil {
			return 0, err
		}
	case *Array:
		data.mu.RLock()
		defer data.mu.RUnlock()
		if data.storage == nil {
			return 0, fmt.Errorf("JVM heap array has no storage")
		}
		record.Kind = "array"
		record.Array = &HeapArrayState{Component: data.Component.Descriptor(), Length: data.storage.Len()}
		if record.Array.Length < 0 || record.Array.Length > capture.vm.config.MaxArrayLength {
			return 0, fmt.Errorf("JVM heap array exceeds policy")
		}
		if values, ok := data.storage.(valueStorage); ok {
			if err := capture.budget.add(len(values), 32); err != nil {
				return 0, err
			}
			record.Array.Values = make([]HeapValueState, len(values))
			for i, value := range values {
				record.Array.Values[i] = capture.value(value)
			}
		} else {
			if capture.codec.CaptureArray == nil {
				return 0, fmt.Errorf("JVM heap has an unsupported array storage %T", data.storage)
			}
			external, err := capture.codec.CaptureArray(data.storage)
			if err != nil {
				return 0, err
			}
			record.ExternalKind, record.References = external.Kind, capture.references(external.References)
			if external.Kind == "" {
				return 0, fmt.Errorf("JVM external array codec has no kind")
			}
			if err := copyBytes(external.Data); err != nil {
				return 0, err
			}
		}
	default:
		if capture.codec.CaptureNative == nil {
			return 0, fmt.Errorf("JVM heap has an unsupported native payload %T", native)
		}
		external, err := capture.codec.CaptureNative(native)
		if err != nil {
			return 0, err
		}
		record.Kind, record.ExternalKind, record.References = "external", external.Kind, capture.references(external.References)
		if external.Kind == "" {
			return 0, fmt.Errorf("JVM external native codec has no kind")
		}
		if err := copyBytes(external.Data); err != nil {
			return 0, err
		}
	}
	if capture.err != nil {
		return 0, capture.err
	}
	id := uint32(len(capture.saved.Payloads) + 1)
	capture.saved.Payloads = append(capture.saved.Payloads, record)
	if key != nil {
		capture.payloadIDs[key] = id
	}
	return id, nil
}

func restoreHeapPayload(record HeapPayloadState, objects []*Object, codec HeapCodec) (any, error) {
	refs := make([]*Object, len(record.References))
	for i, id := range record.References {
		refs[i] = objects[id]
	}
	external := HeapExternalPayload{Kind: record.ExternalKind, Data: append([]byte(nil), record.Data...), References: refs}
	switch record.Kind {
	case "string":
		return string(record.Data), nil
	case "int32":
		return int32(binary.LittleEndian.Uint32(record.Data)), nil
	case "int64":
		return int64(binary.LittleEndian.Uint64(record.Data)), nil
	case "objects":
		return refs[:int(binary.LittleEndian.Uint32(record.Data))], nil
	case "buffer":
		data := &stringBufferData{units: make([]uint16, len(record.Data)/2)}
		for i := range data.units {
			data.units[i] = binary.LittleEndian.Uint16(record.Data[2*i:])
		}
		return data, nil
	case "timezone":
		return &timeZoneData{offset: int32(binary.LittleEndian.Uint32(record.Data)), id: string(record.Data[4:])}, nil
	case "calendar":
		seconds, nanos := int64(binary.LittleEndian.Uint64(record.Data)), binary.LittleEndian.Uint32(record.Data[8:])
		offset := int32(binary.LittleEndian.Uint32(record.Data[12:]))
		return &calendarData{time: time.Unix(seconds, int64(nanos)).In(time.FixedZone(string(record.Data[16:]), int(offset)))}, nil
	case "calendar-rules-v1":
		return restoreHeapCalendar(record.Data)
	case "array":
		component, _ := ParseFieldDescriptor(record.Array.Component)
		var storage ArrayStorage
		if record.ExternalKind == "" {
			values := make(valueStorage, len(record.Array.Values))
			for i, value := range record.Array.Values {
				values[i] = restoreHeapValue(value, objects)
			}
			storage = values
		} else {
			if codec.RestoreArray == nil {
				return nil, fmt.Errorf("JVM heap requires an external array codec")
			}
			var err error
			storage, err = codec.RestoreArray(component, record.Array.Length, external)
			if err != nil {
				return nil, err
			}
			if storage == nil || storage.Len() != record.Array.Length {
				return nil, fmt.Errorf("JVM restored external array has a different length")
			}
		}
		return &Array{Component: component, storage: storage}, nil
	case "external":
		if codec.RestoreNative == nil {
			return nil, fmt.Errorf("JVM heap requires an external native codec")
		}
		return codec.RestoreNative(external)
	default:
		return nil, fmt.Errorf("JVM heap has unknown payload kind %q", record.Kind)
	}
}

func restoreHeapValue(value HeapValueState, objects []*Object) Value {
	return Value{kind: value.Kind, bits: value.Bits, ref: objects[value.Reference]}
}
