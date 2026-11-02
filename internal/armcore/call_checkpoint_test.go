package armcore

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func checkpointCallCore(t *testing.T, budget uint64) (*Core, *Thread) {
	t.Helper()
	core := NewCore(CoreOptions{MaxSteps: budget})
	loadARM(t, core.Memory(), 0x1000,
		0xe24dd010, // sub sp, sp, #16
		0xef000001, // svc #1
		0xe2800001, // add r0, r0, #1
		0xe28dd010, // add sp, sp, #16
		0xe12fff1e, // bx lr
	)
	loadARM(t, core.Memory(), 0x1100,
		0xe3a00028, // mov r0, #40
		0xe24dd008, // sub sp, sp, #8
		0xef000002, // svc #2
		0xe2800002, // add r0, r0, #2
		0xe28dd008, // add sp, sp, #8
		0xe12fff1e, // bx lr
	)
	initial := NewContext()
	initial.Registers[RegisterSP] = 0x3000
	return core, NewThread(initial)
}

func captureTestCalls(t *testing.T, budget uint64, atLimit bool) []CallFrame {
	t.Helper()
	core, parent := checkpointCallCore(t, budget)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	frames := make(chan []CallFrame, 1)
	park := func() error {
		captured, err := core.CaptureCalls(parent)
		if err != nil {
			return err
		}
		frames <- captured
		<-ctx.Done()
		return ctx.Err()
	}
	if atLimit {
		parent.SetLimitHook(func(context.Context) error { return park() })
	}
	var handler SupervisorCallHandler
	handler = func(ctx context.Context, thread *Thread, svc SupervisorCall) error {
		if svc.Immediate == 1 {
			result, err := core.Call(ctx, thread, 0x1100, 0x2000, nil, handler)
			if err != nil {
				return err
			}
			return thread.SetRegister(0, result.Context.Registers[0])
		}
		return park()
	}
	done := make(chan error, 1)
	go func() {
		_, err := core.Call(ctx, parent, 0x1000, 0x2000, nil, handler)
		done <- err
	}()
	var captured []CallFrame
	select {
	case captured = <-frames:
	case err := <-done:
		t.Fatalf("call ended before capture: %v", err)
	case <-t.Context().Done():
		t.Fatal("capture timed out")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("source cancellation = %v", err)
	}
	if len(parent.LiveContexts()) != 1 {
		t.Fatal("source retained a derived call")
	}
	// Restoration below gets only decoded records, never the old Thread.
	data, err := json.Marshal(captured)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []CallFrame
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func TestCallCheckpointResumesNestedSupervisorReturns(t *testing.T) {
	for _, budget := range []uint64{4, 20} {
		frames := captureTestCalls(t, budget, false)
		if len(frames) != 2 || frames[0].EntryStack != 0x3000 || frames[1].EntryStack != 0x2ff0 {
			t.Fatalf("captured entry stacks = %+v", frames)
		}
		if frames[1].Context.Registers[RegisterSP] != 0x2fe8 || frames[1].Window != 3 {
			t.Fatalf("captured inner call = %+v", frames[1])
		}
		for attempt := 0; attempt < 3; attempt++ {
			core, parent := checkpointCallCore(t, budget)
			var renewals []Context
			parent.SetLimitHook(func(context.Context) error {
				calls, err := core.CaptureCalls(parent)
				if err != nil {
					return err
				}
				renewals = append(renewals, calls[len(calls)-1].Context)
				return nil
			})
			var restore func(*Thread, int) (RunSummary, error)
			restore = func(parent *Thread, index int) (RunSummary, error) {
				return core.ResumeCall(t.Context(), parent, frames[index], func(ctx context.Context, thread *Thread, svc SupervisorCall) error {
					entry, known := thread.EntryStackPointer()
					if !known || entry != frames[index].EntryStack {
						t.Fatalf("restored entry stack = %#x/%v", entry, known)
					}
					if index == 1 {
						return thread.SetRegister(0, 41)
					}
					result, err := restore(thread, index+1)
					if err != nil {
						return err
					}
					if result.Steps != 6 {
						t.Fatalf("inner steps = %d, want 6", result.Steps)
					}
					return thread.SetRegister(0, result.Context.Registers[0])
				}, func(context.Context, *Thread, SupervisorCall) error {
					t.Fatal("replayed a completed supervisor prefix")
					return nil
				})
			}
			result, err := restore(parent, 0)
			if err != nil || result.Steps != 5 || result.Context.Registers[0] != 44 || result.Context.Registers[RegisterSP] != 0x3000 {
				t.Fatalf("restored result = %+v, %v", result, err)
			}
			if budget == 4 && (len(renewals) != 2 || renewals[0].PC() != 0x1110 || renewals[1].PC() != 0x1010) {
				t.Fatalf("instruction windows changed: %+v", renewals)
			}
			if budget == 20 && len(renewals) != 0 {
				t.Fatal("unexpected instruction renewal")
			}
			if len(parent.LiveContexts()) != 1 {
				t.Fatal("restored call remained registered after return")
			}
		}
	}
}

func TestCallCheckpointResumesGrantedBudgetWithoutRepeatingPark(t *testing.T) {
	frames := captureTestCalls(t, 2, true)
	if len(frames) != 2 || frames[1].Stop != CallStoppedAtLimit || frames[1].Window != 2 {
		t.Fatalf("budget capture = %+v", frames)
	}
	core, parent := checkpointCallCore(t, 2)
	var renewed []uint32
	parent.SetLimitHook(func(context.Context) error {
		calls, err := core.CaptureCalls(parent)
		if err != nil {
			return err
		}
		renewed = append(renewed, calls[len(calls)-1].Context.PC())
		return nil
	})
	result, err := core.ResumeCall(t.Context(), parent, frames[0], func(ctx context.Context, thread *Thread, _ SupervisorCall) error {
		inner, err := core.ResumeCall(ctx, thread, frames[1], nil, func(_ context.Context, thread *Thread, svc SupervisorCall) error {
			if svc.Immediate != 2 {
				t.Fatalf("unexpected service = %+v", svc)
			}
			return thread.SetRegister(0, 41)
		})
		if err != nil {
			return err
		}
		return thread.SetRegister(0, inner.Context.Registers[0])
	}, nil)
	if err != nil || result.Context.Registers[0] != 44 {
		t.Fatalf("budget restoration = %+v, %v", result, err)
	}
	if len(renewed) == 0 || renewed[0] != 0x1110 {
		t.Fatalf("the saved budget park ran twice: %v", renewed)
	}
}

func TestCallCheckpointRefusesInvalidRecordBeforeEntering(t *testing.T) {
	frames := captureTestCalls(t, 20, false)
	for _, damage := range []func(*CallFrame){
		func(f *CallFrame) { f.Version++ },
		func(f *CallFrame) { f.Stop = 255 },
		func(f *CallFrame) { f.Window = f.Budget + 1 },
		func(f *CallFrame) { f.Steps = 0 },
		func(f *CallFrame) { f.Context.Registers[RegisterPC]++ },
		func(f *CallFrame) { f.SupervisorCall.ResumePC++ },
		func(f *CallFrame) { f.Context.CPSR = 0 },
	} {
		core, parent := checkpointCallCore(t, 20)
		frame := frames[0]
		damage(&frame)
		_, err := core.ResumeCall(t.Context(), parent, frame, func(context.Context, *Thread, SupervisorCall) error {
			t.Fatal("invalid state executed a continuation")
			return nil
		}, nil)
		if err == nil || core.Steps() != 0 || len(parent.LiveContexts()) != 1 {
			t.Fatalf("invalid state changed destination: %v", err)
		}
	}
	core, parent := checkpointCallCore(t, 20)
	if _, err := core.CaptureCalls(parent); err == nil {
		t.Fatal("accepted a thread with no parked calls")
	}
	if _, err := core.ResumeCall(t.Context(), parent, frames[0], nil, nil); err == nil {
		t.Fatal("accepted a pending service without its continuation")
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
