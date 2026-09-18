package ktf

import (
	"runtime"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
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
	rt.collectHostResources()
	if len(rt.imageSurfaces) != 1 {
		t.Fatal("live Graphics lost its Image")
	}
	runtime.KeepAlive(graphics)
	for i := 0; i < 8; i++ {
		handle := makeOrphanImage(t, rt)
		runtime.GC()
		rt.collectHostResources()
		if _, exists := rt.wipicAllocations[handle]; exists {
			t.Fatal("orphan Image retained its surface")
		}
	}
}

func makeOrphanClip(t *testing.T, rt *initializationRuntime, play bool) backend.AudioHandle {
	t.Helper()
	object := &jvm.Object{ClassName: "org/kwis/msp/media/Clip"}
	state := rt.clip(object)
	handle, err := rt.client.audio.Load(oneNoteSMAF())
	if err != nil {
		t.Fatal(err)
	}
	state.handle, state.loaded = handle, true
	if play {
		if err := rt.client.audio.Play(handle, 0, false); err != nil {
			t.Fatal(err)
		}
	}
	return handle
}

func TestOrphanClipWaitsForPlaybackThenCloses(t *testing.T) {
	client, rt := newTestRuntime(t)
	client.audio = backend.NewAudio(nil)
	makeOrphanClip(t, rt, false)
	playing := makeOrphanClip(t, rt, true)
	runtime.GC()
	rt.collectHostResources()
	if len(rt.clips) != 1 || !client.audio.Playing(playing) {
		t.Fatal("playing orphan was lost or stopped orphan retained")
	}
	client.audio.Advance(time.Hour)
	rt.collectHostResources()
	if len(rt.clips) != 0 {
		t.Fatal("finished orphan retained")
	}
	if err := client.audio.Play(playing, time.Hour, false); err == nil {
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
		if _, exists := rt.wipicAllocations[handle]; exists {
			t.Fatal("collected guest Image retains its surface")
		}
	}
	if !reused {
		t.Fatal("fixture did not exercise guest address reuse")
	}
	old := makeOrphanImage(t, rt)
	rt.destroyWIPICFramebufferRecord(old)
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
	rt.collectHostResources()
	if _, exists := rt.wipicAllocations[uint32(handle)]; !exists {
		t.Fatal("old owner freed the replacement surface")
	}
	runtime.KeepAlive(object)
}
