package jvm

import (
	"fmt"
	"math"
	"sync"
	"sync/atomic"
)

type ValueKind uint8

const (
	ValueVoid ValueKind = iota
	ValueInt
	ValueLong
	ValueFloat
	ValueDouble
	ValueReference
	valueReturnAddress
	valueTop
)

type Value struct {
	kind ValueKind
	bits uint64
	ref  *Object
}

type Object struct {
	ClassName string
	Fields    map[string]Value
	Native    any
	monitor   monitor
	identity  atomic.Uint32
	// aotAddress is the guest address this object is bound behind, or zero
	// while it is Go-only. It lives on the object rather than in a second map
	// so the binding costs nothing per object to look up or to forget.
	aotAddress atomic.Uint32
	// aotRetain holds the objects this one's guest payload names, and only
	// while the platform has released it — see ReleaseAOTObject for why the
	// guest's reference graph has to be mirrored into Go's for exactly that
	// window. Read and written under the VM's aotMu.
	aotRetain []*Object
	fieldMu   sync.RWMutex
}

// ArrayStorage is where an array's elements live. The default keeps them in a
// Go slice. A platform whose guest already holds the elements in its own
// memory supplies its own storage instead, so an array that crosses the
// boundary has exactly one copy of each element rather than two the runtime
// has to keep in agreement by hand.
//
// Bounds are checked by Array before any of these are called.
type ArrayStorage interface {
	Len() int
	Load(index int) (Value, error)
	Store(index int, value Value) error
	// LoadRange returns a copy of count elements starting at offset.
	LoadRange(offset, count int) ([]Value, error)
	StoreRange(offset int, values []Value) error
}

// ByteArrayReader is the fast path for reading a byte array. A storage
// implements it when it can fill bytes without going through `[]Value`, which
// is the difference between one copy and three: reading a guest byte array used
// to build a `[]Value` — twenty-four bytes an element — and then walk it into
// the `[]byte` the caller wanted. On one archive that was 62% of everything
// allocated in five seconds of play.
//
// A storage that does not implement it is read through LoadRange as before,
// because a storage that keeps its elements somewhere else may have a reason to
// fetch a run at once rather than an element at a time.
type ByteArrayReader interface {
	ReadBytes(offset int, into []byte) error
}

type Array struct {
	Component Type
	storage   ArrayStorage
	mu        sync.RWMutex
}

// valueStorage is the default in-heap element storage.
type valueStorage []Value

func (storage valueStorage) Len() int { return len(storage) }

func (storage valueStorage) Load(index int) (Value, error) { return storage[index], nil }

func (storage valueStorage) Store(index int, value Value) error {
	storage[index] = value
	return nil
}

func (storage valueStorage) LoadRange(offset, count int) ([]Value, error) {
	return append([]Value(nil), storage[offset:offset+count]...), nil
}

func (storage valueStorage) ReadBytes(offset int, into []byte) error {
	for index := range into {
		integer, err := storage[offset+index].Int32()
		if err != nil {
			return fmt.Errorf("byte array element %d: %w", offset+index, err)
		}
		into[index] = byte(integer)
	}
	return nil
}

func (storage valueStorage) StoreRange(offset int, values []Value) error {
	copy(storage[offset:offset+len(values)], values)
	return nil
}

// Length reports how many elements the array holds.
func (array *Array) Length() int {
	array.mu.RLock()
	defer array.mu.RUnlock()
	return array.storage.Len()
}

func (array *Array) inRange(offset, count int) bool {
	return offset >= 0 && count >= 0 && offset+count <= array.storage.Len()
}

// Load reads one element.
func (array *Array) Load(index int) (Value, error) {
	array.mu.RLock()
	defer array.mu.RUnlock()
	if !array.inRange(index, 1) {
		return VoidValue(), guestException(
			"java/lang/ArrayIndexOutOfBoundsException",
			fmt.Sprintf("index %d for length %d", index, array.storage.Len()),
		)
	}
	return array.storage.Load(index)
}

// Store writes one element. The caller has already validated the value against
// the component type.
func (array *Array) Store(index int, value Value) error {
	array.mu.Lock()
	defer array.mu.Unlock()
	if !array.inRange(index, 1) {
		return guestException(
			"java/lang/ArrayIndexOutOfBoundsException",
			fmt.Sprintf("index %d for length %d", index, array.storage.Len()),
		)
	}
	return array.storage.Store(index, value)
}

// LoadRange copies count elements out of the array.
func (array *Array) LoadRange(offset, count int) ([]Value, error) {
	array.mu.RLock()
	defer array.mu.RUnlock()
	if !array.inRange(offset, count) {
		return nil, guestException("java/lang/ArrayIndexOutOfBoundsException", "array range")
	}
	return array.storage.LoadRange(offset, count)
}

// StoreRange writes values into the array starting at offset.
func (array *Array) StoreRange(offset int, values []Value) error {
	array.mu.Lock()
	defer array.mu.Unlock()
	if !array.inRange(offset, len(values)) {
		return guestException("java/lang/ArrayIndexOutOfBoundsException", "array range")
	}
	return array.storage.StoreRange(offset, values)
}

// ArraySnapshot returns the component type and a stable copy of a guest array.
// Native services use this boundary instead of retaining the array's own
// storage.
func ArraySnapshot(object *Object) (Type, []Value, error) {
	array, err := objectArray(object)
	if err != nil {
		return Type{}, nil, err
	}
	values, err := array.LoadRange(0, array.Length())
	if err != nil {
		return Type{}, nil, err
	}
	return array.Component, values, nil
}

// SetArrayRange validates and stores values in a guest array atomically. It is
// intended for native methods such as Image.getRGB that fill caller-owned
// arrays.
func SetArrayRange(object *Object, offset int, values []Value) error {
	array, err := objectArray(object)
	if err != nil {
		return err
	}
	for _, value := range values {
		if err := validateValue(value, array.Component); err != nil {
			return err
		}
	}
	return array.StoreRange(offset, values)
}

// ArrayComponent reports an array object's component type and length without
// reading any of its elements. A platform binding an array to guest memory
// needs the shape, not the contents.
func ArrayComponent(object *Object) (Type, int, bool) {
	array, err := objectArray(object)
	if err != nil {
		return Type{}, 0, false
	}
	return array.Component, array.Length(), true
}

// BindArrayStorage moves an array's elements into new storage. Every element
// held now is written through the new storage before it takes over, so an
// array the Host built keeps its contents when it becomes guest-backed.
func BindArrayStorage(object *Object, storage ArrayStorage) error {
	array, err := objectArray(object)
	if err != nil {
		return err
	}
	array.mu.Lock()
	defer array.mu.Unlock()
	if storage.Len() != array.storage.Len() {
		return fmt.Errorf("array storage holds %d elements, want %d", storage.Len(), array.storage.Len())
	}
	values, err := array.storage.LoadRange(0, array.storage.Len())
	if err != nil {
		return err
	}
	if len(values) > 0 {
		if err := storage.StoreRange(0, values); err != nil {
			return err
		}
	}
	array.storage = storage
	return nil
}

// NewByteArray creates an isolated guest byte array for a Host-owned payload.
func NewByteArray(data []byte) *Object {
	values := make([]Value, len(data))
	for index, value := range data {
		values[index] = IntValue(int32(int8(value)))
	}
	return &Object{
		ClassName: "[B",
		Native: &Array{
			Component: Type{Kind: TypeByte},
			storage:   valueStorage(values),
		},
	}
}

// ByteArraySnapshot copies a guest byte array into Host memory.
func ByteArraySnapshot(object *Object) ([]byte, error) {
	array, err := objectArray(object)
	if err != nil {
		return nil, err
	}
	array.mu.RLock()
	defer array.mu.RUnlock()
	if array.Component.Kind != TypeByte {
		return nil, fmt.Errorf("array component is %s, not byte", array.Component.Descriptor())
	}
	length := array.storage.Len()
	data := make([]byte, length)
	if length == 0 {
		return data, nil
	}
	if reader, ok := array.storage.(ByteArrayReader); ok {
		if err := reader.ReadBytes(0, data); err != nil {
			return nil, err
		}
		return data, nil
	}
	values, err := array.storage.LoadRange(0, length)
	if err != nil {
		return nil, err
	}
	for index, value := range values {
		integer, err := value.Int32()
		if err != nil {
			return nil, fmt.Errorf("byte array element %d: %w", index, err)
		}
		data[index] = byte(integer)
	}
	return data, nil
}

func VoidValue() Value {
	return Value{}
}

func IntValue(value int32) Value {
	return Value{kind: ValueInt, bits: uint64(uint32(value))}
}

func LongValue(value int64) Value {
	return Value{kind: ValueLong, bits: uint64(value)}
}

func FloatValue(value float32) Value {
	return Value{kind: ValueFloat, bits: uint64(math.Float32bits(value))}
}

func DoubleValue(value float64) Value {
	return Value{kind: ValueDouble, bits: math.Float64bits(value)}
}

func ReferenceValue(value *Object) Value {
	return Value{kind: ValueReference, ref: value}
}

func returnAddressValue(pc int) Value {
	return Value{kind: valueReturnAddress, bits: uint64(pc)}
}

func (v Value) Kind() ValueKind {
	return v.kind
}

func (v Value) Int32() (int32, error) {
	if v.kind != ValueInt {
		return 0, fmt.Errorf("value is %s, not int", v.kind)
	}
	return int32(uint32(v.bits)), nil
}

func (v Value) Int64() (int64, error) {
	if v.kind != ValueLong {
		return 0, fmt.Errorf("value is %s, not long", v.kind)
	}
	return int64(v.bits), nil
}

func (v Value) Float32() (float32, error) {
	if v.kind != ValueFloat {
		return 0, fmt.Errorf("value is %s, not float", v.kind)
	}
	return math.Float32frombits(uint32(v.bits)), nil
}

func (v Value) Float64() (float64, error) {
	if v.kind != ValueDouble {
		return 0, fmt.Errorf("value is %s, not double", v.kind)
	}
	return math.Float64frombits(v.bits), nil
}

func (v Value) Reference() (*Object, error) {
	if v.kind != ValueReference {
		return nil, fmt.Errorf("value is %s, not reference", v.kind)
	}
	return v.ref, nil
}

func (v Value) slots() int {
	if v.kind == ValueLong || v.kind == ValueDouble {
		return 2
	}
	if v.kind == ValueVoid || v.kind == valueTop {
		return 0
	}
	return 1
}

func (k ValueKind) String() string {
	switch k {
	case ValueVoid:
		return "void"
	case ValueInt:
		return "int"
	case ValueLong:
		return "long"
	case ValueFloat:
		return "float"
	case ValueDouble:
		return "double"
	case ValueReference:
		return "reference"
	case valueReturnAddress:
		return "return-address"
	case valueTop:
		return "top"
	default:
		return fmt.Sprintf("value-kind-%d", k)
	}
}

func zeroValue(typeInfo Type) Value {
	switch typeInfo.Kind {
	case TypeBoolean, TypeByte, TypeChar, TypeShort, TypeInt:
		return IntValue(0)
	case TypeLong:
		return LongValue(0)
	case TypeFloat:
		return FloatValue(0)
	case TypeDouble:
		return DoubleValue(0)
	case TypeReference, TypeArray:
		return ReferenceValue(nil)
	default:
		return VoidValue()
	}
}

func validateValue(value Value, typeInfo Type) error {
	want := ValueVoid
	switch typeInfo.Kind {
	case TypeBoolean, TypeByte, TypeChar, TypeShort, TypeInt:
		want = ValueInt
	case TypeLong:
		want = ValueLong
	case TypeFloat:
		want = ValueFloat
	case TypeDouble:
		want = ValueDouble
	case TypeReference, TypeArray:
		want = ValueReference
	case TypeVoid:
		want = ValueVoid
	}
	if value.kind != want {
		return fmt.Errorf("expected %s, got %s", want, value.kind)
	}
	return nil
}
