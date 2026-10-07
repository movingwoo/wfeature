package jvm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/jvm/classfile"
)

type checkpointTestNativeWait struct {
	thread  *Object
	until   <-chan struct{}
	resumes *atomic.Int32
	reject  bool
}

func (wait *checkpointTestNativeWait) ValidateCheckpointWait(className, name, descriptor string, timed bool, remaining time.Duration) error {
	if wait.reject || className != "ThreadCheckpointProbe" || name != "entered" || descriptor != "(LThreadCheckpointProbe;)V" {
		return fmt.Errorf("invalid fixture wait method")
	}
	return nil
}

func (wait *checkpointTestNativeWait) ResumeCheckpointWait(call *Invocation, timed bool, remaining time.Duration) (Value, error) {
	wait.resumes.Add(1)
	return wait.complete(call, timed, remaining)
}

func (wait *checkpointTestNativeWait) complete(call *Invocation, timed bool, remaining time.Duration) (Value, error) {
	if _, err := call.WaitCheckpointAsGuestThread(&Object{ClassName: ObjectClass, Native: wait}, timed, remaining, wait.until); err != nil {
		return VoidValue(), err
	}
	return VoidValue(), call.VM().SetField(wait.thread, "ThreadCheckpointProbe", "stopped", "Z", IntValue(1))
}

func checkpointNativeWaitCodec(until <-chan struct{}, resumes *atomic.Int32) HeapCodec {
	return HeapCodec{
		CaptureNative: func(value any) (HeapExternalPayload, error) {
			wait, ok := value.(*checkpointTestNativeWait)
			if !ok {
				return HeapExternalPayload{}, fmt.Errorf("unexpected fixture payload %T", value)
			}
			return HeapExternalPayload{Kind: "fixture-native-wait", References: []*Object{wait.thread}}, nil
		},
		RestoreNative: func(saved HeapExternalPayload) (any, error) {
			if saved.Kind != "fixture-native-wait" || len(saved.References) != 1 || len(saved.Data) != 0 {
				return nil, fmt.Errorf("invalid fixture wait payload")
			}
			return &checkpointTestNativeWait{thread: saved.References[0], until: until, resumes: resumes}, nil
		},
	}
}

func TestCheckpointNativeWaitHostZeroInterruptAndClose(t *testing.T) {
	vm := New(nil, Options{})
	defer vm.Close()
	if waited, err := (&Invocation{vm: vm}).WaitCheckpointAsGuestThread(nil, false, 0, nil); waited || err != nil {
		t.Fatalf("host wait = %v, %v", waited, err)
	}
	thread := &Object{ClassName: ThreadClass}
	call := &Invocation{vm: vm, state: &execution{thread: thread}}
	token := &Object{ClassName: ObjectClass, Native: &checkpointTestNativeWait{}}
	finished := make(chan error, 1)
	go func() {
		waited, err := call.WaitCheckpointAsGuestThread(token, true, 0, nil)
		if !waited && err == nil {
			err = fmt.Errorf("guest did not wait")
		}
		finished <- err
	}()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed zero waited forever")
	}
	if _, err := vm.InvokeVirtual(thread, "interrupt", "()V"); err != nil {
		t.Fatal(err)
	}
	if waited, err := call.WaitCheckpointAsGuestThread(token, false, 0, nil); !waited || err != nil {
		t.Fatalf("interrupted wait = %v, %v", waited, err)
	}
	state := vm.threadState(thread)
	state.mu.Lock()
	interrupted := state.interrupted
	state.mu.Unlock()
	if !interrupted || len(state.wake) != 0 {
		t.Fatal("native wait did not preserve the interrupt flag and consume its wake")
	}
	vm.Close()
	if waited, err := call.WaitCheckpointAsGuestThread(token, false, 0, nil); !waited || !errors.Is(err, ErrClosed) {
		t.Fatalf("closed wait = %v, %v", waited, err)
	}
	if call.state.wait != nil {
		t.Fatal("finished native wait retained its continuation")
	}
}

func TestCheckpointNativeWaitRoundTripDoesNotReplayStart(t *testing.T) {
	until := make(chan struct{})
	entered := make(chan struct{})
	var prefixes, resumes atomic.Int32
	var sourceToken *checkpointTestNativeWait
	source := newThreadCheckpointVM(t, func(call *Invocation, args []Value) (Value, error) {
		prefixes.Add(1)
		thread, _ := args[0].Reference()
		sourceToken = &checkpointTestNativeWait{thread: thread, until: until, resumes: &resumes}
		close(entered)
		return sourceToken.complete(call, true, time.Minute)
	})
	thread := startCheckpointThread(t, source, -1)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("native prefix did not run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	paused, err := source.PauseThreads(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer paused.Resume()
	codec := checkpointNativeWaitCodec(until, &resumes)
	saved, err := paused.CaptureState([]*Object{thread}, codec)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Threads) != 1 || saved.Threads[0].Wait != "native" || saved.Threads[0].WaitObject == 0 || !saved.Threads[0].Timed || saved.Threads[0].Remaining <= 0 {
		t.Fatalf("native continuation = %+v", saved.Threads)
	}
	data, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}
	decode := func() ThreadCheckpointState {
		t.Helper()
		var record ThreadCheckpointState
		if err := json.Unmarshal(data, &record); err != nil {
			t.Fatal(err)
		}
		return record
	}
	fresh := newThreadCheckpointVM(t, func(*Invocation, []Value) (Value, error) {
		prefixes.Add(1)
		return VoidValue(), fmt.Errorf("restored native prefix ran again")
	})
	prepared, roots, err := fresh.PrepareThreadCheckpoint(decode(), codec)
	if err != nil {
		t.Fatal(err)
	}
	defer prepared.Discard()
	state := fresh.threadState(roots[0]).execution
	restoredToken, ok := state.wait.object.Native.(*checkpointTestNativeWait)
	if !ok || restoredToken == sourceToken || restoredToken.thread != roots[0] || roots[0] == thread {
		t.Fatal("native token or referenced thread was not restored independently")
	}
	// A rejected token never gets a worker grant, including when its payload
	// exists but belongs to a different native method.
	mutations := map[string]func(*ThreadCheckpointState){
		"missing token": func(record *ThreadCheckpointState) { record.Threads[0].WaitObject = 0 },
		"null token": func(record *ThreadCheckpointState) {
			wait := record.Threads[0]
			record.Heap.Roots[wait.RootStart+int(wait.WaitObject)-1] = 0
		},
		"negative remaining": func(record *ThreadCheckpointState) { record.Threads[0].Remaining = -1 },
		"untimed duration":   func(record *ThreadCheckpointState) { record.Threads[0].Timed = false },
		"released monitor":   func(record *ThreadCheckpointState) { record.Threads[0].ReleasedDepth = 1 },
		"notified":           func(record *ThreadCheckpointState) { record.Threads[0].Notified = true },
		"reentry":            func(record *ThreadCheckpointState) { record.Threads[0].Reenter = true },
		"monitor interrupt":  func(record *ThreadCheckpointState) { record.Threads[0].Interrupted = true },
		"overflowed invoke": func(record *ThreadCheckpointState) {
			record.Threads[0].Execution.Frames[0].InvokePC = int(^uint(0) >> 1)
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			bad := decode()
			mutate(&bad)
			if err := bad.validate(fresh); err == nil {
				t.Fatal("malformed native wait was accepted")
			}
		})
	}
	for _, wrongType := range []bool{false, true} {
		badVM := newThreadCheckpointVM(t, func(*Invocation, []Value) (Value, error) { return VoidValue(), nil })
		badCodec := codec
		badCodec.RestoreNative = func(payload HeapExternalPayload) (any, error) {
			if wrongType {
				return "wrong native payload", nil
			}
			return &checkpointTestNativeWait{reject: true}, nil
		}
		if bad, _, err := badVM.PrepareThreadCheckpoint(decode(), badCodec); err == nil {
			bad.Discard()
			t.Fatal("invalid native token was prepared")
		}
		badVM.Close()
	}
	if resumes.Load() != 0 {
		t.Fatal("detached preparation resumed a native call")
	}
	close(until)
	prepared.Start()
	paused.Resume()
	for _, run := range []struct {
		vm     *VM
		thread *Object
	}{{source, thread}, {fresh, roots[0]}} {
		select {
		case <-run.vm.threadState(run.thread).done:
		case <-ctx.Done():
			t.Fatal("native wait did not complete")
		}
		if got := checkpointTicks(t, run.vm, run.thread); got != 0 {
			t.Fatalf("native cleanup did not precede bytecode resume: ticks=%d", got)
		}
	}
	if prefixes.Load() != 1 || resumes.Load() != 1 {
		t.Fatalf("native prefixes=%d, resumes=%d", prefixes.Load(), resumes.Load())
	}
}

func TestCheckpointNativeWaitExcludesBarrierDuration(t *testing.T) {
	entered := make(chan struct{})
	vm := newThreadCheckpointVM(t, func(call *Invocation, args []Value) (Value, error) {
		thread, _ := args[0].Reference()
		wait := &checkpointTestNativeWait{thread: thread}
		close(entered)
		return wait.complete(call, true, 100*time.Millisecond)
	})
	thread := startCheckpointThread(t, vm, -1)
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("native prefix did not run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	paused, err := vm.PauseThreads(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer paused.Resume()
	if len(paused.calls) != 1 || paused.calls[0].wait == nil {
		t.Fatal("native wait did not reach the barrier")
	}
	remaining := paused.calls[0].wait.remaining
	time.Sleep(remaining + 20*time.Millisecond)
	resumed := time.Now()
	paused.Resume()
	select {
	case <-vm.threadState(thread).done:
		if time.Since(resumed) < remaining-5*time.Millisecond {
			t.Fatal("checkpoint barrier consumed native wait duration")
		}
	case <-ctx.Done():
		t.Fatal("native timer did not expire after resume")
	}
}

func TestCheckpointNativeWaitValidatesInterfaceCallsite(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func([]byte) []byte
		body   bool
		valid  bool
	}{
		{name: "interface", valid: true},
		{name: "truncated", mutate: func(code []byte) []byte { return code[:4] }},
		{name: "zero count", mutate: func(code []byte) []byte { code[3] = 0; return code }},
		{name: "reserved operand", mutate: func(code []byte) []byte { code[4] = 1; return code }},
		{name: "static abstract", mutate: func(code []byte) []byte { code[0] = 0xb8; return code }},
		{name: "field opcode", mutate: func(code []byte) []byte { code[0] = 0xb4; return code }},
		{name: "bytecode method", body: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			builder := newTestClassBuilder("NativeWaitCaller")
			reference := builder.methodReference("NativeWaitInterface", "play", "()V")
			builder.entries[reference-1][0] = byte(classfile.ConstantInterfaceMethodRef)
			code := []byte{0xb9, byte(reference >> 8), byte(reference), 1, 0, 0xb1}
			if test.mutate != nil {
				code = test.mutate(code)
			}
			vm := New(mapClassSource{"NativeWaitCaller": builder.build(nil, []testMethod{{name: "run", descriptor: "()V", maxStack: 1, code: code}})}, Options{})
			defer vm.Close()
			if err := vm.DefineClass(ClassDefinition{
				Name: "NativeWaitInterface", SuperName: ObjectClass, Access: AccessPublic | AccessAbstract | AccessInterface,
				Methods: []MethodDefinition{{Name: "play", Descriptor: "()V", Access: AccessPublic | AccessAbstract}},
			}); err != nil {
				t.Fatal(err)
			}
			if test.body {
				class, err := vm.loader.Load("NativeWaitInterface")
				if err != nil {
					t.Fatal(err)
				}
				class.Methods[0].Attributes = []classfile.Attribute{{Name: "Code", Code: &classfile.Code{Bytecode: []byte{0xb1}}}}
			}
			resolved, err := vm.checkpointNativeWaitMethod(BytecodeExecutionState{
				Frames: []BytecodeFrameState{{Class: "NativeWaitCaller", Method: "run", Descriptor: "()V", InvokePC: 0}},
			})
			if test.valid {
				if err != nil || resolved.Class != "NativeWaitInterface" || resolved.Name != "play" || resolved.Descriptor != "()V" {
					t.Fatalf("resolved callsite = %+v, %v", resolved, err)
				}
			} else if err == nil {
				t.Fatal("invalid interface continuation was accepted")
			}
		})
	}
}
