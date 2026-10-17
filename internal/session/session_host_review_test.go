package session

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/ktf"
)

// A title whose menu quits on a held key ends inside the handset repeat a
// tick delivers before it runs the guest. That ending carries its reason the
// same as one from anywhere else: a Host reports exits from ExitReason, and
// an ending with nothing attached reads as a crash.
func TestAnEndingInsideAKeyRepeatKeepsItsReason(t *testing.T) {
	running, clock := startSpeedChangeFixture(t, 1)
	vm := running.ktf.Client.JVM()
	const card = "test/QuitOnRepeat"
	for name, descriptor := range map[string]string{
		"showNotify": "(Z)V",
		"paint":      "(Lorg/kwis/msp/lcdui/Graphics;)V",
	} {
		if err := vm.RegisterNative(card, name, descriptor, func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
			return jvm.VoidValue(), nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := vm.RegisterNative(card, "keyNotify", "(II)Z", func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		if eventType, _ := arguments[1].Int32(); eventType == ktf.KeyRepeated {
			return jvm.VoidValue(), fmt.Errorf("Jlet.notifyDestroyed: %w", ktf.ErrGuestExited)
		}
		return jvm.IntValue(0), nil
	}); err != nil {
		t.Fatal(err)
	}
	display := &jvm.Object{ClassName: "org/kwis/msp/lcdui/Display", Fields: map[string]jvm.Value{}}
	quitting := &jvm.Object{ClassName: card, Fields: map[string]jvm.Value{}}
	if _, err := vm.InvokeVirtual(display, "pushCard", "(Lorg/kwis/msp/lcdui/Card;)V", jvm.ReferenceValue(quitting)); err != nil {
		t.Fatalf("push the quitting card: %v", err)
	}

	if _, err := running.Tick(t.Context(), 8*time.Millisecond); err != nil {
		t.Fatalf("anchoring tick: %v", err)
	}
	if err := running.SendKey(t.Context(), KeyPress, '5'); err != nil {
		t.Fatalf("press: %v", err)
	}
	clock.Advance(600 * time.Millisecond)
	progress, err := running.Tick(t.Context(), 8*time.Millisecond)
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if !progress.Exited {
		t.Fatalf("progress = %+v, want the repeat to have ended the game", progress)
	}
	if !strings.Contains(progress.ExitReason, "Jlet.notifyDestroyed") {
		t.Fatalf("ExitReason = %q, want the platform's own text", progress.ExitReason)
	}
	if progress.ExitReason != running.ExitReason() {
		t.Fatalf("ExitReason = %q, session recorded %q", progress.ExitReason, running.ExitReason())
	}
}
