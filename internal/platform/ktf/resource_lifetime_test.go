package ktf

import (
	"runtime"
	goruntime "runtime"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt/wipicrttest"
)

func makeOrphanImage(t *testing.T, rt *initializationRuntime) uint32 {
	t.Helper()
	value, err := runtimeImageCreateSized(rt, rt.client.vm, []jvm.Value{jvm.IntValue(2), jvm.IntValue(2)})
	if err != nil {
		t.Fatal(err)
	}
	object, _ := value.Reference()
	handle, _ := object.Fields["guestFramebuffer:I"].Int32()
	return uint32(handle)
}

func TestOrphanImageSurfaceIsReclaimedAndGraphicsRetainsImage(t *testing.T) {
	_, rt := newTestRuntime(t)
	value, err := runtimeImageCreateSized(rt, rt.client.vm, []jvm.Value{jvm.IntValue(2), jvm.IntValue(2)})
	if err != nil {
		t.Fatal(err)
	}
	graphics, err := runtimeImageGetGraphics(rt, rt.client.vm, []jvm.Value{value})
	if err != nil {
		t.Fatal(err)
	}
	value = jvm.VoidValue()
	runtime.GC()
	rt.collectHostObjects()
	if len(rt.imageSurfaces) != 1 {
		t.Fatal("live Graphics lost its Image")
	}
	runtime.KeepAlive(graphics)
	for i := 0; i < 8; i++ {
		handle := makeOrphanImage(t, rt)
		runtime.GC()
		rt.collectHostObjects()
		if _, exists := rt.WIPICAllocations[handle]; exists {
			t.Fatal("orphan Image retained its surface")
		}
	}
}

func makeOrphanClip(t *testing.T, rt *initializationRuntime, play bool) backend.AudioHandle {
	t.Helper()
	object := &jvm.Object{ClassName: "org/kwis/msp/media/Clip"}
	state := rt.clip(object)
	handle, err := rt.client.Audio.Load(wipicrttest.OneNoteSMAF())
	if err != nil {
		t.Fatal(err)
	}
	state.Handle, state.Loaded = handle, true
	if play {
		if err := rt.client.Audio.Play(handle, 0, false); err != nil {
			t.Fatal(err)
		}
	}
	return handle
}

func TestOrphanClipWaitsForPlaybackThenCloses(t *testing.T) {
	client, rt := newTestRuntime(t)
	client.Audio = backend.NewAudio(nil)
	makeOrphanClip(t, rt, false)
	playing := makeOrphanClip(t, rt, true)
	runtime.GC()
	rt.collectHostObjects()
	if len(rt.clips) != 1 || !client.Audio.Playing(playing) {
		t.Fatal("playing orphan was lost or stopped orphan retained")
	}
	client.Audio.Advance(time.Hour)
	rt.collectHostObjects()
	if len(rt.clips) != 0 {
		t.Fatal("finished orphan retained")
	}
	if err := client.Audio.Play(playing, time.Hour, false); err == nil {
		t.Fatal("finished orphan handle was not closed")
	}
}

func bindOrphanImage(t *testing.T, rt *initializationRuntime) (uint32, uint32) {
	t.Helper()
	value, err := runtimeImageCreateSized(rt, rt.client.vm, []jvm.Value{jvm.IntValue(2), jvm.IntValue(2)})
	if err != nil {
		t.Fatal(err)
	}
	object, _ := value.Reference()
	if err := rt.ensureResultBound(object); err != nil {
		t.Fatal(err)
	}
	address, _ := rt.client.vm.AOTAddress(object)
	handle, _ := object.Fields["guestFramebuffer:I"].Int32()
	return address, uint32(handle)
}

func TestGuestImageCollectionAndReusedSurfaceOwnership(t *testing.T) {
	_, rt := newCollectorRuntime(t)
	addresses := make(map[uint32]bool)
	reused := false
	for i := 0; i < 12; i++ {
		address, handle := bindOrphanImage(t, rt)
		reused = reused || addresses[address]
		addresses[address] = true
		collectTwice(t, rt)
		if _, exists := rt.objects[address]; exists {
			t.Fatal("orphan guest Image remains rooted")
		}
		if _, exists := rt.WIPICAllocations[handle]; exists {
			t.Fatal("collected guest Image retains its surface")
		}
	}
	if !reused {
		t.Fatal("fixture did not exercise guest address reuse")
	}
	old := makeOrphanImage(t, rt)
	rt.DestroyWIPICFramebufferRecord(old)
	value, err := runtimeImageCreateSized(rt, rt.client.vm, []jvm.Value{jvm.IntValue(2), jvm.IntValue(2)})
	if err != nil {
		t.Fatal(err)
	}
	object, _ := value.Reference()
	handle, _ := object.Fields["guestFramebuffer:I"].Int32()
	if uint32(handle) != old {
		t.Fatal("fixture did not reuse the framebuffer address")
	}
	runtime.GC()
	rt.collectHostObjects()
	if _, exists := rt.WIPICAllocations[uint32(handle)]; !exists {
		t.Fatal("old owner freed the replacement surface")
	}
	runtime.KeepAlive(object)
}

func orphanDecodedImageSurface(t *testing.T, rt *initializationRuntime) uint32 {
	t.Helper()
	value, err := runtimeImageFromEncoded(rt, wipicrttest.EncodePalettedPNG(t, 2, 2))
	if err != nil {
		t.Fatal(err)
	}
	object, _ := value.Reference()
	handle, err := rt.imageFramebufferHandle(object)
	if err != nil {
		t.Fatal(err)
	}
	return handle
}

func orphanCopiedImageSurface(t *testing.T, rt *initializationRuntime, source jvm.Value, sub bool) uint32 {
	t.Helper()
	var value jvm.Value
	var err error
	if sub {
		value, err = runtimeImageCreateSubImage(rt, rt.client.vm, []jvm.Value{source, jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(1), jvm.IntValue(1), jvm.IntValue(0)})
	} else {
		value, err = runtimeImageCreateCopy(rt, rt.client.vm, []jvm.Value{source})
	}
	if err != nil {
		t.Fatal(err)
	}
	object, _ := value.Reference()
	handle, err := object.Fields["guestFramebuffer:I"].Int32()
	if err != nil {
		t.Fatal(err)
	}
	return uint32(handle)
}

// The frame buffer a decoded image is rasterized into on first use, and the
// one createImage(Image) and createSubImage copy into, go back to the arena
// when the image dies, as createImage(int, int) always did.
func TestImageSurfacesMadeLazilyOrByCopyAreReclaimed(t *testing.T) {
	_, rt := newTestRuntime(t)
	for i := 0; i < 4; i++ {
		handle := orphanDecodedImageSurface(t, rt)
		goruntime.GC()
		rt.collectHostObjects()
		if _, exists := rt.WIPICAllocations[handle]; exists {
			t.Fatal("a dead decoded image kept the frame buffer it was rasterized into")
		}
	}
	source, err := runtimeImageFromEncoded(rt, wipicrttest.EncodePalettedPNG(t, 2, 2))
	if err != nil {
		t.Fatal(err)
	}
	sourceObject, _ := source.Reference()
	sourceHandle, err := rt.imageFramebufferHandle(sourceObject)
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range []bool{false, true} {
		for i := 0; i < 4; i++ {
			handle := orphanCopiedImageSurface(t, rt, source, sub)
			goruntime.GC()
			rt.collectHostObjects()
			if _, exists := rt.WIPICAllocations[handle]; exists {
				t.Fatalf("a dead copied image (sub-image %v) kept its frame buffer", sub)
			}
			if _, exists := rt.WIPICAllocations[sourceHandle]; !exists {
				t.Fatal("a live source image lost its frame buffer")
			}
		}
	}
	goruntime.KeepAlive(sourceObject)
}
