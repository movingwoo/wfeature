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
		if !registry.hasFix(key, SKTInclusiveSetClip) {
			t.Fatalf("entry %q lost its clip fix", entry.ID)
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
	for _, name := range []string{"version", "id", "platform", "wrong platform", "kind", "digest", "uppercase", "evidence", "no fixes", "unknown fix", "duplicate fix", "duplicate id", "duplicate digest", "unknown field", "trailing document"} {
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
	for key := range embeddedRegistry {
		if !HasFix(key.Platform, key.Kind, key.Digest, SKTInclusiveSetClip) {
			t.Fatal("registered fix missing")
		}
		for _, platform := range []string{"lgt", "ktf", ""} {
			if HasFix(platform, key.Kind, key.Digest, SKTInclusiveSetClip) {
				t.Fatalf("SKT fix leaked to %q", platform)
			}
		}
		if HasFix(key.Platform, "other", key.Digest, SKTInclusiveSetClip) {
			t.Fatal("fingerprint kinds collided")
		}
		if HasFix(key.Platform, key.Kind, key.Digest, "unknown") {
			t.Fatal("unknown fix selected")
		}
	}
}
