package skt

import (
	"bytes"
	"encoding/hex"
	"fmt"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/compatibility"
)

// A native package is a handset's installed directory, so what the title wrote
// there names that handset, and both local titles compare it with the handset
// they run on and stop when it differs. Each check is the same class of
// offline authentication the KTF and LGT platforms already adapt: the exact
// executable is recognized by its SHA-256 in the compatibility registry, one
// comparison is accepted in the code before it is loaded, and every other check
// the title makes still runs with no file changed. One executable gates more
// than once — a packaged save's digits at start-up and then its certificate's
// subscriber field when play begins — and each gate is its own registered fix,
// applied where the registry names it. See docs/skt-native.md#skt-native-identity.
//
// The certificate gates match the specification's own `cert.c2s` subscriber
// contract, the same one a KTF title reports error 5001 for and `ktf-certificate`
// adapts; here the subscriber comparison is relaxed where the recognized
// reader makes it, leaving the application-id and token comparisons it makes
// either side intact.
type nativeIdentityFix struct {
	fix    string
	status backend.AuthenticationStatus
	// offset is where the compiled comparison sits in the code region, window
	// the bytes there that have to match exactly before anything is changed,
	// relax where in the window the replacement goes, and replacement what
	// goes there.
	offset      uint32
	window      string
	relax       uint32
	replacement string
}

var nativeIdentityFixes = []nativeIdentityFix{
	{
		// The last digits of the number a packaged save was written under,
		// compared with the handset's. The branch that keeps the rejection
		// becomes `mov r8, r8`.
		fix: compatibility.SKTNativeSaveIdentity, status: backend.AuthenticationSKTNativeSaveSubscriber,
		offset: 0x89018, window: "00 23 00 28 00 d1 01 23 1b 06 18 0e", relax: 4, replacement: "c0 46",
	},
	{
		// The `cert.c2s` subscriber comparison, which reports error 5001 on a
		// mismatch. The three fields are compared in a row — subscriber (5001),
		// a second field (5002) and a token (5003); only the subscriber branch
		// is made to continue (`beq` to the next field becomes `b`), so the
		// others still run.
		fix: compatibility.SKTNativeCertSubscriber, status: backend.AuthenticationSKTNativeCertificate,
		offset: 0x1774e, window: "00 28 06 d0 36 4b 52 46 13 60", relax: 2, replacement: "06 e0",
	},
}

// registeredFixes answers the fixes the registry selects for this executable's
// digest, in table order.
func (executable *NativeExecutable) registeredFixes() []nativeIdentityFix {
	if executable == nil {
		return nil
	}
	digest := hex.EncodeToString(executable.Digest[:])
	var selected []nativeIdentityFix
	for _, fix := range nativeIdentityFixes {
		if compatibility.HasFix("skt", compatibility.NativeModule, digest, fix.fix) {
			selected = append(selected, fix)
		}
	}
	return selected
}

// IdentityCompatibility reports the authentication status this executable's
// registered fixes carry, if any. A certificate adaptation outranks a save one
// in what the session reports, because reaching play is what the certificate
// gate decides.
func (executable *NativeExecutable) IdentityCompatibility() (backend.AuthenticationStatus, bool) {
	selected := executable.registeredFixes()
	if len(selected) == 0 {
		return "", false
	}
	return representativeStatus(selected), true
}

// ApplyIdentityCompatibility applies every fix the registry selects to code
// laid out by Load, and reports the representative status. It refuses, and
// changes nothing, when any selected fix's compiled window is not the one it
// was written for.
func (executable *NativeExecutable) ApplyIdentityCompatibility(code []byte) (backend.AuthenticationStatus, error) {
	if executable == nil {
		return "", fmt.Errorf("executable is not parsed")
	}
	selected := executable.registeredFixes()
	if len(selected) == 0 {
		return backend.AuthenticationUnsupported, nil
	}
	// Check every window before changing anything, so a refusal leaves the
	// code untouched rather than half-adapted.
	for _, fix := range selected {
		window := mustHex(fix.window)
		end := uint64(fix.offset) + uint64(len(window))
		if end > uint64(len(code)) || !bytes.Equal(code[fix.offset:end], window) {
			return "", fmt.Errorf("SKT native identity compatibility %s: the compiled comparison is not the recognized one", fix.fix)
		}
	}
	for _, fix := range selected {
		copy(code[fix.offset+fix.relax:], mustHex(fix.replacement))
	}
	return representativeStatus(selected), nil
}

// representativeStatus picks the one status the session reports for a set of
// applied fixes: a certificate adaptation if any, otherwise the first.
func representativeStatus(selected []nativeIdentityFix) backend.AuthenticationStatus {
	for _, fix := range selected {
		if fix.status == backend.AuthenticationSKTNativeCertificate {
			return fix.status
		}
	}
	return selected[0].status
}

// DiscardsOwnerState reports whether the registry recognizes this executable
// as a device dump whose previous owner's install-directory state should be
// withheld, so the game starts fresh rather than loading another handset's
// device-bound saves. See ProgramFiles.
func (executable *NativeExecutable) DiscardsOwnerState() bool {
	if executable == nil {
		return false
	}
	digest := hex.EncodeToString(executable.Digest[:])
	return compatibility.HasFix("skt", compatibility.NativeModule, digest, compatibility.SKTNativeDiscardOwnerState)
}

func mustHex(text string) []byte {
	data, err := hex.DecodeString(string(bytes.ReplaceAll([]byte(text), []byte(" "), nil)))
	if err != nil {
		panic(err)
	}
	return data
}
