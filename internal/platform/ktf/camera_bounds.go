package ktf

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// This revision clamps the camera against mapHeight - viewportHeight without
// bounding that maximum at zero. A map shorter than the selected handset then
// alternates between zero and a negative camera Y. This is a compatibility
// correction to guest logic, not a WIPI display contract or an ARM behavior.
const cameraBoundsImage = "9ba7fbd6552f8176dfe9411ba8815a33dcf463ff50418e9f8f92707244f99b9c"

const (
	cameraBoundsStart = uint32(0x11dbb8)
	cameraBoundsStore = uint32(0x11dbe2)
	cameraBoundsEnd   = uint32(0x11dc0e)
	cameraBoundsCode  = "f8b2da16b44d8741c0dca1ca0c1e7abcd29861b12a3d19556faf9965997ca1ba"
)

func (runtime *initializationRuntime) installCameraBoundsCompatibility() error {
	if fmt.Sprintf("%x", sha256.Sum256(runtime.client.image.Data)) != cameraBoundsImage {
		return nil
	}
	code := make([]byte, cameraBoundsEnd-cameraBoundsStart)
	if err := runtime.client.core.Memory().Read(cameraBoundsStart, code); err != nil {
		return err
	}
	// Check the relocated routine too. Unknown layouts retain their code.
	if fmt.Sprintf("%x", sha256.Sum256(code)) != cameraBoundsCode {
		return nil
	}
	// Replace only STR r3,[r0,#0x74]. The surrounding smoothing, horizontal
	// bounds, registers, flags and return sequence remain guest instructions.
	if err := runtime.client.core.Memory().Load(cameraBoundsStore, []byte{byte(svcCategoryCameraBounds), 0xdf}); err != nil {
		return err
	}
	runtime.cameraBoundsStore = cameraBoundsStore
	runtime.countDiagnostic("compatibility nonnegative camera bounds")
	return nil
}

func (runtime *initializationRuntime) storeCameraBounds(thread *armcore.Thread, call armcore.SupervisorCall) error {
	if runtime.cameraBoundsStore == 0 || call.Address != runtime.cameraBoundsStore || call.ResumePC != call.Address+2 {
		return fmt.Errorf("KTF camera compatibility call outside recognized instruction at %#x", call.Address)
	}
	base, err := thread.Register(0)
	if err != nil {
		return err
	}
	value, err := thread.Register(3)
	if err != nil {
		return err
	}
	if uint64(base)+0x74+4 > 1<<32 {
		return fmt.Errorf("KTF camera field address overflows at %#x", base)
	}
	if int32(value) < 0 {
		value = 0
		runtime.countDiagnostic("compatibility camera Y clamp")
	}
	var word [4]byte
	binary.LittleEndian.PutUint32(word[:], value)
	return runtime.client.core.Memory().Write(base+0x74, word[:])
}
