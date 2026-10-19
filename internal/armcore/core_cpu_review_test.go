package armcore

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// limitHookSteps runs a Thumb fill of count halfwords followed by `b .` under
// a step budget, and answers how many guest steps had been retired when the
// limit hook was called for the stop-th time.
func limitHookSteps(t *testing.T, refuse bool, budget uint64, count uint32, stop int) uint64 {
	t.Helper()
	core := NewCore(CoreOptions{MaxSteps: 1 << 20})
	core.Memory().standInsRefused = refuse
	loadThumb(t, core.Memory(), 0x1000,
		0x8001, // strh r1, [r0]
		0x3002, // adds r0, #2
		0x3a01, // subs r2, #1
		0xd2fb, // bhs  -10
		0xe7fe, // b    .
	)
	if err := core.Memory().Map(0x4000, 0x1000, PermissionReadWrite); err != nil {
		t.Fatal(err)
	}
	initial := NewContext()
	if err := initial.SetPC(0x1001); err != nil {
		t.Fatal(err)
	}
	initial.Registers[0] = 0x4000
	initial.Registers[1] = 0xbeef
	initial.Registers[2] = count
	thread := NewThread(initial)
	thread.SetStepBudget(budget)
	calls := 0
	var at uint64
	finished := errors.New("enough windows")
	thread.SetLimitHook(func(context.Context) error {
		calls++
		if calls == stop {
			at = core.Steps()
			return finished
		}
		return nil
	})
	if _, err := core.Run(context.Background(), thread, 0x3000, nil); !errors.Is(err, finished) {
		t.Fatalf("Run() error = %v, want the hook's own stop", err)
	}
	return at
}

// A stand-in charges every instruction the loop it replaced would have
// retired, which can be several budget windows at once. The windows it used
// up have to reach the hook, which paces the guest by them, rather than being
// dropped when the hook grants the next one.
func TestALimitHookSeesTheWindowsAStandInUsedUp(t *testing.T) {
	const budget, count, stop = 100, 200, 12
	interpreted := limitHookSteps(t, true, budget, count, stop)
	stood := limitHookSteps(t, false, budget, count, stop)
	if interpreted != budget*stop {
		t.Fatalf("interpreted run reached hook %d at step %d, want %d", stop, interpreted, budget*stop)
	}
	if stood != interpreted {
		t.Fatalf("the stand-in run reached hook %d at step %d, the interpreted run at %d", stop, stood, interpreted)
	}
}

// A call restored at a limit stop carries the same overrun the live run would
// have carried past the grant.
func TestACallResumedAtALimitCarriesItsOverrun(t *testing.T) {
	const budget = 20
	core := NewCore(CoreOptions{MaxSteps: budget})
	loadARM(t, core.Memory(), 0x1000,
		0xe2800001, // loop: add r0, r0, #1
		0xeafffffd, // b loop
	)
	parent := NewThread(NewContext())
	var at uint64
	finished := errors.New("first renewal")
	parent.SetLimitHook(func(context.Context) error {
		at = core.Steps()
		return finished
	})
	saved := NewContext()
	if err := saved.SetPC(0x1000); err != nil {
		t.Fatal(err)
	}
	saved.Registers[RegisterSP] = 0x3000
	frame := CallFrame{
		Version: callFrameVersion, Context: saved, EntryStack: 0x3000, End: 0x2000,
		Budget: budget, Steps: budget + 5, Window: budget + 5, Stop: CallStoppedAtLimit,
	}
	if _, err := core.ResumeCall(context.Background(), parent, frame, nil, nil); !errors.Is(err, finished) {
		t.Fatalf("ResumeCall() error = %v, want the hook's own stop", err)
	}
	if at != budget-5 {
		t.Fatalf("the restored call reached its next renewal after %d steps, want %d", at, budget-5)
	}
}

// A limit stop's carried overrun comes from one quantum, whose step count the
// engine reports in 32 bits. A record claiming more is not one this core wrote,
// and restoring it would call the hook for every window it claims.
func TestACallFrameClaimingAnImpossibleOverrunIsRefused(t *testing.T) {
	frame := CallFrame{
		Version: callFrameVersion, Context: NewContext(), End: 0x2000,
		Budget: 20, Steps: 1 << 40, Window: 1 << 40, Stop: CallStoppedAtLimit,
	}
	if err := frame.Validate(); err == nil {
		t.Fatal("a limit stop with an overrun no quantum can produce was accepted")
	}
}

// Host reads share the lock: they read the last-mapping cache the guest's
// accesses keep and write no cache at all. Two of them at once must not race;
// run with -race. The two mappings lie a whole number of page-cache ways
// apart, so their pages share ways, and a read that went through the page
// cache, which refreshes a way on every miss, would race here.
func TestConcurrentHostReadsDoNotRace(t *testing.T) {
	memory := NewMemory()
	bases := []uint32{0x100000, 0x100000 + dataPageWays<<memoryPageShift}
	for _, base := range bases {
		if dataPageWay(base>>memoryPageShift) != dataPageWay(bases[0]>>memoryPageShift) {
			t.Fatalf("mapping %#x does not share the first mapping's page-cache way", base)
		}
		if err := memory.Map(base, 0x2000, PermissionReadWrite); err != nil {
			t.Fatal(err)
		}
		if err := memory.Write(base, []byte{1, 2, 3, 4}); err != nil {
			t.Fatal(err)
		}
	}
	var group sync.WaitGroup
	for _, base := range bases {
		group.Add(1)
		go func(base uint32) {
			defer group.Done()
			buffer := make([]byte, 4)
			for range 2000 {
				if err := memory.Read(base, buffer); err != nil {
					t.Error(err)
					return
				}
				if err := memory.Read(base+0x1000, buffer); err != nil {
					t.Error(err)
					return
				}
				if !memory.Holds([]Span{{Address: base, Data: []byte{1, 2, 3, 4}}, {Address: base + 0x1000, Data: buffer}}) {
					t.Error("guest memory does not hold what was written and read")
					return
				}
			}
		}(base)
	}
	group.Wait()
}
