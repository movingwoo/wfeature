package jvm

import (
	_ "embed"
	"sync"
	"testing"
	"time"
)

//go:embed testdata/Arithmetic.class
var arithmeticClass []byte

//go:embed testdata/Arithmetic$Operation.class
var arithmeticOperationClass []byte

//go:embed testdata/Arithmetic$AddOperation.class
var arithmeticAddOperationClass []byte

//go:embed testdata/Arithmetic$SetResult.class
var arithmeticSetResultClass []byte

func TestInterpreterRunsJavacClass(t *testing.T) {
	vm := New(arithmeticSource(), Options{})

	assertIntInvocation(t, vm, "sumTwice", "(I)I", []Value{IntValue(10)}, 90)
	assertIntInvocation(t, vm, "denseSwitch", "(I)I", []Value{IntValue(1)}, 10)
	assertIntInvocation(t, vm, "denseSwitch", "(I)I", []Value{IntValue(3)}, 30)
	assertIntInvocation(t, vm, "denseSwitch", "(I)I", []Value{IntValue(99)}, -1)
	assertIntInvocation(t, vm, "sparseSwitch", "(I)I", []Value{IntValue(-100)}, 1)
	assertIntInvocation(t, vm, "sparseSwitch", "(I)I", []Value{IntValue(7)}, 2)
	assertIntInvocation(t, vm, "sparseSwitch", "(I)I", []Value{IntValue(1000)}, 3)
	assertIntInvocation(t, vm, "sparseSwitch", "(I)I", []Value{IntValue(8)}, -1)
	assertIntInvocation(t, vm, "objectMath", "(II)I", []Value{IntValue(40), IntValue(2)}, 42)
	assertIntInvocation(t, vm, "plainObjectMath", "()I", nil, 1)
	if _, err := vm.InvokeStatic("Arithmetic", "startThread", "()V"); err != nil {
		t.Fatalf("startThread() error = %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		result, err := vm.InvokeStatic("Arithmetic", "threadResult", "()I")
		if err != nil {
			t.Fatal(err)
		}
		value, _ := result.Int32()
		if value == 42 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("guest Thread did not run before deadline")
		}
		time.Sleep(time.Millisecond)
	}
	assertIntInvocation(t, vm, "arraySum", "(I)I", []Value{IntValue(5)}, 15)
	assertIntInvocation(t, vm, "nextSeed", "()I", nil, 21)
	assertIntInvocation(t, vm, "nextSeed", "()I", nil, 22)
	assertLongInvocation(t, vm, "nextLongSeed", "()J", nil, 5)
	assertLongInvocation(t, vm, "nextLongSeed", "()J", nil, 6)
	assertIntInvocation(t, vm, "safeDivide", "(II)I", []Value{IntValue(12), IntValue(3)}, 4)
	assertIntInvocation(t, vm, "safeDivide", "(II)I", []Value{IntValue(12), IntValue(0)}, -1)
	assertIntInvocation(t, vm, "typeMath", "(I)I", []Value{IntValue(42)}, 42)
	assertIntInvocation(t, vm, "matrixMath", "()I", nil, 47)
	assertIntInvocation(t, vm, "synchronizedBlock", "(I)I", []Value{IntValue(41)}, 42)
	assertIntInvocation(t, vm, "nativeMath", "(I)I", []Value{IntValue(-42)}, 42)
	assertIntInvocation(t, vm, "copyMath", "()I", nil, 9)
	assertIntInvocation(t, vm, "stringMath", "()I", nil, 3)
	assertIntInvocation(t, vm, "commonLibraryMath", "()I", nil, 1)
	assertIntInvocation(t, vm, "interfaceMath", "(I)I", []Value{IntValue(41)}, 42)
	assertSynchronizedInvocations(t, vm)

	clockVM := New(arithmeticSource(), Options{Clock: func() int64 { return 123456789 }})
	assertLongInvocation(t, clockVM, "clock", "()J", nil, 123456789)

	result, err := vm.InvokeStatic("Arithmetic", "longMath", "(JJ)J", LongValue(4), LongValue(5))
	if err != nil {
		t.Fatalf("longMath() error = %v", err)
	}
	value, err := result.Int64()
	if err != nil {
		t.Fatal(err)
	}
	if value != 18 {
		t.Fatalf("longMath() = %d, want 18", value)
	}
}

func arithmeticSource() mapClassSource {
	return mapClassSource{
		"Arithmetic":              arithmeticClass,
		"Arithmetic$Operation":    arithmeticOperationClass,
		"Arithmetic$AddOperation": arithmeticAddOperationClass,
		"Arithmetic$SetResult":    arithmeticSetResultClass,
	}
}

func assertSynchronizedInvocations(t *testing.T, vm *VM) {
	t.Helper()
	const workers = 32
	results := make(chan int32, workers)
	errors := make(chan error, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := vm.InvokeStatic("Arithmetic", "syncIncrement", "()I")
			if err != nil {
				errors <- err
				return
			}
			value, err := result.Int32()
			if err != nil {
				errors <- err
				return
			}
			results <- value
		}()
	}
	wait.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Fatalf("syncIncrement() error = %v", err)
	}
	seen := make([]bool, workers+1)
	for result := range results {
		if result < 1 || result > workers {
			t.Fatalf("syncIncrement() result = %d", result)
		}
		if seen[result] {
			t.Fatalf("syncIncrement() returned duplicate %d", result)
		}
		seen[result] = true
	}
	for value := 1; value <= workers; value++ {
		if !seen[value] {
			t.Fatalf("syncIncrement() never returned %d", value)
		}
	}
}

func assertIntInvocation(t *testing.T, vm *VM, method, descriptor string, arguments []Value, want int32) {
	t.Helper()
	result, err := vm.InvokeStatic("Arithmetic", method, descriptor, arguments...)
	if err != nil {
		t.Fatalf("%s() error = %v", method, err)
	}
	value, err := result.Int32()
	if err != nil {
		t.Fatal(err)
	}
	if value != want {
		t.Fatalf("%s() = %d, want %d", method, value, want)
	}
}

func assertLongInvocation(t *testing.T, vm *VM, method, descriptor string, arguments []Value, want int64) {
	t.Helper()
	result, err := vm.InvokeStatic("Arithmetic", method, descriptor, arguments...)
	if err != nil {
		t.Fatalf("%s() error = %v", method, err)
	}
	value, err := result.Int64()
	if err != nil {
		t.Fatal(err)
	}
	if value != want {
		t.Fatalf("%s() = %d, want %d", method, value, want)
	}
}

// A shipped game is full of leftover debug printing, and a getstatic that
// cannot resolve System.out kills whichever method happened to contain it. The
// output itself goes to the logging boundary; what this pins is that the call
// resolves, runs, and returns.
func TestSystemPrintStreamsResolveAndAcceptOutput(t *testing.T) {
	vm := New(arithmeticSource(), Options{})
	for _, field := range []string{"out", "err"} {
		value, err := vm.StaticField(SystemClass, field, "Ljava/io/PrintStream;")
		if err != nil {
			t.Fatalf("System.%s error = %v", field, err)
		}
		stream, err := value.Reference()
		if err != nil {
			t.Fatal(err)
		}
		if stream == nil || stream.ClassName != PrintStreamClass {
			t.Fatalf("System.%s = %#v, want a PrintStream", field, stream)
		}
		if _, err := vm.InvokeVirtual(stream, "println", "(Ljava/lang/String;)V",
			ReferenceValue(vm.NewString("printed"))); err != nil {
			t.Fatalf("System.%s.println(String) error = %v", field, err)
		}
		if _, err := vm.InvokeVirtual(stream, "println", "(I)V", IntValue(42)); err != nil {
			t.Fatalf("System.%s.println(int) error = %v", field, err)
		}
		if _, err := vm.InvokeVirtual(stream, "print", "(Ljava/lang/Object;)V",
			ReferenceValue(nil)); err != nil {
			t.Fatalf("System.%s.print(null) error = %v", field, err)
		}
	}
}
