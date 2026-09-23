package lgt

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/compatibility"
)

func hasSaveIdentityCompatibility(module []byte) bool {
	digest := sha256.Sum256(module)
	return compatibility.HasFix("lgt", compatibility.NativeModule, hex.EncodeToString(digest[:]), compatibility.LGTSaveSubscriberIdentity)
}

// The recognized reader validates length, decodes the payload, compares the
// subscriber, and then validates the checksum. Only the subscriber comparison
// is relaxed. Original archives, stored identity bytes and checksums are retained.
func correctSaveSubscriberIdentity(memory *armcore.Memory) error {
	const address = 0x69d98
	want := []byte{0x00, 0x28, 0x02, 0xd0, 0x1b, 0x48, 0x1c, 0x49, 0x13, 0xe0}
	var code [10]byte
	if err := memory.Read(address, code[:]); err != nil {
		return fmt.Errorf("LGT save subscriber compatibility: %w", err)
	}
	if !bytes.Equal(code[:], want) {
		return fmt.Errorf("LGT save subscriber compatibility: unexpected validation branch")
	}
	// cmp r0,#0; beq checksum becomes cmp r0,#0; b checksum.
	return memory.Write(address+2, []byte{0x02, 0xe0})
}
