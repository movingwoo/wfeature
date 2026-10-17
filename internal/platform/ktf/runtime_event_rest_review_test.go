package ktf

import (
	"fmt"
	"slices"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// TestNotifyFanOutSurvivesAListenerRemovingItself covers a Jlet listener that
// removes itself inside notifyEvent. The removal shifts the listener list in
// place under the loop delivering the event, which skipped the next listener
// and delivered to the last one twice.
func TestNotifyFanOutSurvivesAListenerRemovingItself(t *testing.T) {
	client, runtime := newTestRuntime(t)
	var seen []string
	listeners := make([]*jvm.Object, 3)
	for index, name := range []string{"A", "B", "C"} {
		className := "fixture/FanOut" + name
		listener := &jvm.Object{ClassName: className}
		listeners[index] = listener
		if err := client.JVM().RegisterNative(className, "notifyEvent", "(III)V", func(vm *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
			seen = append(seen, name)
			if name == "A" {
				return runtimeDisplayRemoveListener(runtime, vm, []jvm.Value{jvm.ReferenceValue(nil), jvm.ReferenceValue(listener)})
			}
			return jvm.VoidValue(), nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	runtime.jletListeners = slices.Clone(listeners)
	if err := runtime.dispatchNotifyEvent(client.JVM(), guestEvent{kind: eventKindNotify, param1: 1}); err != nil {
		t.Fatal(err)
	}
	if want := []string{"A", "B", "C"}; !slices.Equal(seen, want) {
		t.Fatalf("notified %v, want each listener once: %v", seen, want)
	}
	if !slices.Equal(runtime.jletListeners, listeners[1:]) {
		t.Fatalf("listeners after the removal = %d entries, want B and C", len(runtime.jletListeners))
	}
}

// TestCardDispatchSurvivesAHandlerShrinkingTheStack covers a card handler that
// tears down the card stack and asks for the event to propagate. The loop
// walked the stack by an index taken before the call, so the next index was
// past the end of what was left and the Host panicked. The event now goes on
// to the cards below that are still on the stack, and to none it removed or
// pushed meanwhile.
func TestCardDispatchSurvivesAHandlerShrinkingTheStack(t *testing.T) {
	for _, pointer := range []bool{false, true} {
		name := "key"
		if pointer {
			name = "pointer"
		}
		t.Run(name, func(t *testing.T) {
			client, runtime := newTestRuntime(t)
			var seen []string
			title := &jvm.Object{ClassName: "fixture/StackTitle"}
			cards := make([]*jvm.Object, 3)
			for index, card := range []string{"A", "B", "C", "Title"} {
				className := "fixture/Stack" + card
				object := title
				if index < len(cards) {
					object = &jvm.Object{ClassName: className}
					cards[index] = object
				}
				handler := func(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
					seen = append(seen, card)
					if card == "C" {
						// removeAllCards, then pushCard: what is left is a card
						// that was not on the stack when the event started.
						runtime.displayCards = []*jvm.Object{title}
					}
					return jvm.IntValue(1), nil
				}
				method, descriptor := "keyNotify", "(II)Z"
				if pointer {
					method, descriptor = "pointerNotify", "(III)Z"
				}
				if err := client.JVM().RegisterNative(object.ClassName, method, descriptor, handler); err != nil {
					t.Fatal(err)
				}
			}
			runtime.displayCards = slices.Clone(cards)
			err := func() (err error) {
				defer func() {
					if recovered := recover(); recovered != nil {
						err = fmt.Errorf("dispatch panicked: %v", recovered)
					}
				}()
				if pointer {
					return runtime.dispatchPointerToCards(0, 1, 1)
				}
				return runtime.dispatchKeyToCards(KeyReleased, KeyFire)
			}()
			if err != nil {
				t.Fatal(err)
			}
			if want := []string{"C"}; !slices.Equal(seen, want) {
				t.Fatalf("delivered to %v, want %v", seen, want)
			}
		})
	}
}
