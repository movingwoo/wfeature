package skt

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/compatibility"
	"github.com/movingwoo/wfeature/internal/sgsvm"
)

// A scriptIdentitySite is the branch a recognized script takes when the user
// ID its installation was issued does not belong to the handset. expect is the
// code that has to be at `at` in the script image for the branch to be the one
// that was examined, target the offset in it of the low word of the branch's
// destination, and accept the low word of the address the matching answer
// continues at.
type scriptIdentitySite struct {
	at     int
	expect []byte
	target int
	accept uint16
}

// scriptIdentitySites are, by script image digest, the comparisons
// skt.script_install_identity accepts. A carrier issued each installation a
// user ID derived from the handset that downloaded it, and the capture carries
// that ID in its download descriptor: the script derives the same value from
// MIN and stops at its notice when the two differ. A digest the fix selects
// without sites here fails starting rather than run with the comparison in
// place. See docs/authentication.md#archived-subscriber-records.
var scriptIdentitySites = map[string][]scriptIdentitySite{
	"eab7d4de11fba9a131ec0aef8a7b3947aef41317fc3d23903d0d1a3812cbe9c2": {
		// MIN and the installed ID are read, the first derivation is called
		// and compared: `if pop != 0 goto second` becomes a branch to the next
		// instruction, where the matching answer pushes 0 and returns.
		{at: 0x1dc, expect: []byte{
			0x04, 0x00, 0x1a, 0x00, 0x92, 0x00, 0x01, 0x00, 0x04, 0x00, 0x19, 0x00, 0x92, 0x00, 0x02, 0x00,
			0x0b, 0x00, 0x11, 0x00, 0x04, 0x00, 0x18, 0x00, 0x04, 0x00, 0x14, 0x00, 0x04, 0x00, 0x15, 0x00,
			0x04, 0x00, 0x1a, 0x00, 0x05, 0x00, 0x00, 0x00, 0x85, 0x00, 0x00, 0x00, 0x0a, 0xb7, 0x04, 0x00,
			0x18, 0x00, 0x04, 0x00, 0x19, 0x00, 0x92, 0x00, 0xa4, 0x00, 0x80, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x24, 0x02, 0x05, 0x00, 0x00, 0x00, 0x86, 0x00,
		}, target: 0x40, accept: 0x021e},
	},
}

// scriptIdentity answers the program a session of this archive runs and the
// policy that names it. A recognized script on the implicit handset runs a
// session-owned copy with the comparison accepted; the archive's own image is
// never written. A configured handset, and a session with compatibility
// disabled, run the script as packaged.
func (a *Archive) scriptIdentity(disabled, configured bool) (*sgsvm.Program, backend.AuthenticationStatus, error) {
	if disabled {
		return a.Script, backend.AuthenticationOff, nil
	}
	if !compatibility.HasFix("skt", compatibility.ScriptImage, a.scriptDigest, compatibility.SKTScriptInstallIdentity) {
		return a.Script, backend.AuthenticationUnsupported, nil
	}
	sites := scriptIdentitySites[a.scriptDigest]
	if len(sites) == 0 {
		return nil, "", fmt.Errorf("SGS install identity compatibility: no comparison is known for script %s", a.scriptDigest)
	}
	image := a.Script.Data
	for _, site := range sites {
		if site.at < 0 || site.at > len(image) || len(site.expect) > len(image)-site.at ||
			!bytes.Equal(image[site.at:site.at+len(site.expect)], site.expect) {
			return nil, "", fmt.Errorf("SGS install identity compatibility: unexpected comparison at %#x", site.at)
		}
	}
	if configured {
		return a.Script, backend.AuthenticationGuestManaged, nil
	}
	program := *a.Script
	program.Data = bytes.Clone(image)
	for _, site := range sites {
		binary.LittleEndian.PutUint16(program.Data[site.at+site.target:], site.accept)
	}
	return &program, backend.AuthenticationSKTScriptInstall, nil
}
