package lgt

import (
	"errors"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// A try region is entered, armed at a point the guest can be sent back to, and
// left. The buffer is the platform's own and the same one comes back at the
// same depth, because a title runs through a region far too often for one
// allocation per entry.
func TestJavaTryRegionIsEnteredArmedAndLeft(t *testing.T) {
	client := fixtureClient(t)
	thread := armcore.NewThread(armcore.NewContext())
	if err := thread.SetRegister(armcore.RegisterLR, 0x1000); err != nil {
		t.Fatal(err)
	}

	buffer, err := client.enterJavaTry()
	if err != nil {
		t.Fatalf("enterJavaTry() error = %v", err)
	}
	if buffer == 0 {
		t.Fatal("enterJavaTry() answered no buffer")
	}
	if err := client.armJavaTry(thread, buffer); err != nil {
		t.Fatalf("armJavaTry() error = %v", err)
	}
	if answer, _ := thread.Register(0); answer != 0 {
		t.Errorf("the jump call answered %#x on the way in, want 0", answer)
	}
	if err := client.leaveJavaTry(); err != nil {
		t.Fatalf("leaveJavaTry() error = %v", err)
	}
	if len(client.javaTry) != 0 {
		t.Errorf("%d regions are still open", len(client.javaTry))
	}

	again, err := client.enterJavaTry()
	if err != nil {
		t.Fatal(err)
	}
	if again != buffer {
		t.Errorf("the second region at the same depth got %#x, want the first's %#x", again, buffer)
	}
	if err := client.leaveJavaTry(); err != nil {
		t.Fatal(err)
	}
	if err := client.leaveJavaTry(); err == nil {
		t.Error("leaving a region that was never entered is not reported")
	}
	if err := client.armJavaTry(thread, 0xdeadbeef); err == nil {
		t.Error("arming a buffer this platform never handed out is not reported")
	}
}

// A throw with a region armed is a long jump: the guest comes back to where the
// jump call was made, with the exception where the answer would have been.
func TestJavaThrowJumpsBackToTheArmedPoint(t *testing.T) {
	client := fixtureClient(t)
	thread := armcore.NewThread(armcore.NewContext())
	const resume = 0x2000
	if err := thread.SetRegister(armcore.RegisterLR, resume); err != nil {
		t.Fatal(err)
	}
	if err := thread.SetRegister(4, 0x1234); err != nil {
		t.Fatal(err)
	}
	buffer, err := client.enterJavaTry()
	if err != nil {
		t.Fatal(err)
	}
	if err := client.armJavaTry(thread, buffer); err != nil {
		t.Fatal(err)
	}
	// The guest runs on and changes the registers the saved point held.
	if err := thread.SetRegister(4, 0x9999); err != nil {
		t.Fatal(err)
	}
	if err := thread.SetRegister(armcore.RegisterPC, 0x8000); err != nil {
		t.Fatal(err)
	}

	// A caught throw reports that it was caught, so nothing writes a result
	// over the exception the jump just put in the answer register.
	if err := client.throwJavaPlatform(thread, javaThrowNullClass, ""); !errors.Is(err, errJavaThrowHandled) {
		t.Fatalf("throwJavaPlatform() error = %v, want it reported as handled", err)
	}
	if err := javaThrowDelivered(client.throwJavaPlatform(thread, javaThrowNullClass, "")); err == nil {
		t.Error("a second throw with nothing armed is not reported")
	}
	resumed := thread.Context()
	if pc := resumed.PC(); pc != resume {
		t.Errorf("the guest resumes at %#x, want the armed point %#x", pc, resume)
	}
	if saved, _ := thread.Register(4); saved != 0x1234 {
		t.Errorf("r4 came back as %#x, want the saved %#x", saved, 0x1234)
	}
	object, _ := thread.Register(0)
	if object == 0 {
		t.Fatal("the jump answered no exception object")
	}
	class, ok := client.javaClassOfObject(object)
	if !ok || class.Name != javaThrowNullClass {
		t.Errorf("the exception is %v, want a %s", class, javaThrowNullClass)
	}
	if len(client.javaTry) != 0 {
		t.Errorf("%d regions are still open after the jump", len(client.javaTry))
	}
}

// With nothing armed, a throw is what stops the title, and it says which
// exception it was rather than failing at the next dereference.
func TestJavaUncaughtThrowNamesTheException(t *testing.T) {
	client := fixtureClient(t)
	thread := armcore.NewThread(armcore.NewContext())
	err := client.throwJavaPlatform(thread, javaThrowArrayClass, ": index 7 of 3")
	if err == nil {
		t.Fatal("an uncaught throw is not reported")
	}
	if !strings.Contains(err.Error(), javaThrowArrayClass) || !strings.Contains(err.Error(), "index 7 of 3") {
		t.Errorf("the failure is %q, want the class and what was out of bounds", err)
	}
}

// A region belongs to the call that opened it: once that call has returned,
// the registers it saved name a frame that no longer exists.
func TestJavaTryRegionsDoNotOutliveTheirCall(t *testing.T) {
	client := fixtureClient(t)
	client.javaCallDepth = 2
	if _, err := client.enterJavaTry(); err != nil {
		t.Fatal(err)
	}
	client.javaCallDepth = 1
	client.dropJavaTryFrames(client.javaCallDepth)
	if len(client.javaTry) != 0 {
		t.Errorf("%d regions survived the call that opened them", len(client.javaTry))
	}
}

// An exception the application had nothing left to catch with ends the
// callback it was thrown out of, and nothing else does. A fault, a slot this
// platform does not serve and a limit a Host imposes all still stop the run:
// the absorbed case is the one where the guest behaved exactly as written and
// the only question is who catches it.
func TestUncaughtCallbackEndsTheCallbackAndNothingElse(t *testing.T) {
	client := fixtureClient(t)
	thread := armcore.NewThread(armcore.NewContext())
	thrown := client.throwJavaPlatform(thread, javaThrowNullClass, ": drawImage")
	if thrown == nil {
		t.Fatal("an uncaught throw is not reported")
	}
	// It still reads as an LGT Java title stopping, because everything that
	// reports on one asks that question.
	if !errors.Is(thrown, ErrJavaAppUnsupported) {
		t.Errorf("the throw %q does not unwrap to the sentinel", thrown)
	}
	if err := client.absorbUncaughtCallback("paint", thrown); err != nil {
		t.Fatalf("an uncaught exception ended the session: %v", err)
	}
	uncaught, first := client.UncaughtCallbacks()
	if uncaught != 1 {
		t.Fatalf("callbacks ended by an uncaught exception = %d, want 1", uncaught)
	}
	if !strings.Contains(first, javaThrowNullClass) || !strings.Contains(first, "paint") {
		t.Errorf("the first uncaught callback reads %q, want the callback and the class", first)
	}

	fault := errors.New("guest memory at 0x4 is not mapped")
	if err := client.absorbUncaughtCallback("paint", fault); !errors.Is(err, fault) {
		t.Errorf("a fault was absorbed: %v", err)
	}
	unsupported := errors.New("LGT Java apps are not supported (it called something)")
	if err := client.absorbUncaughtCallback("paint", unsupported); !errors.Is(err, unsupported) {
		t.Errorf("a slot this platform does not serve was absorbed: %v", err)
	}
	if uncaught, _ := client.UncaughtCallbacks(); uncaught != 1 {
		t.Errorf("the two that were not absorbed were counted: %d", uncaught)
	}
}

// The specification gives drawImage a NullPointerException for an image
// argument that is null, and two local titles depend on which of the two
// answers this platform gives: they release the pictures of the scene they are
// leaving and are painted once before the next scene's are loaded. A platform
// refusal ends the run there; the exception the handset threw ends one frame.
func TestANullImageArgumentThrowsRatherThanRefusing(t *testing.T) {
	client := fixtureClient(t)
	thread := armcore.NewThread(armcore.NewContext())

	_, err := client.javaImageArgument(thread, 0, "drawImage")
	if err == nil {
		t.Fatal("a null image was accepted")
	}
	var throw *javaUncaughtThrow
	if !errors.As(err, &throw) || throw.Class != javaThrowNullClass {
		t.Fatalf("a null image answered %q, want the specification's NullPointerException", err)
	}
	if !strings.Contains(err.Error(), "drawImage") {
		t.Errorf("the throw %q does not name the call", err)
	}

	// An object that is not null and not an image is a different answer: this
	// platform never issued it, which is a defect rather than the language's
	// own null, and it is reported as one.
	_, err = client.javaImageArgument(thread, 0x30001000, "drawImage")
	if err == nil {
		t.Fatal("an object this platform never issued was accepted")
	}
	if errors.As(err, &throw) {
		t.Errorf("an object that is not an image was answered with a throw: %v", err)
	}
}
