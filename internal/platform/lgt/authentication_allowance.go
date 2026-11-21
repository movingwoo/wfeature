package lgt

import (
	"bytes"
	"fmt"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/compatibility"
)

// An allowanceSite is one instruction of a recognized module's first-run
// authentication scene: expect is the code that has to be at `at` once the
// module is loaded, and replace the two bytes written at offset in it.
type allowanceSite struct {
	at      uint32
	expect  []byte
	offset  int
	replace [2]byte
}

// allowanceDismissal is the scene's handling of OK on its failure notice in
// every recognized revision: adds r0,#5; bne out; clear the sub-state; count
// the failure; cmp r3,#4; ble out; bl complete; b out. Up to the fourth
// failure the notice returns to the prompt; the fifth calls the routine a
// successful reply reaches, which records the authentication in the title's
// setup file. The comparison becomes cmp r3,#0, so the first failure is the
// one the title lets through.
var allowanceDismissal = allowanceSite{
	at: 0x19cb4,
	expect: []byte{0x05, 0x30, 0x0d, 0xd1, 0x00, 0x23, 0xeb, 0x60, 0xab, 0x6a, 0x01, 0x33, 0xab, 0x62,
		0x04, 0x2b, 0x06, 0xdd, 0xff, 0xf7, 0x91, 0xff, 0x03, 0xe0},
	offset: 14, replace: [2]byte{0x00, 0x2b},
}

// allowanceSites are, by module digest, where lgt.authentication_allowance
// shortens a title's own allowance for failed attempts at its one-time
// network authentication. The title asks once whether to authenticate. Every
// attempt fails here, because nothing answers its server, and the title gives
// the attempt up after five failures in one visit of the scene, as it would
// have on a handset out of service. A player sees the same failure notice
// five times before that, which reads as a title that cannot be started. The
// prompt, the attempt, one failure notice and the refusal of No stay. A digest
// the fix selects without a site here fails starting.
// See docs/authentication.md#lgt-authentication-allowance.
var allowanceSites = map[string]allowanceSite{
	"f159d0d09bb8ae3347e427dea4d0be452d913b64e7d0a6949a955af0a539601b": allowanceDismissal,
	// A modification of the same title whose network start differs elsewhere.
	"2cac5015ed4a7642d7a2781a30f7fcb9ff764fa7b7850bef6aeb1eea8f22299e": allowanceDismissal,
}

func hasAuthenticationAllowance(module []byte) bool {
	return compatibility.HasFix("lgt", compatibility.NativeModule, moduleDigest(module), compatibility.LGTAuthenticationAllowance)
}

// applyAuthenticationAllowance checks the recognized dismissal and then
// shortens the allowance. Nothing is written unless the code is the code that
// was examined, and the archive is not touched.
func applyAuthenticationAllowance(memory *armcore.Memory, digest string) error {
	site, ok := allowanceSites[digest]
	if !ok {
		return fmt.Errorf("LGT authentication allowance compatibility: no site is known for module %s", digest)
	}
	code := make([]byte, len(site.expect))
	if err := memory.Read(site.at, code); err != nil {
		return fmt.Errorf("LGT authentication allowance compatibility: %w", err)
	}
	if !bytes.Equal(code, site.expect) {
		return fmt.Errorf("LGT authentication allowance compatibility: unexpected code at %#x", site.at)
	}
	return memory.Write(site.at+uint32(site.offset), site.replace[:])
}
