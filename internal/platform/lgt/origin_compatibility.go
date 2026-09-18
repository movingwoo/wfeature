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
