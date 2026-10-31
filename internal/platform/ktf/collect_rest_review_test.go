package ktf

import (
	goruntime "runtime"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// TestCollectorScansTheRunningWorker covers a collection an allocation
// triggers inside a guest worker's slice. The worker is out of the parked list
// while it runs, and an object held only in its registers is still live: a
// collection that missed them released it, and a second one freed it while the
// guest was still writing through the register.
func TestCollectorScansTheRunningWorker(t *testing.T) {
	client, runtime := newCollectorRuntime(t)
	held := allocateTestArray(t, runtime, 4)
	thread := armcore.NewThread(armcore.NewContext())
	if err := thread.SetRegister(4, held); err != nil {
		t.Fatal(err)
	}
	client.activeWorker = &guestWorker{armThread: thread}
	defer func() { client.activeWorker = nil }()

	collectTwice(t, runtime)

	record, tracked := runtime.objects[held]
	if !tracked {
		t.Fatal("collected an object the running worker holds in a register")
	}
	if record.released {
		t.Fatal("released an object the running worker holds in a register")
	}
}

// TestNewStringKeepsItsCharactersAcrossACollection covers the String
// allocation that runs a collection after its character array was allocated.
// Until the String is written, the array's address is only a Go value, so the
// collection has to treat it as a root; otherwise it was released, Go could
// collect its binding, and the next cycle lost it although the String names
// it.
func TestNewStringKeepsItsCharactersAcrossACollection(t *testing.T) {
	client, runtime := newCollectorRuntime(t)
	stringClass, err := runtime.ensureJavaClass("java/lang/String")
	if err != nil {
		t.Fatal(err)
	}
	metadata, ok := client.vm.AOTClassAt(stringClass)
	if !ok {
		t.Fatal("java/lang/String is not registered")
	}
	if _, err := runtime.ensureJavaClass("[C"); err != nil {
		t.Fatal(err)
	}
	// Something unreachable the collection can free, so the String's own
	// allocation succeeds on the retry.
	allocateTestArray(t, runtime, 64)
	runtime.CollectAt = 0
	if _, err := runtime.collectGuestObjects(nil); err != nil {
		t.Fatal(err)
	}
	goruntime.GC()

	// Room for the two-unit character array and nothing more, so the String
	// itself is what the arena refuses.
	charactersSize := wipicrt.AlignArenaSize(uint64(javaInstanceSize + javaInstanceHeader + javaArrayLengthSize + 2*2))
	start := (runtime.Arena.Cursor + wipicrt.ArenaAlignment - 1) &^ uint64(wipicrt.ArenaAlignment-1)
	runtime.Arena.Limit = start + charactersSize

	text := client.vm.NewString("hi")
	address, err := runtime.allocateAOTObject(metadata, make([]byte, metadata.InstanceSize), text)
	if err != nil {
		t.Fatal(err)
	}
	payload := readTestBytes(t, client, address+javaInstanceSize+javaInstanceHeader, 4)
	characters := uint32(payload[0]) | uint32(payload[1])<<8 | uint32(payload[2])<<16 | uint32(payload[3])<<24
	if record, tracked := runtime.objects[characters]; !tracked || record.released {
		t.Fatalf("the new String's characters were released by its own allocation (tracked=%t)", tracked)
	}
	goruntime.KeepAlive(text)
}
