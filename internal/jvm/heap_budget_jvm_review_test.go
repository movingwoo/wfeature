package jvm

import (
	_ "embed"
	"runtime"
	"strings"
	"testing"
)

//go:embed testdata/HeapBudget.class
var heapBudgetClass []byte

//go:embed testdata/HeapBudget$Node.class
var heapBudgetNodeClass []byte

// testHeapBudget is small enough that the fixture's loops reach it in a few
// thousand allocations.
const testHeapBudget = 4 << 20

func heapBudgetSource() mapClassSource {
	return mapClassSource{"HeapBudget": heapBudgetClass, "HeapBudget$Node": heapBudgetNodeClass}
}

// newBudgetVM samples far more finely than a session does, so a budget of a
// few megabytes is reached within a few objects of where it lies.
func newBudgetVM(source ClassSource, limit int64) *VM {
	vm := New(source, Options{MaxHeapBytes: limit})
	vm.heap = newGuestHeap(limit, 4<<10, nil)
	return vm
}

func heapBudgetInt(t *testing.T, vm *VM, method, descriptor string, arguments ...Value) int32 {
	t.Helper()
	result, err := vm.InvokeStatic("HeapBudget", method, descriptor, arguments...)
	if err != nil {
		t.Fatalf("%s() error = %v", method, err)
	}
	value, err := result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func heapBudgetString(t *testing.T, vm *VM, method string) string {
	t.Helper()
	result, err := vm.InvokeStatic("HeapBudget", method, "()Ljava/lang/String;")
	if err != nil {
		t.Fatalf("%s() error = %v", method, err)
	}
	object, _ := result.Reference()
	text, ok := StringText(object)
	if !ok {
		t.Fatalf("%s() = %v, want a String", method, object)
	}
	return text
}

// A guest that keeps allocating is refused with an OutOfMemoryError it can
// catch once what it keeps reaches the budget, and the arrays it then drops are
// collected, so the same loop run again reaches the same count instead of
// failing at once.
func TestHeapBudgetRefusesAGuestThatKeepsAllocating(t *testing.T) {
	vm := newBudgetVM(heapBudgetSource(), testHeapBudget)
	chunk := arrayHeapBytes + 16384*valueHeapBytes
	most := int32(testHeapBudget / chunk)
	first := heapBudgetInt(t, vm, "retainArrays", "()I")
	if first < most-2 || first > most {
		t.Fatalf("retainArrays() kept %d arrays, want %d at most and no fewer than %d", first, most, most-2)
	}
	if again := heapBudgetInt(t, vm, "retainArrays", "()I"); again < first-1 {
		t.Fatalf("retainArrays() again kept %d arrays after %d: what was dropped was not collected", again, first)
	}

	// Small objects are counted from samples rather than one by one, so the
	// count is near the budget rather than exactly at it.
	objects := heapBudgetInt(t, vm, "retainObjects", "()I")
	expected := int32(testHeapBudget / objectHeapBytes)
	if objects < expected*4/5 || objects > expected*6/5 {
		t.Fatalf("retainObjects() kept %d objects, want about %d", objects, expected)
	}

	// A StringBuffer's characters are allocations too.
	length := heapBudgetInt(t, vm, "growBuffer", "()I")
	if length < 0 || length > testHeapBudget/2 {
		t.Fatalf("growBuffer() = %d, want refused below %d characters", length, testHeapBudget/2)
	}
	t.Logf("kept %d arrays (at most %d), %d objects (about %d), %d characters", first, most, objects, expected, length)
}

// Garbage is not charged for long: a guest that allocates many times the
// budget and keeps none of it is never refused.
func TestHeapBudgetCollectsGarbageBeforeRefusing(t *testing.T) {
	vm := newBudgetVM(heapBudgetSource(), testHeapBudget)
	const rounds = 200 // about twenty times the budget
	if got := heapBudgetInt(t, vm, "churn", "(I)I", IntValue(rounds)); got != rounds {
		t.Fatalf("churn(%d) = %d", rounds, got)
	}
}

// An array larger than the whole budget is refused before it is made, so the
// host never commits it.
func TestHeapBudgetRefusesAnArrayBeforeMakingIt(t *testing.T) {
	vm := newBudgetVM(heapBudgetSource(), testHeapBudget)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	message := heapBudgetString(t, vm, "refuseOne")
	runtime.ReadMemStats(&after)
	if !strings.Contains(message, "heap budget") {
		t.Fatalf("refuseOne() = %q, want the budget's OutOfMemoryError message", message)
	}
	if grown := after.TotalAlloc - before.TotalAlloc; grown > 64<<20 {
		t.Fatalf("refusing the array allocated %d bytes", grown)
	}
}

// The text of the error a title catches is answered even at the limit, and
// so is a string the Host makes: neither can be refused.
func TestHeapBudgetNeverRefusesHostStringsOrAnExceptionMessage(t *testing.T) {
	vm := newBudgetVM(heapBudgetSource(), testHeapBudget)
	if _, err := vm.makeArray(Type{Kind: TypeInt}, 1<<20, false); err != nil {
		t.Fatalf("an unrefused array past the budget failed: %v", err)
	}
	if charged, limit := vm.HeapUsage(); charged <= limit {
		t.Fatalf("HeapUsage() = %d of %d, want the unrefused array over the limit", charged, limit)
	}
	if text, _ := StringText(vm.NewString("over")); text != "over" {
		t.Fatalf("NewString past the budget = %q", text)
	}
	if message := heapBudgetString(t, vm, "refuseOne"); !strings.Contains(message, "heap budget") {
		t.Fatalf("refuseOne() past the budget = %q", message)
	}
	if _, err := vm.NewArray(Type{Kind: TypeInt}, 1<<20); !vm.IsGuestException(err, "java/lang/OutOfMemoryError") {
		t.Fatalf("NewArray past the budget error = %v, want OutOfMemoryError", err)
	}
}

// A checkpoint restores whatever heap it carries, even one larger than the
// destination's budget: restored objects are not charged, and the budget
// bounds what the guest allocates after the restore.
func TestHeapBudgetDoesNotRefuseARestoredHeap(t *testing.T) {
	source := heapBudgetSource()
	vm := New(source, Options{MaxHeapBytes: 4 * testHeapBudget})
	chunk := arrayHeapBytes + 16384*valueHeapBytes
	keep := int32(2 * testHeapBudget / chunk)
	if _, err := vm.InvokeStatic("HeapBudget", "keep", "(I)V", IntValue(keep)); err != nil {
		t.Fatal(err)
	}
	saved, err := vm.CaptureHeapState(nil, HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	restored := newBudgetVM(source, testHeapBudget)
	if _, err := restored.RestoreHeapState(saved, HeapCodec{}); err != nil {
		t.Fatalf("restoring a heap twice the budget: %v", err)
	}
	if got := heapBudgetInt(t, restored, "keptCount", "()I"); got != keep {
		t.Fatalf("keptCount() = %d after restore, want %d", got, keep)
	}
	most := int32(testHeapBudget / chunk)
	if got := heapBudgetInt(t, restored, "retainArrays", "()I"); got < most-2 || got > most {
		t.Fatalf("retainArrays() after restore kept %d arrays, want the full budget's %d", got, most)
	}
}

// A StringBuffer that reuses its storage still reads zeros where setLength
// lengthened it.
func TestStringBufferSetLengthZeroesReusedStorage(t *testing.T) {
	vm := New(heapBudgetSource(), Options{})
	if got := heapBudgetString(t, vm, "refill"); got != "axy\x00\x00" {
		t.Fatalf("refill() = %q, want %q", got, "axy\x00\x00")
	}
}

// A title that catches OutOfMemoryError and goes on allocating keeps only what
// the budget holds: after a refusal every allocation is weighed until one fits,
// so the ones between two samples no longer pass uncharged.
func TestHeapBudgetHoldsAGuestThatGoesOnAfterARefusal(t *testing.T) {
	vm := newBudgetVM(heapBudgetSource(), testHeapBudget)
	const attempts = 60000
	kept := heapBudgetInt(t, vm, "retainThroughRefusals", "(I)I", IntValue(attempts))
	expected := int32(testHeapBudget / objectHeapBytes)
	if kept > expected*5/4 {
		t.Fatalf("retainThroughRefusals(%d) kept %d objects, want about %d", attempts, kept, expected)
	}
	if charged, limit := vm.HeapUsage(); charged > limit+limit/4 {
		t.Fatalf("HeapUsage() = %d of %d after the refusals", charged, limit)
	}
	t.Logf("kept %d objects of %d attempts (about %d fit)", kept, attempts, expected)
}

// The text a String constructor builds is charged with the String: the object
// was weighed at its `new`, before there was any text.
func TestHeapBudgetChargesTheTextAStringConstructorBuilds(t *testing.T) {
	const length = 20000
	for kind, name := range []string{"char[]", "byte[]", "StringBuffer", "byte[] in UTF-8"} {
		vm := newBudgetVM(heapBudgetSource(), testHeapBudget)
		kept := heapBudgetInt(t, vm, "retainCopies", "(II)I", IntValue(int32(kind)), IntValue(length))
		if kept < 0 || kept > testHeapBudget/length {
			t.Errorf("String(%s): kept %d copies of %d characters under a %d-byte budget", name, kept, length, testHeapBudget)
		}
	}
}

// A refusal that no collection could help is made without one, and a guest
// that goes on being refused is collected for only as its refusals double: a
// collection stops every session in the process.
func TestHeapBudgetRationsItsCollections(t *testing.T) {
	collections := func() uint32 {
		var stats runtime.MemStats
		runtime.ReadMemStats(&stats)
		return stats.NumGC
	}
	vm := newBudgetVM(heapBudgetSource(), testHeapBudget)
	before := collections()
	for range 30 {
		if message := heapBudgetString(t, vm, "refuseOne"); !strings.Contains(message, "heap budget") {
			t.Fatalf("refuseOne() = %q", message)
		}
	}
	if ran := collections() - before; ran > 3 {
		t.Errorf("30 refusals of an array larger than the budget ran %d collections", ran)
	}

	vm = newBudgetVM(heapBudgetSource(), testHeapBudget)
	before = collections()
	refused := heapBudgetInt(t, vm, "refusedWhileFull", "(I)I", IntValue(60))
	ran := collections() - before
	if refused < 30 || ran > 10 {
		t.Errorf("refusedWhileFull(60) was refused %d times and ran %d collections, want at least 30 and at most 10", refused, ran)
	}
	t.Logf("%d refusals while full ran %d collections (%d by the budget)", refused, ran, vm.heap.collections)
}

// An object the host makes for itself — the Long a player event carries from a
// tick to a listener — is charged and never refused, where the same object a
// guest asks for is: a refusal there reached the platform, which ended the
// session.
func TestHeapBudgetNeverRefusesAHostObject(t *testing.T) {
	vm := newBudgetVM(heapBudgetSource(), testHeapBudget)
	held, err := vm.makeArray(Type{Kind: TypeInt}, 1<<20, false)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.KeepAlive(held)
	// Past the limit the guest's next sampled Long is refused, and from
	// there every allocation is weighed.
	refused := false
	for attempt := 0; attempt < 10000 && !refused; attempt++ {
		_, err := vm.NewObject(LongClass, "(J)V", LongValue(5))
		if err != nil && !vm.IsGuestException(err, "java/lang/OutOfMemoryError") {
			t.Fatal(err)
		}
		refused = err != nil
	}
	if !refused {
		t.Fatal("no guest Long was refused past the budget")
	}
	boxed, err := vm.NewHostObject(LongClass, "(J)V", LongValue(5))
	if err != nil {
		t.Fatalf("NewHostObject(Long) past the budget: %v", err)
	}
	if value, err := vm.InvokeVirtual(boxed, "longValue", "()J"); err != nil {
		t.Fatal(err)
	} else if got, _ := value.Int64(); got != 5 {
		t.Fatalf("the host's Long holds %d", got)
	}
}
