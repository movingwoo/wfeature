package compatibility

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedCompatibilityRegistry(t *testing.T) {
	registry, err := parseCompatibility(compatibilityJSON)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry) == 0 {
		t.Fatal("missing compatibility entries")
	}
	for key, entry := range registry {
		for _, fix := range entry.Fixes {
			if !registry.hasFix(key, fix) {
				t.Fatalf("entry %q lost fix %q", entry.ID, fix)
			}
		}
		document := strings.SplitN(entry.Evidence, "#", 2)[0]
		if !strings.HasPrefix(document, "docs/") {
			t.Fatalf("entry %q must reference repository documentation", entry.ID)
		}
		if _, err := os.Stat(filepath.Join("../../..", document)); err != nil {
			t.Fatal(err)
		}
	}
	if registry.hasFix(target{"skt", JavaClassSet, strings.Repeat("0", 64)}, SKTInclusiveSetClip) {
		t.Fatal("unregistered code received a fix")
	}
}

func TestCompatibilityRejectsInvalidEntries(t *testing.T) {
	for _, name := range []string{"version", "id", "platform", "wrong platform", "kind", "digest", "uppercase", "evidence", "no fixes", "unknown fix", "duplicate fix", "two resume rules", "duplicate id", "duplicate digest", "unknown field", "trailing document"} {
		t.Run(name, func(t *testing.T) {
			var document struct {
				Version int                  `json:"version"`
				Entries []compatibilityEntry `json:"entries"`
			}
			if err := json.Unmarshal(compatibilityJSON, &document); err != nil {
				t.Fatal(err)
			}
			entry := &document.Entries[0]
			switch name {
			case "version":
				document.Version = 2
			case "id":
				entry.ID = ""
			case "platform":
				entry.Platform = "unknown"
			case "wrong platform":
				entry.Platform = "lgt"
			case "kind":
				entry.Match.Kind = "unknown"
			case "digest":
				entry.Match.SHA256 = "abcd"
			case "uppercase":
				entry.Match.SHA256 = strings.ToUpper(entry.Match.SHA256)
			case "evidence":
				entry.Evidence = ""
			case "no fixes":
				entry.Fixes = nil
			case "unknown fix":
				entry.Fixes = []string{"unknown"}
			case "duplicate fix":
				entry.Fixes = append(entry.Fixes, entry.Fixes[0])
			case "two resume rules":
				// Each is a fix an SKT class set may select, and not both.
				entry.Platform, entry.Match.Kind = "skt", JavaClassSet
				entry.Fixes = []string{SKTResumeWithoutStart, SKTResumeThroughStart}
			case "duplicate id":
				document.Entries = append(document.Entries, *entry)
			case "duplicate digest":
				other := *entry
				other.ID += "-other"
				document.Entries = append(document.Entries, other)
			}
			data, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			if name == "unknown field" {
				data = append([]byte(`{"unexpected":true,`), data[1:]...)
			}
			if name == "trailing document" {
				data = append(data, []byte(` {}`)...)
			}
			if _, err := parseCompatibility(data); err == nil {
				t.Fatal("invalid registry accepted")
			}
		})
	}
}

func TestCompatibilitySeparatesPlatformsAndFingerprintKinds(t *testing.T) {
	for key, entry := range embeddedRegistry {
		fix := entry.Fixes[0]
		if !HasFix(key.Platform, key.Kind, key.Digest, fix) {
			t.Fatal("registered fix missing")
		}
		for _, platform := range []string{"skt", "lgt", "ktf", ""} {
			if platform != key.Platform && HasFix(platform, key.Kind, key.Digest, fix) {
				t.Fatalf("fix leaked to %q", platform)
			}
		}
		for _, kind := range []string{JavaClassSet, NativeModule, ScriptImage, "other"} {
			if kind != key.Kind && HasFix(key.Platform, kind, key.Digest, fix) {
				t.Fatal("fingerprint kinds collided")
			}
		}
		if HasFix(key.Platform, key.Kind, key.Digest, "unknown") {
			t.Fatal("unknown fix selected")
		}
	}
}

// A fix is accepted only with the platform and the fingerprint its
// implementation reads, so an entry cannot select code that never looks.
func TestCompatibilityBindsIdentityFixes(t *testing.T) {
	digest := strings.Repeat("ab", 32)
	for _, tc := range []struct {
		platform, kind, fix string
		valid               bool
	}{
		{"skt", ScriptImage, SKTScriptInstallIdentity, true},
		{"skt", NativeModule, SKTScriptInstallIdentity, false},
		{"skt", JavaClassSet, SKTScriptInstallIdentity, false},
		{"lgt", ScriptImage, SKTScriptInstallIdentity, false},
		{"skt", ScriptImage, SKTNativeSaveIdentity, false},
		{"ktf", NativeModule, KTFSaveSubscriberIdentity, true},
		{"ktf", JavaClassSet, KTFSaveSubscriberIdentity, false},
		{"lgt", NativeModule, KTFSaveSubscriberIdentity, false},
		{"ktf", NativeModule, KTFNullPageMemory, true},
		{"ktf", JavaClassSet, KTFNullPageMemory, false},
		{"lgt", NativeModule, KTFNullPageMemory, false},
		{"ktf", NativeModule, KTFDialFailureNotice, true},
		{"ktf", JavaClassSet, KTFDialFailureNotice, false},
		{"lgt", NativeModule, KTFDialFailureNotice, false},
		{"ktf", NativeModule, KTFWithheldPrivateFile, true},
		{"ktf", JavaClassSet, KTFWithheldPrivateFile, false},
		{"lgt", NativeModule, KTFWithheldPrivateFile, false},
		{"ktf", NativeModule, KTFSaveFileTime, true},
		{"ktf", JavaClassSet, KTFSaveFileTime, false},
		{"lgt", NativeModule, KTFSaveFileTime, false},
		{"ktf", NativeModule, KTFSaveSubscriberKey, true},
		{"ktf", JavaClassSet, KTFSaveSubscriberKey, false},
		{"lgt", NativeModule, KTFSaveSubscriberKey, false},
		{"lgt", NativeModule, LGTDataServiceLock, true},
		{"lgt", NativeModule, LGTAuthenticationAllowance, true},
		{"ktf", NativeModule, LGTAuthenticationAllowance, false},
		{"lgt", NativeModule, LGTNullPageMemory, true},
		{"lgt", JavaClassSet, LGTNullPageMemory, false},
		{"ktf", NativeModule, LGTNullPageMemory, false},
		{"lgt", JavaClassSet, LGTDataServiceLock, false},
		{"ktf", NativeModule, LGTDataServiceLock, false},
		{"lgt", NativeModule, LGTSaveSubscriberIdentity, true},
		{"ktf", NativeModule, LGTSaveSubscriberIdentity, false},
	} {
		entry := compatibilityEntry{ID: "candidate", Platform: tc.platform, Fixes: []string{tc.fix}, Evidence: "docs/authentication.md"}
		entry.Match.Kind, entry.Match.SHA256 = tc.kind, digest
		data, err := json.Marshal(map[string]any{"version": 1, "entries": []compatibilityEntry{entry}})
		if err != nil {
			t.Fatal(err)
		}
		registry, err := parseCompatibility(data)
		if (err == nil) != tc.valid {
			t.Errorf("%s %s %s: error %v, want valid=%v", tc.platform, tc.kind, tc.fix, err, tc.valid)
		}
		if err == nil && !registry.hasFix(target{tc.platform, tc.kind, digest}, tc.fix) {
			t.Errorf("%s %s %s: accepted entry does not select its fix", tc.platform, tc.kind, tc.fix)
		}
	}
}
