package armcore

import "testing"

// A stand-in charges the instructions it stood in for, so the step count a
// quantum reports has to be the one the interpreter would have reported for
// the same loop. Anything else moves the guest's own clock, which is derived
// from the count, and makes two builds' throughput incomparable.
func TestAStandInChargesWhatItStoodInFor(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	for name, arm := range map[string]struct {
		body  []uint16
		setup func(*Context)
	}{
		"the borrow ending": {countedFillBody, func(context *Context) {
			context.Registers[RegisterSP] = 0x70000800
			context.Registers[4] = destination
			context.Registers[5] = 40
		}},
		"the zero test": {zeroTestedFillBody, func(context *Context) {
			context.Registers[1] = destination
			context.Registers[3] = 40
			context.Registers[7] = 0xbeef
		}},
		"an indexed store": {indexedFillBody, func(context *Context) {
			context.Registers[0] = 0x1234
			context.Registers[2] = 0x40
			context.Registers[3] = destination
			context.Registers[5] = 33
		}},
	} {
		branchPC := base + uint32(len(arm.body)-1)*2
		run := func(refuse bool) uint32 {
			memory := fillLoopMemory(t, arm.body, base, destination)
			memory.standInsRefused = refuse
			holder := NewContext()
			context := &holder
			arm.setup(context)
			context.setThumbPC(base)
			result, err := (Engine{}).Run(context, memory, branchPC+2, 1000000)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			return result.Steps
		}
		if interpreted, stood := run(true), run(false); interpreted != stood {
			t.Errorf("%s: the stand-in charged %d steps, interpreting it retired %d",
				name, stood, interpreted)
		}
	}
}

// A stand-in charges for every instruction the loop it replaced would have
// retired, and that charge can be many times the quantum it happened inside.
// Answering the quantum's own size on the exhausted path threw the difference
// away, which is not a rounding error: the budget the Host schedules on stops
// matching what the guest has done, and a stand-in charging thirty times the
// quantum moves the guest's own pacing far enough to change what it draws.
func TestAQuantumReportsTheChargeItOverran(t *testing.T) {
	const base, destination = 0x00100000, 0x00200000
	const pixels = 600
	// A fill of six hundred halfwords is six instructions a pixel, so the
	// stand-in charges thousands inside a quantum of ten.
	memory := fillLoopMemory(t, countedFillBody, base, destination)
	memory.beginQuantum()
	if err := memory.writeData16(0x70000000+24, 0x1234); err != nil {
		t.Fatal(err)
	}
	memory.endQuantum()

	context := NewContext()
	context.Registers[RegisterSP] = 0x70000000
	context.Registers[4] = destination
	context.Registers[5] = pixels
	context.setThumbPC(base)

	branchPC := base + uint32(len(countedFillBody)-1)*2
	result, err := (Engine{}).Run(&context, memory, branchPC+2, 10)
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != StopCountExhausted {
		t.Fatalf("the quantum ended as %d, want the exhausted path", result.Reason)
	}
	if result.Steps <= 10 {
		t.Fatalf("the quantum reported %d steps, want the whole charge the stand-in overran it by", result.Steps)
	}
	if result.Steps < pixels {
		t.Errorf("the quantum reported %d steps for a %d-pixel fill", result.Steps, pixels)
	}
}
