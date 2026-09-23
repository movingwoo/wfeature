package armcore

import (
	"context"
	"errors"
	"testing"
)

func TestLiveContextsIncludesCallsParkedAtStepLimit(t *testing.T) {
	core := NewCore(CoreOptions{MaxSteps: 4})
	loadARM(t, core.Memory(), 0x1000,
		0xe3a04011, // mov r4, #17
		0xef000001, // svc #1
		0xe12fff1e, // bx lr
	)
	loadARM(t, core.Memory(), 0x2000,
		0xe3a04000, // mov r4, #0
		0xe3a0502a, // mov r5, #42
		0xeafffffe, // b .
	)
	parent := NewThread(NewContext())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	parked := make(chan struct{})
	parent.SetLimitHook(func(ctx context.Context) error {
		close(parked)
		<-ctx.Done()
		return ctx.Err()
	})
	done := make(chan error, 1)
	go func() {
		_, err := core.Call(ctx, parent, 0x1000, 0x3000, nil,
			func(ctx context.Context, thread *Thread, _ SupervisorCall) error {
				_, err := core.Call(ctx, thread, 0x2000, 0x3000, nil, nil)
				return err
			})
		done <- err
	}()
	select {
	case <-parked:
	case err := <-done:
		t.Fatalf("call exited before parking: %v", err)
	}
	contexts := parent.LiveContexts()
	if len(contexts) != 3 {
		t.Errorf("live contexts = %d, want parent and two calls", len(contexts))
	} else if contexts[1].Registers[4] != 17 || contexts[2].Registers[4] != 0 || contexts[2].Registers[5] != 42 {
		t.Errorf("nested register snapshots = %v", contexts)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled call error = %v", err)
	}
	if contexts := parent.LiveContexts(); len(contexts) != 1 || contexts[0] != parent.Context() {
		t.Fatalf("completed calls remain live: %v", contexts)
	}
}
