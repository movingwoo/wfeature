package testfixture

import "encoding/binary"

const (
	KTFNativeCheckpointApplicationID   = 0x10203040
	KTFNativeCheckpointStartupCounter  = ktfImageBase + 0x248
	KTFNativeCheckpointFrameCounter    = ktfImageBase + 0x24c
	KTFNativeCheckpointLastEvent       = ktfImageBase + 0x250
	KTFNativeCheckpointLastKey         = ktfImageBase + 0x254
	KTFNativeCheckpointCallbackCounter = ktfImageBase + 0x25c
	KTFNativeCheckpointCallback        = ktfImageBase + 0x180
)

// KTFNativeCheckpointArchive authors the native package's entry, factory,
// application events and a scheduled frame entirely in ARM instructions. Every
// frame changes the red channel and presents the screen through ordinary slots.
func KTFNativeCheckpointArchive() ([]byte, error) {
	module := make([]byte, 0x1000)
	words := func(offset int, values ...uint32) {
		for index, value := range values {
			binary.LittleEndian.PutUint32(module[offset+4*index:], value)
		}
	}
	// Entry writes the factory through its third argument. The factory writes
	// the applet through its fourth argument and retains the platform object.
	words(0x00, 0xe59f3010, 0xe5823000, 0xe3a00001, 0xe12fff1e)
	words(0x18, ktfImageBase+0x200)
	words(0x20, 0xe59f0010, 0xe5830000, 0xe5801004, 0xe3a00001, 0xe12fff1e)
	words(0x38, ktfImageBase+0x240)
	// Record each event/key. Only event zero increments startup and obtains
	// the display interface before scheduling the 16 ms frame callback.
	words(0x40,
		0xe92d4010, 0xe1a04000, 0xe5841010, 0xe5842014, 0xe3510000, 0x1a000011,
		0xe5941008, 0xe2811001, 0xe5841008, 0xe5940004, 0xe59f1038, 0xe2842018,
		0xe590c000, 0xe59cc008, 0xe1a0e00f, 0xe12fff1c,
		0xe5940004, 0xe3a01010, 0xe59f201c, 0xe1a03004, 0xe590c000, 0xe59cc02c,
		0xe1a0e00f, 0xe12fff1c, 0xe3a00001, 0xe8bd8010,
		0x01001001, ktfImageBase+0x100)
	// Frame increments its counter, fills the screen, and presents it.
	words(0x100,
		0xe92d4010, 0xe1a04000, 0xe594300c, 0xe2833001, 0xe584300c, 0xe1a03c03,
		0xe5940018, 0xe3a01000, 0xe3a02000, 0xe590c000, 0xe59cc014,
		0xe1a0e00f, 0xe12fff1c,
		0xe5940018, 0xe590c000, 0xe59cc01c, 0xe1a0e00f, 0xe12fff1c,
		0xe3a00001, 0xe8bd8010)
	// A listener/resume callback increments the word its context points at.
	words(0x180, 0xe5903000, 0xe2833001, 0xe5803000, 0xe3a00001, 0xe12fff1e)
	words(0x200, ktfImageBase+0x210, 1, 0, 0)
	words(0x210, ktfImageBase+0x0c, ktfImageBase+0x0c, ktfImageBase+0x20, 0, 0)
	words(0x240, ktfImageBase+0x280)
	words(0x280, ktfImageBase+0x0c, ktfImageBase+0x0c, ktfImageBase+0x40)

	// One numeric applet record, a three-word identity trailer, and the
	// package's closing marker. No handset or third-party package bytes.
	info := make([]byte, 0x74)
	put := func(offset int, value uint32) { binary.LittleEndian.PutUint32(info[offset:], value) }
	put(0x08, 0x20)
	put(0x0c, 0x28)
	put(0x10, 0x48)
	put(0x14, 1)
	put(0x18, 0x50)
	put(0x48, 0x50)
	put(0x4c, 0x64)
	put(0x50, KTFNativeCheckpointApplicationID)
	put(0x58, 1000)
	put(0x60, ktfImageBase)
	put(0x64, 1)
	put(0x68, KTFNativeCheckpointApplicationID)
	put(0x6c, 0xff)
	copy(info[0x70:], "1fim")
	return fixtureZIP([]fixtureEntry{{"checkpoint.mod", module}, {"checkpoint.mif", info}})
}
