package jvm

import "testing"

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
