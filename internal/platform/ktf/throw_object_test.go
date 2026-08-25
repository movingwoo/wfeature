package ktf

import (
	"errors"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// The exception hierarchy is published rather than left to the fallback that
// gives every unknown name Object's parent and Object's methods. Without it a
// title's `new Exception()` resolves to `Object.<init>` and `getMessage` on
// what it caught resolves to nothing, so the title stops inside its own error
// handling with no sign of what it was handling.
func TestTheExceptionHierarchyIsPublished(t *testing.T) {
	_, runtime := newTestRuntime(t)
	for name, parent := range map[string]string{
		"java/lang/Exception":            "java/lang/Throwable",
		"java/io/IOException":            "java/lang/Exception",
		"java/lang/NullPointerException": "java/lang/RuntimeException",
	} {
		definition, published := runtimeJavaClasses[name]
		if !published {
			t.Fatalf("%s is not published", name)
		}
		if definition.superName != parent {
			t.Fatalf("%s extends %s, want %s", name, definition.superName, parent)
		}
		class, err := runtime.ensureJavaClass(name)
		if err != nil {
			t.Fatalf("resolve %s: %v", name, err)
		}
		method, ok, err := runtime.client.vm.FindAOTMethod(class, "<init>", "()V")
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatalf("%s has no no-argument constructor", name)
		}
		if _, owner, err := runtime.readAOTMethod(method.Address); err != nil {
			t.Fatal(err)
		} else if owner != class {
			t.Fatalf("%s's constructor is declared by another class", name)
		}
	}
}

// Slot 2 of the callbacks table throws an exception the guest built itself,
// which is what a title's own `throw e` compiles to. The slot used to be zero,
// and a title read that zero as a method pointer and called it.
func TestThrowingAGuestExceptionObjectRefusesAWordThatIsNotOne(t *testing.T) {
	_, runtime := newTestRuntime(t)
	context := armcore.NewContext()
	context.Registers[0] = ImageBase
	_, err := runtime.throwAOTExceptionObject(armcore.NewThread(context))
	if err == nil {
		t.Fatal("a word that is not an object was thrown")
	}
	if !strings.Contains(err.Error(), "not bound to a JVM object") {
		t.Fatalf("the refusal does not say what was wrong: %v", err)
	}
}

// Throwing a null is a NullPointerException, which is what the language says.
func TestThrowingANullExceptionRaisesANullPointerException(t *testing.T) {
	_, runtime := newTestRuntime(t)
	_, err := runtime.throwAOTExceptionObject(armcore.NewThread(armcore.NewContext()))
	if err == nil {
		t.Fatal("a null throw was accepted")
	}
	var uncaught *UncaughtAOTException
	if !errors.As(err, &uncaught) {
		t.Fatalf("a null throw with no handler is not reported as uncaught: %v", err)
	}
	if uncaught.Exception == nil || uncaught.Exception.Object.ClassName != "java/lang/NullPointerException" {
		t.Fatalf("a null throw raised %v", uncaught.Exception)
	}
}
