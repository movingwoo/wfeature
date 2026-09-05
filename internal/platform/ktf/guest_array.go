package ktf

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// guestArrayStorage keeps a bound array's elements in guest memory and nowhere
// else. Guest code parses array bytes directly, so a second copy on the Go
// side would be a cache the runtime has to write back and re-read by hand at
// every boundary crossing; making guest memory the only storage removes both
// the copying and the chance of the two disagreeing.
type guestArrayStorage struct {
	runtime   *initializationRuntime
	elements  uint32
	length    int
	component jvm.Type
	size      int
}

// newGuestArrayStorage describes the element payload of an allocated array
// object, whose header holds the instance words, the dispatch header and the
// length before the elements begin.
func (runtime *initializationRuntime) newGuestArrayStorage(address uint32, component jvm.Type, length int) (*guestArrayStorage, error) {
	size, err := aotArrayElementBytes(component)
	if err != nil {
		return nil, err
	}
	return &guestArrayStorage{
		runtime:   runtime,
		elements:  address + javaInstanceSize + javaInstanceHeader + javaArrayLengthSize,
		length:    length,
		component: component,
		size:      size,
	}, nil
}

func (storage *guestArrayStorage) Len() int {
	return storage.length
}

func (storage *guestArrayStorage) Load(index int) (jvm.Value, error) {
	var slot [8]byte
	data := slot[:storage.size]
	if err := storage.runtime.client.core.Memory().Read(storage.address(index), data); err != nil {
		return jvm.VoidValue(), fmt.Errorf("read KTF array element %d: %w", index, err)
	}
	return storage.decode(data, index)
}

func (storage *guestArrayStorage) Store(index int, value jvm.Value) error {
	var slot [8]byte
	data := slot[:storage.size]
	if err := storage.encode(data, value, index); err != nil {
		return err
	}
	if err := storage.runtime.client.core.Memory().Write(storage.address(index), data); err != nil {
		return fmt.Errorf("write KTF array element %d: %w", index, err)
	}
	return nil
}

// LoadRange reads a run of elements with one guest memory access, which is how
// the snapshot boundaries native code uses stay a single read.
func (storage *guestArrayStorage) LoadRange(offset, count int) ([]jvm.Value, error) {
	values := make([]jvm.Value, count)
	if count == 0 {
		return values, nil
	}
	data := make([]byte, count*storage.size)
	if err := storage.runtime.client.core.Memory().Read(storage.address(offset), data); err != nil {
		return nil, fmt.Errorf("read KTF array elements %d..%d: %w", offset, offset+count-1, err)
	}
	for index := range values {
		value, err := storage.decode(data[index*storage.size:(index+1)*storage.size], offset+index)
		if err != nil {
			return nil, err
		}
		values[index] = value
	}
	return values, nil
}

// ReadBytes fills bytes straight out of guest memory, which for a byte array is
// what the memory already holds: one read, no decode, and no `[]Value` in
// between. See jvm.ByteArrayReader. A component of any other width is left to
// LoadRange, which is where the decode belongs.
func (storage *guestArrayStorage) ReadBytes(offset int, into []byte) error {
	if storage.size != 1 {
		return fmt.Errorf("KTF array element is %d bytes, not one", storage.size)
	}
	if len(into) == 0 {
		return nil
	}
	if err := storage.runtime.client.core.Memory().Read(storage.address(offset), into); err != nil {
		return fmt.Errorf("read KTF array elements %d..%d: %w", offset, offset+len(into)-1, err)
	}
	return nil
}

func (storage *guestArrayStorage) StoreRange(offset int, values []jvm.Value) error {
	if len(values) == 0 {
		return nil
	}
	data := make([]byte, len(values)*storage.size)
	for index, value := range values {
		if err := storage.encode(data[index*storage.size:(index+1)*storage.size], value, offset+index); err != nil {
			return err
		}
	}
	if err := storage.runtime.client.core.Memory().Write(storage.address(offset), data); err != nil {
		return fmt.Errorf("write KTF array elements %d..%d: %w", offset, offset+len(values)-1, err)
	}
	return nil
}

func (storage *guestArrayStorage) address(index int) uint32 {
	return storage.elements + uint32(index*storage.size)
}

func (storage *guestArrayStorage) decode(slot []byte, index int) (jvm.Value, error) {
	switch storage.component.Kind {
	case jvm.TypeBoolean:
		return jvm.IntValue(int32(slot[0] & 1)), nil
	case jvm.TypeByte:
		return jvm.IntValue(int32(int8(slot[0]))), nil
	case jvm.TypeChar:
		return jvm.IntValue(int32(binary.LittleEndian.Uint16(slot))), nil
	case jvm.TypeShort:
		return jvm.IntValue(int32(int16(binary.LittleEndian.Uint16(slot)))), nil
	case jvm.TypeInt:
		return jvm.IntValue(int32(binary.LittleEndian.Uint32(slot))), nil
	case jvm.TypeFloat:
		return jvm.FloatValue(math.Float32frombits(binary.LittleEndian.Uint32(slot))), nil
	case jvm.TypeLong:
		return jvm.LongValue(int64(binary.LittleEndian.Uint64(slot))), nil
	case jvm.TypeDouble:
		return jvm.DoubleValue(math.Float64frombits(binary.LittleEndian.Uint64(slot))), nil
	case jvm.TypeReference, jvm.TypeArray:
		word := binary.LittleEndian.Uint32(slot)
		if word == 0 {
			return jvm.ReferenceValue(nil), nil
		}
		element, bound := storage.runtime.client.vm.AOTObject(word)
		if !bound {
			return jvm.VoidValue(), fmt.Errorf("KTF array element %d references unbound guest address %#x", index, word)
		}
		return jvm.ReferenceValue(element), nil
	default:
		return jvm.VoidValue(), fmt.Errorf("KTF array has invalid component %s", storage.component.Descriptor())
	}
}

func (storage *guestArrayStorage) encode(slot []byte, value jvm.Value, index int) error {
	switch storage.component.Kind {
	case jvm.TypeBoolean, jvm.TypeByte, jvm.TypeChar, jvm.TypeShort, jvm.TypeInt:
		integer, err := value.Int32()
		if err != nil {
			return fmt.Errorf("KTF array element %d: %w", index, err)
		}
		switch storage.size {
		case 1:
			slot[0] = byte(integer)
		case 2:
			binary.LittleEndian.PutUint16(slot, uint16(integer))
		default:
			binary.LittleEndian.PutUint32(slot, uint32(integer))
		}
	case jvm.TypeFloat:
		floating, err := value.Float32()
		if err != nil {
			return fmt.Errorf("KTF array element %d: %w", index, err)
		}
		binary.LittleEndian.PutUint32(slot, math.Float32bits(floating))
	case jvm.TypeLong:
		integer, err := value.Int64()
		if err != nil {
			return fmt.Errorf("KTF array element %d: %w", index, err)
		}
		binary.LittleEndian.PutUint64(slot, uint64(integer))
	case jvm.TypeDouble:
		floating, err := value.Float64()
		if err != nil {
			return fmt.Errorf("KTF array element %d: %w", index, err)
		}
		binary.LittleEndian.PutUint64(slot, math.Float64bits(floating))
	case jvm.TypeReference, jvm.TypeArray:
		element, err := value.Reference()
		if err != nil {
			return fmt.Errorf("KTF array element %d: %w", index, err)
		}
		var word uint32
		if element != nil {
			// An object stored into a guest-visible array has to have a guest
			// address before the word is written, or the guest would read a
			// null where a live reference belongs.
			if err := storage.runtime.ensureResultBound(element); err != nil {
				return err
			}
			word, _ = storage.runtime.client.vm.AOTAddress(element)
		}
		binary.LittleEndian.PutUint32(slot, word)
	default:
		return fmt.Errorf("KTF array has invalid component %s", storage.component.Descriptor())
	}
	return nil
}
