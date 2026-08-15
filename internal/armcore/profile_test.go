package armcore

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
)

// The guest loop below leaves a supervisor call every second instruction, so
// quanta end after two steps no matter how large the quantum is. Sampling per
// quantum would report a sample every two instructions and, worse, would report
// every one of them at the instruction after the supervisor call — making the
// platform stub the hottest address in a game that is really busy elsewhere.
// Ten samples, all on the loop's own branch, is what says neither happened.
func TestProfileSamplesTrackExecutedStepsNotQuantumBoundaries(t *testing.T) {
	core := NewCore(CoreOptions{MaxSteps: 10_000})
	loadThumb(t, core.Memory(), 0x1000,
		0xdf00, // svc #0
		0xe7fd, // b 0x1000
	)
	core.EnableProfile(1000)

	thread := newThumbThread(t, 0x1000, 0)
	_, err := core.Run(context.Background(), thread, 0x2000, func(context.Context, *Thread, SupervisorCall) error {
		return nil
	})
	if !errors.Is(err, ErrStepLimit) {
		t.Fatalf("Run() error = %v, want ErrStepLimit", err)
	}

	profile := core.Profile()
	if profile.Steps != 10_000 {
		t.Fatalf("profiled steps = %d, want 10000", profile.Steps)
	}
	if profile.Taken != 10 {
		t.Fatalf("samples taken = %d, want 10 (one per 1000 steps, not one per quantum)", profile.Taken)
	}
	// Every thousandth instruction of this loop is its branch, so an unbiased
	// sampler lands on 0x1000 — the supervisor call it is about to reach —
	// every time, and never on the stub-return address at 0x1002.
	if len(profile.Samples) != 1 {
		t.Fatalf("distinct stacks = %d, want 1", len(profile.Samples))
	}
	if got := profile.Samples[0].Stack[0]; got != 0x1000 {
		t.Fatalf("sampled PC = %#x, want 0x1000", got)
	}
	if got := profile.Samples[0].Count; got != 10 {
		t.Fatalf("sample count = %d, want 10", got)
	}
}

func TestProfileWalksThumbFramePointerChain(t *testing.T) {
	core := NewCore(CoreOptions{MaxSteps: 3000})
	loadThumb(t, core.Memory(), 0x1000, 0xe7fe) // b .
	mapGuestStack(t, core.Memory())
	// Two stacked frames: [r7] is the caller's r7 and [r7+4] its return
	// address, with bit zero set because the guest returns into Thumb code.
	writeFrame(t, core.Memory(), 0x8000, 0x8010, 0x1201)
	writeFrame(t, core.Memory(), 0x8010, 0, 0x1301)
	core.EnableProfile(1000)

	thread := newThumbThread(t, 0x1000, 0x8000)
	if _, err := core.Run(context.Background(), thread, 0x2000, nil); !errors.Is(err, ErrStepLimit) {
		t.Fatalf("Run() error = %v, want ErrStepLimit", err)
	}

	profile := core.Profile()
	if len(profile.Samples) != 1 {
		t.Fatalf("distinct stacks = %d, want 1", len(profile.Samples))
	}
	want := []uint32{0x1000, 0x1200, 0x1300}
	got := profile.Samples[0].Stack
	if len(got) != len(want) {
		t.Fatalf("stack = %#x, want %#x", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("stack = %#x, want %#x", got, want)
		}
	}
}

// A return address without the Thumb bit is not a frame, so the walk stops
// there rather than reporting whatever unrelated data it landed in.
func TestProfileStopsWalkingAtNonThumbReturnAddress(t *testing.T) {
	core := NewCore(CoreOptions{MaxSteps: 2000})
	loadThumb(t, core.Memory(), 0x1000, 0xe7fe)
	mapGuestStack(t, core.Memory())
	writeFrame(t, core.Memory(), 0x8000, 0x8010, 0x1200)
	core.EnableProfile(1000)

	thread := newThumbThread(t, 0x1000, 0x8000)
	if _, err := core.Run(context.Background(), thread, 0x2000, nil); !errors.Is(err, ErrStepLimit) {
		t.Fatalf("Run() error = %v, want ErrStepLimit", err)
	}
	if got := core.Profile().Samples[0].Stack; len(got) != 1 || got[0] != 0x1000 {
		t.Fatalf("stack = %#x, want [0x1000]", got)
	}
}

// A chain that does not walk upwards is a cycle; the walk has to give up
// rather than fill the sample with the same frame thirty-two times.
func TestProfileStopsWalkingOnFramePointerCycle(t *testing.T) {
	core := NewCore(CoreOptions{MaxSteps: 2000})
	loadThumb(t, core.Memory(), 0x1000, 0xe7fe)
	mapGuestStack(t, core.Memory())
	writeFrame(t, core.Memory(), 0x8000, 0x8000, 0x1201)
	core.EnableProfile(1000)

	thread := newThumbThread(t, 0x1000, 0x8000)
	if _, err := core.Run(context.Background(), thread, 0x2000, nil); !errors.Is(err, ErrStepLimit) {
		t.Fatalf("Run() error = %v, want ErrStepLimit", err)
	}
	if got := core.Profile().Samples[0].Stack; len(got) != 2 {
		t.Fatalf("stack = %#x, want the sampled PC and one frame", got)
	}
}

// Past the distinct-stack limit the walk is folded to the leaf PC. Samples
// keep being counted — dropping them would silently understate exactly the
// code that produced the flood.
func TestProfileFoldsToLeafPCPastDistinctStackLimit(t *testing.T) {
	core := NewCore(CoreOptions{MaxSteps: 6000})
	loadThumb(t, core.Memory(), 0x1000, 0xe7fe)
	mapGuestStack(t, core.Memory())
	core.EnableProfile(1000)
	core.profile.maxStacks = 1

	// Each run gets a different frame chain, so without folding every sample
	// would be its own stack.
	for round := uint32(0); round < 3; round++ {
		writeFrame(t, core.Memory(), 0x8000, 0, 0x1201+round*0x100)
		thread := newThumbThread(t, 0x1000, 0x8000)
		if _, err := core.Run(context.Background(), thread, 0x2000, nil); !errors.Is(err, ErrStepLimit) {
			t.Fatalf("round %d: Run() error = %v, want ErrStepLimit", round, err)
		}
	}

	profile := core.Profile()
	if profile.Taken != 18 {
		t.Fatalf("samples taken = %d, want 18", profile.Taken)
	}
	if profile.CallersDropped == 0 {
		t.Fatal("CallersDropped = 0, want the cut-back samples to be reported")
	}
	total := uint64(0)
	for _, sample := range profile.Samples {
		total += sample.Count
	}
	if total != profile.Taken {
		t.Fatalf("counted %d samples across stacks, want all %d", total, profile.Taken)
	}
}

func TestProfileResetKeepsSamplingAndClearsCounts(t *testing.T) {
	core := NewCore(CoreOptions{MaxSteps: 2000})
	loadThumb(t, core.Memory(), 0x1000, 0xe7fe)
	core.EnableProfile(1000)

	run := func() {
		thread := newThumbThread(t, 0x1000, 0)
		if _, err := core.Run(context.Background(), thread, 0x2000, nil); !errors.Is(err, ErrStepLimit) {
			t.Fatalf("Run() error = %v, want ErrStepLimit", err)
		}
	}
	run()
	core.ResetProfile()
	if profile := core.Profile(); profile.Taken != 0 || profile.Steps != 0 || len(profile.Samples) != 0 {
		t.Fatalf("after reset = %+v, want empty", profile)
	}
	run()
	if profile := core.Profile(); profile.Taken != 2 {
		t.Fatalf("samples after reset = %d, want 2", profile.Taken)
	}
}

func TestProfileIsOffUntilEnabled(t *testing.T) {
	core := NewCore(CoreOptions{MaxSteps: 2000})
	loadThumb(t, core.Memory(), 0x1000, 0xe7fe)

	thread := newThumbThread(t, 0x1000, 0)
	if _, err := core.Run(context.Background(), thread, 0x2000, nil); !errors.Is(err, ErrStepLimit) {
		t.Fatalf("Run() error = %v, want ErrStepLimit", err)
	}
	if profile := core.Profile(); profile.Taken != 0 || profile.Steps != 0 {
		t.Fatalf("profile without EnableProfile = %+v, want empty", profile)
	}
}

func newThumbThread(t *testing.T, entry, framePointer uint32) *Thread {
	t.Helper()
	initial := NewContext()
	if err := initial.SetPC(entry | 1); err != nil {
		t.Fatal(err)
	}
	initial.Registers[7] = framePointer
	return NewThread(initial)
}

// mapGuestStack reserves the region the frame-chain tests build their frames
// in. Callers map it once and then write frames into it.
func mapGuestStack(t *testing.T, memory *Memory) {
	t.Helper()
	if err := memory.Map(0x8000, 0x1000, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
}

func writeFrame(t *testing.T, memory *Memory, address, callerFrame, returnAddress uint32) {
	t.Helper()
	var frame [8]byte
	binary.LittleEndian.PutUint32(frame[0:4], callerFrame)
	binary.LittleEndian.PutUint32(frame[4:8], returnAddress)
	if err := memory.Write(address, frame[:]); err != nil {
		t.Fatal(err)
	}
}
