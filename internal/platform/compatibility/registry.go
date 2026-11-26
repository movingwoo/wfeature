package compatibility

import (
	"bytes"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
)

//go:embed registry.json
var compatibilityJSON []byte

// JavaClassSet identifies the sorted, length-prefixed Java class-set digest.
const JavaClassSet = "java_class_set"

// NativeModule identifies the SHA-256 digest of a complete native module.
const NativeModule = "native_module"

// ScriptImage identifies the SHA-256 digest of a complete SGS script image.
const ScriptImage = "script_image"

// KTFBREWPackagedCertificate identifies a BREW module with a known packaged certificate.
const KTFBREWPackagedCertificate = "ktf.brew_packaged_certificate"

// KTFSaveSubscriberIdentity permits a recognized client's archived subscriber record.
const KTFSaveSubscriberIdentity = "ktf.save_subscriber_identity"

// KTFSaveOwnerHandset derives the implicit handset from two private save records.
const KTFSaveOwnerHandset = "ktf.save_owner_handset"

// KTFNullPageMemory gives a recognized client zero-filled memory at address
// zero, for a title whose own code reads and stores through a null pointer on
// a path a handset completes.
const KTFNullPageMemory = "ktf.null_page_memory"

// KTFDialFailureNotice makes a recognized client's failed dial reach the
// notice the title has for a network failure, for a title whose dial callback
// drops the failure and waits without end.
const KTFDialFailureNotice = "ktf.dial_failure_notice"

// KTFWithheldPrivateFile keeps a named private file of a recognized capture
// from the guest, for a capture whose file lists saves it does not carry.
const KTFWithheldPrivateFile = "ktf.withheld_private_file"

// KTFSaveFileTime accepts a recognized client's comparison of the file time
// its save records with the file's time now, for a save a capture carries.
const KTFSaveFileTime = "ktf.save_file_time"

// KTFSaveSubscriberKey presents a recognized capture's saves enciphered under
// the key the session's handset gives, for a client that enciphers its saves
// under a key it derives from PHONENUMBER.
const KTFSaveSubscriberKey = "ktf.save_subscriber_key"

// SKTInclusiveSetClip selects the SKT runtime's inclusive clip correction.
const SKTInclusiveSetClip = "skt.inclusive_set_clip"

// SKTTutorialNameCache clears a stale name cache at a tutorial transition.
const SKTTutorialNameCache = "skt.tutorial_name_cache"

// SKTResumeWithoutStart preserves a live game whose start callback recreates it.
const SKTResumeWithoutStart = "skt.resume_without_start"

// SKTResumeThroughStart resumes a recognized MIDlet through startApp again and
// lets it start a thread whose run has returned, as the handset VM it was
// written for did: its pauseApp ends a loop and its startApp starts that same
// Thread object again.
const SKTResumeThroughStart = "skt.resume_through_start"

// LGTVisibleFramebufferOrigin removes a recognized native display-strip offset.
const LGTVisibleFramebufferOrigin = "lgt.visible_framebuffer_origin"

// LGTDirectFramebufferOrigin removes a strip offset used by direct pixel writers.
const LGTDirectFramebufferOrigin = "lgt.direct_framebuffer_origin"

// LGTImageFramebufferOrigin removes a strip offset from a native image path.
const LGTImageFramebufferOrigin = "lgt.image_framebuffer_origin"

// LGTColorKeySampleOrigin aligns a native transparency calibration sample.
const LGTColorKeySampleOrigin = "lgt.color_key_sample_origin"

// LGTScreenStripConstant clears the 24-row handset strip a recognized Clet
// adds to its own screen setup or pixel pointers, at instructions the platform
// verifies for each recognized module.
const LGTScreenStripConstant = "lgt.screen_strip_constant"

// LGTSaveSubscriberIdentity permits a recognized save reader's foreign identity.
const LGTSaveSubscriberIdentity = "lgt.save_subscriber_identity"

// LGTCertificateMessage selects the recognized two-stage local certificate exchange.
const LGTCertificateMessage = "lgt.certificate_message"

// LGTPackagedCertificate serves a recognized archive's own packaged
// certificate at the bare name the title opens it by, so a title whose online
// certificate exchange cannot complete offline reads the certificate the
// archive already ships. A saved certificate the title writes still wins.
const LGTPackagedCertificate = "lgt.packaged_certificate"

// LGTWideExclusiveClip selects a wide context with exclusive clip edges.
const LGTWideExclusiveClip = "lgt.wide_exclusive_clip"

// LGTWideGraphicsContext selects the evidenced 56-byte native context ABI.
const LGTWideGraphicsContext = "lgt.wide_graphics_context"

// LGTRecordTableTerminator ends a recognized record table its search runs off.
const LGTRecordTableTerminator = "lgt.record_table_terminator"

// LGTDataServiceLock answers a recognized module that its handset has wireless
// internet locked, for a title that leaves its own notice for that answer and
// has no way out of a dial that fails.
const LGTDataServiceLock = "lgt.data_service_lock"

// LGTAuthenticationAllowance lets a recognized title give its one-time network
// authentication up after the first failed attempt rather than the fifth.
const LGTAuthenticationAllowance = "lgt.authentication_allowance"

// LGTNullPageMemory maps the first page read only and zero-filled for a
// recognized module whose own code reads through a null pointer.
const LGTNullPageMemory = "lgt.null_page_memory"

// SKTScriptInstallIdentity accepts a recognized script's comparison of the
// handset with the user ID its installation was issued.
const SKTScriptInstallIdentity = "skt.script_install_identity"

// SKTNativeSaveIdentity accepts a recognized native executable's packaged
// save, which names the handset it was written on.
const SKTNativeSaveIdentity = "skt.native_save_identity"

// SKTNativeCertSubscriber accepts a recognized native executable's packaged
// cert.c2s subscriber field, the comparison that reports error 5001.
const SKTNativeCertSubscriber = "skt.native_cert_subscriber"

// SKTNativePairedCertificate identifies a reader of a paired native USIM and
// application certificate, which can establish the session's handset identity.
const SKTNativePairedCertificate = "skt.native_paired_certificate"

// SKTNativeCertDataSubscriber accepts a recognized native executable's packaged
// CERTIDATA record subscriber comparison, the one that stops the title on a
// blank screen.
const SKTNativeCertDataSubscriber = "skt.native_certdata_subscriber"

// SKTNativeDiscardOwnerState withholds a recognized device dump's install
// directory, so the game starts fresh rather than loading the previous
// owner's device-bound saves.
const SKTNativeDiscardOwnerState = "skt.native_discard_owner_state"

// SKTNativeServiceAllowance raises a recognized native executable's Host
// service step allowance, for a title whose own one-shot load legitimately
// runs past the default ceiling. The raised figure is in the skt package.
const SKTNativeServiceAllowance = "skt.native_service_allowance"

type fixScope struct {
	platform string
	kind     string
}

var fixScopes = map[string]fixScope{
	KTFBREWPackagedCertificate:  {"ktf", NativeModule},
	KTFDialFailureNotice:        {"ktf", NativeModule},
	KTFNullPageMemory:           {"ktf", NativeModule},
	KTFSaveFileTime:             {"ktf", NativeModule},
	KTFSaveOwnerHandset:         {"ktf", NativeModule},
	KTFSaveSubscriberIdentity:   {"ktf", NativeModule},
	KTFSaveSubscriberKey:        {"ktf", NativeModule},
	KTFWithheldPrivateFile:      {"ktf", NativeModule},
	LGTAuthenticationAllowance:  {"lgt", NativeModule},
	LGTCertificateMessage:       {"lgt", NativeModule},
	LGTColorKeySampleOrigin:     {"lgt", NativeModule},
	LGTDataServiceLock:          {"lgt", NativeModule},
	LGTDirectFramebufferOrigin:  {"lgt", NativeModule},
	LGTImageFramebufferOrigin:   {"lgt", NativeModule},
	LGTNullPageMemory:           {"lgt", NativeModule},
	LGTPackagedCertificate:      {"lgt", NativeModule},
	LGTRecordTableTerminator:    {"lgt", NativeModule},
	LGTSaveSubscriberIdentity:   {"lgt", NativeModule},
	LGTScreenStripConstant:      {"lgt", NativeModule},
	LGTVisibleFramebufferOrigin: {"lgt", NativeModule},
	LGTWideExclusiveClip:        {"lgt", NativeModule},
	LGTWideGraphicsContext:      {"lgt", NativeModule},
	SKTInclusiveSetClip:         {"skt", JavaClassSet},
	SKTResumeThroughStart:       {"skt", JavaClassSet},
	SKTResumeWithoutStart:       {"skt", JavaClassSet},
	SKTTutorialNameCache:        {"skt", JavaClassSet},
	SKTNativeCertDataSubscriber: {"skt", NativeModule},
	SKTNativeCertSubscriber:     {"skt", NativeModule},
	SKTNativeDiscardOwnerState:  {"skt", NativeModule},
	SKTNativePairedCertificate:  {"skt", NativeModule},
	SKTNativeSaveIdentity:       {"skt", NativeModule},
	SKTNativeServiceAllowance:   {"skt", NativeModule},
	SKTScriptInstallIdentity:    {"skt", ScriptImage},
}

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
		if entry.Match.Kind != JavaClassSet && entry.Match.Kind != NativeModule && entry.Match.Kind != ScriptImage {
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
			scope, known := fixScopes[fix]
			if !known || scope.platform != entry.Platform || scope.kind != entry.Match.Kind {
				return nil, fmt.Errorf("entry %q has unknown fix %q", entry.ID, fix)
			}
			if seen[fix] {
				return nil, fmt.Errorf("entry %q repeats fix %q", entry.ID, fix)
			}
			seen[fix] = true
		}
		// The two resume rules answer the same question two ways — call
		// nothing, or run startApp again — and the runtime takes the first it
		// checks, so an entry with both would mean whichever that happened to
		// be.
		if seen[SKTResumeWithoutStart] && seen[SKTResumeThroughStart] {
			return nil, fmt.Errorf("entry %q selects both %q and %q", entry.ID, SKTResumeWithoutStart, SKTResumeThroughStart)
		}
		registry[key] = entry
	}
	return registry, nil
}

// HasFix reports whether the exact platform and code fingerprint select a fix.
// Platforms with an existing fingerprint use this lookup directly.
func HasFix(platform, kind, digest, fix string) bool {
	return embeddedRegistry.hasFix(target{platform, kind, digest}, fix)
}

func (registry compatibilityRegistry) hasFix(key target, fix string) bool {
	return slices.Contains(registry[key].Fixes, fix)
}

// DigestsForFix returns the sorted code digests registered for a fix.
// Each fix belongs to one platform and fingerprint kind.
func DigestsForFix(fix string) []string {
	var digests []string
	for key, entry := range embeddedRegistry {
		if slices.Contains(entry.Fixes, fix) {
			digests = append(digests, key.Digest)
		}
	}
	slices.Sort(digests)
	return digests
}
