package ktf

import (
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// createOffScreenThroughTable and destroyOffScreenThroughTable drive
// MC_grpCreateOffScreenFrameBuffer and MC_grpDestroyOffScreenFrameBuffer
// through the graphics table the way a game does, because what these tests pin
// is the dispatch: the destroy slot was answered with a no-op for as long as
// the create slot handed out memory.
func createOffScreenThroughTable(t *testing.T, runtime *initializationRuntime, width, height uint32) uint32 {
	t.Helper()
	context := armcore.NewContext()
	context.Registers[0] = width
	context.Registers[1] = height
	handle, err := runtime.handleWIPICTableCall(armcore.NewThread(context), wipicTableGraphics, 4)
	if err != nil {
		t.Fatalf("MC_grpCreateOffScreenFrameBuffer(%d, %d) error = %v", width, height, err)
	}
	return handle
}

func destroyThroughTable(t *testing.T, runtime *initializationRuntime, function, handle uint32) {
	t.Helper()
	context := armcore.NewContext()
	context.Registers[0] = handle
	if _, err := runtime.handleWIPICTableCall(armcore.NewThread(context), wipicTableGraphics, function); err != nil {
		t.Fatalf("graphics function %d on %#x error = %v", function, handle, err)
	}
}

// One local title draws each frame into an off-screen buffer it creates and
// destroys again, eight times a tick. While the destroy slot did nothing that
// loop walked the arena at a quarter of a megabyte a second and the session
// died inside ten minutes of play, so what this pins is that the loop is flat.
func TestDestroyOffScreenFrameBufferReturnsTheRecordAndItsPixels(t *testing.T) {
	_, runtime := newTestRuntime(t)

	first := createOffScreenThroughTable(t, runtime, 240, 320)
	destroyThroughTable(t, runtime, 3, first)
	settled := runtime.arena.used()

	for round := 0; round < 512; round++ {
		handle := createOffScreenThroughTable(t, runtime, 240, 320)
		destroyThroughTable(t, runtime, 3, handle)
		if used := runtime.arena.used(); used != settled {
			t.Fatalf("round %d left %d bytes handed out, want %d", round, used, settled)
		}
	}
}

// "If the screen framebuffer is passed in, nothing happens" is the
// specification's own sentence about this call. This platform hands out one
// cached record for the screen, so obeying it is not politeness: freeing that
// record would take the screen away from every later caller rather than from
// the one that asked.
func TestDestroyOffScreenFrameBufferIgnoresTheScreen(t *testing.T) {
	_, runtime := newTestRuntime(t)

	screen, err := runtime.wipicGetScreenFramebuffer()
	if err != nil {
		t.Fatal(err)
	}
	before := runtime.arena.used()

	destroyThroughTable(t, runtime, 3, screen)

	if used := runtime.arena.used(); used != before {
		t.Fatalf("used() = %d after destroying the screen framebuffer, want %d", used, before)
	}
	if _, err := runtime.readWIPICFramebuffer(screen); err != nil {
		t.Fatalf("the screen framebuffer no longer reads back: %v", err)
	}
	again, err := runtime.wipicGetScreenFramebuffer()
	if err != nil {
		t.Fatal(err)
	}
	if again != screen {
		t.Fatalf("MC_grpGetScreenFrameBuffer() = %#x after a destroy, want the cached %#x", again, screen)
	}
}

// A handle this platform never handed out is the program's own error, and a
// double destroy arrives here as one. Neither may release a block twice: that
// hands one address to two owners.
func TestDestroyOffScreenFrameBufferRefusesWhatItDidNotIssue(t *testing.T) {
	_, runtime := newTestRuntime(t)

	live := createOffScreenThroughTable(t, runtime, 32, 32)
	before := runtime.arena.used()

	destroyThroughTable(t, runtime, 3, 0)
	destroyThroughTable(t, runtime, 3, live+4)
	if used := runtime.arena.used(); used != before {
		t.Fatalf("used() = %d after refused destroys, want %d", used, before)
	}

	destroyThroughTable(t, runtime, 3, live)
	released := runtime.arena.used()
	destroyThroughTable(t, runtime, 3, live)
	if used := runtime.arena.used(); used != released {
		t.Fatalf("a double destroy released the blocks twice: used() = %d, want %d", used, released)
	}
}

// MC_grpCreateImage's contract says the encoded buffer it is handed "is
// released inside the image function", which is why the specification requires
// it to be an MC_knlCalloc identifier. A local title relies on that: it
// allocates a buffer per image and frees none of them. So the destroy has to
// give back three things — the image record, the pixels decoded into it, and
// the source buffer — and the create has to give back the inner framebuffer
// record it copied word for word into the image.
func TestDestroyImageReturnsTheRecordThePixelsAndTheSource(t *testing.T) {
	_, runtime := newTestRuntime(t)
	encoded := encodePalettedPNG(t, 2, 2)

	result, err := runtime.allocateWIPIC(4)
	if err != nil {
		t.Fatal(err)
	}
	settled := runtime.arena.used()

	for round := 0; round < 64; round++ {
		data, err := runtime.allocateWIPIC(uint32(len(encoded)))
		if err != nil {
			t.Fatal(err)
		}
		if err := runtime.client.core.Memory().Write(data+wipicAllocationOverhead, encoded); err != nil {
			t.Fatal(err)
		}
		thread := armcore.NewThread(armcore.Context{})
		for register, value := range []uint32{result + wipicAllocationOverhead, data, 0, uint32(len(encoded))} {
			if err := thread.SetRegister(register, value); err != nil {
				t.Fatal(err)
			}
		}
		if code, err := runtime.wipicCreateImage(thread); err != nil || code != 1 {
			t.Fatalf("round %d: MC_grpCreateImage() = %#x, %v", round, code, err)
		}
		handles, err := runtime.readAOTWords(result+wipicAllocationOverhead, 1, "created image handle")
		if err != nil {
			t.Fatal(err)
		}
		destroyThroughTable(t, runtime, 33, handles[0])
		if used := runtime.arena.used(); used != settled {
			t.Fatalf("round %d left %d bytes handed out, want %d", round, used, settled)
		}
	}
}

// MC_grpDestroyImage reads the encoded source buffer out of word thirteen of
// the image record. A handle that is not an image record — an off-screen
// framebuffer handed to the wrong destroy — has no word thirteen of its own,
// and the word at that offset belongs to whatever was allocated next. The
// neighbours here are framebuffer records whose fields are themselves handles,
// so reading past the block can name a live block and free something the game
// is still drawing with.
func TestDestroyImageDoesNotReadPastTheBlockItWasGiven(t *testing.T) {
	client, runtime := newTestRuntime(t)

	victim := createOffScreenThroughTable(t, runtime, 8, 8)
	target := createOffScreenThroughTable(t, runtime, 4, 4)
	neighbour := callocThroughSVC(t, runtime, 128)

	// Word thirteen of an image record living at target sits sixty-four bytes
	// past its handle, which is inside the neighbour rather than inside target.
	const sourceWordOffset = 64
	stray := target + sourceWordOffset
	if stray < neighbour || uint64(stray) >= uint64(neighbour)+runtime.wipicAllocations[neighbour] {
		t.Skipf("the arena did not lay the blocks out adjacently: %#x is not inside %#x", stray, neighbour)
	}
	var handle [4]byte
	binary.LittleEndian.PutUint32(handle[:], victim)
	if err := client.core.Memory().Write(stray, handle[:]); err != nil {
		t.Fatal(err)
	}

	destroyThroughTable(t, runtime, 33, target)

	if _, ok := runtime.wipicAllocations[victim]; !ok {
		t.Fatal("a destroy read past its block and freed a live framebuffer")
	}
}

// The image handle is what every C-side draw is given, so a destroy that took
// the pixels but left the record — or the other way round — would be found by
// the next title rather than here.
func TestDestroyImageLeavesNothingReadable(t *testing.T) {
	_, runtime := newTestRuntime(t)
	encoded := encodePalettedPNG(t, 2, 2)

	data, err := runtime.allocateWIPIC(uint32(len(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.client.core.Memory().Write(data+wipicAllocationOverhead, encoded); err != nil {
		t.Fatal(err)
	}
	result, err := runtime.allocateWIPIC(4)
	if err != nil {
		t.Fatal(err)
	}
	thread := armcore.NewThread(armcore.Context{})
	for register, value := range []uint32{result + wipicAllocationOverhead, data, 0, uint32(len(encoded))} {
		if err := thread.SetRegister(register, value); err != nil {
			t.Fatal(err)
		}
	}
	if code, err := runtime.wipicCreateImage(thread); err != nil || code != 1 {
		t.Fatalf("MC_grpCreateImage() = %#x, %v", code, err)
	}
	handles, err := runtime.readAOTWords(result+wipicAllocationOverhead, 1, "created image handle")
	if err != nil {
		t.Fatal(err)
	}
	image := handles[0]
	if runtime.framebufferOpacityOf(image) == nil {
		t.Fatal("the image records no transparency to begin with")
	}

	destroyThroughTable(t, runtime, 33, image)

	if _, ok := runtime.wipicAllocations[image]; ok {
		t.Fatal("the image record is still recorded as live")
	}
	if _, ok := runtime.wipicAllocations[data]; ok {
		t.Fatal("the encoded source buffer is still recorded as live")
	}
	if runtime.framebufferOpacityOf(image) != nil {
		t.Fatal("the destroyed image kept its transparency mask")
	}
}
