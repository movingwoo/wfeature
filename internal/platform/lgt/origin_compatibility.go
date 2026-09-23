package lgt

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/compatibility"
)

const framebufferOriginAddress uint32 = 0x1ef8c

func hasFramebufferOriginCorrection(module []byte) bool {
	digest := sha256.Sum256(module)
	return compatibility.HasFix("lgt", compatibility.NativeModule, hex.EncodeToString(digest[:]), compatibility.LGTVisibleFramebufferOrigin)
}

func (client *Client) applyOriginCompatibility() error {
	digest := sha256.Sum256(client.archive.Module)
	if compatibility.HasFix("lgt", compatibility.NativeModule, hex.EncodeToString(digest[:]), compatibility.LGTColorKeySampleOrigin) {
		if err := correctColorKeySampleOrigin(client.core.Memory()); err != nil {
			return err
		}
		if client.logger != nil {
			client.logger.Debug("LGT compatibility applied", "fix", compatibility.LGTColorKeySampleOrigin)
		}
	}
	if compatibility.HasFix("lgt", compatibility.NativeModule, hex.EncodeToString(digest[:]), compatibility.LGTImageFramebufferOrigin) {
		if err := correctImageFramebufferOrigin(client.core.Memory()); err != nil {
			return err
		}
		if client.logger != nil {
			client.logger.Debug("LGT compatibility applied", "fix", compatibility.LGTImageFramebufferOrigin)
		}
	}
	if compatibility.HasFix("lgt", compatibility.NativeModule, hex.EncodeToString(digest[:]), compatibility.LGTDirectFramebufferOrigin) {
		if err := correctDirectFramebufferOrigin(client.core.Memory()); err != nil {
			return err
		}
		if client.logger != nil {
			client.logger.Debug("LGT compatibility applied", "fix", compatibility.LGTDirectFramebufferOrigin)
		}
	}
	if !hasFramebufferOriginCorrection(client.archive.Module) {
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
// See docs/lgt-qa-2026-09-21.md#image-path-origin.
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
// See docs/lgt-qa-2026-09-21.md#direct-framebuffer-origin.
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
