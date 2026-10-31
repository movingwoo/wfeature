package ktf

import (
	goruntime "runtime"
	"testing"
	"weak"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/jvm"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

// TestCheckpointPrunesADroppedImageSurface covers a quick save taken between
// an Image being dropped and the next round pruning its surface. A surface
// whose owner Go has collected cannot be named in the record, and the capture
// refused the whole quick save over it; it now prunes what the next round
// would have pruned and goes ahead.
func TestCheckpointPrunesADroppedImageSurface(t *testing.T) {
	archive, err := testfixture.KTFCheckpointArchive()
	if err != nil {
		t.Fatal(err)
	}
	store, err := backend.NewMemorySaveStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	session, err := StartSession(t.Context(), archive, SessionOptions{SaveStore: store})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	runtime := session.Client.runtime

	// Registered the way a mutable Image registers its framebuffer, and then
	// dropped by the title before any round ran.
	owner := func() weak.Pointer[jvm.Object] {
		handle, err := runtime.NewWIPICFramebufferRecord(4, 4)
		if err != nil {
			t.Fatal(err)
		}
		image := &jvm.Object{ClassName: "org/kwis/msp/lcdui/Image", Fields: map[string]jvm.Value{
			"guestFramebuffer:I": jvm.IntValue(int32(handle)),
		}}
		if runtime.imageSurfaces == nil {
			runtime.imageSurfaces = make(map[weak.Pointer[jvm.Object]]uint32)
		}
		pointer := weak.Make(image)
		runtime.imageSurfaces[pointer] = handle
		return pointer
	}()
	for range 3 {
		goruntime.GC()
	}
	if owner.Value() != nil {
		t.Skip("the dropped image was not collected")
	}

	if _, err := session.CaptureCheckpoint(t.Context()); err != nil {
		t.Fatalf("capture with a dropped image surface: %v", err)
	}
	if _, kept := runtime.imageSurfaces[owner]; kept {
		t.Fatal("the capture left the dropped image's surface registered")
	}
}
