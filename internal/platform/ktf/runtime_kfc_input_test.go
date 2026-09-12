package ktf

import (
	"context"
	"errors"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
)

func shownVendorTextField(t *testing.T, text string) (*Session, *jvm.Object, *jvm.Object, *jvm.Object) {
	t.Helper()
	client, runtime := newTestRuntime(t)
	form := newWidget(runtimeGFormClass)
	field := newWidget(runtimeGTextFieldClass)
	listener := newWidget("test/VendorFieldListener")
	if _, err := runtimeGFormConstructor(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(form)}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeGTextFieldConstructor(runtime, client.JVM(), []jvm.Value{
		jvm.ReferenceValue(field), jvm.ReferenceValue(nil), jvm.ReferenceValue(client.JVM().NewString(text)), jvm.IntValue(12),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeComponentSetEventListener(runtime, client.JVM(), []jvm.Value{
		jvm.ReferenceValue(field), jvm.ReferenceValue(listener), jvm.ReferenceValue(nil),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeComponentAddComponent(runtime, client.JVM(), []jvm.Value{
		jvm.ReferenceValue(form), jvm.ReferenceValue(field),
	}); err != nil {
		t.Fatal(err)
	}
	callVendorFormMethod(t, runtime, client.JVM(), form, "show")
	return &Session{Client: client}, form, field, listener
}

func callVendorFormMethod(t *testing.T, runtime *initializationRuntime, vm *jvm.VM, form *jvm.Object, name string) {
	t.Helper()
	definition := runtimeGFormClassDefinition(form.ClassName, runtimeShellComponentClass)
	for _, method := range definition.methods {
		if method.name == name && method.descriptor == "()V" {
			if _, err := method.implementation(runtime, vm, []jvm.Value{jvm.ReferenceValue(form)}); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatalf("missing %s", name)
}

func TestShownVendorFormOffersItsUniqueListenedTextField(t *testing.T) {
	session, form, field, _ := shownVendorTextField(t, "old")
	input, err := session.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if input.Text != "old" || input.MaxLength != 12 || input.Multiline || input.InputMode != "text" {
		t.Fatalf("TextInput() = %+v", input)
	}
	if err := input.Commit(context.Background(), "한글🙂"); err != nil {
		t.Fatal(err)
	}
	if got := runtimeComponentText(field); got != "한글🙂" {
		t.Fatalf("component text = %q, want complete Host composition", got)
	}
	if got := widgetInt(t, form, componentShownField); got != 1 {
		t.Fatalf("Host commit hid the guest form: shown = %d", got)
	}
}

func TestVendorFormDoesNotGuessAmongFieldsOrUnlistenedChildren(t *testing.T) {
	for _, probe := range []struct {
		name   string
		mutate func(*testing.T, *Session, *jvm.Object, *jvm.Object)
	}{
		{
			name: "two listened fields",
			mutate: func(t *testing.T, session *Session, form, _ *jvm.Object) {
				other := newWidget(runtimeGTextFieldClass)
				other.Fields[componentEventListenerField] = jvm.ReferenceValue(newWidget("test/OtherListener"))
				if _, err := runtimeComponentAddComponent(session.Client.runtime, session.Client.JVM(), []jvm.Value{
					jvm.ReferenceValue(form), jvm.ReferenceValue(other),
				}); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "listener removed",
			mutate: func(_ *testing.T, _ *Session, _ *jvm.Object, field *jvm.Object) {
				field.Fields[componentEventListenerField] = jvm.ReferenceValue(nil)
			},
		},
	} {
		t.Run(probe.name, func(t *testing.T) {
			session, form, field, _ := shownVendorTextField(t, "old")
			probe.mutate(t, session, form, field)
			if _, err := session.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
				t.Fatalf("TextInput() error = %v", err)
			}
		})
	}
}

func TestVendorFormSkipsANilChild(t *testing.T) {
	session, form, _, _ := shownVendorTextField(t, "old")
	form.Native = append([]*jvm.Object{nil}, runtimeComponentChildren(form)...)
	if _, err := session.TextInput(context.Background()); err != nil {
		t.Fatalf("TextInput with nil sibling: %v", err)
	}
}

func TestVendorTextInputRejectsHideRemovalAndReshow(t *testing.T) {
	for _, probe := range []struct {
		name   string
		mutate func(*testing.T, *Session, *jvm.Object, *jvm.Object)
	}{
		{
			name: "hide",
			mutate: func(t *testing.T, session *Session, form, _ *jvm.Object) {
				callVendorFormMethod(t, session.Client.runtime, session.Client.JVM(), form, "hide")
			},
		},
		{
			name: "remove",
			mutate: func(t *testing.T, session *Session, form, field *jvm.Object) {
				if _, err := runtimeComponentRemoveComponent(session.Client.runtime, session.Client.JVM(), []jvm.Value{
					jvm.ReferenceValue(form), jvm.ReferenceValue(field),
				}); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "remove and re-add",
			mutate: func(t *testing.T, session *Session, form, field *jvm.Object) {
				if _, err := runtimeComponentRemoveComponent(session.Client.runtime, session.Client.JVM(), []jvm.Value{
					jvm.ReferenceValue(form), jvm.ReferenceValue(field),
				}); err != nil {
					t.Fatal(err)
				}
				if _, err := runtimeComponentAddComponent(session.Client.runtime, session.Client.JVM(), []jvm.Value{
					jvm.ReferenceValue(form), jvm.ReferenceValue(field),
				}); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "listener remove and restore",
			mutate: func(t *testing.T, session *Session, _ *jvm.Object, field *jvm.Object) {
				listener := field.Fields[componentEventListenerField]
				data := field.Fields[componentEventDataField]
				for _, value := range []jvm.Value{jvm.ReferenceValue(nil), listener} {
					if _, err := runtimeComponentSetEventListener(session.Client.runtime, session.Client.JVM(), []jvm.Value{
						jvm.ReferenceValue(field), value, data,
					}); err != nil {
						t.Fatal(err)
					}
				}
			},
		},
		{
			name: "text change and restore",
			mutate: func(t *testing.T, session *Session, _ *jvm.Object, field *jvm.Object) {
				original := field.Fields[componentTextField]
				if _, err := runtimeGTextFieldSetString(session.Client.runtime, session.Client.JVM(), []jvm.Value{
					jvm.ReferenceValue(field), jvm.ReferenceValue(session.Client.JVM().NewString("changed")),
				}); err != nil {
					t.Fatal(err)
				}
				field.Fields[componentTextField] = original
			},
		},
		{
			name: "hide and reshow",
			mutate: func(t *testing.T, session *Session, form, _ *jvm.Object) {
				callVendorFormMethod(t, session.Client.runtime, session.Client.JVM(), form, "hide")
				callVendorFormMethod(t, session.Client.runtime, session.Client.JVM(), form, "show")
			},
		},
	} {
		t.Run(probe.name, func(t *testing.T) {
			session, form, field, _ := shownVendorTextField(t, "old")
			input, err := session.TextInput(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			probe.mutate(t, session, form, field)
			if err := input.Commit(context.Background(), "stale"); !errors.Is(err, backend.ErrTextInputChanged) {
				t.Fatalf("stale commit error = %v", err)
			}
			if got := runtimeComponentText(field); got != "old" {
				t.Fatalf("stale commit changed field to %q", got)
			}
		})
	}
}

func TestVendorFieldListenerAcceptsCommittedTextAndOwnsTrailingRelease(t *testing.T) {
	session, form, field, listener := shownVendorTextField(t, "old")
	runtime := session.Client.runtime
	accepted := ""
	var listenerEvents [][4]int32
	if err := session.Client.JVM().RegisterNative(listener.ClassName, "eventNotify", "(IIIILjava/lang/Object;)Z", func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		var event [4]int32
		for index := range event {
			value, err := arguments[index+1].Int32()
			if err != nil {
				return jvm.VoidValue(), err
			}
			event[index] = value
		}
		listenerEvents = append(listenerEvents, event)
		data, err := arguments[5].Reference()
		if err != nil {
			return jvm.VoidValue(), err
		}
		if data != nil {
			return jvm.VoidValue(), errors.New("event data is not null")
		}
		if event[2] == KeyFire {
			accepted = runtimeComponentText(field)
			form.Fields[componentShownField] = jvm.IntValue(0)
			runtime.clearActiveVendorForm(form)
		}
		// The observed guest listener returns false after both branches.
		return jvm.IntValue(0), nil
	}); err != nil {
		t.Fatal(err)
	}
	cardEvents := 0
	if err := session.Client.JVM().RegisterNative("test/UnderlyingCard", "keyNotify", "(II)Z", func(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
		cardEvents++
		return jvm.IntValue(0), nil
	}); err != nil {
		t.Fatal(err)
	}
	runtime.displayCards = append(runtime.displayCards, newWidget("test/UnderlyingCard"))

	input, err := session.TextInput(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := input.Commit(context.Background(), "새 이름🙂"); err != nil {
		t.Fatal(err)
	}
	for _, eventType := range []int32{KeyPressed, KeyReleased} {
		if err := runtime.dispatchKeyToCards(eventType, KeyFire); err != nil {
			t.Fatal(err)
		}
	}
	if accepted != "새 이름🙂" {
		t.Fatalf("guest accepted %q", accepted)
	}
	if len(listenerEvents) != 1 || listenerEvents[0] != [4]int32{3, KeyPressed, KeyFire, 0} {
		t.Fatalf("listener events = %v", listenerEvents)
	}
	if cardEvents != 0 {
		t.Fatalf("underlying card received %d events from a dismissed form", cardEvents)
	}
	if got := widgetInt(t, form, componentShownField); got != 0 {
		t.Fatalf("guest acceptance left form shown = %d", got)
	}
	if _, err := session.TextInput(context.Background()); !errors.Is(err, backend.ErrNoTextInput) {
		t.Fatalf("TextInput after guest dismissal error = %v", err)
	}
}

func TestOpeningReleaseStaysWithTheCardThatOwnedItsPress(t *testing.T) {
	client, runtime := newTestRuntime(t)
	form := newWidget(runtimeGFormClass)
	field := newWidget(runtimeGTextFieldClass)
	listener := newWidget("test/VendorFieldListener")
	field.Fields[componentEventListenerField] = jvm.ReferenceValue(listener)
	form.Native = []*jvm.Object{field}
	listenerEvents := 0
	if err := client.JVM().RegisterNative(listener.ClassName, "eventNotify", "(IIIILjava/lang/Object;)Z", func(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
		listenerEvents++
		return jvm.IntValue(0), nil
	}); err != nil {
		t.Fatal(err)
	}
	cardEvents := 0
	if err := client.JVM().RegisterNative("test/OpeningCard", "keyNotify", "(II)Z", func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
		eventType, _ := arguments[1].Int32()
		if eventType == KeyPressed {
			callVendorFormMethod(t, runtime, client.JVM(), form, "show")
		}
		cardEvents++
		return jvm.IntValue(0), nil
	}); err != nil {
		t.Fatal(err)
	}
	runtime.displayCards = append(runtime.displayCards, newWidget("test/OpeningCard"))

	for _, eventType := range []int32{KeyPressed, KeyReleased} {
		if err := runtime.dispatchKeyToCards(eventType, KeyFire); err != nil {
			t.Fatal(err)
		}
	}
	if listenerEvents != 0 {
		t.Fatalf("new form received %d events from the opening key", listenerEvents)
	}
	if cardEvents != 2 {
		t.Fatalf("opening card received %d events, want press and release", cardEvents)
	}
	if _, active := runtime.activeVendorTextInput(); !active {
		t.Fatal("opening release dismissed the newly shown field")
	}
}

func TestOwnedReleaseDoesNotReachAReplacementVendorLifecycle(t *testing.T) {
	for _, probe := range []struct {
		name    string
		replace func(*testing.T, *initializationRuntime, *jvm.VM, *jvm.Object)
	}{
		{
			name: "another form",
			replace: func(t *testing.T, runtime *initializationRuntime, vm *jvm.VM, _ *jvm.Object) {
				otherForm := newWidget(runtimeGFormClass)
				otherField := newWidget(runtimeGTextFieldClass)
				otherField.Fields[componentEventListenerField] = jvm.ReferenceValue(newWidget("test/ReplacementListener"))
				otherForm.Native = []*jvm.Object{otherField}
				callVendorFormMethod(t, runtime, vm, otherForm, "show")
			},
		},
		{
			name: "same form reshown",
			replace: func(t *testing.T, runtime *initializationRuntime, vm *jvm.VM, form *jvm.Object) {
				callVendorFormMethod(t, runtime, vm, form, "hide")
				callVendorFormMethod(t, runtime, vm, form, "show")
			},
		},
	} {
		t.Run(probe.name, func(t *testing.T) {
			session, form, _, listener := shownVendorTextField(t, "old")
			runtime := session.Client.runtime
			currentEvents := 0
			if err := session.Client.JVM().RegisterNative(listener.ClassName, "eventNotify", "(IIIILjava/lang/Object;)Z", func(_ *jvm.VM, arguments []jvm.Value) (jvm.Value, error) {
				currentEvents++
				eventType, _ := arguments[2].Int32()
				if eventType == KeyPressed {
					probe.replace(t, runtime, session.Client.JVM(), form)
				}
				return jvm.IntValue(0), nil
			}); err != nil {
				t.Fatal(err)
			}
			replacementEvents := 0
			if err := session.Client.JVM().RegisterNative("test/ReplacementListener", "eventNotify", "(IIIILjava/lang/Object;)Z", func(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
				replacementEvents++
				return jvm.IntValue(0), nil
			}); err != nil {
				t.Fatal(err)
			}
			for _, eventType := range []int32{KeyPressed, KeyReleased} {
				if err := runtime.dispatchKeyToCards(eventType, KeyFire); err != nil {
					t.Fatal(err)
				}
			}
			if currentEvents != 1 || replacementEvents != 0 {
				t.Fatalf("events before/after lifecycle replacement = %d/%d", currentEvents, replacementEvents)
			}
		})
	}
}

func TestVendorKeyReleaseDoesNotReachReplacementListener(t *testing.T) {
	session, _, field, listener := shownVendorTextField(t, "old")
	runtime := session.Client.runtime
	replacement := newWidget("test/ReplacementFieldListener")
	deliveries := 0
	if err := session.Client.JVM().RegisterNative(replacement.ClassName, "eventNotify", "(IIIILjava/lang/Object;)Z", func(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) { deliveries++; return jvm.IntValue(0), nil }); err != nil {
		t.Fatal(err)
	}
	if err := session.Client.JVM().RegisterNative(listener.ClassName, "eventNotify", "(IIIILjava/lang/Object;)Z", func(_ *jvm.VM, _ []jvm.Value) (jvm.Value, error) {
		_, err := runtimeComponentSetEventListener(runtime, session.Client.JVM(), []jvm.Value{jvm.ReferenceValue(field), jvm.ReferenceValue(replacement), jvm.ReferenceValue(nil)})
		return jvm.IntValue(0), err
	}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []int32{KeyPressed, KeyReleased} {
		if err := runtime.dispatchKeyToCards(kind, KeyFire); err != nil {
			t.Fatal(err)
		}
	}
	if deliveries != 0 {
		t.Fatalf("replacement listener received %d events without its own press", deliveries)
	}
}

func TestVendorReleaseErrorClearsKeyOwner(t *testing.T) {
	session, form, _, listener := shownVendorTextField(t, "old")
	runtime := session.Client.runtime
	failure := errors.New("release callback failed")
	if err := session.Client.JVM().RegisterNative(listener.ClassName, "eventNotify", "(IIIILjava/lang/Object;)Z", func(_ *jvm.VM, args []jvm.Value) (jvm.Value, error) {
		kind, _ := args[2].Int32()
		if kind == KeyReleased {
			callVendorFormMethod(t, runtime, session.Client.JVM(), form, "hide")
			return jvm.VoidValue(), failure
		}
		return jvm.IntValue(0), nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.dispatchKeyToVendorForm(KeyPressed, KeyFire); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.dispatchKeyToVendorForm(KeyReleased, KeyFire); !errors.Is(err, failure) {
		t.Fatalf("release error = %v", err)
	}
	if handled, err := runtime.dispatchKeyToVendorForm(KeyPressed, KeyFire); handled || err != nil {
		t.Fatalf("next card press handled=%v error=%v", handled, err)
	}
}
