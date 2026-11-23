package lgt

import (
	"crypto/sha256"
	"maps"
	"slices"
	"testing"

	"github.com/movingwoo/wfeature/internal/platform/compatibility"
)

// Each of these fixes needs implementation data for every registered revision.
func TestRegistryMatchesRevisionTables(t *testing.T) {
	for fix, digests := range map[string][]string{
		compatibility.LGTCertificateMessage:      slices.Sorted(maps.Keys(certificateMessageRevisions)),
		compatibility.LGTSaveSubscriberIdentity:  slices.Sorted(maps.Keys(subscriberSites)),
		compatibility.LGTAuthenticationAllowance: slices.Sorted(maps.Keys(allowanceSites)),
		compatibility.LGTDataServiceLock:         slices.Sorted(maps.Keys(dataServiceLockReaders)),
	} {
		if registered := compatibility.DigestsForFix(fix); !slices.Equal(registered, digests) {
			t.Errorf("%s: registry digests %v, Go table %v", fix, registered, digests)
		}
	}
}

// A caller may change the module returned by Open before the next Load.
func TestLoadIdentifiesTheCurrentModule(t *testing.T) {
	archive, err := Open(fixtureArchive(t))
	if err != nil {
		t.Fatal(err)
	}
	first, err := Load(archive, Options{})
	if err != nil {
		t.Fatal(err)
	}
	original := sha256.Sum256(archive.Module)
	archive.Module[15] ^= 1 // Loader-ignored ELF padding is part of the code identity.
	second, err := Load(archive, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if first.revision.Hash() != original || second.revision.Hash() != sha256.Sum256(archive.Module) {
		t.Fatal("two loads of different module bytes shared a fingerprint")
	}
}
