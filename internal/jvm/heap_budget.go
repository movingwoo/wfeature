package jvm

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"unicode/utf16"
	"weak"
)

// DefaultMaxHeapBytes is the heap budget a VM gets when Options.MaxHeapBytes is
// zero. See "The heap budget" in docs/jvm.md for the titles it was measured
// against and the margin it leaves.
const DefaultMaxHeapBytes int64 = 256 << 20

// heapSampleBytes is the mean distance, in charged bytes, between two samples.
// An allocation of at least this size is always a sample and counts exactly.
const heapSampleBytes = 256 << 10

// heapCompactSamples is the fewest samples a budget holds before it looks for
// the dead ones on its own rather than when the budget is reached.
const heapCompactSamples = 1024

// What the budget charges for each kind of allocation: the Go memory this
// runtime spends on it, which is not what a handset spent. An array element is
// a whole Value whatever the component type, so a guest's arrays cost here
// many times what they cost on the handset, and the budget is measured in
// these bytes.
var (
	objectStructBytes = int64(reflect.TypeFor[Object]().Size())
	// An object's field table: the map header and its first group of slots.
	objectHeapBytes = objectStructBytes + 48 + 8*int64(reflect.TypeFor[string]().Size()+reflect.TypeFor[Value]().Size()) + 8
	arrayHeapBytes  = objectStructBytes + int64(reflect.TypeFor[Array]().Size()+reflect.TypeFor[valueStorage]().Size())
	valueHeapBytes  = int64(reflect.TypeFor[Value]().Size())
	stringHeapBytes = objectStructBytes + int64(reflect.TypeFor[string]().Size())
	// A small object the class library hands back with its state in Native.
	libraryObjectHeapBytes = objectStructBytes + 16
)

// guestHeap bounds what one VM's guest can keep alive (see "The heap budget"
// in docs/jvm.md).
//
// Go's collector is this runtime's collector, so whether an object is still
// alive is Go's answer, and asking it per object costs a weak pointer. The
// budget asks for a sample instead: allocations are charged as a stream of
// bytes, and the allocation that crosses each sampling point, drawn from an
// exponential distribution with a mean of interval bytes, is recorded with a
// weak pointer and a weight that makes the sum of live weights an unbiased
// estimate of the live bytes. That is the method Go's own heap profile uses.
// An allocation that is not a sample costs one atomic subtraction. One of at
// least interval bytes is always a sample and weighs exactly its size, so a
// large array is counted exactly and refused before it is made.
//
// When a sample would take the charge past the limit, the samples whose
// objects have died are dropped; if that is not enough, the budget runs a
// collection and drops again, and only then refuses with OutOfMemoryError.
// Between samples nothing is checked, so the guest can pass the limit by about
// one interval before it is told.
type guestHeap struct {
	limit int64
	// interval is heapSampleBytes outside tests.
	interval int64
	// countdown is the bytes left before the next sampling point.
	countdown atomic.Int64

	mu      sync.Mutex
	random  *rand.Rand
	samples []heapSample
	// charged is the weight of every sample, live or not yet known to be
	// dead, plus pending.
	charged int64
	// pending is weight reserved by sample for an allocation that has not
	// been recorded yet.
	pending   int64
	compactAt int
	logger    *slog.Logger
	refused   bool
}

// heapSample is one sampled allocation. Exactly one of the weak pointers is
// set: the object, the element storage of an array (which a platform may
// replace, freeing it while the array lives on), or a StringBuffer's
// characters (which are replaced each time the buffer outgrows them).
type heapSample struct {
	weight int64
	object weak.Pointer[Object]
	values weak.Pointer[Value]
	units  weak.Pointer[uint16]
}

func (sample heapSample) live() bool {
	return sample.object.Value() != nil || sample.values.Value() != nil || sample.units.Value() != nil
}

func newGuestHeap(limit, interval int64, logger *slog.Logger) *guestHeap {
	budget := &guestHeap{
		limit:    limit,
		interval: interval,
		// A fixed seed: the same allocations sample the same points, so a
		// run that reaches the limit reaches it at the same allocation.
		random:    rand.New(rand.NewPCG(0x5f3759df, 0x9e3779b97f4a7c15)),
		compactAt: heapCompactSamples,
		logger:    logger,
	}
	budget.countdown.Store(budget.nextInterval())
	return budget
}

func (budget *guestHeap) nextInterval() int64 {
	return max(1, int64(budget.random.ExpFloat64()*float64(budget.interval)))
}

// sampleWeight is what one sampled allocation of a size below the sampling
// interval stands for: its size over the chance that a sampling point fell
// inside it.
func (budget *guestHeap) sampleWeight(bytes int64) int64 {
	return int64(math.Ceil(float64(bytes) / -math.Expm1(-float64(bytes)/float64(budget.interval))))
}

// charge reserves bytes for an allocation about to be made. It answers the
// weight to record once the allocation exists, zero when the allocation is not
// a sample, or OutOfMemoryError when refuse is set and a collection cannot make
// room. Every non-zero weight must be recorded, or it stays charged for good.
//
// The check before sample is the whole cost of an allocation that is not a
// sample.
func (budget *guestHeap) charge(bytes int64, refuse bool) (int64, error) {
	if bytes < budget.interval && budget.countdown.Add(-bytes) > 0 {
		return 0, nil
	}
	return budget.sample(bytes, refuse)
}

// sample is charge for an allocation at or past a sampling point.
func (budget *guestHeap) sample(bytes int64, refuse bool) (int64, error) {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	weight := bytes
	if bytes < budget.interval {
		if budget.countdown.Load() > 0 {
			// Another allocation took this sampling point and drew the next.
			return 0, nil
		}
		budget.countdown.Store(budget.nextInterval())
		weight = budget.sampleWeight(bytes)
	}
	if refuse && budget.charged+weight > budget.limit && !budget.collect(weight) {
		budget.logRefusal(bytes)
		return 0, guestException("java/lang/OutOfMemoryError",
			fmt.Sprintf("heap budget of %d bytes exhausted (%d in use, %d requested)", budget.limit, budget.charged, bytes))
	}
	budget.charged += weight
	budget.pending += weight
	return weight, nil
}

// logRefusal reports the first refusal as a warning and the rest at debug
// level, because a guest that catches the error may meet it every frame.
func (budget *guestHeap) logRefusal(bytes int64) {
	if budget.logger == nil {
		return
	}
	level := slog.LevelDebug
	if !budget.refused {
		level = slog.LevelWarn
		budget.refused = true
	}
	budget.logger.Log(context.Background(), level, "guest heap budget exhausted",
		"limit", budget.limit, "charged", budget.charged, "requested", bytes)
}

// collect makes room for weight more if dead objects are holding it, first
// from what Go's collector has already found and then after a collection of
// its own.
func (budget *guestHeap) collect(weight int64) bool {
	budget.compact()
	if budget.charged+weight <= budget.limit {
		return true
	}
	runtime.GC()
	budget.compact()
	return budget.charged+weight <= budget.limit
}

func (budget *guestHeap) record(sample heapSample) {
	budget.mu.Lock()
	defer budget.mu.Unlock()
	budget.pending -= sample.weight
	budget.samples = append(budget.samples, sample)
	if len(budget.samples) >= budget.compactAt {
		budget.compact()
	}
}

// compact drops the samples whose allocation has died and recounts the charge
// from the rest.
func (budget *guestHeap) compact() {
	kept := budget.samples[:0]
	charged := budget.pending
	for _, sample := range budget.samples {
		if sample.live() {
			kept = append(kept, sample)
			charged += sample.weight
		}
	}
	clear(budget.samples[len(kept):])
	budget.samples = kept
	budget.charged = charged
	budget.compactAt = max(2*len(kept), heapCompactSamples)
}

// chargeHeap reserves bytes for an allocation about to be made; see
// guestHeap.charge.
func (vm *VM) chargeHeap(bytes int64) (int64, error) {
	return vm.heap.charge(bytes, true)
}

// chargeObject charges an object that already exists, for the allocations
// small enough that making them before asking costs nothing worth saving.
func (vm *VM) chargeObject(object *Object, bytes int64) error {
	if bytes < vm.heap.interval && vm.heap.countdown.Add(-bytes) > 0 {
		return nil
	}
	weight, err := vm.heap.sample(bytes, true)
	if err != nil {
		return err
	}
	vm.recordHeapObject(weight, object)
	return nil
}

func (vm *VM) recordHeapObject(weight int64, object *Object) {
	if weight != 0 {
		vm.heap.record(heapSample{weight: weight, object: weak.Make(object)})
	}
}

// recordHeapArray records an array by its element storage when it has any,
// because a platform that binds the array to its own storage frees that
// storage while the array object lives on.
func (vm *VM) recordHeapArray(weight int64, object *Object, values []Value) {
	if weight == 0 {
		return
	}
	if len(values) == 0 {
		vm.recordHeapObject(weight, object)
		return
	}
	vm.heap.record(heapSample{weight: weight, values: weak.Make(&values[0])})
}

// HeapUsage answers what the heap budget currently charges to allocations that
// are still alive, as far as Go's collector has found, and the limit. The
// charge is an estimate from samples (see guestHeap); a caller that wants it
// to reflect only live objects runs a collection first.
func (vm *VM) HeapUsage() (charged, limit int64) {
	if vm == nil {
		return 0, 0
	}
	vm.heap.mu.Lock()
	defer vm.heap.mu.Unlock()
	vm.heap.compact()
	return vm.heap.charged, vm.heap.limit
}

// newStringObject makes a String the class library hands back, charged to the
// heap budget.
func (vm *VM) newStringObject(text string) (*Object, error) {
	object := nativeStringValue(text)
	if err := vm.chargeObject(object, stringHeapBytes+int64(len(text))); err != nil {
		return nil, err
	}
	return object, nil
}

// stringResult is newStringObject as a method's result.
func (vm *VM) stringResult(text string) (Value, error) {
	object, err := vm.newStringObject(text)
	if err != nil {
		return VoidValue(), err
	}
	return ReferenceValue(object), nil
}

// unrefusedString makes a String that is charged and never refused: the text
// of a caught exception, which a title reads in the handler for the
// OutOfMemoryError this budget raised. A handset answered it without
// allocating.
func (vm *VM) unrefusedString(text string) *Object {
	object := nativeStringValue(text)
	weight, _ := vm.heap.charge(stringHeapBytes+int64(len(text)), false)
	vm.recordHeapObject(weight, object)
	return object
}

// libraryObject makes one of the small objects a class-library method hands
// back with its state in Native, charged to the heap budget.
func (vm *VM) libraryObject(className string, native any) (Value, error) {
	object := &Object{ClassName: className, Native: native}
	if err := vm.chargeObject(object, libraryObjectHeapBytes); err != nil {
		return VoidValue(), err
	}
	return ReferenceValue(object), nil
}

// newByteArrayObject is NewByteArray for a class-library method, charged to the
// heap budget before the elements are made.
func (vm *VM) newByteArrayObject(data []byte) (*Object, error) {
	weight, err := vm.chargeHeap(arrayHeapBytes + int64(len(data))*valueHeapBytes)
	if err != nil {
		return nil, err
	}
	object := NewByteArray(data)
	vm.recordHeapArray(weight, object, object.Native.(*Array).storage.(valueStorage))
	return object, nil
}

// byteArrayResult is newByteArrayObject as a method's result.
func (vm *VM) byteArrayResult(data []byte) (Value, error) {
	object, err := vm.newByteArrayObject(data)
	if err != nil {
		return VoidValue(), err
	}
	return ReferenceValue(object), nil
}

// minimumUnitsCapacity keeps a StringBuffer's characters out of Go's tiny
// allocator, which packs small pointer-free blocks together so that one of
// them cannot be seen to die while its neighbours live.
const minimumUnitsCapacity = 16

// unitsWithRoom answers units with room for extra more characters. When they
// do not fit it makes the larger buffer itself, doubling as a Java
// StringBuffer does, after charging it to the heap budget, so that appending
// to the answer never allocates.
func (vm *VM) unitsWithRoom(units []uint16, extra int) ([]uint16, error) {
	need := len(units) + extra
	if need <= cap(units) {
		return units, nil
	}
	capacity := max(need, 2*cap(units), minimumUnitsCapacity)
	weight, err := vm.chargeHeap(2 * int64(capacity))
	if err != nil {
		return nil, err
	}
	grown := make([]uint16, len(units), capacity)
	copy(grown, units)
	if weight != 0 {
		vm.heap.record(heapSample{weight: weight, units: weak.Make(&grown[:1][0])})
	}
	return grown, nil
}

// appendUnits appends text to units as UTF-16, growing them through
// unitsWithRoom. Each rune is encoded as utf16.Encode would, a value that is
// not a scalar becoming U+FFFD.
func (vm *VM) appendUnits(units []uint16, text string) ([]uint16, error) {
	count := 0
	for _, character := range text {
		if utf16.RuneLen(character) == 2 {
			count += 2
		} else {
			count++
		}
	}
	units, err := vm.unitsWithRoom(units, count)
	if err != nil {
		return nil, err
	}
	for _, character := range text {
		units = utf16.AppendRune(units, character)
	}
	return units, nil
}
