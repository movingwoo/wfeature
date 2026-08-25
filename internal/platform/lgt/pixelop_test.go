package lgt

import (
	"context"
	"encoding/binary"
	"testing"
)

// installThumb writes a run of Thumb halfwords into the loaded module and
// answers the address to call them at, with the Thumb bit set. The module maps
// read/write/execute, and a fixture never runs its own code, so borrowing the
// tail of its span costs nothing.
func installThumb(t *testing.T, client *Client, instructions ...uint16) uint32 {
	t.Helper()
	_, high := client.module.Span()
	address := (high - 64) &^ 1
	data := make([]byte, len(instructions)*2)
	for index, instruction := range instructions {
		binary.LittleEndian.PutUint16(data[index*2:], instruction)
	}
	if err := client.core.Memory().Write(address, data); err != nil {
		t.Fatal(err)
	}
	return address | 1
}

// A word this platform did not write is not a function. The field is the
// game's own structure, and a title that leaves a handle or a colour there
// would otherwise have the platform branch to an address that is not code —
// KTF has a title that stores a font handle in the equivalent word.
func TestPixelOperationIsOnlyTakenFromACodeAddress(t *testing.T) {
	client := fixtureClient(t)
	low, high := client.module.Span()
	// Every address below is offered to the reader as if a title had set it,
	// so that what the test measures is the code-address rule rather than the
	// installed-operation one beneath it.
	for _, address := range []uint32{low | 1, low, 1, (low - 0x1000) | 1, high | 1, high + 0x1000} {
		client.installPixelOp(address)
	}

	if op := client.readContextPixelOp(low|1, 0x1234); !op.active() {
		t.Fatal("a Thumb address inside the module was refused")
	} else if op.param != 0x1234 {
		t.Fatalf("param = %#x, want %#x", op.param, 0x1234)
	}
	// An even word is an ARM address or, far more likely, not an address: the
	// operations these titles install are Thumb.
	if op := client.readContextPixelOp(low, 0x1234); op.active() {
		t.Fatal("an even word was taken as a function")
	}
	for _, outside := range []uint32{1, (low - 0x1000) | 1, high | 1, high + 0x1000} {
		if op := client.readContextPixelOp(outside, 0); op.active() {
			t.Fatalf("%#x is outside the module and was taken as a function", outside)
		}
	}
}

// contextFor reads the operation out of the game's own MC_GrpContext, so the
// two words have to be the ones MC_grpSetContext writes for fields 5 and 6.
func TestContextCarriesTheOperationTheGameInstalled(t *testing.T) {
	client := fixtureClient(t)
	low, _ := client.module.Span()

	pointer, err := client.allocateBytes(make([]byte, grpContextSize))
	if err != nil {
		t.Fatal(err)
	}
	if code := client.initContext(pointer); code != wipiSuccess {
		t.Fatalf("initContext answered %d", code)
	}
	// Field 5 is the operation and field 6 its parameter; the specification
	// numbers them and transferContextField maps them to these offsets.
	if code := client.transferContextFieldFor(t, pointer, grpFieldPixelOp, low|1); code != wipiSuccess {
		t.Fatalf("setContext(pixelOp) answered %d", code)
	}
	if code := client.transferContextFieldFor(t, pointer, grpFieldParam1, 0x07e0); code != wipiSuccess {
		t.Fatalf("setContext(param1) answered %d", code)
	}

	target, err := client.newFramebuffer(4, 4, false)
	if err != nil {
		t.Fatal(err)
	}
	drawContext, err := client.contextFor(context.Background(), client.thread, target, pointer)
	if err != nil {
		t.Fatal(err)
	}
	if !drawContext.op.active() {
		t.Fatal("the context did not carry the operation the game set")
	}
	if drawContext.op.function != low|1 || drawContext.op.param != 0x07e0 {
		t.Fatalf("operation = %+v, want function %#x param %#x", drawContext.op, low|1, 0x07e0)
	}
}

// **This is the test that pins the argument order**, which is the one thing a
// port of KTF's pixel operation would get wrong: on this platform the first
// argument is the pixel already in the framebuffer and the second is the pixel
// being drawn, and the two local operations only make sense that way.
//
// Two operations, one instruction apart, separate the readings. `bx lr`
// answers argument zero; `adds r0, r1, #0; bx lr` answers argument one. If the
// first leaves the surface untouched and the second paints it, argument zero
// is what was already there.
func TestDrawRunsTheGuestOperationWithTheFramebufferPixelFirst(t *testing.T) {
	const existing = uint16(0x1234)
	const incoming = uint16(0xf81f)

	for _, test := range []struct {
		name         string
		instructions []uint16
		want         uint16
	}{
		{"returning argument zero keeps the framebuffer", []uint16{0x4770}, existing},
		{"returning argument one paints the draw", []uint16{0x1c08, 0x4770}, incoming},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := fixtureClient(t)
			function := installThumb(t, client, test.instructions...)

			pointer, target := pixelOpFixture(t, client, function, incoming)
			for index := range target.pixels {
				target.pixels[index] = existing
			}
			if err := client.syncToGuest(target); err != nil {
				t.Fatal(err)
			}

			callDrawSlot(t, client, slotFillRect, target.handle, 0, 0, 2, 2, pointer)

			if got := target.pixels[0]; got != test.want {
				t.Fatalf("pixel = %#x, want %#x", got, test.want)
			}
		})
	}
}

// A pixel outside the clip is never drawn, so it must never reach the
// operation either: running guest code for a pixel that is then discarded is
// both wrong and the expensive half of a draw.
func TestClippedPixelsDoNotReachTheOperation(t *testing.T) {
	client := fixtureClient(t)
	// An operation that would be obvious if it ran: it answers a colour that
	// is neither the existing pixel nor the incoming one.
	function := installThumb(t, client, 0x2001, 0x0240, 0x4770) // movs r0,#1; lsls r0,#9; bx lr

	pointer, target := pixelOpFixture(t, client, function, 0xf81f)
	// A clip of (2,2)-(3,3): a real rectangle, since an empty one is taken as
	// the whole surface, and one the fill below sits entirely outside of.
	if err := client.writeWord(pointer+grpContextClip, 2|2<<16); err != nil {
		t.Fatal(err)
	}
	if err := client.writeWord(pointer+grpContextClip+4, 3|3<<16); err != nil {
		t.Fatal(err)
	}
	for index := range target.pixels {
		target.pixels[index] = 0x1234
	}
	if err := client.syncToGuest(target); err != nil {
		t.Fatal(err)
	}

	// The fill sits in the opposite corner from the clip, so no pixel of it
	// survives to be drawn.
	callDrawSlot(t, client, slotFillRect, target.handle, 0, 0, 2, 2, pointer)

	for index, pixel := range target.pixels {
		if pixel != 0x1234 {
			t.Fatalf("pixel %d = %#x, want the surface untouched", index, pixel)
		}
	}
}

// The cache is what makes an operation affordable: a draw asks it once per
// pair of pixels rather than once per pixel. Changing the code under a cached
// answer and getting the cached answer back is what proves the guest is not
// being called again; changing the operation's identity throws it away.
func TestPixelOperationAnswersAreCachedAndInvalidated(t *testing.T) {
	client := fixtureClient(t)
	function := installThumb(t, client, 0x1c08, 0x4770) // adds r0, r1, #0; bx lr
	client.installPixelOp(function)

	op := client.readContextPixelOp(function, 0)
	first, err := client.applyPixelOp(context.Background(), client.thread, op, 0x1111, 0x2222)
	if err != nil {
		t.Fatal(err)
	}
	if first != 0x2222 {
		t.Fatalf("operation answered %#x, want the incoming pixel %#x", first, 0x2222)
	}

	// Rewrite the function to answer argument zero. A cached pair must not
	// notice.
	installThumb(t, client, 0x4770)
	again, err := client.applyPixelOp(context.Background(), client.thread, op, 0x1111, 0x2222)
	if err != nil {
		t.Fatal(err)
	}
	if again != first {
		t.Fatalf("cached pair answered %#x, want the remembered %#x", again, first)
	}
	// A pair it has not been asked about runs the code that is there now.
	fresh, err := client.applyPixelOp(context.Background(), client.thread, op, 0x3333, 0x4444)
	if err != nil {
		t.Fatal(err)
	}
	if fresh != 0x3333 {
		t.Fatalf("uncached pair answered %#x, want argument zero %#x", fresh, 0x3333)
	}

	// A different parameter is a different operation and gets a cache of its
	// own, rather than displacing the first one's answers.
	changed := pixelOp{function: op.function, param: op.param + 1}
	if _, err := client.applyPixelOp(context.Background(), client.thread, changed, 0x1111, 0x2222); err != nil {
		t.Fatal(err)
	}
	if len(client.pixelOps) != 2 {
		t.Fatalf("a second operation left %d caches, want 2", len(client.pixelOps))
	}
	original := client.pixelOps[uint64(op.function)<<32|uint64(op.param)]
	if original == nil || original.results[0x11112222] != first {
		t.Fatal("a second operation displaced the first one's answers")
	}
}

// A title switching between operations must not throw away what it learned
// about the one it is switching away from. The title this was measured on
// alternates about twice a tick, which turned a single cache into 3,879
// rebuilds over 2,000 ticks and most of the way back to calling the guest for
// every pixel.
func TestAlternatingOperationsKeepTheirOwnAnswers(t *testing.T) {
	client := fixtureClient(t)
	installed := installThumb(t, client, 0x4770)
	client.installPixelOp(installed)
	keepArgumentZero := client.readContextPixelOp(installed, 0)

	_, high := client.module.Span()
	second := (high - 128) &^ 1
	if err := client.core.Memory().Write(second, []byte{0x08, 0x1c, 0x70, 0x47}); err != nil {
		t.Fatal(err)
	}
	client.installPixelOp(second | 1)
	keepArgumentOne := client.readContextPixelOp(second|1, 0)

	// Alternate, the way the measured title does.
	for round := 0; round < 8; round++ {
		zero, err := client.applyPixelOp(context.Background(), client.thread, keepArgumentZero, 0xaaaa, 0x5555)
		if err != nil {
			t.Fatal(err)
		}
		one, err := client.applyPixelOp(context.Background(), client.thread, keepArgumentOne, 0xaaaa, 0x5555)
		if err != nil {
			t.Fatal(err)
		}
		if zero != 0xaaaa || one != 0x5555 {
			t.Fatalf("round %d answered %#x and %#x", round, zero, one)
		}
	}
	if len(client.pixelOps) != 2 {
		t.Fatalf("alternating left %d caches, want one per operation", len(client.pixelOps))
	}
	for identity, cache := range client.pixelOps {
		if len(cache.results) != 1 {
			t.Fatalf("cache %#x holds %d answers, want the one pair asked about",
				identity, len(cache.results))
		}
	}
}

// The number of operations remembered is bounded, because the function pointer
// is a word out of guest memory and a title that wrote a new one every frame
// would otherwise grow this without limit.
func TestTheOperationCacheIsBounded(t *testing.T) {
	client := fixtureClient(t)
	function := installThumb(t, client, 0x4770)
	for param := uint32(0); param < maxCachedPixelOps*3; param++ {
		op := pixelOp{function: function, param: param}
		if _, err := client.applyPixelOp(context.Background(), client.thread, op, 0x1111, 0x2222); err != nil {
			t.Fatal(err)
		}
		if len(client.pixelOps) > maxCachedPixelOps {
			t.Fatalf("the cache grew to %d operations, past the bound of %d",
				len(client.pixelOps), maxCachedPixelOps)
		}
	}
}

// A context with no operation writes what the draw asked for, which is what
// every draw did before there was one. This is the property that keeps a title
// that never installs one from noticing this code at all.
func TestADrawWithoutAnOperationIsUnchanged(t *testing.T) {
	client := fixtureClient(t)
	pointer, target := pixelOpFixture(t, client, 0, 0x07e0)
	if err := client.syncToGuest(target); err != nil {
		t.Fatal(err)
	}

	callDrawSlot(t, client, slotFillRect, target.handle, 0, 0, 2, 2, pointer)

	if got := target.pixels[0]; got != 0x07e0 {
		t.Fatalf("pixel = %#x, want the foreground %#x", got, 0x07e0)
	}
}

// pixelOpFixture builds a guest-memory context carrying an operation and a
// foreground colour, plus the surface a draw slot names by handle.
func pixelOpFixture(t *testing.T, client *Client, function uint32, colour uint16) (uint32, *framebuffer) {
	t.Helper()
	pointer, err := client.allocateBytes(make([]byte, grpContextSize))
	if err != nil {
		t.Fatal(err)
	}
	if code := client.initContext(pointer); code != wipiSuccess {
		t.Fatalf("initContext answered %d", code)
	}
	if err := client.writeWord(pointer+grpContextForeground, uint32(colour)); err != nil {
		t.Fatal(err)
	}
	if function != 0 {
		// Through MC_grpSetContext rather than straight into the word: a draw
		// only runs an operation this platform was handed, and a fixture that
		// writes the field itself is a title that never installed one.
		if code := client.transferContextFieldFor(t, pointer, grpFieldPixelOp, function); code != wipiSuccess {
			t.Fatalf("setContext(pixel operation) answered %d", code)
		}
	}
	target, err := client.newFramebuffer(4, 4, false)
	if err != nil {
		t.Fatal(err)
	}
	return pointer, target
}

// transferContextFieldFor drives MC_grpSetContext for one scalar field.
func (client *Client) transferContextFieldFor(t *testing.T, pointer, field, value uint32) int32 {
	t.Helper()
	return int32(callSlot(t, client, slotSetContext, pointer, field, value))
}

// A word that is a code address and was never installed is a leftover, not an
// operation. One title's context carried the middle of a routine of its own —
// a return address, which passes the "inside the module" test — and running it
// as an operation walked a structure base it had never been given and faulted.
func TestOnlyAnInstalledOperationIsRun(t *testing.T) {
	client := fixtureClient(t)
	function := installThumb(t, client, 0x4770)

	if op := client.readContextPixelOp(function, 0); op.active() {
		t.Fatal("a code address this platform was never handed was run as an operation")
	}
	if client.uninstalledPixelOps[function] != 1 {
		t.Fatalf("the refusal was not counted: %v", client.uninstalledPixelOps)
	}

	// The same address, once a title installs it anywhere, is an operation
	// from then on — including through a context this platform never saw
	// being set, which is what a title copying or restoring one produces.
	pointer, err := client.allocateBytes(make([]byte, grpContextSize))
	if err != nil {
		t.Fatal(err)
	}
	if code := client.initContext(pointer); code != wipiSuccess {
		t.Fatalf("initContext answered %d", code)
	}
	if code := client.transferContextFieldFor(t, pointer, grpFieldPixelOp, function); code != wipiSuccess {
		t.Fatalf("setContext(pixel operation) answered %d", code)
	}
	if op := client.readContextPixelOp(function, 0); !op.active() {
		t.Fatal("an installed operation was refused")
	}
}
