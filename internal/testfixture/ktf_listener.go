package testfixture

import "encoding/binary"

const (
	KTFListenerClass           = "fixture/Listener"
	KTFListenerStartupCounter  = ktfImageBase + 0xc00
	KTFListenerHistoryCount    = ktfImageBase + 0xc04
	KTFListenerRestartOnce     = ktfImageBase + 0xc08
	KTFListenerRestartBody     = ktfImageBase + 0xc0c
	KTFListenerRestartResult   = ktfImageBase + 0xc10
	KTFListenerHistory         = ktfImageBase + 0xc20
	KTFListenerHistoryCapacity = 64
)

// KTFListenerArchive contains a newly authored application and PlayListener.
// Its Thumb callback records up to 64 receiver/Clip/event/parm word tuples in
// image memory. No Host callback implements the listener's behavior.
func KTFListenerArchive() ([]byte, error) {
	data := make([]byte, 0x2000)
	words := func(offset int, values ...uint32) {
		for index, value := range values {
			binary.LittleEndian.PutUint32(data[offset+4*index:], value)
		}
	}
	thumb := func(offset int, values ...uint16) {
		for index, value := range values {
			binary.LittleEndian.PutUint16(data[offset+2*index:], value)
		}
	}
	// The entry exposes the ordinary executable interface; GetClass returns
	// the authored application class, whose constructor has no side effects.
	thumb(0x00, 0x4800, 0x4770)
	words(0x04, ktfImageBase+0x40)
	thumb(0x20, 0x2000, 0x4770)
	thumb(0x28, 0x4800, 0x4770)
	words(0x2c, ktfImageBase+0x400)
	words(0x40, ktfImageBase+0x80, ktfImageBase+0xc0, 0, 0, 0, ktfImageBase+0x21, 0, 0, 0, 0)
	words(0x80, ktfImageBase+0xa0, ktfImageBase+0xd0, 0, 0, 0, 0, 0, 0)
	words(0xa0, 0, 0, ktfImageBase+0x21, 0, ktfImageBase+0x29, 0, 0)
	copy(data[0xc0:], "WIPI_exe\x00")
	copy(data[0xd0:], "ExeInterface\x00")
	thumb(0x100, 0x4770)
	thumb(0x110, 0x4802, 0x6801, 0x3101, 0x6001, 0x4770, 0x46c0)
	words(0x11c, KTFListenerStartupCounter)
	// AOT arguments are r0=context, r1=receiver, r2=Clip, r3=event,
	// [sp]=parm. Preserve callee-saved registers while storing the tuple.
	thumb(0x180,
		0xb5f0, // push {r4-r7,lr}
		0x4c1b, // ldr r4, [pc,#108]: history count address
		0x6825, // ldr r5, [r4]
		0x2d40, // cmp r5, #64
		0xd217, // bhs return
		0x012e, // lsls r6, r5, #4
		0x4f19, // ldr r7, [pc,#100]: history address
		0x19f6, // adds r6, r6, r7
		0x6031, // str r1, [r6]
		0x6072, // str r2, [r6,#4]
		0x60b3, // str r3, [r6,#8]
		0x9f05, // ldr r7, [sp,#20]: caller's parm
		0x60f7, // str r7, [r6,#12]
		0x3501, // adds r5, #1
		0x6025, // str r5, [r4]
		// An optional END callback restarts its Clip once through the real
		// Player.play direct entry installed by the test after class loading.
		0x2b01, // cmp r3, #1: END_OF_DATA
		0xd10b, // bne return
		0x4c15, // ldr r4, [pc,#84]: restart controls
		0x6825, // ldr r5, [r4]
		0x2d00, // cmp r5, #0
		0xd007, // beq return
		0x2500, // movs r5, #0
		0x6025, // str r5, [r4]: consume before calling back into Player
		0x4611, // mov r1, r2: Clip
		0x2200, // movs r2, #0: repeat=false
		0x2000, // movs r0, #0: context
		0x6863, // ldr r3, [r4,#4]: Player.play direct stub
		0x4798, // blx r3
		0x60a0, // str r0, [r4,#8]: boolean return value
		0xbdf0, // pop {r4-r7,pc}
	)
	words(0x1f0, KTFListenerHistoryCount, KTFListenerHistory, KTFListenerRestartOnce)
	// Three concrete methods, one declared PlayListener interface, and no
	// fields. Each method name carries the AOT prefix byte.
	words(0x400, ktfImageBase+0x404, 0, ktfImageBase+0x440, ktfImageBase+0x630)
	binary.LittleEndian.PutUint16(data[0x410:], 3)
	words(0x440, ktfImageBase+0x800, 0, 0, ktfImageBase+0x600, ktfImageBase+0x640, ktfImageBase+0x620)
	binary.LittleEndian.PutUint16(data[0x458:], 3)
	binary.LittleEndian.PutUint16(data[0x45a:], 4)
	binary.LittleEndian.PutUint16(data[0x45c:], 0x21)
	binary.LittleEndian.PutUint16(data[0x45e:], 1)
	words(0x500, ktfImageBase+0x101, ktfImageBase+0x400, 0, ktfImageBase+0x820, 0, 1<<16, 0)
	words(0x520, ktfImageBase+0x111, ktfImageBase+0x400, 0, ktfImageBase+0x840, 0, 1<<16|1, 0)
	words(0x540, ktfImageBase+0x181, ktfImageBase+0x400, 0, ktfImageBase+0x880, 0, 1<<16|2, 0)
	words(0x600, ktfImageBase+0x500, ktfImageBase+0x520, ktfImageBase+0x540, 0)
	words(0x630, ktfImageBase+0x500, ktfImageBase+0x520, ktfImageBase+0x540)
	words(0x640, ktfImageBase+0x680)
	words(0x680, ktfImageBase+0x684, 0, ktfImageBase+0x6c0, 0)
	words(0x6c0, ktfImageBase+0x920, 0, 0, ktfImageBase+0x720, 0, ktfImageBase+0x724)
	binary.LittleEndian.PutUint16(data[0x6da:], 4)
	binary.LittleEndian.PutUint16(data[0x6dc:], 0x601)
	copy(data[0x800:], KTFListenerClass+"\x00")
	copy(data[0x820:], "\x00()V+<init>\x00")
	copy(data[0x840:], "\x00([Ljava/lang/String;)V+startApp\x00")
	copy(data[0x880:], "\x00(Lorg/kwis/msp/media/Clip;II)V+playUpdate\x00")
	copy(data[0x920:], "org/kwis/msp/media/PlayListener\x00")
	jar, err := fixtureZIP([]fixtureEntry{{"client.bin0", data}})
	if err != nil {
		return nil, err
	}
	return fixtureZIP([]fixtureEntry{{"__adf__", []byte("AID:fixture\nPID:P0001\nMClass:fixture.Listener\n")}, {"fixture.jar", jar}})
}
