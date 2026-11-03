package ktf

import (
	"fmt"
	"slices"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

func TestGFormVisibilityThroughPublishedMethods(t *testing.T) {
	client, runtime := newTestRuntime(t)
	for _, class := range []string{runtimeGFormClass, runtimeGMenubarFormClass, runtimeGMsgBoxClass} {
		t.Run(class, func(t *testing.T) {
			object := newWidget(class)
			definition := runtimeGFormClassDefinition(class, runtimeShellComponentClass)
			call := func(name string) {
				t.Helper()
				for _, method := range definition.methods {
					if method.name == name && method.descriptor == "()V" {
						if _, err := method.implementation(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(object)}); err != nil {
							t.Fatal(err)
						}
						return
					}
				}
				t.Fatalf("missing %s", name)
			}
			call("show")
			if got := widgetInt(t, object, componentShownField); got != 1 {
				t.Fatalf("show state = %d", got)
			}
			call("hide")
			if got := widgetInt(t, object, componentShownField); got != 0 {
				t.Fatalf("hide state = %d", got)
			}
		})
	}
}

// A compiled caller can resolve setString on TextFieldComponent while its
// receiver is a GTextField. Both classes must agree on the virtual slot.
func TestGTextFieldDispatchThroughTextFieldComponent(t *testing.T) {
	_, runtime := newTestRuntime(t)
	parent, err := runtime.ensureJavaClass(runtimeTextFieldComponentClass)
	if err != nil {
		t.Fatal(err)
	}
	child, err := runtime.ensureJavaClass(runtimeGTextFieldClass)
	if err != nil {
		t.Fatal(err)
	}
	parentTable, err := runtime.inheritedVTable(parent)
	if err != nil {
		t.Fatal(err)
	}
	childTable, err := runtime.inheritedVTable(child)
	if err != nil {
		t.Fatal(err)
	}
	for slot, method := range parentTable.entries {
		if method.name != "setString" || method.descriptor != "(Ljava/lang/String;)V" {
			continue
		}
		if slot >= len(childTable.entries) {
			t.Fatalf("receiver has no slot %d", slot)
		}
		got := childTable.entries[slot]
		if got.name != method.name || got.descriptor != method.descriptor {
			t.Fatalf("TextFieldComponent.setString slot %d dispatches to %s%s", slot, got.name, got.descriptor)
		}
		return
	}
	t.Fatal("TextFieldComponent.setString missing")
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
				return runtime.dispatchKeyToCards(wipicrt.KeyReleased, wipicrt.KeyFire)
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
