package compatibility

import (
	"bytes"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

//go:embed registry.json
var compatibilityJSON []byte

// JavaClassSet identifies the sorted, length-prefixed Java class-set digest.
const JavaClassSet = "java_class_set"

// NativeModule identifies the SHA-256 digest of a complete native module.
const NativeModule = "native_module"

// SKTInclusiveSetClip selects the SKT runtime's inclusive clip correction.
const SKTInclusiveSetClip = "skt.inclusive_set_clip"

// LGTVisibleFramebufferOrigin removes a recognized native display-strip offset.
const LGTVisibleFramebufferOrigin = "lgt.visible_framebuffer_origin"

type target struct {
	Platform string
	Kind     string
	Digest   string
}

type compatibilityEntry struct {
	ID       string `json:"id"`
	Platform string `json:"platform"`
	Match    struct {
		Kind   string `json:"kind"`
		SHA256 string `json:"sha256"`
	} `json:"match"`
	Fixes    []string `json:"fixes"`
	Evidence string   `json:"evidence"`
}

type compatibilityRegistry map[target]compatibilityEntry

// This is trusted, embedded release data. Invalid entries are build defects,
// caught by tests, rather than grounds for silently changing guest behavior.
var embeddedRegistry = func() compatibilityRegistry {
	registry, err := parseCompatibility(compatibilityJSON)
	if err != nil {
		panic(fmt.Sprintf("invalid embedded compatibility registry: %v", err))
	}
	return registry
}()

func parseCompatibility(data []byte) (compatibilityRegistry, error) {
	var document struct {
		Version int                  `json:"version"`
		Entries []compatibilityEntry `json:"entries"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("expected one JSON document")
	}
	if document.Version != 1 {
		return nil, fmt.Errorf("unsupported version %d", document.Version)
	}
	registry := make(compatibilityRegistry, len(document.Entries))
	ids := make(map[string]bool)
	for _, entry := range document.Entries {
		if strings.TrimSpace(entry.ID) == "" || ids[entry.ID] {
			return nil, fmt.Errorf("missing or duplicate id %q", entry.ID)
		}
		ids[entry.ID] = true
		if entry.Platform != "skt" && entry.Platform != "ktf" && entry.Platform != "lgt" {
			return nil, fmt.Errorf("entry %q has unsupported platform %q", entry.ID, entry.Platform)
		}
		if entry.Match.Kind != JavaClassSet && entry.Match.Kind != NativeModule {
			return nil, fmt.Errorf("entry %q has unsupported fingerprint kind %q", entry.ID, entry.Match.Kind)
		}
		digest := entry.Match.SHA256
		key := target{entry.Platform, entry.Match.Kind, digest}
		decoded, err := hex.DecodeString(digest)
		if err != nil || len(decoded) != 32 || digest != strings.ToLower(digest) {
			return nil, fmt.Errorf("entry %q needs a lowercase SHA-256 digest", entry.ID)
		}
		if _, exists := registry[key]; exists {
			return nil, fmt.Errorf("duplicate code digest for entry %q", entry.ID)
		}
		if strings.TrimSpace(entry.Evidence) == "" {
			return nil, fmt.Errorf("entry %q needs evidence", entry.ID)
		}
		if len(entry.Fixes) == 0 {
			return nil, fmt.Errorf("entry %q needs a fix", entry.ID)
		}
		seen := make(map[string]bool)
		for _, fix := range entry.Fixes {
			supported := fix == SKTInclusiveSetClip && entry.Platform == "skt" && entry.Match.Kind == JavaClassSet ||
				fix == LGTVisibleFramebufferOrigin && entry.Platform == "lgt" && entry.Match.Kind == NativeModule
			if !supported {
				return nil, fmt.Errorf("entry %q has unknown fix %q", entry.ID, fix)
			}
			if seen[fix] {
				return nil, fmt.Errorf("entry %q repeats fix %q", entry.ID, fix)
			}
			seen[fix] = true
		}
		registry[key] = entry
	}
	return registry, nil
}

// HasFix reports whether the exact platform and code fingerprint select a fix.
// Platform runtimes compute their fingerprints and implement the selected fixes.
func HasFix(platform, kind, digest, fix string) bool {
	return embeddedRegistry.hasFix(target{platform, kind, digest}, fix)
}

func (registry compatibilityRegistry) hasFix(key target, fix string) bool {
	for _, candidate := range registry[key].Fixes {
		if candidate == fix {
			return true
		}
	}
	return false
}
