package ktf

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"golang.org/x/text/encoding/korean"
)

type deferredCInputFixture struct {
	session      *Session
	text         *[]byte
	queued       *int32
	beforeHandle func()
	afterQueue   func() error
}

func newDeferredCInputFixture(t *testing.T) *deferredCInputFixture {
	t.Helper()
	s, text, _ := cInputFixture(t)
	t.Cleanup(s.Close)
	client, runtime := s.Client, s.Client.runtime
	clock := NewManualClock(time.Time{})
	client.clock = clock
	original := runtime.displayCards[0]
	runtime.displayCards[0] = newWidget("test/QueuedInputCard")
	var queued int32
	fixture := &deferredCInputFixture{session: s, text: text, queued: &queued}
	if err := client.vm.RegisterNative("test/QueuedInputCard", "keyNotify", "(II)Z", func(_ *jvm.VM, args []jvm.Value) (jvm.Value, error) {
		kind, _ := args[1].Int32()
		if kind == KeyPressed {
			queued, _ = args[2].Int32()
			if fixture.afterQueue != nil {
				return jvm.VoidValue(), fixture.afterQueue()
			}
		}
		return jvm.IntValue(0), nil
	}); err != nil {
		t.Fatal(err)
	}
	var callback uint32
	initialized := false
	rearm := func() {
		runtime.pendingTimers = append(runtime.pendingTimers, wipicTimer{pointer: 0x100, callback: callback, due: client.now().Add(50 * time.Millisecond)})
	}
	if err := client.vm.RegisterNative("test/InputTimer", "run", "(II)V", func(vm *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
		defer rearm()
		if !initialized {
			initialized = true
			_, err := cInputCall(t, runtime, wipicIMSetCurrentMode, 2)
			return jvm.VoidValue(), err
		}
		key := queued
		queued = 0
		if key != 0 {
			if fixture.beforeHandle != nil {
				fixture.beforeHandle()
			}
			_, err := vm.InvokeVirtual(original, "keyNotify", "(II)Z", jvm.IntValue(KeyPressed), jvm.IntValue(key))
			return jvm.VoidValue(), err
		}
		return jvm.VoidValue(), nil
	}); err != nil {
		t.Fatal(err)
	}
	var err error
	callback, err = runtime.runtimeJavaStub(runtimeJavaMethod{class: "test/InputTimer", name: "run", descriptor: "(II)V", accessFlags: 9}, false)
	if err != nil {
		t.Fatal(err)
	}
	rearm()
	clock.Advance(50 * time.Millisecond)
	if _, err := client.ServiceTimers(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func TestCInputHostCommitThroughTimer(t *testing.T) {
	for _, clock := range []string{"manual", "wall"} {
		t.Run(clock, func(t *testing.T) {
			fixture := newDeferredCInputFixture(t)
			s, text, queued := fixture.session, fixture.text, fixture.queued
			if clock == "wall" {
				s.Client.clock = wallClock{}
				s.Client.runtime.pendingTimers[0].due = time.Now().Add(time.Millisecond)
			}
			edit, err := s.TextInput(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if err := edit.Commit(context.Background(), "가나다라마"); err != nil {
				t.Fatalf("deferred text: %v", err)
			}
			want, _ := korean.EUCKR.NewEncoder().Bytes([]byte("가나다라마"))
			if string(*text) != string(want) || *queued != 0 {
				t.Fatalf("text=%x queued=%d, want %x and no queued carrier", *text, *queued, want)
			}
		})
	}
}

func TestCInputTimerClearKeepsEditorAvailable(t *testing.T) {
	fixture := newDeferredCInputFixture(t)
	s := fixture.session
	edit, err := s.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := edit.Commit(context.Background(), "가나"); err != nil {
		t.Fatal(err)
	}
	if err := s.SendKey(context.Background(), KeyPressed, KeyClear); err != nil {
		t.Fatal(err)
	}
	// A repeat can arrive before the timer drains the original press.
	if err := s.SendKey(context.Background(), KeyRepeated, KeyClear); err != nil {
		t.Fatal(err)
	}
	s.Client.clock.(*ManualClock).Advance(50 * time.Millisecond)
	if _, err := s.Client.ServiceTimers(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if err := edit.Commit(context.Background(), "x"); !errors.Is(err, backend.ErrTextInputChanged) {
		t.Fatalf("old edit after CLR: %v", err)
	}
	edit, err = s.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := edit.Commit(context.Background(), "다"); err != nil {
		t.Fatal(err)
	}
	want, _ := korean.EUCKR.NewEncoder().Bytes([]byte("가다"))
	if string(*fixture.text) != string(want) {
		t.Fatalf("replacement text=%x, want %x", *fixture.text, want)
	}
	if err := s.SendKey(context.Background(), KeyPressed, KeyFire); err != nil {
		t.Fatal(err)
	}
	s.Client.clock.(*ManualClock).Advance(50 * time.Millisecond)
	if _, err := s.Client.ServiceTimers(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("dismissed editor: %v", err)
	}
}

func TestCInputTimerRejectsTargetChanges(t *testing.T) {
	for _, change := range []string{"mode", "card", "cancel timer", "cancel wait", "distant timer"} {
		t.Run(change, func(t *testing.T) {
			fixture := newDeferredCInputFixture(t)
			s := fixture.session
			runtime := s.Client.runtime
			edit, err := s.TextInput(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			ctx := context.Background()
			switch change {
			case "mode":
				fixture.beforeHandle = func() { cInputCall(t, runtime, wipicIMSetCurrentMode, 1) }
			case "card":
				fixture.beforeHandle = func() { runtime.displayCards[0] = newWidget("test/OtherCard") }
			case "cancel timer":
				runtime.pendingTimers = nil
			case "distant timer":
				runtime.pendingTimers[0].due = s.Client.now().Add(2 * time.Second)
			case "cancel wait":
				s.Client.clock = wallClock{}
				runtime.pendingTimers[0].due = time.Now().Add(100 * time.Millisecond)
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Millisecond)
				defer cancel()
			}
			err = edit.Commit(ctx, "가")
			if !errors.Is(err, backend.ErrTextInputChanged) && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("commit: %v", err)
			}
			if len(*fixture.text) != 0 || *fixture.queued != 0 {
				t.Fatalf("text=%x queued=%d", *fixture.text, *fixture.queued)
			}
		})
	}
}

func TestCInputCanceledDeliveryDiscardsQueuedCarrier(t *testing.T) {
	fixture := newDeferredCInputFixture(t)
	s := fixture.session
	s.Client.runtime.cInput.mode = 3
	edit, err := s.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fixture.afterQueue = func() error { return context.Canceled }
	if err := edit.Commit(context.Background(), "가"); !errors.Is(err, context.Canceled) {
		t.Fatalf("commit: %v", err)
	}
	fixture.afterQueue = nil
	if _, err := s.Client.ServiceTimers(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if len(*fixture.text) != 0 || *fixture.queued != 0 || s.Client.runtime.cInput.discardCarrier {
		t.Fatalf("canceled delivery left text=%x queued=%d", *fixture.text, *fixture.queued)
	}
	edit, err = s.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := edit.Commit(context.Background(), "7"); err != nil || string(*fixture.text) != "7" {
		t.Fatalf("retry: text=%x err=%v", *fixture.text, err)
	}
}

func TestCInputSynchronousActivationReplacesTimerOwner(t *testing.T) {
	fixture := newDeferredCInputFixture(t)
	runtime := fixture.session.Client.runtime
	if runtime.cInput.timerCallback == 0 {
		t.Fatal("timer activation was not recorded")
	}
	if _, err := cInputCall(t, runtime, wipicIMSetCurrentMode, 2); err != nil {
		t.Fatal(err)
	}
	if runtime.cInput.timerCallback != 0 || runtime.cInput.timerPointer != 0 {
		t.Fatal("synchronous activation retained the previous timer owner")
	}
}
