package lgt

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
)

// The collector is the one piece here that can break a title by working: a
// live object it frees is memory the guest goes on reading, and the fault
// lands far from the release. So every test below is about what must *not* be
// reclaimed as much as what must.

// newCollectableObject builds an instance of a platform class nothing holds a
// reference to, and answers it with the block it was given.
func newCollectableObject(t *testing.T, client *Client, name string) uint32 {
	t.Helper()
	object, err := newTestObject(t, client, name)
	if err != nil {
		t.Fatalf("allocate %s: %v", name, err)
	}
	// Every allocation is pinned for the platform call that built it; these
	// tests are that call, and they are done building.
	client.releaseJavaPins(0)
	return object
}

func collect(t *testing.T, client *Client) CollectionStats {
	t.Helper()
	stats, err := client.collectJavaObjects(nil)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	return stats
}

func tracked(client *Client, object uint32) bool {
	_, ok := client.javaRuntimeState().objects[object]
	return ok
}

// A cycle never frees what it has only just found unreachable. That grace is
// what makes the collector safe to run from inside a platform call, so it is
// pinned here rather than left as an implementation detail.
func TestJavaCollectorFreesOnlyAfterAGraceCycle(t *testing.T) {
	client := fixtureClient(t)
	object := newCollectableObject(t, client, "java/lang/Object")
	before := client.arena.used()

	first := collect(t, client)
	if first.Condemned == 0 {
		t.Fatal("an unreachable object was not condemned")
	}
	if first.Freed != 0 {
		t.Fatalf("an object was freed in the cycle that first found it unreachable: %d", first.Freed)
	}
	if !tracked(client, object) {
		t.Fatal("a condemned object stopped being tracked")
	}

	second := collect(t, client)
	if second.Freed == 0 {
		t.Fatal("a condemned object was not freed by the next cycle")
	}
	if tracked(client, object) {
		t.Fatal("a freed object is still tracked")
	}
	if after := client.arena.used(); after >= before {
		t.Fatalf("the arena did not shrink: %d bytes before, %d after", before, after)
	}
}

// A word of guest memory that names an object is the only evidence the
// collector has that the guest still holds it, and it has to be enough
// wherever it is: the platform's own arena is scanned as a root exactly
// because the module's statics live there.
func TestJavaCollectorKeepsAnObjectAGuestWordNames(t *testing.T) {
	client := fixtureClient(t)
	object := newCollectableObject(t, client, "java/lang/Object")
	slot, err := client.allocate(4)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.writeWord(slot, object); err != nil {
		t.Fatal(err)
	}
	for cycle := 0; cycle < 3; cycle++ {
		collect(t, client)
	}
	if !tracked(client, object) {
		t.Fatal("an object a guest word names was freed")
	}

	// And once nothing names it, it goes.
	if err := client.writeWord(slot, 0); err != nil {
		t.Fatal(err)
	}
	collect(t, client)
	collect(t, client)
	if tracked(client, object) {
		t.Fatal("an object nothing names any more was kept")
	}
}

// An interior pointer is what a field access actually holds: the guest loads
// the block out of the object and indexes from there, so the block's address
// is in a register far more often than the object's.
func TestJavaCollectorKeepsAnObjectNamedByItsFieldBlock(t *testing.T) {
	client := fixtureClient(t)
	object := newCollectableObject(t, client, "java/lang/Object")
	record := client.javaRuntimeState().objects[object]
	if record.blockSize == 0 {
		t.Fatal("the object was tracked without a field block")
	}
	slot, err := client.allocate(4)
	if err != nil {
		t.Fatal(err)
	}
	// Four bytes into the block, which is where field one lives.
	if err := client.writeWord(slot, record.block+4); err != nil {
		t.Fatal(err)
	}
	collect(t, client)
	collect(t, client)
	if !tracked(client, object) {
		t.Fatal("an object named by a pointer into its fields was freed")
	}
}

// Two objects that reference only each other are reached by nothing, which is
// the whole reason the collector traces rather than counts.
func TestJavaCollectorReclaimsACycle(t *testing.T) {
	client := fixtureClient(t)
	left := newCollectableObject(t, client, "java/lang/Object")
	right := newCollectableObject(t, client, "java/lang/Object")
	leftBlock := client.javaRuntimeState().objects[left].block
	rightBlock := client.javaRuntimeState().objects[right].block
	if err := client.writeWord(leftBlock, right); err != nil {
		t.Fatal(err)
	}
	if err := client.writeWord(rightBlock, left); err != nil {
		t.Fatal(err)
	}
	collect(t, client)
	collect(t, client)
	if tracked(client, left) || tracked(client, right) {
		t.Fatal("a cycle of dead objects was kept alive by its own references")
	}
}

// The first of the three payload edges no guest word expresses. A vector holds
// its elements in this platform's own map, so an element the guest reaches only
// through the vector is named nowhere in guest memory.
func TestJavaCollectorFollowsAVectorToItsElements(t *testing.T) {
	client := fixtureClient(t)
	runtime := client.javaRuntimeState()
	vector := newCollectableObject(t, client, "java/util/Vector")
	element := newCollectableObject(t, client, "java/lang/Object")
	runtime.vectors[vector] = []uint32{element}
	runtime.jlet = vector

	collect(t, client)
	collect(t, client)
	if !tracked(client, element) {
		t.Fatal("an element only the vector names was freed")
	}

	runtime.jlet = 0
	collect(t, client)
	collect(t, client)
	if tracked(client, vector) || tracked(client, element) {
		t.Fatal("a dead vector and its element were kept")
	}
	if _, held := runtime.vectors[vector]; held {
		t.Fatal("the vector's contents outlived the object that held them")
	}
}

// The second edge: a container names its children, and an input-method handler
// the listener it was told to hand characters to.
func TestJavaCollectorFollowsAWidgetToItsChildrenAndListener(t *testing.T) {
	client := fixtureClient(t)
	runtime := client.javaRuntimeState()
	container := newCollectableObject(t, client, "java/lang/Object")
	child := newCollectableObject(t, client, "java/lang/Object")
	listener := newCollectableObject(t, client, "java/lang/Object")
	widget := client.javaWidgetState(container)
	widget.children = []uint32{child}
	widget.listener = listener
	runtime.jlet = container

	collect(t, client)
	collect(t, client)
	if !tracked(client, child) || !tracked(client, listener) {
		t.Fatal("a widget's children or listener were freed while the widget lived")
	}
}

// The third: a wrapper stands for another object's sink, and a stream for the
// File it was opened on. Neither relationship is a word in guest memory.
func TestJavaCollectorFollowsWrapperAndFileBindings(t *testing.T) {
	client := fixtureClient(t)
	runtime := client.javaRuntimeState()
	wrapper := newCollectableObject(t, client, "java/lang/Object")
	sink := newCollectableObject(t, client, "java/lang/Object")
	file := newCollectableObject(t, client, javaFileClass)
	runtime.wrapped[wrapper] = sink
	runtime.streamFiles[wrapper] = file
	runtime.jlet = wrapper

	collect(t, client)
	collect(t, client)
	if !tracked(client, sink) {
		t.Fatal("a sink only its wrapper names was freed")
	}
	if !tracked(client, file) {
		t.Fatal("a File only the stream opened on it names was freed")
	}
}

// A surface is the payload that costs the most to leak — java_stream.go's note
// about a title filling the surface region is about exactly this — and the one
// most easily released too early, because two Images may be over one surface.
func TestJavaCollectorReleasesASurfaceOnlyWhenNothingHoldsIt(t *testing.T) {
	client := fixtureClient(t)
	runtime := client.javaRuntimeState()
	surface, err := client.newFramebuffer(8, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	first := newCollectableObject(t, client, javaImageClass)
	second := newCollectableObject(t, client, javaImageClass)
	runtime.images[first] = surface.handle
	runtime.images[second] = surface.handle
	runtime.jlet = second

	collect(t, client)
	stats := collect(t, client)
	if tracked(client, first) {
		t.Fatal("a dead Image was kept")
	}
	if stats.Surfaces != 0 {
		t.Fatal("a surface another Image still holds was released")
	}
	if client.framebuffer(surface.handle) == nil {
		t.Fatal("the surface was released while an alias held it")
	}

	runtime.jlet = 0
	collect(t, client)
	stats = collect(t, client)
	if stats.Surfaces != 1 {
		t.Fatalf("the surface nothing holds was not released: %d released", stats.Surfaces)
	}
	if client.framebuffer(surface.handle) != nil {
		t.Fatal("the surface survived the last Image that held it")
	}
}

// The decode cache is a holder in its own right: it hands the same surface to
// the next Image built from that picture, so a collection must not take it.
func TestJavaCollectorKeepsASurfaceTheDecodeCacheHolds(t *testing.T) {
	client := fixtureClient(t)
	runtime := client.javaRuntimeState()
	surface, err := client.newFramebuffer(8, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	image := newCollectableObject(t, client, javaImageClass)
	runtime.images[image] = surface.handle
	runtime.decodedImages = map[string]uint32{"name:sprites": surface.handle}

	collect(t, client)
	stats := collect(t, client)
	if tracked(client, image) {
		t.Fatal("a dead Image was kept")
	}
	if stats.Surfaces != 0 {
		t.Fatal("the decode cache's surface was released out from under it")
	}
	if client.framebuffer(surface.handle) == nil {
		t.Fatal("the cached surface is gone")
	}
}

// A File object nothing holds is one the language would have closed on the
// handset, and a close here has to flush: a write that never reached the store
// is a save the player loses.
func TestJavaCollectorClosesAFileNothingHolds(t *testing.T) {
	client := fixtureClient(t)
	client.saveStore = backend.NewDirectorySaveStore(t.TempDir())
	runtime := client.javaRuntimeState()
	handle := client.openFile("collector.sav", fileOpenWriteTruncate)
	if handle < 0 {
		t.Fatal("the fixture file would not open")
	}
	client.files[uint32(handle)].data = []byte("progress")
	client.files[uint32(handle)].dirty = true

	first := newCollectableObject(t, client, javaFileClass)
	second := newCollectableObject(t, client, javaFileClass)
	runtime.files[first] = uint32(handle)
	runtime.files[second] = uint32(handle)
	runtime.jlet = second

	collect(t, client)
	stats := collect(t, client)
	if stats.Files != 0 {
		t.Fatal("a file another object still stands for was closed")
	}
	if client.files[uint32(handle)] == nil {
		t.Fatal("the file was closed while an alias held it")
	}

	runtime.jlet = 0
	collect(t, client)
	stats = collect(t, client)
	if stats.Files != 1 {
		t.Fatalf("the file nothing holds was not closed: %d closed", stats.Files)
	}
	if client.files[uint32(handle)] != nil {
		t.Fatal("the file survived the last object that stood for it")
	}
	if stored, found := client.readFile("collector.sav"); !found || string(stored) != "progress" {
		t.Fatalf("the close did not flush: found=%v content=%q", found, stored)
	}
}

// A platform call that has built an object and not yet handed it over holds it
// in a Go local, which the scan cannot see. The pin is what stands in for that
// frame, and it is what makes a collection raised by a full arena safe.
func TestJavaCollectorKeepsWhatAPlatformCallIsStillBuilding(t *testing.T) {
	client := fixtureClient(t)
	mark := client.javaPinMark()
	object, err := newTestObject(t, client, "java/lang/Object")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, client)
	collect(t, client)
	if !tracked(client, object) {
		t.Fatal("an object the call that built it still holds was freed")
	}
	client.releaseJavaPins(mark)
	collect(t, client)
	collect(t, client)
	if tracked(client, object) {
		t.Fatal("an object was kept after the call that built it returned")
	}
}

// The platform's own handles are roots. A card the display is showing is not
// named by any guest word between frames, and freeing it ends the title.
func TestJavaCollectorKeepsThePlatformsOwnHandles(t *testing.T) {
	client := fixtureClient(t)
	runtime := client.javaRuntimeState()
	card := newCollectableObject(t, client, "java/lang/Object")
	singleton := newCollectableObject(t, client, "java/lang/Object")
	serial := newCollectableObject(t, client, "java/lang/Object")
	locked := newCollectableObject(t, client, "java/lang/Object")
	runtime.card = card
	runtime.singletons["display"] = singleton
	runtime.serial = []uint32{serial}
	runtime.monitors[locked] = &javaMonitor{count: 1, platform: true}

	collect(t, client)
	collect(t, client)
	for name, object := range map[string]uint32{
		"card": card, "singleton": singleton, "callSerially": serial, "held monitor": locked,
	} {
		if !tracked(client, object) {
			t.Fatalf("the %s the platform holds was freed", name)
		}
	}
}

// The second trigger. Growth alone leaves a title that fills a region between
// two cycles with an allocation it cannot make and objects that are already
// unreachable.
func TestJavaAllocationFailureRunsACollectionAndTheRetrySucceeds(t *testing.T) {
	client := fixtureClient(t)
	// Fill the surface region, then let go of everything.
	var images []uint32
	for {
		surface, err := client.newFramebuffer(256, 256, false)
		if err != nil {
			break
		}
		image := newCollectableObject(t, client, javaImageClass)
		client.javaRuntimeState().images[image] = surface.handle
		images = append(images, image)
		if len(images) > 4096 {
			t.Fatal("the surface region did not fill")
		}
	}
	if len(images) == 0 {
		t.Fatal("no surface could be allocated at all")
	}
	// One cycle to condemn them; the allocation below is what runs the second.
	collect(t, client)

	surface, err := client.newFramebuffer(256, 256, false)
	if err != nil {
		t.Fatalf("an allocation that a collection would have made room for failed: %v", err)
	}
	if surface == nil {
		t.Fatal("no surface came back")
	}
}
