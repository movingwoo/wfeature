package lgt

import (
	"bytes"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/compatibility"
)

const framebufferOriginAddress uint32 = 0x1ef8c

func (client *Client) applyOriginCompatibility() error {
	if client.revision.Has(compatibility.LGTScreenStripConstant) {
		if err := clearScreenStripConstant(client.core.Memory(), client.revision.Digest()); err != nil {
			return err
		}
		if client.logger != nil {
			client.logger.Debug("LGT compatibility applied", "fix", compatibility.LGTScreenStripConstant)
		}
	}
	if client.revision.Has(compatibility.LGTColorKeySampleOrigin) {
		if err := correctColorKeySampleOrigin(client.core.Memory()); err != nil {
			return err
		}
		if client.logger != nil {
			client.logger.Debug("LGT compatibility applied", "fix", compatibility.LGTColorKeySampleOrigin)
		}
	}
	if client.revision.Has(compatibility.LGTImageFramebufferOrigin) {
		if err := correctImageFramebufferOrigin(client.core.Memory()); err != nil {
			return err
		}
		if client.logger != nil {
			client.logger.Debug("LGT compatibility applied", "fix", compatibility.LGTImageFramebufferOrigin)
		}
	}
	if client.revision.Has(compatibility.LGTDirectFramebufferOrigin) {
		if err := correctDirectFramebufferOrigin(client.core.Memory()); err != nil {
			return err
		}
		if client.logger != nil {
			client.logger.Debug("LGT compatibility applied", "fix", compatibility.LGTDirectFramebufferOrigin)
		}
	}
	if !client.revision.Has(compatibility.LGTVisibleFramebufferOrigin) {
		return nil
	}
	if err := correctFramebufferOrigin(client.core.Memory()); err != nil {
		return err
	}
	if client.logger != nil {
		client.logger.Debug("LGT compatibility applied", "fix", compatibility.LGTVisibleFramebufferOrigin)
	}
	return nil
}

// This revision shifts image rows but not platform primitives. Its 240/320
// width branch adds a 24-row strip that our visible framebuffer does not have.
// See docs/history/lgt.md#lgt-qa-2026-09-21-image-path-origin.
func correctImageFramebufferOrigin(memory *armcore.Memory) error {
	const address = 0x28a10
	var code [6]byte
	if err := memory.Read(address, code[:]); err != nil {
		return fmt.Errorf("LGT image framebuffer origin: %w", err)
	}
	// movs r2,#24; b store; movs r2,#20 (the separate 176-wide branch).
	if !bytes.Equal(code[:], []byte{0x18, 0x22, 0x02, 0xe0, 0x14, 0x22}) {
		return fmt.Errorf("LGT image framebuffer origin: unexpected initialization")
	}
	return memory.Write(address, []byte{0, 0x22})
}

// This revision adds a separate handset strip to its centered drawing origin
// before writing pixels directly. Keep centering and remove only the strip.
// See docs/history/lgt.md#lgt-qa-2026-09-21-direct-framebuffer-origin.
func correctDirectFramebufferOrigin(memory *armcore.Memory) error {
	const address = 0x12bc
	var code [6]byte
	if err := memory.Read(address, code[:]); err != nil {
		return fmt.Errorf("LGT direct framebuffer origin: %w", err)
	}
	// movs r3,#24; str r3,[r7]; str r0,[r4,#0x3c].
	if !bytes.Equal(code[:], []byte{0x18, 0x23, 0x3b, 0x60, 0xe0, 0x63}) {
		return fmt.Errorf("LGT direct framebuffer origin: unexpected initialization")
	}
	return memory.Write(address, []byte{0, 0x23})
}

// This revision adds a 24-row handset strip to its own display origin during
// initialization. Our framebuffer starts at the visible origin. Remove only
// that addition in mapped memory, before execution; archive bytes remain intact.
// See docs/platform-compatibility.md#lgt-lgt-visible-framebuffer-origin-2026-09-15. Never select this by instruction shape.
func correctFramebufferOrigin(memory *armcore.Memory) error {
	var code [8]byte
	if err := memory.Read(framebufferOriginAddress, code[:]); err != nil {
		return fmt.Errorf("LGT display origin compatibility: %w", err)
	}
	// adds r2,#0x90; ldr r3,[r2]; adds r3,#24; str r3,[r2].
	if !bytes.Equal(code[:], []byte{0x90, 0x32, 0x13, 0x68, 0x18, 0x33, 0x13, 0x60}) {
		return fmt.Errorf("LGT display origin compatibility: unexpected initialization")
	}
	// adds r3,#0 retains the existing origin without adding the handset strip.
	return memory.Write(framebufferOriginAddress+4, []byte{0x00, 0x33})
}

// The native blitter calibrates its key by drawing a pixel, then sampling the
// framebuffer through a pointer that includes a 24-row handset strip. Align
// the calibration write with that read without translating other drawing.
func correctColorKeySampleOrigin(memory *armcore.Memory) error {
	const address = 0x1742
	var code [6]byte
	if err := memory.Read(address, code[:]); err != nil {
		return fmt.Errorf("LGT color key sample origin: %w", err)
	}
	// adds r0,r6,#0; movs r1,#0; movs r2,#0.
	if !bytes.Equal(code[:], []byte{0x30, 0x1c, 0, 0x21, 0, 0x22}) {
		return fmt.Errorf("LGT color key sample origin: unexpected calibration")
	}
	return memory.Write(address+4, []byte{24, 0x22})
}

// A screenStripSite is one `movs` that loads the handset strip into a
// recognized module's screen setup — 24 rows, or 48 where the module counts
// them in the bytes of a 16-bit row's width — with the code around it that has
// to be there for it to be the instruction that was examined. at is where
// expect begins and immediate the offset in it of the instruction's immediate
// byte.
type screenStripSite struct {
	at        uint32
	expect    []byte
	immediate int
}

// screenStripSites are, by module digest, the sites lgt.screen_strip_constant
// clears. Each revision keeps its strip somewhere else — in the y of the
// rectangle it draws its screen into, in the pointer it writes the frame
// through, in the height it adds to its own, or in every routine that takes
// the screen's pixel pointer — so each has its own sites, and a digest the fix
// selects without sites here fails loading rather than run with the strip.
// Branches for a 176-wide screen or a 32-bit one are left alone: this
// platform's screen is neither. See docs/history/lgt.md#lgt-new-archives-2026-11-17.
var screenStripSites = map[string][]screenStripSite{
	// ldr r4,=screen; movs r3,#24; str r3,[r4,#4]: the screen rectangle's y.
	"2d3ba68dd05da42f4e7e82467c093f9bb7ec080739044270cf3fc4e17a781066": {
		{at: 0x436fc, expect: []byte{0x40, 0x4c, 0x18, 0x23, 0x63, 0x60}, immediate: 2},
	},
	// movs r3,#20; b scale; …; movs r3,#24; muls r3,r2: rows of the strip,
	// 20 on a 176-wide screen and 24 on a wider one, scaled to bytes and added
	// to the screen pointer. The 176 branch is left alone.
	"de22b7a161e64b843714a9ee815df1fd4daf6c480d4e7a442b5665315738fb43": {
		{at: 0x5319c, expect: []byte{0x14, 0x23, 0x03, 0xe0, 0x1b, 0x06, 0x1b, 0x0e, 0x5a, 0x43, 0x18, 0x23, 0x53, 0x43}, immediate: 10},
	},
	// ldr r4,=strip; str r2,[r3]; movs r3,#24; str r3,[r4]: the strip the
	// module adds to the screen height, in the initialization that runs, and
	// in an identical copy of it that no local run reaches.
	"3fb3d20a77abe89a2483b59d2eb3af355a9d475d262afc47aa5f4a3cd8c15f08": {
		{at: 0x2980e, expect: []byte{0x4f, 0x4c, 0x1a, 0x60, 0x18, 0x23, 0x23, 0x60}, immediate: 4},
		{at: 0x25302, expect: []byte{0x4f, 0x4b, 0x18, 0x22, 0x1a, 0x60}, immediate: 2},
	},
	// Every routine that takes the screen's pixel pointer adds the strip to it
	// itself: the 16-bit fill and one blit scale the screen width by 48
	// (`movs r3,#48`, beside the fill's 176-wide `#40`), and four more scale
	// the row stride by 24 (`movs rN,#24`), three of them for the pointer the
	// module caches between calls.
	"272c563375b086a182d8214f151ee4a75d3158bb1bd0f6a10ccb8979d1d18afc": {
		{at: 0x4d228, expect: []byte{0xef, 0x2a, 0x01, 0xdc, 0x28, 0x23, 0x00, 0xe0, 0x30, 0x23, 0x53, 0x43}, immediate: 8},
		{at: 0x4d62a, expect: []byte{0x23, 0x69, 0x52, 0x00, 0x18, 0x20, 0x87, 0x49, 0x02, 0x92}, immediate: 4},
		{at: 0x4d650, expect: []byte{0x23, 0x69, 0x18, 0x21, 0x4b, 0x43, 0xc0, 0x18}, immediate: 2},
		{at: 0x4ef54, expect: []byte{0x22, 0x68, 0x18, 0x23, 0x53, 0x43, 0xc0, 0x18}, immediate: 2},
		{at: 0x503da, expect: []byte{0x0b, 0x68, 0x18, 0x21, 0x4b, 0x43, 0xc0, 0x18}, immediate: 2},
		{at: 0x50ee0, expect: []byte{0xc0, 0x49, 0x30, 0x23, 0x0a, 0x68, 0x53, 0x43, 0xc6, 0x18}, immediate: 2},
	},
	// The same in seven routines, each scaling the surface's row stride by
	// `movs r3,#24` right after it takes the screen's pixel pointer.
	"eb92c694ea9d88ff53690ea464114b907ce83225b745e55f10a86681a9aae6f0": {
		{at: 0x1f60, expect: []byte{0xe2, 0x6c, 0x18, 0x23, 0x53, 0x43, 0xc0, 0x18}, immediate: 2},
		{at: 0x2696, expect: []byte{0xf2, 0x6c, 0x18, 0x23, 0x53, 0x43, 0xc0, 0x18}, immediate: 2},
		{at: 0x27da, expect: []byte{0xea, 0x6c, 0x18, 0x23, 0x53, 0x43, 0xaa, 0x6e, 0xc0, 0x18}, immediate: 2},
		{at: 0x4455c, expect: []byte{0x10, 0x99, 0x18, 0x23, 0xca, 0x6c, 0x53, 0x43, 0xc0, 0x18}, immediate: 2},
		{at: 0x4534c, expect: []byte{0x13, 0x9a, 0x18, 0x23, 0xd2, 0x6c, 0x9c, 0x46, 0x63, 0x46, 0x53, 0x43, 0xc0, 0x18}, immediate: 2},
		{at: 0x459e6, expect: []byte{0x16, 0x99, 0x18, 0x23, 0xcd, 0x6c, 0x17, 0x9a, 0x6b, 0x43, 0xc0, 0x18}, immediate: 2},
		{at: 0x45efe, expect: []byte{0x11, 0x99, 0x18, 0x23, 0xca, 0x6c, 0x53, 0x43, 0xc0, 0x18}, immediate: 2},
	},
	// The drawing routines add this row count times the stride to the screen
	// pointer. The width <= 128 branch stores 14 in the same word.
	"ca3f14e043fcd1659b894144d31053042d7f8f0f8d2a4c07b3aff8ab8879f8e4": {
		{at: 0x211e, expect: []byte{0xa4, 0x4a, 0x18, 0x23, 0x13, 0x60}, immediate: 2},
	},
}

// clearScreenStripConstant replaces the strip each site loads with 0, after
// checking every site, so a module that is not the one examined is refused
// with nothing written. Mapped code changes; the archive does not.
func clearScreenStripConstant(memory *armcore.Memory, digest string) error {
	sites, known := screenStripSites[digest]
	if !known {
		return fmt.Errorf("LGT screen strip: no sites for this module")
	}
	for _, site := range sites {
		found := make([]byte, len(site.expect))
		if err := memory.Read(site.at, found); err != nil {
			return fmt.Errorf("LGT screen strip: %w", err)
		}
		if !bytes.Equal(found, site.expect) || !stripImmediate(site) {
			return fmt.Errorf("LGT screen strip: unexpected code at %#x", site.at)
		}
	}
	for _, site := range sites {
		if err := memory.Write(site.at+uint32(site.immediate), []byte{0}); err != nil {
			return fmt.Errorf("LGT screen strip: %w", err)
		}
	}
	return nil
}

// stripImmediate reports whether a site names the immediate of a Thumb
// `movs rN,#imm8` holding the strip: 24 rows, or 48 bytes for them per pixel
// column of a 16-bit row.
func stripImmediate(site screenStripSite) bool {
	if site.immediate < 0 || site.immediate+1 >= len(site.expect) {
		return false
	}
	value, opcode := site.expect[site.immediate], site.expect[site.immediate+1]
	return opcode&0xf8 == 0x20 && (value == 24 || value == 48)
}
