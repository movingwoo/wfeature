package ktf

import (
	"crypto/sha256"
	"maps"
	"slices"
	"testing"

	"github.com/movingwoo/wfeature/internal/armcore"
	"github.com/movingwoo/wfeature/internal/platform/compatibility"
)

// Each of these fixes needs implementation data for every registered revision.
func TestRegistryMatchesRevisionTables(t *testing.T) {
	var certificates []string
	for _, binding := range nativeCertificatePackages {
		certificates = append(certificates, binding.moduleSHA256)
	}
	slices.Sort(certificates)
	for fix, digests := range map[string][]string{
		compatibility.KTFDialFailureNotice:       slices.Sorted(maps.Keys(dialFailureRewrites)),
		compatibility.KTFSaveSubscriberIdentity:  slices.Sorted(maps.Keys(subscriberSites)),
		compatibility.KTFSaveSubscriberKey:       slices.Sorted(maps.Keys(saveKeyCode)),
		compatibility.KTFSaveFileTime:            slices.Sorted(maps.Keys(saveFileTimeRewrites)),
		compatibility.KTFWithheldPrivateFile:     slices.Sorted(maps.Keys(withheldFiles)),
		compatibility.KTFBREWPackagedCertificate: slices.Compact(certificates),
	} {
		if registered := compatibility.DigestsForFix(fix); !slices.Equal(registered, digests) {
			t.Errorf("%s: registry digests %v, Go table %v", fix, registered, digests)
		}
	}
	// Only some subscriber comparisons take the save-deletion branch.
	for digest := range deletingClients {
		if _, ok := subscriberSites[digest]; !ok {
			t.Errorf("save-deletion policy without subscriber sites for %s", digest)
		}
	}
}

// Compatibility and checkpoints identify the packaged bytes of a module
// whose in-memory pointers the loader relocates.
func TestLoadIdentifiesThePackagedClient(t *testing.T) {
	image := ClientImage{Name: "client.bin36", Data: syntheticRelocatableClient(36), BSSSize: 36}
	original := sha256.Sum256(image.Data)
	first, err := LoadClient(image, armcore.CoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	image.Data[len(image.Data)-1] ^= 1
	second, err := LoadClient(image, armcore.CoreOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if first.revision.Hash() != original || second.revision.Hash() != sha256.Sum256(image.Data) {
		t.Fatal("client fingerprint does not identify the original bytes of each load")
	}
}
