package compatibility

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestNativeRevisionSelectsOnlyOriginalModuleBytes(t *testing.T) {
	module := []byte("authored module")
	hash := sha256.Sum256(module)
	digest := hex.EncodeToString(hash[:])
	registry := compatibilityRegistry{
		{"lgt", NativeModule, digest}: {Fixes: []string{LGTWideGraphicsContext, LGTWideExclusiveClip}},
		{"skt", ScriptImage, digest}:  {Fixes: []string{SKTScriptInstallIdentity}},
	}
	revision := registry.identifyNative("lgt", module)
	if revision.Hash() != hash || revision.Digest() != digest ||
		!revision.Has(LGTWideGraphicsContext) || !revision.Has(LGTWideExclusiveClip) || revision.Has(LGTNullPageMemory) {
		t.Fatal("wrong identity or fix selection for registered module")
	}
	if registry.identifyNative("skt", module).Has(SKTScriptInstallIdentity) ||
		registry.identifyNative("ktf", module).Has(LGTWideGraphicsContext) {
		t.Fatal("a native revision selected another fingerprint kind or platform")
	}
	module[0]++
	if registry.identifyNative("lgt", module).Has(LGTWideGraphicsContext) || revision.Hash() != hash {
		t.Fatal("changing the source did not distinguish the two loads")
	}
}
