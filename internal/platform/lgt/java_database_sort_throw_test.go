package lgt

import (
	"errors"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// installThrowingCallback installs a callback whose body throws object:
//
//	ldr  r0, [pc, #8]   ; the object
//	movs r3, #0x21      ; the throw slot
//	mov  ip, r3
//	svc  #5             ; the Java table
//	bx   lr
//	nop
//	.word object
func installThrowingCallback(t *testing.T, client *Client, name, iface, method string, offset, object uint32) uint32 {
	t.Helper()
	return installDatabaseCallback(t, client, name, iface, method, offset, false,
		0x4802, 0x2300|uint16(javaSVCThrow), 0x469c, 0xdf00|uint16(svcCategoryJava), 0x4770, 0x46c0,
		uint16(object), uint16(object>>16))
}

// An exception a sortRecord comparator or filter throws, and does not catch,
// is the title's: it reaches the try region around the title's own call, as
// it does on KTF and SKT (docs/wipi-storage.md), where it used to stop the
// run because the frame that catches it is outside the call into the title.
func TestStorageContractDatabaseSortRecordThrowsToTheTitle(t *testing.T) {
	for _, which := range []string{"comparator", "filter"} {
		t.Run(which, func(t *testing.T) {
			fixture := newDatabaseFixture(t, newMemorySaveStore())
			client := fixture.client
			database := fixture.mustOpen("save", 4, true)
			fixture.insert(database, "\x02a")
			fixture.insert(database, "\x01b")

			class, err := client.preparePlatformJavaClass("java/lang/IllegalStateException")
			if err != nil {
				t.Fatal(err)
			}
			thrown, err := client.allocateJavaObject(class)
			if err != nil {
				t.Fatal(err)
			}
			iface, method := javaDataComparatorInterface, "compare"
			if which == "filter" {
				iface, method = javaDataFilterInterface, "filter"
			}
			callback := installThrowingCallback(t, client, "test/Throwing", iface, method, 64, thrown)

			// The title's try region around its sortRecord call.
			thread := client.thread
			const resume = 0x3000
			if err := thread.SetRegister(armcore.RegisterLR, resume); err != nil {
				t.Fatal(err)
			}
			buffer, err := client.enterJavaTry()
			if err != nil {
				t.Fatal(err)
			}
			if err := client.armJavaTry(thread, buffer); err != nil {
				t.Fatal(err)
			}

			filter, comparator := uint32(0), callback
			if which == "filter" {
				filter, comparator = callback, 0
			}
			_, err = javaCall(t, client, dbSort, database, filter, comparator)
			if !errors.Is(err, errJavaThrowHandled) {
				t.Fatalf("sortRecord with a throwing %s = %v, want the title's handler to take it", which, err)
			}
			context := thread.Context()
			if pc := context.PC(); pc != resume {
				t.Errorf("the title resumes at %#x, want its handler at %#x", pc, resume)
			}
			if object, _ := thread.Register(0); object != thrown {
				t.Errorf("the handler was handed %#x, want the object the %s threw, %#x", object, which, thrown)
			}
			if len(client.javaTry) != 0 {
				t.Errorf("%d try regions are still open", len(client.javaTry))
			}
		})
	}
}

// With no try region of the title's around the call, the throw stops the
// title as any uncaught exception does, and still names what was thrown.
func TestSortRecordThrowWithNoHandlerIsUncaught(t *testing.T) {
	fixture := newDatabaseFixture(t, newMemorySaveStore())
	client := fixture.client
	database := fixture.mustOpen("save", 4, true)
	fixture.insert(database, "\x02a")
	fixture.insert(database, "\x01b")
	class, err := client.preparePlatformJavaClass("java/lang/IllegalStateException")
	if err != nil {
		t.Fatal(err)
	}
	thrown, err := client.allocateJavaObject(class)
	if err != nil {
		t.Fatal(err)
	}
	comparator := installThrowingCallback(t, client, "test/Throwing", javaDataComparatorInterface, "compare", 64, thrown)
	_, err = javaCall(t, client, dbSort, database, 0, comparator)
	var uncaught *javaUncaughtThrow
	if !errors.As(err, &uncaught) || uncaught.Object != thrown || uncaught.Class != "java/lang/IllegalStateException" {
		t.Fatalf("an uncaught throw from a comparator = %v, want it named and carried", err)
	}
}
