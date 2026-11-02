package armcore

import (
	"context"
	"errors"
	"testing"
)

// runARMProgram runs ARM code at 0x1000 until it returns to 0x3000 and
// answers the final context. The registers in initial are the program's
// inputs.
func runARMProgram(t *testing.T, initial Context, program ...uint32) (Context, error) {
	t.Helper()
	core := NewCore(CoreOptions{MaxSteps: 1000})
	loadARM(t, core.Memory(), 0x1000, program...)
	if err := initial.SetPC(0x1000); err != nil {
		t.Fatal(err)
	}
	initial.Registers[RegisterLR] = 0x3000
	summary, err := core.Run(context.Background(), NewThread(initial), 0x3000, nil)
	return summary.Context, err
}

// The handsets are ARMv5TE cores, and the miscellaneous encodings that
// architecture adds sit where opcodes 8-11 of data processing would be with S
// clear. They used to run as a flag-setting TST/TEQ/CMP/CMN: no result, no
// branch, and the flags overwritten.
func TestARMv5TEMiscellaneousInstructions(t *testing.T) {
	cases := []struct {
		name        string
		instruction uint32
		inputs      map[int]uint32
		cpsr        uint32
		register    int
		want        uint32
		wantCPSR    uint32
	}{
		{"clz of a middle bit", 0xe16f0f11, map[int]uint32{1: 0x00010000}, 0, 0, 15, 0},
		{"clz of zero", 0xe16f0f11, map[int]uint32{1: 0}, 0, 0, 32, 0},
		{"clz leaves the flags", 0xe16f0f11, map[int]uint32{1: 1}, flagZero | flagCarry, 0, 31, flagZero | flagCarry},
		{"qadd in range", 0xe1020051, map[int]uint32{1: 5, 2: 7}, 0, 0, 12, 0},
		{"qadd saturates high", 0xe1020051, map[int]uint32{1: 0x7fffffff, 2: 1}, 0, 0, 0x7fffffff, flagSaturation},
		{"qsub saturates low", 0xe1220051, map[int]uint32{1: 0x80000000, 2: 1}, 0, 0, 0x80000000, flagSaturation},
		{"qdadd in range", 0xe1420051, map[int]uint32{1: 10, 2: 3}, 0, 0, 16, 0},
		{"qdadd saturates the doubling", 0xe1420051, map[int]uint32{1: 1, 2: 0x40000000}, 0, 0, 0x7fffffff, flagSaturation},
		{"qdsub in range", 0xe1620051, map[int]uint32{1: 10, 2: 3}, 0, 0, 4, 0},
		{"smulbb", 0xe1600281, map[int]uint32{1: 0x0003fffe, 2: 0x00050007}, 0, 0, 0xfffffff2, 0},
		{"smultb", 0xe16002a1, map[int]uint32{1: 0x0003fffe, 2: 0x00050007}, 0, 0, 21, 0},
		{"smulbt", 0xe16002c1, map[int]uint32{1: 0x0003fffe, 2: 0x00050007}, 0, 0, 0xfffffff6, 0},
		{"smultt", 0xe16002e1, map[int]uint32{1: 0x0003fffe, 2: 0x00050007}, 0, 0, 15, 0},
		{"smlabb", 0xe1003281, map[int]uint32{1: 0x0003fffe, 2: 0x00050007, 3: 100}, 0, 0, 86, 0},
		{"smlabb overflows into Q", 0xe1003281, map[int]uint32{1: 0x8000, 2: 0x8000, 3: 0x40000000}, 0, 0, 0x80000000, flagSaturation},
		{"smlawb", 0xe1203281, map[int]uint32{1: 0x00010000, 2: 7, 3: 100}, 0, 0, 107, 0},
		{"smlawb negative", 0xe1203281, map[int]uint32{1: 0xffff0000, 2: 7, 3: 100}, 0, 0, 93, 0},
		{"smulwb", 0xe12002a1, map[int]uint32{1: 0x00030000, 2: 0xfffe}, 0, 0, 0xfffffffa, 0},
		{"smlalbb low word", 0xe1454281, map[int]uint32{1: 0xfffe, 2: 7, 4: 0xffffffff, 5: 0}, 0, 4, 0xfffffff1, 0},
		{"smlalbb high word", 0xe1454281, map[int]uint32{1: 0xfffe, 2: 7, 4: 0xffffffff, 5: 0}, 0, 5, 0, 0},
		{"a failed condition does nothing", 0x016f0f11, map[int]uint32{0: 0x55, 1: 0x10000}, 0, 0, 0x55, 0},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			initial := NewContext()
			initial.CPSR |= test.cpsr
			for register, value := range test.inputs {
				initial.Registers[register] = value
			}
			final, err := runARMProgram(t, initial, test.instruction, 0xe12fff1e) // bx lr
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if got := final.Registers[test.register]; got != test.want {
				t.Errorf("r%d = %#x, want %#x", test.register, got, test.want)
			}
			const flags = flagNegative | flagZero | flagCarry | flagOverflow | flagSaturation
			if got := final.CPSR & flags; got != test.wantCPSR {
				t.Errorf("flags = %#x, want %#x", got, test.wantCPSR)
			}
		})
	}
}

// BLX register is the ARM-state indirect call. It used to fall through to the
// next instruction without calling and without setting LR.
func TestARMBLXRegisterCallsAndReturns(t *testing.T) {
	core := NewCore(CoreOptions{MaxSteps: 100})
	loadARM(t, core.Memory(), 0x1000,
		0xe3a00000, // mov r0, #0
		0xe12fff32, // blx r2
		0xe2800001, // add r0, r0, #1
		0xe12fff17, // bx r7
	)
	loadThumb(t, core.Memory(), 0x2000,
		0x202a, // movs r0, #42
		0x4770, // bx lr
	)
	initial := NewContext()
	if err := initial.SetPC(0x1000); err != nil {
		t.Fatal(err)
	}
	initial.Registers[2] = 0x2001
	initial.Registers[7] = 0x3000
	summary, err := core.Run(context.Background(), NewThread(initial), 0x3000, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := summary.Context.Registers[0]; got != 43 {
		t.Fatalf("r0 = %d, want 43 from the Thumb callee and the add after the call", got)
	}
	if got := summary.Context.Registers[RegisterLR]; got != 0x1008 {
		t.Fatalf("lr = %#x, want the ARM instruction after the call", got)
	}
}

// What ARMv5TE does not define in that space has to fault, as anything else
// the core does not implement does, rather than run as a compare.
func TestARMUndefinedMiscellaneousEncodingsFault(t *testing.T) {
	for _, test := range []struct {
		name        string
		instruction uint32
	}{
		{"bkpt", 0xe1200070},
		{"an unallocated opcode-8 register form", 0xe1000010},
		{"movw, which is ARMv6T2", 0xe3010234},
		{"movt, which is ARMv6T2", 0xe3410234},
	} {
		t.Run(test.name, func(t *testing.T) {
			initial := NewContext()
			_, err := runARMProgram(t, initial, test.instruction, 0xe12fff1e)
			if !errors.Is(err, ErrUndefinedInstruction) {
				t.Fatalf("Run() error = %v, want ErrUndefinedInstruction", err)
			}
		})
	}
}

// Q is part of the flags field on ARMv5TE, so MSR has to be able to clear it
// after a saturating instruction set it.
func TestARMMSRWritesTheSaturationFlag(t *testing.T) {
	initial := NewContext()
	initial.Registers[1] = 0x7fffffff
	initial.Registers[2] = 1
	final, err := runARMProgram(t, initial,
		0xe1020051, // qadd r0, r1, r2 — sets Q
		0xe10f4000, // mrs r4, cpsr
		0xe3a03000, // mov r3, #0
		0xe128f003, // msr cpsr_f, r3 — clears every flag, Q included
		0xe10f5000, // mrs r5, cpsr
		0xe12fff1e, // bx lr
	)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if final.Registers[4]&flagSaturation == 0 {
		t.Fatalf("cpsr after qadd = %#x, want Q set", final.Registers[4])
	}
	if final.Registers[5]&flagSaturation != 0 {
		t.Fatalf("cpsr after msr = %#x, want Q cleared", final.Registers[5])
	}
}

// ARMv5T loads to the PC interwork: bit zero of the loaded word selects Thumb.
// That is how an ARM function entered from Thumb through BLX returns, and the
// Thumb POP {pc} already behaved that way.
func TestARMLoadsToPCInterwork(t *testing.T) {
	for _, test := range []struct {
		name        string
		instruction uint32
	}{
		{"ldr pc, [r0]", 0xe590f000},
		{"ldmia r0, {pc}", 0xe8908000},
		{"ldmfd sp!, {r4, pc}", 0xe8bd8010},
	} {
		t.Run(test.name, func(t *testing.T) {
			core := NewCore(CoreOptions{MaxSteps: 100})
			loadARM(t, core.Memory(), 0x1000, test.instruction)
			loadThumb(t, core.Memory(), 0x2000,
				0x202a, // movs r0, #42
				0x4738, // bx r7
			)
			if err := core.Memory().Map(0x4000, 0x1000, PermissionReadWrite); err != nil {
				t.Fatal(err)
			}
			// The Thumb return address in both places a load could read it.
			core.Memory().beginQuantum()
			for _, address := range []uint32{0x4000, 0x4100, 0x4104} {
				if err := core.Memory().writeData32(address, 0x2001); err != nil {
					t.Fatal(err)
				}
			}
			core.Memory().endQuantum()
			initial := NewContext()
			if err := initial.SetPC(0x1000); err != nil {
				t.Fatal(err)
			}
			initial.Registers[0] = 0x4000
			initial.Registers[RegisterSP] = 0x4100
			initial.Registers[7] = 0x3000
			summary, err := core.Run(context.Background(), NewThread(initial), 0x3000, nil)
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if got := summary.Context.Registers[0]; got != 42 {
				t.Fatalf("r0 = %d, want 42 from the Thumb code the load returned to", got)
			}
		})
	}
}

// A word-aligned load to the PC still stays in ARM state.
func TestARMLoadToPCStaysInARMForAnAlignedAddress(t *testing.T) {
	core := NewCore(CoreOptions{MaxSteps: 100})
	loadARM(t, core.Memory(), 0x1000, 0xe590f000) // ldr pc, [r0]
	loadARM(t, core.Memory(), 0x2000,
		0xe3a0002a, // mov r0, #42
		0xe12fff17, // bx r7
	)
	if err := core.Memory().Map(0x4000, 0x1000, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	core.Memory().beginQuantum()
	if err := core.Memory().writeData32(0x4000, 0x2000); err != nil {
		t.Fatal(err)
	}
	core.Memory().endQuantum()
	initial := NewContext()
	if err := initial.SetPC(0x1000); err != nil {
		t.Fatal(err)
	}
	initial.Registers[0] = 0x4000
	initial.Registers[7] = 0x3000
	summary, err := core.Run(context.Background(), NewThread(initial), 0x3000, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := summary.Context.Registers[0]; got != 42 {
		t.Fatalf("r0 = %d, want 42 from the ARM code the load branched to", got)
	}
}
