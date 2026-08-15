package armcore

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"
)

func TestCoreRunsARMFunctionAcrossBoundedQuanta(t *testing.T) {
	core := NewCore(CoreOptions{Quantum: 3, MaxSteps: 100})
	loadARM(t, core.Memory(), 0x1000,
		0xe3a00006, // mov r0, #6
		0xe3a01001, // mov r1, #1
		0xe0811001, // loop: add r1, r1, r1
		0xe2500001, // subs r0, r0, #1
		0x1afffffc, // bne loop
		0xe1a00001, // mov r0, r1
		0xe12fff1e, // bx lr
	)

	initial := NewContext()
	if err := initial.SetPC(0x1000); err != nil {
		t.Fatal(err)
	}
	initial.Registers[RegisterLR] = 0x2000
	thread := NewThread(initial)
	summary, err := core.Run(context.Background(), thread, 0x2000, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := summary.Context.Registers[0]; got != 64 {
		t.Fatalf("r0 = %d, want 64", got)
	}
	if summary.Steps != 22 {
		t.Fatalf("steps = %d, want 22", summary.Steps)
	}
	if core.Steps() != summary.Steps {
		t.Fatalf("core steps = %d, want %d", core.Steps(), summary.Steps)
	}
	if thread.State() != ThreadHalted {
		t.Fatalf("thread state = %s, want halted", thread.State())
	}
}

func TestCoreExecutesARMGuestMemoryTransfers(t *testing.T) {
	core := NewCore(CoreOptions{MaxSteps: 20})
	loadARM(t, core.Memory(), 0x1000,
		0xe3a00a02, // mov r0, #0x2000
		0xe3a0102a, // mov r1, #42
		0xe5801000, // str r1, [r0]
		0xe5902000, // ldr r2, [r0]
		0xe1a00002, // mov r0, r2
		0xe12fff1e, // bx lr
	)
	if err := core.Memory().Map(0x2000, 4, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	initial := NewContext()
	if err := initial.SetPC(0x1000); err != nil {
		t.Fatal(err)
	}
	initial.Registers[RegisterLR] = 0x3000
	summary, err := core.Run(context.Background(), NewThread(initial), 0x3000, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if summary.Context.Registers[0] != 42 {
		t.Fatalf("r0 = %d, want 42", summary.Context.Registers[0])
	}
	var stored [4]byte
	if err := core.Memory().Read(0x2000, stored[:]); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(stored[:]); got != 42 {
		t.Fatalf("stored word = %d, want 42", got)
	}
}

func TestCoreExecutesARMHalfwordAndSignedTransfers(t *testing.T) {
	core := NewCore(CoreOptions{MaxSteps: 20})
	loadARM(t, core.Memory(), 0x1000,
		0xe12010b5, // strh r1, [r0, -r5]!
		0xe09020b5, // ldrh r2, [r0], r5
		0xe15030d3, // ldrsb r3, [r0, #-3]
		0xe11040f5, // ldrsh r4, [r0, -r5]
		0xe12fff1e, // bx lr
	)
	if err := core.Memory().Map(0x2000, 4, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}

	initial := NewContext()
	if err := initial.SetPC(0x1000); err != nil {
		t.Fatal(err)
	}
	initial.Registers[0] = 0x2003
	initial.Registers[1] = 0xff80
	initial.Registers[5] = 2
	initial.Registers[RegisterLR] = 0x3000
	summary, err := core.Run(context.Background(), NewThread(initial), 0x3000, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if summary.Steps != 5 {
		t.Fatalf("steps = %d, want 5", summary.Steps)
	}
	if got := summary.Context.Registers[0]; got != 0x2003 {
		t.Fatalf("r0 writeback = %#x, want 0x2003", got)
	}
	if got := summary.Context.Registers[2]; got != 0xff80 {
		t.Fatalf("LDRH result = %#x, want 0xff80", got)
	}
	if got := summary.Context.Registers[3]; got != 0xffffff80 {
		t.Fatalf("LDRSB result = %#x, want 0xffffff80", got)
	}
	if got := summary.Context.Registers[4]; got != 0xffffff80 {
		t.Fatalf("LDRSH result = %#x, want 0xffffff80", got)
	}
	var stored [2]byte
	if err := core.Memory().Read(0x2000, stored[:]); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint16(stored[:]); got != 0xff80 {
		t.Fatalf("stored halfword = %#x, want 0xff80", got)
	}
}

func TestCoreAppliesARMv4UnalignedWordAccess(t *testing.T) {
	t.Run("ARM", func(t *testing.T) {
		core := NewCore(CoreOptions{MaxSteps: 10})
		loadARM(t, core.Memory(), 0x1000,
			0xe5901001, // ldr r1, [r0, #1]
			0xe5802003, // str r2, [r0, #3]
			0xe12fff1e, // bx lr
		)
		if err := core.Memory().Map(0x2000, 8, PermissionReadWrite); err != nil {
			t.Fatal(err)
		}
		if err := core.Memory().Load(0x2000, []byte{0x44, 0x33, 0x22, 0x11}); err != nil {
			t.Fatal(err)
		}
		initial := NewContext()
		if err := initial.SetPC(0x1000); err != nil {
			t.Fatal(err)
		}
		initial.Registers[0] = 0x2000
		initial.Registers[2] = 0xaabbccdd
		initial.Registers[RegisterLR] = 0x3000
		summary, err := core.Run(context.Background(), NewThread(initial), 0x3000, nil)
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if got := summary.Context.Registers[1]; got != 0x44112233 {
			t.Fatalf("rotated LDR result = %#x, want 0x44112233", got)
		}
		assertMemoryWord(t, core.Memory(), 0x2000, 0xaabbccdd)
	})

	t.Run("Thumb", func(t *testing.T) {
		core := NewCore(CoreOptions{MaxSteps: 10})
		loadThumb(t, core.Memory(), 0x1000,
			0x6801, // ldr r1, [r0, #0]
			0x6002, // str r2, [r0, #0]
			0x4770, // bx lr
		)
		if err := core.Memory().Map(0x2000, 8, PermissionReadWrite); err != nil {
			t.Fatal(err)
		}
		if err := core.Memory().Load(0x2000, []byte{0x44, 0x33, 0x22, 0x11}); err != nil {
			t.Fatal(err)
		}
		initial := NewContext()
		if err := initial.SetPC(0x1001); err != nil {
			t.Fatal(err)
		}
		initial.Registers[0] = 0x2001
		initial.Registers[2] = 0x55667788
		initial.Registers[RegisterLR] = 0x3000
		summary, err := core.Run(context.Background(), NewThread(initial), 0x3000, nil)
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if got := summary.Context.Registers[1]; got != 0x44112233 {
			t.Fatalf("rotated LDR result = %#x, want 0x44112233", got)
		}
		assertMemoryWord(t, core.Memory(), 0x2000, 0x55667788)
	})
}

func TestCoreRejectsARMSignedHalfwordStore(t *testing.T) {
	memory := NewMemory()
	loadARM(t, memory, 0x1000, 0xe1c010d0) // signed stores are undefined
	context := NewContext()
	if err := context.SetPC(0x1000); err != nil {
		t.Fatal(err)
	}
	_, err := (Engine{}).Run(&context, memory, 0x2000, 1)
	if !errors.Is(err, ErrUndefinedInstruction) {
		t.Fatalf("Run() error = %v, want ErrUndefinedInstruction", err)
	}
}

func TestCoreExecutesARMSwap(t *testing.T) {
	core := NewCore(CoreOptions{MaxSteps: 10})
	loadARM(t, core.Memory(), 0x1000,
		0xe1002091, // swp r2, r1, [r0]
		0xe1453094, // swpb r3, r4, [r5]
		0xe12fff1e, // bx lr
	)
	if err := core.Memory().Map(0x2000, 8, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	if err := core.Memory().Load(0x2000, []byte{0x44, 0x33, 0x22, 0x11, 0, 0x7f}); err != nil {
		t.Fatal(err)
	}
	initial := NewContext()
	if err := initial.SetPC(0x1000); err != nil {
		t.Fatal(err)
	}
	initial.Registers[0] = 0x2001
	initial.Registers[1] = 0xaabbccdd
	initial.Registers[4] = 0x80
	initial.Registers[5] = 0x2005
	initial.Registers[RegisterLR] = 0x3000
	summary, err := core.Run(context.Background(), NewThread(initial), 0x3000, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := summary.Context.Registers[2]; got != 0x11223344 {
		t.Fatalf("SWP result = %#x, want 0x11223344", got)
	}
	if got := summary.Context.Registers[3]; got != 0x7f {
		t.Fatalf("SWPB result = %#x, want 0x7f", got)
	}
	assertMemoryWord(t, core.Memory(), 0x2000, 0xaabbccdd)
	var swappedByte [1]byte
	if err := core.Memory().Read(0x2005, swappedByte[:]); err != nil {
		t.Fatal(err)
	}
	if swappedByte[0] != 0x80 {
		t.Fatalf("swapped byte = %#x, want 0x80", swappedByte[0])
	}
}

func TestCoreExecutesARMLongMultiplyVariants(t *testing.T) {
	tests := []struct {
		name         string
		instruction  uint32
		r0           uint32
		r1           uint32
		r2           uint32
		r3           uint32
		wantLow      uint32
		wantHigh     uint32
		wantNegative bool
	}{
		{name: "UMULL", instruction: 0xe0832190, r0: 0xffffffff, r1: 2, wantLow: 0xfffffffe, wantHigh: 1},
		{name: "UMLAL", instruction: 0xe0a32190, r0: 3, r1: 4, r2: 5, r3: 1, wantLow: 17, wantHigh: 1},
		{name: "SMULL", instruction: 0xe0c32190, r0: 0xfffffffe, r1: 3, wantLow: 0xfffffffa, wantHigh: 0xffffffff},
		{name: "SMLALS", instruction: 0xe0f32190, r0: 0xfffffffe, r1: 3, r2: 0xfffffff0, r3: 0xffffffff, wantLow: 0xffffffea, wantHigh: 0xffffffff, wantNegative: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			core := NewCore(CoreOptions{MaxSteps: 10})
			loadARM(t, core.Memory(), 0x1000, test.instruction, 0xe12fff1e)
			initial := NewContext()
			if err := initial.SetPC(0x1000); err != nil {
				t.Fatal(err)
			}
			initial.Registers[0] = test.r0
			initial.Registers[1] = test.r1
			initial.Registers[2] = test.r2
			initial.Registers[3] = test.r3
			initial.Registers[RegisterLR] = 0x3000
			summary, err := core.Run(context.Background(), NewThread(initial), 0x3000, nil)
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			if got := summary.Context.Registers[2]; got != test.wantLow {
				t.Fatalf("low result = %#x, want %#x", got, test.wantLow)
			}
			if got := summary.Context.Registers[3]; got != test.wantHigh {
				t.Fatalf("high result = %#x, want %#x", got, test.wantHigh)
			}
			if got := summary.Context.CPSR&flagNegative != 0; got != test.wantNegative {
				t.Fatalf("negative flag = %t, want %t", got, test.wantNegative)
			}
		})
	}
}

func TestCoreExecutesARMProgramStatusTransfers(t *testing.T) {
	core := NewCore(CoreOptions{MaxSteps: 10})
	loadARM(t, core.Memory(), 0x1000,
		0xe10f1000, // mrs r1, cpsr
		0xe128f002, // msr cpsr_f, r2
		0xe10f3000, // mrs r3, cpsr
		0xe328f480, // msr cpsr_f, #0x80000000
		0xe10f4000, // mrs r4, cpsr
		0xe12fff1e, // bx lr
	)
	initial := NewContext()
	if err := initial.SetPC(0x1000); err != nil {
		t.Fatal(err)
	}
	initial.CPSR |= flagCarry
	initial.Registers[2] = flagZero | flagOverflow
	initial.Registers[RegisterLR] = 0x3000
	summary, err := core.Run(context.Background(), NewThread(initial), 0x3000, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := summary.Context.Registers[1]; got != modeUser|flagCarry {
		t.Fatalf("initial MRS result = %#x, want %#x", got, modeUser|flagCarry)
	}
	if got := summary.Context.Registers[3]; got != modeUser|flagZero|flagOverflow {
		t.Fatalf("register MSR result = %#x, want %#x", got, modeUser|flagZero|flagOverflow)
	}
	if got := summary.Context.Registers[4]; got != modeUser|flagNegative {
		t.Fatalf("immediate MSR result = %#x, want %#x", got, modeUser|flagNegative)
	}
}

func TestCorePreservesARM7MultipleTransferOrdering(t *testing.T) {
	t.Run("ARM store sees writeback and pipeline PC", func(t *testing.T) {
		core := NewCore(CoreOptions{MaxSteps: 10})
		loadARM(t, core.Memory(), 0x1000,
			0xe8a18003, // stmia r1!, {r0, r1, pc}
			0xe12fff1e, // bx lr
		)
		if err := core.Memory().Map(0x2000, 12, PermissionReadWrite); err != nil {
			t.Fatal(err)
		}
		initial := NewContext()
		if err := initial.SetPC(0x1000); err != nil {
			t.Fatal(err)
		}
		initial.Registers[0] = 0xaabbccdd
		initial.Registers[1] = 0x2000
		initial.Registers[RegisterLR] = 0x3000
		summary, err := core.Run(context.Background(), NewThread(initial), 0x3000, nil)
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if got := summary.Context.Registers[1]; got != 0x200c {
			t.Fatalf("r1 writeback = %#x, want 0x200c", got)
		}
		assertMemoryWord(t, core.Memory(), 0x2000, 0xaabbccdd)
		assertMemoryWord(t, core.Memory(), 0x2004, 0x200c)
		assertMemoryWord(t, core.Memory(), 0x2008, 0x100c)
	})

	t.Run("ARM load of base replaces writeback", func(t *testing.T) {
		core := NewCore(CoreOptions{MaxSteps: 10})
		loadARM(t, core.Memory(), 0x1000,
			0xe8b00003, // ldmia r0!, {r0, r1}
			0xe12fff1e, // bx lr
		)
		if err := core.Memory().Map(0x2000, 8, PermissionReadWrite); err != nil {
			t.Fatal(err)
		}
		if err := core.Memory().Load(0x2000, []byte{0x11, 0x11, 0, 0, 0x22, 0x22, 0, 0}); err != nil {
			t.Fatal(err)
		}
		initial := NewContext()
		if err := initial.SetPC(0x1000); err != nil {
			t.Fatal(err)
		}
		initial.Registers[0] = 0x2000
		initial.Registers[RegisterLR] = 0x3000
		summary, err := core.Run(context.Background(), NewThread(initial), 0x3000, nil)
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if got := summary.Context.Registers[0]; got != 0x1111 {
			t.Fatalf("loaded r0 = %#x, want 0x1111", got)
		}
		if got := summary.Context.Registers[1]; got != 0x2222 {
			t.Fatalf("loaded r1 = %#x, want 0x2222", got)
		}
	})

	t.Run("Thumb store sees writeback", func(t *testing.T) {
		core := NewCore(CoreOptions{MaxSteps: 10})
		loadThumb(t, core.Memory(), 0x1000,
			0xc103, // stmia r1!, {r0, r1}
			0x4770, // bx lr
		)
		if err := core.Memory().Map(0x2000, 8, PermissionReadWrite); err != nil {
			t.Fatal(err)
		}
		initial := NewContext()
		if err := initial.SetPC(0x1001); err != nil {
			t.Fatal(err)
		}
		initial.Registers[0] = 0x55667788
		initial.Registers[1] = 0x2000
		initial.Registers[RegisterLR] = 0x3000
		summary, err := core.Run(context.Background(), NewThread(initial), 0x3000, nil)
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if got := summary.Context.Registers[1]; got != 0x2008 {
			t.Fatalf("r1 writeback = %#x, want 0x2008", got)
		}
		assertMemoryWord(t, core.Memory(), 0x2000, 0x55667788)
		assertMemoryWord(t, core.Memory(), 0x2004, 0x2008)
	})

	t.Run("Thumb load of base replaces writeback", func(t *testing.T) {
		core := NewCore(CoreOptions{MaxSteps: 10})
		loadThumb(t, core.Memory(), 0x1000,
			0xc803, // ldmia r0!, {r0, r1}
			0x4770, // bx lr
		)
		if err := core.Memory().Map(0x2000, 8, PermissionReadWrite); err != nil {
			t.Fatal(err)
		}
		if err := core.Memory().Load(0x2000, []byte{0x33, 0x33, 0, 0, 0x44, 0x44, 0, 0}); err != nil {
			t.Fatal(err)
		}
		initial := NewContext()
		if err := initial.SetPC(0x1001); err != nil {
			t.Fatal(err)
		}
		initial.Registers[0] = 0x2000
		initial.Registers[RegisterLR] = 0x3000
		summary, err := core.Run(context.Background(), NewThread(initial), 0x3000, nil)
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if got := summary.Context.Registers[0]; got != 0x3333 {
			t.Fatalf("loaded r0 = %#x, want 0x3333", got)
		}
		if got := summary.Context.Registers[1]; got != 0x4444 {
			t.Fatalf("loaded r1 = %#x, want 0x4444", got)
		}
	})
}

func TestCoreDispatchesOriginalStyleThumbSVCStub(t *testing.T) {
	core := NewCore(CoreOptions{Quantum: 2, MaxSteps: 100})
	loadThumb(t, core.Memory(), 0x1000,
		0xb410, // push {r4}
		0x4c02, // ldr r4, [pc, #8]
		0x46a4, // mov r12, r4
		0xbc10, // pop {r4}
		0xdf01, // svc #1
		0x4770, // bx lr
		0x5678, // stub id, little-endian low half
		0x1234, // stub id, little-endian high half
	)
	if err := core.Memory().Map(0x2000, 0x1000, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}

	initial := NewContext()
	if err := initial.SetPC(0x1001); err != nil {
		t.Fatal(err)
	}
	initial.Registers[4] = 0xaabbccdd
	initial.Registers[RegisterSP] = 0x3000
	initial.Registers[RegisterLR] = 0x4000
	thread := NewThread(initial)

	var calls int
	summary, err := core.Run(context.Background(), thread, 0x4000, func(_ context.Context, suspended *Thread, call SupervisorCall) error {
		calls++
		if call.Immediate != 1 || call.Address != 0x1008 || call.ResumePC != 0x100a {
			t.Fatalf("supervisor call = %+v", call)
		}
		if suspended.State() != ThreadSuspended {
			t.Fatalf("handler thread state = %s, want suspended", suspended.State())
		}
		context := suspended.Context()
		if context.Registers[12] != 0x12345678 {
			t.Fatalf("r12 = %#x, want stub id", context.Registers[12])
		}
		if context.Registers[4] != 0xaabbccdd || context.Registers[RegisterSP] != 0x3000 {
			t.Fatalf("stub did not restore r4/sp: r4=%#x sp=%#x", context.Registers[4], context.Registers[RegisterSP])
		}
		return suspended.SetRegister(0, 42)
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if calls != 1 || summary.Context.Registers[0] != 42 {
		t.Fatalf("calls=%d r0=%d, want 1 and 42", calls, summary.Context.Registers[0])
	}
}

func TestSuspendedThreadReleasesCoreAndKeepsItsContext(t *testing.T) {
	core := NewCore(CoreOptions{Quantum: 1, MaxSteps: 20})
	loadThumb(t, core.Memory(), 0x1000,
		0xdf07, // svc #7
		0x3001, // adds r0, #1
		0x4770, // bx lr
	)

	type arrival struct {
		thread *Thread
		resume chan struct{}
	}
	arrivals := make(chan arrival, 2)
	results := make(chan RunSummary, 2)
	errors := make(chan error, 2)
	start := func() (*Thread, chan struct{}) {
		initial := NewContext()
		if err := initial.SetPC(0x1001); err != nil {
			t.Fatal(err)
		}
		initial.Registers[RegisterLR] = 0x2000
		thread := NewThread(initial)
		resume := make(chan struct{})
		go func() {
			summary, err := core.Run(context.Background(), thread, 0x2000, func(_ context.Context, suspended *Thread, _ SupervisorCall) error {
				arrivals <- arrival{thread: suspended, resume: resume}
				<-resume
				return nil
			})
			results <- summary
			errors <- err
		}()
		return thread, resume
	}

	first, firstResume := start()
	second, secondResume := start()
	for range 2 {
		select {
		case <-arrivals:
		case <-time.After(2 * time.Second):
			t.Fatal("both guest threads did not reach the host call")
		}
	}
	if err := first.SetRegister(0, 10); err != nil {
		t.Fatal(err)
	}
	if err := second.SetRegister(0, 20); err != nil {
		t.Fatal(err)
	}
	close(firstResume)
	close(secondResume)

	values := make(map[uint32]bool)
	for range 2 {
		if err := <-errors; err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		values[(<-results).Context.Registers[0]] = true
	}
	if !values[11] || !values[21] {
		t.Fatalf("thread results = %v, want independent 11 and 21", values)
	}
}

func TestGuestThreadLocalWordsRemainIsolatedAcrossSuspension(t *testing.T) {
	core := NewCore(CoreOptions{Quantum: 1, MaxSteps: 20})
	loadThumb(t, core.Memory(), 0x1000,
		0x6001, // str r1, [r0]
		0xdf01, // svc #1
		0x6802, // ldr r2, [r0]
		0x4770, // bx lr
	)
	const wordAddress = uint32(0x2000)
	if err := core.Memory().Map(wordAddress, 4, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	if err := core.RegisterThreadLocalWord(wordAddress); err != nil {
		t.Fatal(err)
	}

	type arrival struct {
		thread *Thread
		value  uint32
		resume chan struct{}
	}
	arrivals := make(chan arrival, 2)
	results := make(chan RunSummary, 2)
	errors := make(chan error, 2)
	start := func(value uint32) {
		initial := NewContext()
		if err := initial.SetPC(0x1001); err != nil {
			t.Fatal(err)
		}
		initial.Registers[0] = wordAddress
		initial.Registers[1] = value
		initial.Registers[RegisterLR] = 0x3000
		thread := NewThread(initial)
		resume := make(chan struct{})
		go func() {
			summary, err := core.Run(context.Background(), thread, 0x3000, func(_ context.Context, suspended *Thread, _ SupervisorCall) error {
				got, err := core.ThreadLocalWord(suspended, wordAddress)
				if err != nil {
					return err
				}
				arrivals <- arrival{thread: suspended, value: got, resume: resume}
				<-resume
				return nil
			})
			results <- summary
			errors <- err
		}()
	}
	start(11)
	start(22)

	seen := make(map[*Thread]uint32)
	var waiting []arrival
	for range 2 {
		select {
		case current := <-arrivals:
			seen[current.thread] = current.value
			waiting = append(waiting, current)
		case <-time.After(2 * time.Second):
			t.Fatal("both thread-local guests did not suspend")
		}
	}
	if len(seen) != 2 {
		t.Fatalf("thread-local arrivals = %d, want 2", len(seen))
	}
	values := make(map[uint32]bool)
	for _, value := range seen {
		values[value] = true
	}
	if !values[11] || !values[22] {
		t.Fatalf("thread-local values = %v, want 11 and 22", values)
	}
	for _, current := range waiting {
		close(current.resume)
	}
	for range 2 {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
		summary := <-results
		if summary.Context.Registers[2] != summary.Context.Registers[1] {
			t.Fatalf("thread reloaded %#x, want its own %#x", summary.Context.Registers[2], summary.Context.Registers[1])
		}
	}
	var shared [4]byte
	if err := core.Memory().Read(wordAddress, shared[:]); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(shared[:]); got != 0 {
		t.Fatalf("shared backing word = %d, want 0", got)
	}
}

func TestSuspendedHandlerCanMakeNestedGuestCall(t *testing.T) {
	core := NewCore(CoreOptions{Quantum: 1, MaxSteps: 50})
	loadThumb(t, core.Memory(), 0x1000,
		0xdf01, // svc #1
		0x3001, // adds r0, #1
		0x4770, // bx lr
	)
	loadThumb(t, core.Memory(), 0x1100,
		0x9c00, // ldr r4, [sp, #0] (fifth argument)
		0x1840, // adds r0, r0, r1
		0x1900, // adds r0, r0, r4
		0x4770, // bx lr
	)
	if err := core.Memory().Map(0x4000, 0x1000, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	initial := NewContext()
	if err := initial.SetPC(0x1001); err != nil {
		t.Fatal(err)
	}
	initial.Registers[RegisterSP] = 0x5000
	initial.Registers[RegisterLR] = 0x3000
	thread := NewThread(initial)

	summary, err := core.Run(context.Background(), thread, 0x3000, func(ctx context.Context, suspended *Thread, _ SupervisorCall) error {
		nested, err := core.Call(ctx, suspended, 0x1101, 0x3000, []uint32{20, 21, 1, 2, 22}, nil)
		if err != nil {
			return err
		}
		if nested.Context.Registers[0] != 63 {
			t.Fatalf("nested r0 = %d, want 63", nested.Context.Registers[0])
		}
		if suspended.Context().Registers[RegisterSP] != 0x5000 {
			t.Fatal("nested call changed the suspended parent stack pointer")
		}
		return suspended.SetRegister(0, nested.Context.Registers[0])
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if summary.Context.Registers[0] != 64 {
		t.Fatalf("outer r0 = %d, want 64", summary.Context.Registers[0])
	}
}

func TestCoreEnforcesInstructionLimit(t *testing.T) {
	core := NewCore(CoreOptions{Quantum: 2, MaxSteps: 5})
	loadARM(t, core.Memory(), 0x1000, 0xeafffffe) // b .
	initial := NewContext()
	if err := initial.SetPC(0x1000); err != nil {
		t.Fatal(err)
	}
	thread := NewThread(initial)
	summary, err := core.Run(context.Background(), thread, 0x2000, nil)
	if !errors.Is(err, ErrStepLimit) {
		t.Fatalf("Run() error = %v, want ErrStepLimit", err)
	}
	if summary.Steps != 5 || thread.State() != ThreadFaulted {
		t.Fatalf("steps=%d state=%s, want 5/faulted", summary.Steps, thread.State())
	}
}

func TestEngineRejectsUndefinedInstructionWithPCDiagnostic(t *testing.T) {
	memory := NewMemory()
	loadARM(t, memory, 0x1000, 0xee000010)
	context := NewContext()
	if err := context.SetPC(0x1000); err != nil {
		t.Fatal(err)
	}
	_, err := (Engine{}).Run(&context, memory, 0x2000, 1)
	if !errors.Is(err, ErrUndefinedInstruction) {
		t.Fatalf("Run() error = %v, want ErrUndefinedInstruction", err)
	}
	var instructionError *InstructionError
	if !errors.As(err, &instructionError) || instructionError.PC != 0x1000 {
		t.Fatalf("Run() error = %v, want instruction PC diagnostic", err)
	}
}

// BLX is the ARMv5T call that changes instruction set, and it is how a
// Thumb-compiled module reaches its own code: LGT modules are built that way,
// so without it every LGT title stops at its first call.
func TestCoreExecutesARMBLXIntoThumbAndBack(t *testing.T) {
	core := NewCore(CoreOptions{MaxSteps: 100})
	loadARM(t, core.Memory(), 0x1000,
		0xe3a00000, // mov r0, #0
		// blx 0x2002 — the H bit is the odd halfword a Thumb target can sit
		// on, which a word-aligned BL cannot express.
		0xfb0003fd,
		0xe2800001, // add r0, r0, #1   (reached only by returning to ARM)
		0xe12fff17, // bx r7
	)
	loadThumb(t, core.Memory(), 0x2000,
		0x0000, // padding: the call targets the halfword after this one
		0x202a, // movs r0, #42
		0x4770, // bx lr
	)

	initial := NewContext()
	if err := initial.SetPC(0x1000); err != nil {
		t.Fatal(err)
	}
	initial.Registers[7] = 0x3000
	summary, err := core.Run(context.Background(), NewThread(initial), 0x3000, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	// 42 from the Thumb routine and 1 from the ARM instruction after the call:
	// anything else means the call or the return took the wrong state.
	if got := summary.Context.Registers[0]; got != 43 {
		t.Fatalf("r0 = %d, want 43", got)
	}
	// The caller was in ARM, so the return address it left has no Thumb bit.
	if got := summary.Context.Registers[RegisterLR]; got != 0x1008 {
		t.Fatalf("lr = %#x, want the instruction after the call", got)
	}
}

// The register form of the same call, which is what an indirect call through a
// function pointer compiles to.
func TestCoreExecutesThumbBLXRegister(t *testing.T) {
	core := NewCore(CoreOptions{MaxSteps: 100})
	loadThumb(t, core.Memory(), 0x1000,
		0x4788, // blx r1
		0x1c40, // adds r0, r0, #1
		0x4738, // bx r7
	)
	loadThumb(t, core.Memory(), 0x2000,
		0x202a, // movs r0, #42
		0x4770, // bx lr
	)

	initial := NewContext()
	if err := initial.SetPC(0x1001); err != nil {
		t.Fatal(err)
	}
	initial.Registers[1] = 0x2001
	initial.Registers[7] = 0x3000
	summary, err := core.Run(context.Background(), NewThread(initial), 0x3000, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := summary.Context.Registers[0]; got != 43 {
		t.Fatalf("r0 = %d, want 43", got)
	}
	// The caller was in Thumb, so the return address carries the Thumb bit —
	// without it the routine returns into ARM and decodes its caller's
	// halfword pairs as one wrong instruction.
	if got := summary.Context.Registers[RegisterLR]; got != 0x1003 {
		t.Fatalf("lr = %#x, want the Thumb address after the call", got)
	}
}

func loadARM(t *testing.T, memory *Memory, address uint32, instructions ...uint32) {
	t.Helper()
	data := make([]byte, len(instructions)*4)
	for index, instruction := range instructions {
		binary.LittleEndian.PutUint32(data[index*4:], instruction)
	}
	if err := memory.Map(address, uint64(len(data)), PermissionReadExecute); err != nil {
		t.Fatal(err)
	}
	if err := memory.Load(address, data); err != nil {
		t.Fatal(err)
	}
}

func loadThumb(t *testing.T, memory *Memory, address uint32, instructions ...uint16) {
	t.Helper()
	data := make([]byte, len(instructions)*2)
	for index, instruction := range instructions {
		binary.LittleEndian.PutUint16(data[index*2:], instruction)
	}
	if err := memory.Map(address, uint64(len(data)), PermissionReadExecute); err != nil {
		t.Fatal(err)
	}
	if err := memory.Load(address, data); err != nil {
		t.Fatal(err)
	}
}

func assertMemoryWord(t *testing.T, memory *Memory, address, want uint32) {
	t.Helper()
	var data [4]byte
	if err := memory.Read(address, data[:]); err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(data[:]); got != want {
		t.Fatalf("memory word at %#x = %#x, want %#x", address, got, want)
	}
}

func TestCoreLimitHookGrantsFreshBudgetWindows(t *testing.T) {
	core := NewCore(CoreOptions{Quantum: 4, MaxSteps: 1000})
	loadARM(t, core.Memory(), 0x1000,
		0xe2800001, // loop: add r0, r0, #1
		0xeafffffd, // b loop
	)
	initial := NewContext()
	if err := initial.SetPC(0x1000); err != nil {
		t.Fatal(err)
	}
	initial.Registers[RegisterLR] = 0x2000
	thread := NewThread(initial)
	thread.SetStepBudget(8)
	grants := 0
	stop := errors.New("stop after three windows")
	thread.SetLimitHook(func(context.Context) error {
		grants++
		if grants == 3 {
			return stop
		}
		return nil
	})
	summary, err := core.Run(context.Background(), thread, 0x2000, nil)
	if !errors.Is(err, stop) {
		t.Fatalf("Run() error = %v, want %v", err, stop)
	}
	if grants != 3 {
		t.Fatalf("grants = %d, want 3", grants)
	}
	// Three windows of 8 steps each ran before the hook stopped the run.
	if summary.Steps != 24 {
		t.Fatalf("steps = %d, want 24", summary.Steps)
	}
	if thread.State() != ThreadFaulted {
		t.Fatalf("thread state = %s, want faulted", thread.State())
	}
}

func TestCoreCallDerivedThreadInheritsBudgetAndHook(t *testing.T) {
	core := NewCore(CoreOptions{Quantum: 4, MaxSteps: 1000})
	loadARM(t, core.Memory(), 0x1000,
		0xe2800001, // loop: add r0, r0, #1
		0xeafffffd, // b loop
	)
	initial := NewContext()
	initial.Registers[RegisterSP] = 0x3000
	parent := NewThread(initial)
	parent.SetStepBudget(8)
	grants := 0
	stop := errors.New("stop nested run")
	parent.SetLimitHook(func(context.Context) error {
		grants++
		return stop
	})
	if err := core.Memory().Map(0x2f00, 0x100, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	_, err := core.Call(context.Background(), parent, 0x1000, 0x2000, nil, nil)
	if !errors.Is(err, stop) {
		t.Fatalf("Call() error = %v, want %v", err, stop)
	}
	if grants != 1 {
		t.Fatalf("grants = %d, want 1", grants)
	}
	if parent.State() != ThreadReady {
		t.Fatalf("parent thread state = %s, want ready", parent.State())
	}
}

// The immediate form of the Thumb-to-ARM call. A Thumb-compiled module reaches
// its own ARM routines through it, and unlike the register form the target is
// word aligned rather than halfword aligned: the low two bits of the computed
// sum are dropped, not carried into an ARM PC that would then fault.
func TestCoreExecutesThumbBLXImmediateIntoARMAndBack(t *testing.T) {
	core := NewCore(CoreOptions{MaxSteps: 100})
	loadThumb(t, core.Memory(), 0x1000,
		0x46c0, // nop — puts the pair on an address whose sum needs the mask
		0xf000, // blx 0x2000, high half: lr = pc + 4
		0xeffe, // blx 0x2000, low half: (lr + 0xffc) & ~3
		0x1c40, // adds r0, r0, #1   (reached only by returning to Thumb)
		0x4738, // bx r7
	)
	loadARM(t, core.Memory(), 0x2000,
		0xe3a0002a, // mov r0, #42
		0xe12fff1e, // bx lr
	)

	initial := NewContext()
	if err := initial.SetPC(0x1001); err != nil {
		t.Fatal(err)
	}
	initial.Registers[7] = 0x3000
	summary, err := core.Run(context.Background(), NewThread(initial), 0x3000, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	// 42 from the ARM routine and 1 from the Thumb instruction after the call.
	// The sum before masking is 0x2002, so anything other than 43 means the
	// call landed a halfword into the ARM routine rather than at its entry.
	if got := summary.Context.Registers[0]; got != 43 {
		t.Fatalf("r0 = %d, want 43", got)
	}
	// The caller was in Thumb, so the return address keeps the Thumb bit even
	// though the callee ran in ARM.
	if got := summary.Context.Registers[RegisterLR]; got != 0x1007 {
		t.Fatalf("lr = %#x, want the Thumb address after the call", got)
	}
}

// Cache maintenance is a write to CP15 c7, and there is no cache here to
// maintain. Modules issue it right after copying code into RAM, so refusing it
// stops a game on an instruction that has nothing to do.
func TestCoreTreatsCP15CacheMaintenanceAsANoOp(t *testing.T) {
	core := NewCore(CoreOptions{MaxSteps: 100})
	loadARM(t, core.Memory(), 0x1000,
		0xe3a0002a, // mov r0, #42
		0xee070f15, // mcr p15, 0, r0, c7, c5, 0 — invalidate instruction cache
		0xee070f9a, // mcr p15, 0, r0, c7, c10, 4 — drain write buffer
		0xe2800001, // add r0, r0, #1
		0xe12fff17, // bx r7
	)

	initial := NewContext()
	if err := initial.SetPC(0x1000); err != nil {
		t.Fatal(err)
	}
	initial.Registers[7] = 0x3000
	summary, err := core.Run(context.Background(), NewThread(initial), 0x3000, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := summary.Context.Registers[0]; got != 43 {
		t.Fatalf("r0 = %d, want 43", got)
	}
}

// Only that one register is answered. The rest of CP15 reports what the
// hardware is, and inventing an answer to an ID or control register would be a
// wrong answer rather than a missing one.
func TestCoreRejectsOtherCP15Access(t *testing.T) {
	for name, instruction := range map[string]uint32{
		"read of the cache register":    0xee170f15, // mrc p15, 0, r0, c7, c5, 0
		"write of the control register": 0xee010f10, // mcr p15, 0, r0, c1, c0, 0
	} {
		t.Run(name, func(t *testing.T) {
			core := NewCore(CoreOptions{MaxSteps: 100})
			loadARM(t, core.Memory(), 0x1000, instruction, 0xe12fff17)

			initial := NewContext()
			if err := initial.SetPC(0x1000); err != nil {
				t.Fatal(err)
			}
			initial.Registers[7] = 0x3000
			if _, err := core.Run(context.Background(), NewThread(initial), 0x3000, nil); !errors.Is(err, ErrUndefinedInstruction) {
				t.Fatalf("Run() error = %v, want ErrUndefinedInstruction", err)
			}
		})
	}
}

// The Thumb hint space executes rather than faulting. A patched archive is
// where they come from — a cracker writes a two-byte `nop` over a branch — and
// there is nothing for any of the five to do on one core with no interrupts,
// so the test is that the instruction after them runs.
func TestCoreExecutesThumbHints(t *testing.T) {
	core := NewCore(CoreOptions{MaxSteps: 16})
	loadThumb(t, core.Memory(), 0x1000,
		0xbf00, // nop
		0xbf10, // yield
		0xbf20, // wfe
		0xbf30, // wfi
		0xbf40, // sev
		0x1c48, // adds r0, r1, #1
		0x4770, // bx lr
	)
	initial := NewContext()
	if err := initial.SetPC(0x1001); err != nil {
		t.Fatal(err)
	}
	initial.Registers[1] = 41
	initial.Registers[RegisterLR] = 0x3000
	summary, err := core.Run(context.Background(), NewThread(initial), 0x3000, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got := summary.Context.Registers[0]; got != 42 {
		t.Fatalf("r0 = %d, want 42: the instruction after the hints did not run", got)
	}
}

// `IT` shares the encoding and does not share the meaning: it changes how the
// instructions after it execute, so running it as a hint would run a
// conditional body unconditionally. It stays undefined.
func TestCoreRefusesThumbIfThen(t *testing.T) {
	core := NewCore(CoreOptions{MaxSteps: 4})
	loadThumb(t, core.Memory(), 0x1000,
		0xbf08, // it eq
		0x1c48, // addeq r0, r1, #1
		0x4770, // bx lr
	)
	initial := NewContext()
	if err := initial.SetPC(0x1001); err != nil {
		t.Fatal(err)
	}
	initial.Registers[RegisterLR] = 0x3000
	if _, err := core.Run(context.Background(), NewThread(initial), 0x3000, nil); !errors.Is(err, ErrUndefinedInstruction) {
		t.Fatalf("Run() error = %v, want %v", err, ErrUndefinedInstruction)
	}
}
