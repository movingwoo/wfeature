package ktf

import (
	"errors"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
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
