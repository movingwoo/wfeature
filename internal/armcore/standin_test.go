package armcore

import "testing"

// compareStandIn runs the same Thumb loop twice from the same start, once with
// the recognisers refused and once allowed, and fails on the first register,
// flag or byte of [from, from+length) that differs, and on a different count
// of charged instructions. build lays out a fresh memory and the context the
// loop starts in.
func compareStandIn(t *testing.T, end uint32, from, length uint32, build func() (*Memory, *Context)) {
	t.Helper()
	interpreted, reference := build()
	stood, subject := build()
	interpreted.standInsRefused = true
	control, err := (Engine{}).Run(reference, interpreted, end, 10_000_000)
	if err != nil {
		t.Fatalf("interpreting the loop: %v", err)
	}
	result, err := (Engine{}).Run(subject, stood, end, 10_000_000)
	if err != nil {
		t.Fatalf("standing in for the loop: %v", err)
	}
	// A stand-in charges what it stood in for, so a budget runs out where it
	// would have.
	if control.Steps != result.Steps {
		t.Errorf("charged %d steps, interpreting the same loop retired %d", result.Steps, control.Steps)
	}
	for register := 0; register < 16; register++ {
		if reference.Registers[register] != subject.Registers[register] {
			t.Errorf("r%d = %#x after standing in, %#x after interpreting",
				register, subject.Registers[register], reference.Registers[register])
		}
	}
	if reference.CPSR != subject.CPSR {
		t.Errorf("CPSR = %#x after standing in, %#x after interpreting", subject.CPSR, reference.CPSR)
	}
	interpreted.beginQuantum()
	stood.beginQuantum()
	defer interpreted.endQuantum()
	defer stood.endQuantum()
	for offset := uint32(0); offset < length; offset++ {
		want, err := interpreted.read8(from + offset)
		if err != nil {
			t.Fatal(err)
		}
		got, err := stood.read8(from + offset)
		if err != nil {
			t.Fatal(err)
		}
		if want != got {
			t.Fatalf("byte at %#x is %#x after standing in, %#x after interpreting", from+offset, got, want)
		}
	}
}
