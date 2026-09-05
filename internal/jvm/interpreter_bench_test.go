package jvm

import (
	_ "embed"
	"testing"
)

//go:embed testdata/CallLoop.class
var callLoopClass []byte

func callLoopSource() mapClassSource {
	return mapClassSource{"CallLoop": callLoopClass}
}

// callsPerOp is how many guest calls one iteration of the loop benchmarks
// below makes. It is large enough that the Host entry around it — a fresh
// execution, a descriptor parse, a class lookup — is a rounding error, which
// is the whole point: what a title spends is guest calls inside one execution,
// not Host calls into the machine.
const callsPerOp = 1000

// benchmarkCallLoop reports the cost of one *guest* call rather than of the
// Host call around it.
func benchmarkCallLoop(b *testing.B, method string) {
	vm := New(callLoopSource(), Options{MaxSteps: 1 << 40})
	arguments := []Value{IntValue(callsPerOp)}
	if _, err := vm.InvokeStatic("CallLoop", method, "(I)I", arguments...); err != nil {
		b.Fatalf("%s() error = %v", method, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := vm.InvokeStatic("CallLoop", method, "(I)I", arguments...); err != nil {
			b.Fatalf("%s() error = %v", method, err)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/callsPerOp, "ns/call")
}

// BenchmarkGuestCallLoopInstance is a virtual call on one receiver, repeated
// inside a single execution.
func BenchmarkGuestCallLoopInstance(b *testing.B) { benchmarkCallLoop(b, "instanceCalls") }

// BenchmarkGuestCallLoopStatic is the same frame path without a receiver.
func BenchmarkGuestCallLoopStatic(b *testing.B) { benchmarkCallLoop(b, "staticCalls") }

// BenchmarkGuestCallLoopAllocating adds an object per call, which is the shape
// of a title's frame loop rather than of a tight arithmetic one.
func BenchmarkGuestCallLoopAllocating(b *testing.B) { benchmarkCallLoop(b, "allocatingCalls") }

// BenchmarkGuestCallLoopStaticNative and BenchmarkGuestCallLoopInstanceNative
// reach a body this runtime implements in Go. They are the other side of the
// borrowed argument slice: a native is handed a copy, so it pays for one where
// a bytecode callee no longer does.
func BenchmarkGuestCallLoopStaticNative(b *testing.B) { benchmarkCallLoop(b, "staticNativeCalls") }

func BenchmarkGuestCallLoopInstanceNative(b *testing.B) {
	benchmarkCallLoop(b, "instanceNativeCalls")
}

// BenchmarkGuestInstanceCall is the interpreter's method-call path, which is
// what a title spends its time in: `objectMath` makes an object and calls an
// instance method on it, so one iteration is an invokestatic, an invokespecial
// and an invokevirtual through the same code a game reaches.
//
// It exists because the end-to-end runs this platform is measured with are not
// a throughput instrument. A MIDlet's threads pace themselves against the wall
// clock, so a fixed number of Host ticks is a fixed number of *seconds* and a
// faster engine spends them doing more work rather than finishing sooner —
// which makes wall time flat, CPU time ambiguous and allocation totals go *up*
// when the engine improves. Here the work is fixed and the clock is the answer.
//
//	go test -run xxx -bench GuestInstanceCall -benchmem ./internal/jvm
func BenchmarkGuestInstanceCall(b *testing.B) {
	vm := New(arithmeticSource(), Options{})
	arguments := []Value{IntValue(40), IntValue(2)}
	if _, err := vm.InvokeStatic("Arithmetic", "objectMath", "(II)I", arguments...); err != nil {
		b.Fatalf("objectMath() error = %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := vm.InvokeStatic("Arithmetic", "objectMath", "(II)I", arguments...); err != nil {
			b.Fatalf("objectMath() error = %v", err)
		}
	}
}

// BenchmarkGuestInterfaceCall is the same for an invokeinterface, which
// dispatches on the receiver rather than on the named class.
func BenchmarkGuestInterfaceCall(b *testing.B) {
	vm := New(arithmeticSource(), Options{})
	arguments := []Value{IntValue(41)}
	if _, err := vm.InvokeStatic("Arithmetic", "interfaceMath", "(I)I", arguments...); err != nil {
		b.Fatalf("interfaceMath() error = %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := vm.InvokeStatic("Arithmetic", "interfaceMath", "(I)I", arguments...); err != nil {
			b.Fatalf("interfaceMath() error = %v", err)
		}
	}
}

// BenchmarkGuestLoop is a bytecode loop with no calls in it, so a change that
// moves only the call path can be told apart from one that moves everything.
func BenchmarkGuestLoop(b *testing.B) {
	vm := New(arithmeticSource(), Options{})
	arguments := []Value{IntValue(64)}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := vm.InvokeStatic("Arithmetic", "sumTwice", "(I)I", arguments...); err != nil {
			b.Fatalf("sumTwice() error = %v", err)
		}
	}
}
