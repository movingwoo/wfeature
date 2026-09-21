package ktf

import (
	"fmt"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// The card stack is where the specification puts `showNotify`: a card is told
// when `pushCard` and `popCard` make it visible or hide it. One local title
// does all of its own initialization there and divided by the field it fills
// on every frame it drew.
func TestCardStackTellsCardsWhenTheyAreShownAndHidden(t *testing.T) {
	client, runtime := newTestRuntime(t)
	var log []string
	if err := client.JVM().RegisterNative("test/Card", "showNotify", "(Z)V",
		func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
			receiver, err := arguments[0].Reference()
			if err != nil {
				return jvm.VoidValue(), err
			}
			shown, err := arguments[1].Int32()
			if err != nil {
				return jvm.VoidValue(), err
			}
			name, _ := receiver.Fields["name:Ljava/lang/String;"].Reference()
			log = append(log, fmt.Sprintf("%s=%d", name.ClassName, shown))
			return jvm.VoidValue(), nil
		}); err != nil {
		t.Fatal(err)
	}
	card := func(name string) *jvm.Object {
		return &jvm.Object{ClassName: "test/Card", Fields: map[string]jvm.Value{
			"name:Ljava/lang/String;": jvm.ReferenceValue(&jvm.Object{ClassName: name}),
		}}
	}
	first, second := card("first"), card("second")
	display := jvm.ReferenceValue(&jvm.Object{ClassName: "org/kwis/msp/lcdui/Display"})

	push := func(target *jvm.Object) {
		t.Helper()
		if _, err := runtimeDisplayPushCard(runtime, client.JVM(), []jvm.Value{display, jvm.ReferenceValue(target)}); err != nil {
			t.Fatal(err)
		}
	}
	push(first)
	push(second)
	if _, err := runtimeDisplayPopCard(runtime, client.JVM(), []jvm.Value{display}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeDisplayRemoveAllCards(runtime, client.JVM(), []jvm.Value{display}); err != nil {
		t.Fatal(err)
	}
	want := []string{"first=1", "first=0", "second=1", "second=0", "first=1", "first=0"}
	if fmt.Sprint(log) != fmt.Sprint(want) {
		t.Fatalf("showNotify calls = %v, want %v", log, want)
	}
}

// Only a change of top is a change of what is shown, which is the rule the
// rest of this runtime already answers by.
func TestPushingTheCardAlreadyOnTopNotifiesNobody(t *testing.T) {
	client, runtime := newTestRuntime(t)
	calls := 0
	if err := client.JVM().RegisterNative("test/Card", "showNotify", "(Z)V",
		func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
			calls++
			return jvm.VoidValue(), nil
		}); err != nil {
		t.Fatal(err)
	}
	card := &jvm.Object{ClassName: "test/Card", Fields: make(map[string]jvm.Value)}
	display := jvm.ReferenceValue(&jvm.Object{ClassName: "org/kwis/msp/lcdui/Display"})
	for round := 0; round < 2; round++ {
		if _, err := runtimeDisplayPushCard(runtime, client.JVM(), []jvm.Value{display, jvm.ReferenceValue(card)}); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("showNotify calls = %d, want 1", calls)
	}
	// Removing the covered copy leaves the same card on top, so nothing there
	// is shown or hidden either.
	if _, err := runtimeDisplayRemoveCard(runtime, client.JVM(), []jvm.Value{display, jvm.ReferenceValue(card)}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("showNotify calls after removing a duplicate = %d, want 1", calls)
	}
}

// A worker may wait for initialization performed by the first paint. Its
// instruction budget is not a reason to withhold the display's initial frame.
func TestShownCardPaintUnblocksBusyWorker(t *testing.T) {
	client, runtime := newTestRuntime(t)
	ready := false
	for name, body := range map[string]jvm.NativeMethod{
		"showNotify": func(*jvm.VM, []jvm.Value) (jvm.Value, error) { return jvm.VoidValue(), nil },
		"paint":      func(*jvm.VM, []jvm.Value) (jvm.Value, error) { ready = true; return jvm.VoidValue(), nil },
	} {
		descriptor := "(Z)V"
		if name == "paint" {
			descriptor = "(Lorg/kwis/msp/lcdui/Graphics;)V"
		}
		if err := client.JVM().RegisterNative("test/InitialCard", name, descriptor, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := client.JVM().RegisterNative("test/InitialWorker", "run", "()V", func(*jvm.VM, []jvm.Value) (jvm.Value, error) {
		for !ready {
			if err := client.activeWorker.parkSlice(true); err != nil {
				return jvm.VoidValue(), err
			}
		}
		return jvm.VoidValue(), nil
	}); err != nil {
		t.Fatal(err)
	}
	card := &jvm.Object{ClassName: "test/InitialCard"}
	if _, err := runtimeDisplayPushCard(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(nil), jvm.ReferenceValue(card)}); err != nil {
		t.Fatal(err)
	}
	runtime.pendingThreads = []*jvm.Object{{ClassName: "test/InitialWorker"}}
	t.Cleanup(client.StopThreads)
	if _, err := client.ServiceThreads(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if !client.forcedSlice {
		t.Fatal("worker did not wait for initial paint")
	}
	if painted, err := client.ServicePaint(t.Context()); err != nil || !painted {
		t.Fatalf("initial paint=%v error=%v", painted, err)
	}
	if _, err := client.ServiceThreads(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	if len(client.workers) != 0 {
		t.Fatal("initial paint did not release waiting worker")
	}
}
