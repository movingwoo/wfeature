package ktf

import (
	"encoding/binary"
	goruntime "runtime"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// newCollectorRuntime prepares a client whose platform structures exist but
// which has run no guest code, so the only object references are the ones a
// test writes itself.
func newCollectorRuntime(t *testing.T) (*Client, *initializationRuntime) {
	t.Helper()
	client, err := LoadClient(ClientImage{Name: "client.bin0", Data: syntheticInitializableClient()}, armcore.CoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := newInitializationRuntime(client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.prepare(); err != nil {
		t.Fatal(err)
	}
	client.runtime = runtime
	return client, runtime
}

// allocateTestArray creates one bound [I guest array and returns its address
// without keeping the Go object, so only the collector's own binding holds it.
func allocateTestArray(t *testing.T, runtime *initializationRuntime, length uint32) uint32 {
	t.Helper()
	classAddress, err := runtime.ensureJavaClass("[I")
	if err != nil {
		t.Fatal(err)
	}
	metadata, ok := runtime.client.vm.AOTClassAt(classAddress)
	if !ok {
		t.Fatal("[I is not registered")
	}
	address, err := runtime.allocateAOTArrayObject(metadata, length)
	if err != nil {
		t.Fatal(err)
	}
	return address
}

// collectTwice runs the two cycles an unreachable object needs: the first
// drops the Host's strong reference and the second frees it once Go has
// confirmed nothing else holds it.
func collectTwice(t *testing.T, runtime *initializationRuntime) CollectionStats {
	t.Helper()
	runtime.collectAt = 0
	if _, err := runtime.collectGuestObjects(nil); err != nil {
		t.Fatal(err)
	}
	goruntime.GC()
	runtime.collectAt = 0
	stats, err := runtime.collectGuestObjects(nil)
	if err != nil {
		t.Fatal(err)
	}
	return stats
}

func writeWord(t *testing.T, runtime *initializationRuntime, address, value uint32) {
	t.Helper()
	var word [4]byte
	binary.LittleEndian.PutUint32(word[:], value)
	if err := runtime.client.core.Memory().Write(address, word[:]); err != nil {
		t.Fatal(err)
	}
}

func TestCollectorKeepsObjectsGuestMemoryStillNames(t *testing.T) {
	client, runtime := newCollectorRuntime(t)
	// A platform word outside any object is what a guest static looks like to
	// the scan: whatever it holds is a root.
	root, err := runtime.allocateWords([]uint32{0})
	if err != nil {
		t.Fatal(err)
	}
	kept := allocateTestArray(t, runtime, 4)
	dead := allocateTestArray(t, runtime, 4)
	writeWord(t, runtime, root, kept)

	collectTwice(t, runtime)

	if _, tracked := runtime.objects[kept]; !tracked {
		t.Error("collected an object a guest word still points at")
	}
	if _, ok := client.JVM().AOTObject(kept); !ok {
		t.Error("a reachable object lost its JVM binding")
	}
	if _, tracked := runtime.objects[dead]; tracked {
		t.Error("kept an object nothing references")
	}
	if _, ok := client.JVM().AOTObject(dead); ok {
		t.Error("a collected object kept its JVM binding")
	}

	// Clearing the only reference makes the survivor collectable in turn.
	writeWord(t, runtime, root, 0)
	collectTwice(t, runtime)
	if _, tracked := runtime.objects[kept]; tracked {
		t.Error("kept an object after its last reference was cleared")
	}
}

func TestCollectorFollowsReferencesBetweenObjects(t *testing.T) {
	_, runtime := newCollectorRuntime(t)
	root, err := runtime.allocateWords([]uint32{0})
	if err != nil {
		t.Fatal(err)
	}
	head := allocateTestArray(t, runtime, 4)
	tail := allocateTestArray(t, runtime, 4)
	// The root names head, and head's first element names tail: only tracing
	// through a live object's own words keeps tail alive.
	writeWord(t, runtime, root, head)
	writeWord(t, runtime, head+javaInstanceSize+javaInstanceHeader+javaArrayLengthSize, tail)

	collectTwice(t, runtime)

	if _, tracked := runtime.objects[head]; !tracked {
		t.Error("collected the object the root names")
	}
	if _, tracked := runtime.objects[tail]; !tracked {
		t.Error("collected an object reachable only through another object")
	}
}

func TestCollectorReclaimsUnreachableCycles(t *testing.T) {
	_, runtime := newCollectorRuntime(t)
	first := allocateTestArray(t, runtime, 4)
	second := allocateTestArray(t, runtime, 4)
	elements := uint32(javaInstanceSize + javaInstanceHeader + javaArrayLengthSize)
	// Two dead objects that reference each other. Scanning memory alone would
	// see both addresses and keep them; only tracing from roots frees them.
	writeWord(t, runtime, first+elements, second)
	writeWord(t, runtime, second+elements, first)

	before := runtime.arena.used()
	collectTwice(t, runtime)

	if _, tracked := runtime.objects[first]; tracked {
		t.Error("kept a dead object that is only referenced by another dead object")
	}
	if _, tracked := runtime.objects[second]; tracked {
		t.Error("kept the other half of a dead cycle")
	}
	if after := runtime.arena.used(); after >= before {
		t.Errorf("arena use is %d after collecting, want less than %d", after, before)
	}
}

func TestCollectorKeepsObjectsHeldOnlyByTheHost(t *testing.T) {
	client, runtime := newCollectorRuntime(t)
	address := allocateTestArray(t, runtime, 4)
	// Nothing in guest memory names it, but the Host still holds the object,
	// which is exactly the case a guest-only scan cannot see.
	object, ok := client.JVM().AOTObject(address)
	if !ok {
		t.Fatal("the allocated array is not bound")
	}

	collectTwice(t, runtime)

	if _, tracked := runtime.objects[address]; !tracked {
		t.Error("collected an object the Host still references")
	}
	if object.ClassName != "[I" {
		t.Errorf("held object is %q, want [I", object.ClassName)
	}
	// Keep the reference alive across the collection above.
	goruntime.KeepAlive(object)
}

func TestCollectorTreatsFrozenCheatAddressesAsRoots(t *testing.T) {
	_, runtime := newCollectorRuntime(t)
	address := allocateTestArray(t, runtime, 4)
	frozen := address + javaInstanceSize + javaInstanceHeader + javaArrayLengthSize

	runtime.collectAt = 0
	if _, err := runtime.collectGuestObjects([]uint32{frozen}); err != nil {
		t.Fatal(err)
	}
	goruntime.GC()
	runtime.collectAt = 0
	if _, err := runtime.collectGuestObjects([]uint32{frozen}); err != nil {
		t.Fatal(err)
	}

	if _, tracked := runtime.objects[address]; !tracked {
		t.Error("collected an object whose field the cheat engine freezes")
	}
}

// The second trigger. Growth alone leaves a title that reaches the end of the
// arena between two cycles with an allocation it cannot make and objects that
// are already unreachable, so a refused block runs a cycle of its own.
func TestAllocationFailureRunsACollectionAndTheRetrySucceeds(t *testing.T) {
	_, runtime := newCollectorRuntime(t)
	const length = 256
	allocateTestArray(t, runtime, length)

	// One cycle to drop the Host's strong reference; the allocation below is
	// what runs the second.
	runtime.collectAt = 0
	if _, err := runtime.collectGuestObjects(nil); err != nil {
		t.Fatal(err)
	}
	goruntime.GC()

	// Nothing left above the cursor, so the only room the arena has is what
	// the collector gives back.
	runtime.arena.limit = runtime.arena.cursor
	address, err := runtime.allocate(uint64(length) * 4)
	if err != nil {
		t.Fatalf("an allocation a collection would have made room for failed: %v", err)
	}
	if address == 0 {
		t.Fatal("the retry answered a null address")
	}
}
