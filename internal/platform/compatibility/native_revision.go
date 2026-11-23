package compatibility

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
)

// NativeRevision identifies the original module bytes supplied by a platform
// loader and the fixes selected for that load.
type NativeRevision struct {
	hash  [sha256.Size]byte
	fixes []string
}

// IdentifyNative selects fixes from the original, unmodified executable bytes.
func IdentifyNative(platform string, module []byte) NativeRevision {
	return embeddedRegistry.identifyNative(platform, module)
}

func (registry compatibilityRegistry) identifyNative(platform string, module []byte) NativeRevision {
	r := NativeRevision{hash: sha256.Sum256(module)}
	r.fixes = registry[target{platform, NativeModule, r.Digest()}].Fixes
	return r
}

// Digest is the key used by platform tables of per-revision implementation data.
func (r NativeRevision) Digest() string { return hex.EncodeToString(r.hash[:]) }

// Hash returns the binary form stored in checkpoints.
func (r NativeRevision) Hash() [sha256.Size]byte { return r.hash }

// Has reports whether the loaded revision selected fix.
func (r NativeRevision) Has(fix string) bool { return slices.Contains(r.fixes, fix) }
