package ktf

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"slices"

	"github.com/movingwoo/wfeature/internal/armcore"
)

// This revision centers its dialog background using the screen height but
// positions the bitmap text for a 280-pixel screen. The settings submenu has
// its own layout, including separate vibration on/off branches. Adapt the
// text argument stores in both layouts; this is not a WIPI text baseline rule.
const menuTextImage = "d4fd51c94269e3143b57802c00898f2ce43dee2e5726076d133b44a0304809c6"

const (
	menuTextStart         = uint32(0x129488)
	menuTextTitle         = uint32(0x1294c2)
	menuTextBody          = uint32(0x1294ec)
	menuTextEnd           = uint32(0x129516)
	menuTextCode          = "b48efed39742fd25c261d96b7cffd33bc30433bdb25224b975bcd458cecb92c8"
	menuTextSettingsTitle = uint32(0x129740)
	menuTextToggleOn      = uint32(0x129784)
	menuTextVolume        = uint32(0x1297c0)
	menuTextSpeed         = uint32(0x1297fa)
	menuTextToggleOff     = uint32(0x129828)
	menuTextSettingsStart = uint32(0x1296e2)
	menuTextSettingsEnd   = uint32(0x129836)
	menuTextSettingsCode  = "ca7b212aa75dc18c2269aaa0e76cb806a2d5fee8547c42c15ced6e767c5d0b08"
)

var menuTextStores = [...]uint32{menuTextTitle, menuTextBody, menuTextSettingsTitle, menuTextToggleOn, menuTextVolume, menuTextSpeed, menuTextToggleOff}

func (runtime *initializationRuntime) installMenuTextCompatibility() error {
	if fmt.Sprintf("%x", sha256.Sum256(runtime.client.image.Data)) != menuTextImage {
		return nil
	}
	// Validate both layouts before installing any store. A changed submenu
	// must not leave a partially adapted dialog family.
	for _, region := range []struct {
		start, end uint32
		digest     string
	}{
		{menuTextStart, menuTextEnd, menuTextCode},
		{menuTextSettingsStart, menuTextSettingsEnd, menuTextSettingsCode},
	} {
		code := make([]byte, region.end-region.start)
		if err := runtime.client.core.Memory().Read(region.start, code); err != nil {
			return err
		}
		if fmt.Sprintf("%x", sha256.Sum256(code)) != region.digest {
			return nil
		}
	}
	for _, address := range menuTextStores {
		// Every instruction is STR r3,[sp,#4]. Keep r3 and the flags intact.
		if err := runtime.client.core.Memory().Load(address, []byte{byte(svcCategoryMenuText), 0xdf}); err != nil {
			return err
		}
	}
	runtime.menuTextCompatibility = true
	runtime.countDiagnostic("compatibility centered menu text")
	return nil
}

func (runtime *initializationRuntime) storeMenuTextY(thread *armcore.Thread, call armcore.SupervisorCall) error {
	if !runtime.menuTextCompatibility || !slices.Contains(menuTextStores[:], call.Address) || call.ResumePC != call.Address+2 {
		return fmt.Errorf("KTF menu text compatibility call outside recognized instruction at %#x", call.Address)
	}
	stack, err := thread.Register(13)
	if err != nil {
		return err
	}
	value, err := thread.Register(3)
	if err != nil {
		return err
	}
	if uint64(stack)+8 > 1<<32 {
		return fmt.Errorf("KTF menu text stack address overflows at %#x", stack)
	}
	_, height := runtime.screenSize()
	// The guest centers the panel at floor(height/2). At height 280 its
	// fixed title/body coordinates already line up with the panel artwork.
	value += uint32(height/2 - 140)
	var word [4]byte
	binary.LittleEndian.PutUint32(word[:], value)
	return runtime.client.core.Memory().Write(stack+4, word[:])
}
