package ktf

import (
	"errors"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/textinput"
)

// newWidget makes the kind of receiver an lwc constructor is handed: a bound
// object with a field map and nothing in it.
func newWidget(class string) *jvm.Object {
	return &jvm.Object{ClassName: class, Fields: make(map[string]jvm.Value)}
}

func widgetInt(t *testing.T, object *jvm.Object, key string) int32 {
	t.Helper()
	value, ok := object.Fields[key]
	if !ok {
		t.Fatalf("%s is not set", key)
	}
	number, err := value.Int32()
	if err != nil {
		t.Fatal(err)
	}
	return number
}

// A dialog's type decides its default timeout: the one that closes itself
// waits three seconds, and the two that wait for a person do not wait at all.
func TestDialogTypeSetsItsDefaultTimeout(t *testing.T) {
	for _, probe := range []struct {
		kind int32
		want int32
	}{
		{runtimeDialogTypeNone, runtimeDialogDefaultMillis},
		{runtimeDialogTypeOK, runtimeDialogInfinite},
		{runtimeDialogTypeOKCancel, runtimeDialogInfinite},
	} {
		dialog := newWidget(runtimeDialogComponentClass)
		if _, err := runtimeDialogConstructor(nil, nil, []jvm.Value{jvm.ReferenceValue(dialog), jvm.IntValue(probe.kind)}); err != nil {
			t.Fatalf("constructor with type %d: %v", probe.kind, err)
		}
		if got := widgetInt(t, dialog, runtimeDialogTimeoutField); got != probe.want {
			t.Fatalf("type %d default timeout = %d, want %d", probe.kind, got, probe.want)
		}
		// Nothing has happened to a dialog that has only been built, and the
		// resting value for that is not a button.
		if got := widgetInt(t, dialog, runtimeDialogActionField); got != -2 {
			t.Fatalf("type %d resting action = %d, want -2", probe.kind, got)
		}
	}
}

// A type outside the three the specification names is refused, as a guest
// exception the title can catch rather than as a Host failure.
func TestDialogRefusesAnUnknownType(t *testing.T) {
	dialog := newWidget(runtimeDialogComponentClass)
	_, err := runtimeDialogConstructor(nil, nil, []jvm.Value{jvm.ReferenceValue(dialog), jvm.IntValue(7)})
	var guest *jvm.GuestException
	if !errors.As(err, &guest) {
		t.Fatalf("constructor with type 7 error = %v, want a guest exception", err)
	}
	if guest.Object.ClassName != "java/lang/IllegalArgumentException" {
		t.Fatalf("threw %s, want java/lang/IllegalArgumentException", guest.Object.ClassName)
	}
}

// Asking a button-less dialog to stay up for ever turns it into the one with
// an OK button, which is the one place the specification lets a timeout change
// a dialog's type.
func TestDialogWithoutButtonsGainsOneWhenItNeverTimesOut(t *testing.T) {
	dialog := newWidget(runtimeDialogComponentClass)
	if _, err := runtimeDialogConstructor(nil, nil, []jvm.Value{jvm.ReferenceValue(dialog), jvm.IntValue(runtimeDialogTypeNone)}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeDialogSetTimeout(nil, nil, []jvm.Value{jvm.ReferenceValue(dialog), jvm.IntValue(runtimeDialogInfinite)}); err != nil {
		t.Fatal(err)
	}
	if got := widgetInt(t, dialog, runtimeDialogTypeField); got != runtimeDialogTypeOK {
		t.Fatalf("type after an infinite timeout = %d, want %d", got, runtimeDialogTypeOK)
	}
}

// The three answers doModal gives, and that it records each one where
// getActionState reads it. See runtime_library.go for why the last of them is
// a guess and testdata/wipi_java_stubs.txt for it being recorded as one.
func TestDialogAnswersFromItsType(t *testing.T) {
	_, runtime := newTestRuntime(t)
	for _, probe := range []struct {
		kind int32
		want int32
	}{
		{runtimeDialogTypeNone, runtimeDialogTimeout},
		{runtimeDialogTypeOK, runtimeDialogOK},
		{runtimeDialogTypeOKCancel, runtimeDialogOK},
	} {
		dialog := newWidget(runtimeDialogComponentClass)
		if _, err := runtimeDialogConstructor(runtime, nil, []jvm.Value{jvm.ReferenceValue(dialog), jvm.IntValue(probe.kind)}); err != nil {
			t.Fatal(err)
		}
		result, err := runtimeDialogDoModal(runtime, nil, []jvm.Value{jvm.ReferenceValue(dialog)})
		if err != nil {
			t.Fatalf("doModal on type %d: %v", probe.kind, err)
		}
		if got, _ := result.Int32(); got != probe.want {
			t.Fatalf("doModal on type %d = %d, want %d", probe.kind, got, probe.want)
		}
		state, err := runtimeDialogGetInt(runtimeDialogActionField)(runtime, nil, []jvm.Value{jvm.ReferenceValue(dialog)})
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := state.Int32(); got != probe.want {
			t.Fatalf("getActionState on type %d = %d, want %d", probe.kind, got, probe.want)
		}
	}
}

// A progress bar holds its value inside the bar and on a step boundary, and
// answers what it settled on rather than what it was asked for.
func TestProgressValueStaysInsideTheBarAndOnAStep(t *testing.T) {
	bar := newWidget(runtimeProgressComponentClass)
	if _, err := runtimeProgressConstructor(nil, nil, []jvm.Value{
		jvm.ReferenceValue(bar), jvm.IntValue(0), jvm.IntValue(100),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeProgressSetStep(nil, nil, []jvm.Value{jvm.ReferenceValue(bar), jvm.IntValue(10)}); err != nil {
		t.Fatal(err)
	}
	for _, probe := range []struct{ asked, want int32 }{{47, 40}, {-5, 0}, {1000, 100}} {
		result, err := runtimeProgressSetValue(nil, nil, []jvm.Value{jvm.ReferenceValue(bar), jvm.IntValue(probe.asked)})
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := result.Int32(); got != probe.want {
			t.Fatalf("setValue(%d) = %d, want %d", probe.asked, got, probe.want)
		}
	}
}

// Changing the step moves the value onto the new one, which is what the
// specification says happens to a value that no longer sits on a boundary.
func TestProgressStepMovesTheValueOntoIt(t *testing.T) {
	bar := newWidget(runtimeProgressComponentClass)
	if _, err := runtimeProgressConstructor(nil, nil, []jvm.Value{
		jvm.ReferenceValue(bar), jvm.IntValue(1), jvm.IntValue(100),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeProgressSetValue(nil, nil, []jvm.Value{jvm.ReferenceValue(bar), jvm.IntValue(47)}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtimeProgressSetStep(nil, nil, []jvm.Value{jvm.ReferenceValue(bar), jvm.IntValue(25)}); err != nil {
		t.Fatal(err)
	}
	if got := widgetInt(t, bar, "value:I"); got != 25 {
		t.Fatalf("value after setStep(25) = %d, want 25", got)
	}
}

// A maximum at or below zero and a step outside the bar are both refused, as
// guest exceptions rather than as Host failures.
func TestProgressRefusesAMaximumAndAStepItCannotHold(t *testing.T) {
	bar := newWidget(runtimeProgressComponentClass)
	_, err := runtimeProgressConstructor(nil, nil, []jvm.Value{
		jvm.ReferenceValue(bar), jvm.IntValue(0), jvm.IntValue(0),
	})
	var guest *jvm.GuestException
	if !errors.As(err, &guest) {
		t.Fatalf("constructor with maximum 0 error = %v, want a guest exception", err)
	}
	if _, err := runtimeProgressConstructor(nil, nil, []jvm.Value{
		jvm.ReferenceValue(bar), jvm.IntValue(0), jvm.IntValue(10),
	}); err != nil {
		t.Fatal(err)
	}
	_, err = runtimeProgressSetStep(nil, nil, []jvm.Value{jvm.ReferenceValue(bar), jvm.IntValue(11)})
	if !errors.As(err, &guest) {
		t.Fatalf("setStep(11) on a bar of 10 error = %v, want a guest exception", err)
	}
}

// URL.find refuses the socket as the exception the specification declares,
// which is the one a title using the network already has a catch for.
func TestURLFindRefusesWithTheDeclaredException(t *testing.T) {
	client, runtime := newTestRuntime(t)
	_, err := runtimeURLFind(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(client.JVM().NewString("socket://host:80"))})
	var guest *jvm.GuestException
	if !errors.As(err, &guest) {
		t.Fatalf("find error = %v, want a guest exception", err)
	}
	if guest.Object.ClassName != runtimeSchemeExceptionClass {
		t.Fatalf("threw %s, want %s", guest.Object.ClassName, runtimeSchemeExceptionClass)
	}
}

// A text component owns the input method handler the specification declares
// protected on it, and three local titles take it off the component rather
// than asking for one. A component whose field is null hands the title
// nothing to register its listener on, so what is pinned is that every
// constructor in the family leaves one there.
func TestEveryTextComponentIsBuiltWithAnInputHandler(t *testing.T) {
	client, runtime := newTestRuntime(t)
	for _, probe := range []struct {
		name        string
		class       string
		build       runtimeJavaImplementation
		arguments   []jvm.Value
		constraint  int32
		hasArgument bool
	}{
		{name: "TextComponent", class: runtimeTextComponentClass, build: runtimeTextComponentConstructor},
		{
			name: "TextFieldComponent", class: runtimeTextFieldComponentClass,
			build:     runtimeTextComponentConstructorWithText,
			arguments: []jvm.Value{jvm.ReferenceValue(client.JVM().NewString("hi")), jvm.IntValue(4)},
			// The field's constraint is the handler's mode: a title that asks
			// for digits has to get a handler that was built for digits.
			constraint: 4, hasArgument: true,
		},
		{
			name: "GTextField", class: runtimeGTextFieldClass,
			build:     runtimeGTextFieldConstructor,
			arguments: []jvm.Value{jvm.ReferenceValue(nil), jvm.ReferenceValue(client.JVM().NewString("hi")), jvm.IntValue(8)},
		},
	} {
		component := newWidget(probe.class)
		arguments := append([]jvm.Value{jvm.ReferenceValue(component)}, probe.arguments...)
		if _, err := probe.build(runtime, client.JVM(), arguments); err != nil {
			t.Fatalf("%s constructor: %v", probe.name, err)
		}
		value, ok := component.Fields[componentInputHandlerField]
		if !ok {
			t.Fatalf("%s was built without an input handler", probe.name)
		}
		handler, err := value.Reference()
		if err != nil {
			t.Fatal(err)
		}
		if handler == nil || handler.ClassName != runtimeInputMethodHandlerClass {
			t.Fatalf("%s input handler = %v", probe.name, handler)
		}
		if probe.hasArgument {
			if got := widgetInt(t, handler, "mode:I"); got != probe.constraint {
				t.Fatalf("%s handler mode = %d, want the component's constraint %d", probe.name, got, probe.constraint)
			}
		}
		// The listener a title registers on the handler is kept, because a
		// handler that cannot be given one has told the title its own input
		// will never work.
		listener := newWidget("test/Listener")
		if _, err := runtimeComponentSetField("InputMethodHandler.setInputMethodListener", inputMethodListenerField)(
			runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(handler), jvm.ReferenceValue(listener)}); err != nil {
			t.Fatalf("%s setInputMethodListener: %v", probe.name, err)
		}
		kept, err := handler.Fields[inputMethodListenerField].Reference()
		if err != nil {
			t.Fatal(err)
		}
		if kept != listener {
			t.Fatalf("%s handler kept %v, want the listener it was given", probe.name, kept)
		}
	}
}

// A button is a string and a picture, and a title that builds one reads both
// back off it.
func TestButtonKeepsItsStringAndItsImage(t *testing.T) {
	client, runtime := newTestRuntime(t)
	button := newWidget(runtimeButtonComponentClass)
	text := client.JVM().NewString("OK")
	image := newWidget("org/kwis/msp/lcdui/Image")
	if _, err := runtimeButtonConstructor(runtime, client.JVM(), []jvm.Value{
		jvm.ReferenceValue(button), jvm.ReferenceValue(text), jvm.ReferenceValue(image),
	}); err != nil {
		t.Fatalf("ButtonComponent constructor: %v", err)
	}
	answered, err := runtimeTextComponentGetString(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(button)})
	if err != nil {
		t.Fatal(err)
	}
	object, err := answered.Reference()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := jvm.StringText(object); got != "OK" {
		t.Fatalf("getString = %q, want the string it was built with", got)
	}
	answered, err = runtimeComponentField(componentImageField)(runtime, client.JVM(), []jvm.Value{jvm.ReferenceValue(button)})
	if err != nil {
		t.Fatal(err)
	}
	if object, err = answered.Reference(); err != nil {
		t.Fatal(err)
	}
	if object != image {
		t.Fatalf("getImage = %v, want the image it was built with", object)
	}
}

// The indexed add puts a child where it was asked for and the replacing set
// puts one over the child that is there. What a title reads back is the order,
// so the order is what is pinned — and a position nothing occupies is refused
// rather than growing the container behind the caller's back.
func TestContainerPlacesChildrenWhereItIsAsked(t *testing.T) {
	client, runtime := newTestRuntime(t)
	container := newWidget(runtimeContainerComponentClass)
	first, second, third := newWidget("test/A"), newWidget("test/B"), newWidget("test/C")

	for _, child := range []*jvm.Object{first, second} {
		if _, err := runtimeComponentAddComponent(runtime, client.JVM(), []jvm.Value{
			jvm.ReferenceValue(container), jvm.ReferenceValue(child),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runtimeComponentAddComponentAt(runtime, client.JVM(), []jvm.Value{
		jvm.ReferenceValue(container), jvm.IntValue(1), jvm.ReferenceValue(third),
	}); err != nil {
		t.Fatalf("addComponent at an index: %v", err)
	}
	children, _ := container.Native.([]*jvm.Object)
	if len(children) != 3 || children[0] != first || children[1] != third || children[2] != second {
		t.Fatalf("children after the indexed add = %v", children)
	}
	if _, err := runtimeComponentSetComponentAt(runtime, client.JVM(), []jvm.Value{
		jvm.ReferenceValue(container), jvm.IntValue(0), jvm.ReferenceValue(second),
	}); err != nil {
		t.Fatalf("setComponent: %v", err)
	}
	children, _ = container.Native.([]*jvm.Object)
	if len(children) != 3 || children[0] != second {
		t.Fatalf("children after the replace = %v", children)
	}
	if _, err := runtimeComponentSetComponentAt(runtime, client.JVM(), []jvm.Value{
		jvm.ReferenceValue(container), jvm.IntValue(3), jvm.ReferenceValue(first),
	}); err == nil {
		t.Fatal("setComponent past the end was accepted")
	}
}

// The vendor form takes a child with the rectangle it goes in. Nothing lays a
// container out here, so the rectangle is dropped and the child still has to
// arrive — a form that swallowed it would leave a title walking an empty
// screen it believes it filled.
func TestVendorFormAddsTheChildAndDropsTheRectangle(t *testing.T) {
	client, runtime := newTestRuntime(t)
	form := newWidget(runtimeGFormComponentClass)
	child := newWidget(runtimeButtonComponentClass)
	answered, err := runtimeGFormAddComponent(runtime, client.JVM(), []jvm.Value{
		jvm.ReferenceValue(form), jvm.ReferenceValue(child),
		jvm.IntValue(4), jvm.IntValue(8), jvm.IntValue(60), jvm.IntValue(20),
	})
	if err != nil {
		t.Fatalf("GFormComponent.addComponent: %v", err)
	}
	if index, err := answered.Int32(); err != nil || index != 0 {
		t.Fatalf("addComponent answered %v (err %v), want the child's index", answered, err)
	}
	children, _ := form.Native.([]*jvm.Object)
	if len(children) != 1 || children[0] != child {
		t.Fatalf("form children = %v", children)
	}
}

// The one vendor class outside the toolkit answers a single instance and the
// handset's subscriber number. Two calls that ask this handset who it is must
// not disagree, so the number is the one every other such question answers.
func TestDeviceInformationAnswersOneInstanceAndTheHandsetNumber(t *testing.T) {
	client, runtime := newTestRuntime(t)
	first, err := runtimeDMInfoInstance(runtime, client.JVM(), nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runtimeDMInfoInstance(runtime, client.JVM(), nil)
	if err != nil {
		t.Fatal(err)
	}
	one, _ := first.Reference()
	other, _ := second.Reference()
	if one == nil || one != other {
		t.Fatalf("getDMInfo answered %v then %v", one, other)
	}
	answered, err := runtimeDMInfoHandsetNumber(runtime, client.JVM(), []jvm.Value{first})
	if err != nil {
		t.Fatal(err)
	}
	object, err := answered.Reference()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := jvm.StringText(object); got != HandsetNumber() {
		t.Fatalf("gethandsetMIN = %q, want the handset number %q", got, HandsetNumber())
	}
}

// The box declares its own editing calls, and what they have to get right is
// the range: a position past the end appends rather than failing, and a delete
// wider than the text is clipped. The caller is the handset's own input method
// working against a caret this platform does not move, so a range it computed
// is not the title's mistake.
func TestTextBoxEditsItsTextAndClipsTheRange(t *testing.T) {
	client, runtime := newTestRuntime(t)
	box := newWidget(runtimeTextBoxComponentClass)
	if _, err := runtimeTextComponentConstructorWithText(runtime, client.JVM(), []jvm.Value{
		jvm.ReferenceValue(box), jvm.ReferenceValue(client.JVM().NewString("abcd")), jvm.IntValue(0),
	}); err != nil {
		t.Fatal(err)
	}

	insert := func(text string, position int32) {
		t.Helper()
		units := make([]jvm.Value, 0, len(text))
		for _, symbol := range text {
			units = append(units, jvm.IntValue(int32(symbol)))
		}
		array := newIntArray(t, client, int32(len(units)))
		array.ClassName = "[C"
		if err := jvm.SetArrayRange(array, 0, units); err != nil {
			t.Fatal(err)
		}
		if _, err := runtimeTextComponentInsert(runtime, client.JVM(), []jvm.Value{
			jvm.ReferenceValue(box), jvm.ReferenceValue(array),
			jvm.IntValue(0), jvm.IntValue(int32(len(units))), jvm.IntValue(position),
		}); err != nil {
			t.Fatalf("insert(%q at %d): %v", text, position, err)
		}
	}
	insert("XY", 2)
	if got := runtimeComponentText(box); got != "abXYcd" {
		t.Fatalf("after inserting at 2 the text is %q", got)
	}
	insert("Z", 99)
	if got := runtimeComponentText(box); got != "abXYcdZ" {
		t.Fatalf("an insert past the end gave %q, want it appended", got)
	}

	if _, err := runtimeTextComponentDelete(runtime, client.JVM(), []jvm.Value{
		jvm.ReferenceValue(box), jvm.IntValue(2), jvm.IntValue(2),
	}); err != nil {
		t.Fatal(err)
	}
	if got := runtimeComponentText(box); got != "abcdZ" {
		t.Fatalf("after delete(2, 2) the text is %q", got)
	}
	if _, err := runtimeTextComponentDelete(runtime, client.JVM(), []jvm.Value{
		jvm.ReferenceValue(box), jvm.IntValue(3), jvm.IntValue(99),
	}); err != nil {
		t.Fatalf("a delete past the end was refused: %v", err)
	}
	if got := runtimeComponentText(box); got != "abc" {
		t.Fatalf("after a clipped delete the text is %q", got)
	}

	// The limit the component was given clips an insert rather than being
	// grown past.
	box.Fields[componentMaxLengthField] = jvm.IntValue(4)
	insert("WXYZ", 0)
	if got := runtimeComponentText(box); got != "WXYZ" {
		t.Fatalf("an insert past the limit gave %q, want it clipped to four", got)
	}
}

// A container answers the children it was given. The adds and the reads used
// to keep two different types in the same field, so every container reported
// itself empty however many components a title had put in it — and a title
// that walks its own form back is reading the answer to this.
func TestContainerAnswersTheChildrenItWasGiven(t *testing.T) {
	container := newWidget(runtimeContainerComponentClass)
	first := newWidget(runtimeButtonComponentClass)
	second := newWidget(runtimeTextFieldComponentClass)
	for index, child := range []*jvm.Object{first, second} {
		got, err := runtimeComponentAddComponent(nil, nil, []jvm.Value{
			jvm.ReferenceValue(container), jvm.ReferenceValue(child),
		})
		if err != nil {
			t.Fatalf("addComponent %d: %v", index, err)
		}
		position, err := got.Int32()
		if err != nil {
			t.Fatal(err)
		}
		if position != int32(index) {
			t.Fatalf("addComponent %d answered %d", index, position)
		}
	}

	count, err := runtimeComponentCount(nil, nil, []jvm.Value{jvm.ReferenceValue(container)})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := count.Int32(); got != 2 {
		t.Fatalf("getNumberOfComponent = %d, want 2", got)
	}

	at, err := runtimeComponentAt(nil, nil, []jvm.Value{jvm.ReferenceValue(container), jvm.IntValue(1)})
	if err != nil {
		t.Fatal(err)
	}
	object, err := at.Reference()
	if err != nil {
		t.Fatal(err)
	}
	if object != second {
		t.Fatalf("getComponent(1) = %v, want the second child", object)
	}

	index, err := runtimeComponentIndexOf(nil, nil, []jvm.Value{
		jvm.ReferenceValue(container), jvm.ReferenceValue(first),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := index.Int32(); got != 0 {
		t.Fatalf("getIndexOf(first) = %d, want 0", got)
	}

	if _, err := runtimeComponentRemoveComponent(nil, nil, []jvm.Value{
		jvm.ReferenceValue(container), jvm.ReferenceValue(first),
	}); err != nil {
		t.Fatal(err)
	}
	count, err = runtimeComponentCount(nil, nil, []jvm.Value{jvm.ReferenceValue(container)})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := count.Int32(); got != 1 {
		t.Fatalf("after removeComponent getNumberOfComponent = %d, want 1", got)
	}
}

// A card that hands its own text field a key gets the handset's keypad. The
// title this is for forwards the key and reads the string back on the very
// next call, so what it draws is whatever the multi-tap automaton has typed.
func TestATextComponentTypesTheKeysItsCardForwards(t *testing.T) {
	// A manual clock, because multi-tap is a question about guest time: the
	// same key twice inside the commit delay cycles the character it just
	// produced, and a test that let the wall clock answer would be asserting
	// how fast it ran.
	clock := NewManualClock(time.Time{})
	runtime := &initializationRuntime{client: &Client{clock: clock}}
	field := newWidget(runtimeTextFieldComponentClass)
	vm := jvm.New(nil, jvm.Options{})

	press := func(key int32) int32 {
		t.Helper()
		result, err := runtimeTextComponentKeyNotify(runtime, vm, []jvm.Value{
			jvm.ReferenceValue(field), jvm.IntValue(KeyPressed), jvm.IntValue(key),
		})
		if err != nil {
			t.Fatalf("keyNotify(%d): %v", key, err)
		}
		answer, err := result.Int32()
		if err != nil {
			t.Fatal(err)
		}
		return answer
	}

	// "4" once is 'g', "4" again inside the commit delay cycles to 'h'; a
	// different key starts its own character.
	if got := press('4'); got != 0 {
		t.Fatalf("a digit answered %d, want 0 — the component took it", got)
	}
	press('4')
	press('6')
	if got := runtimeComponentText(field); got != "hm" {
		t.Fatalf("the field holds %q, want %q", got, "hm")
	}

	// Past the commit delay the same key starts a character instead of
	// cycling one, which is what makes a doubled letter typable.
	clock.Advance(2 * textinput.CommitDelay)
	press('6')
	if got := runtimeComponentText(field); got != "hmm" {
		t.Fatalf("after the commit delay the field holds %q, want %q", got, "hmm")
	}

	// The clear key deletes, and the two keys the keypad spends on editing are
	// the component's too.
	press(KeyClear)
	press(KeyClear)
	if got := runtimeComponentText(field); got != "h" {
		t.Fatalf("after clear the field holds %q, want %q", got, "h")
	}

	// Everything else travels on: a screen with a field on it still has to be
	// able to leave.
	for _, key := range []int32{KeyUp, KeyDown, KeyLeft, KeyRight, KeyFire, KeyLeftSoft} {
		if got := press(key); got != 1 {
			t.Fatalf("key %d answered %d, want 1 — the component did not take it", key, got)
		}
	}
	// A release is not a press: multi-tap counts presses, and cycling on the
	// release would type every character twice.
	result, err := runtimeTextComponentKeyNotify(runtime, vm, []jvm.Value{
		jvm.ReferenceValue(field), jvm.IntValue(KeyReleased), jvm.IntValue('4'),
	})
	if err != nil {
		t.Fatal(err)
	}
	if answer, _ := result.Int32(); answer != 1 {
		t.Fatalf("a release answered %d, want 1", answer)
	}
	if got := runtimeComponentText(field); got != "h" {
		t.Fatalf("a release typed: the field holds %q", got)
	}

	// The value is the component's, not the editor's: a title that sets the
	// string is what the next key edits.
	if _, err := runtimeComponentSetField("setString", componentTextField)(runtime, vm, []jvm.Value{
		jvm.ReferenceValue(field), jvm.ReferenceValue(vm.NewString("ab")),
	}); err != nil {
		t.Fatal(err)
	}
	press('2')
	if got := runtimeComponentText(field); got != "aba" {
		t.Fatalf("after setString the field holds %q, want %q", got, "aba")
	}
}

// The limit a title sets is the limit typing obeys.
func TestTypingStopsAtTheMaximumLength(t *testing.T) {
	runtime := &initializationRuntime{client: &Client{}}
	vm := jvm.New(nil, jvm.Options{})
	field := newWidget(runtimeTextFieldComponentClass)
	if _, err := runtimeTextComponentSetMaxLength(runtime, vm, []jvm.Value{
		jvm.ReferenceValue(field), jvm.IntValue(2),
	}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []int32{'2', '3', '4', '5'} {
		if _, err := runtimeTextComponentKeyNotify(runtime, vm, []jvm.Value{
			jvm.ReferenceValue(field), jvm.IntValue(KeyPressed), jvm.IntValue(key),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if got := runtimeComponentText(field); len([]rune(got)) != 2 {
		t.Fatalf("the field holds %q, want two characters", got)
	}
}
