# Code-revision compatibility

`internal/platform/compatibility/registry.json` is the source-controlled registry of
exceptions for specific code revisions across SKT, KTF and LGT. Go embeds it in every build,
so the CLI and server use the same registry through the platform runtimes. There is
no external database, runtime JSON override, or separate download. Registry
changes require rebuilding and restarting the executable.

Authentication adaptation remains separate. This registry identifies original
code revisions; it does not replace recognition of shared authentication code
patterns.

## Entry format

The document has schema `version: 1` and an `entries` array. Each entry contains:

- `id`: a unique, behavior-based identifier, without an individual game name.
- `platform`: `skt`, `ktf` or `lgt`.
- `match.kind`: the fingerprint algorithm; currently `java_class_set`.
- `match.sha256`: the exact lowercase SHA-256 code fingerprint.
- `fixes`: names of reviewed implementations in Go.
- `evidence`: a repository documentation path, optionally with a section anchor,
  recording the reproduction, reason, validation and limits.

The `java_class_set` fingerprint preserves the existing matching algorithm: sort all `.class`
entry names, then hash each name's UTF-8 bytes and its complete class bytes,
preceding each with its byte length as an unsigned 64-bit big-endian integer.
Any added, removed or changed class changes the fingerprint. Archive filenames,
display names, ZIP metadata and resources do not select compatibility. Identical
code repackaged with different resources receives the same exception.

Only `skt.inclusive_set_clip` is currently implemented. It enables the existing
MIDP clip-extent correction for the recognized session. Unrecognized code keeps
normal clipping. The original archive is never rewritten. The
[rendering investigation](skt-clip-compatibility.md)
records the current entry's evidence and rendering tests.

Matching always includes the platform and fingerprint kind, not just the hash.
The shared package owns metadata validation and matching. Each platform owns
fingerprint calculation and correction behavior. Fix names are platform-prefixed
and validated against their supported platform and fingerprint kind.

LGT and KTF do not yet have registered fixes. Their native or AOT code may need
a different fingerprint algorithm; add that algorithm and an evidence-backed
fix together when an actual exception is identified. Do not hash an empty Java
class set to identify a native archive or add placeholder exceptions.

## Adding or removing an exception

First reproduce the defect and determine whether the platform contract can be
fixed generally. Use this registry when evidence requires a revision-specific
exception. Record the evidence in `docs/`, and add the verified revision's
fingerprint and required fix names to the JSON. Consolidate fixes for the same
platform, fingerprint kind and digest in one entry. A different code revision requires its own evidence
and entry; do not match by title or filename.

A new fix name also requires a Go implementation and tests proving both its
selected behavior and preservation of normal behavior. JSON cannot express
arbitrary bytecode patches, addresses or scripts. If a general platform fix
later makes an exception unnecessary, remove the entry or its obsolete fix.

The parser rejects unsupported schema versions, unknown fields, invalid
fingerprints, missing metadata, duplicate IDs or platform/kind/digest targets, and unknown or
repeated fix names. Embedded-data errors fail initialization and are caught by
the registry tests. Tests also check that evidence documents exist.

Run `go test ./internal/platform/compatibility ./internal/platform/skt` after editing the registry. The optional
`TestLocalClipCompatibilityArchive` uses `WFEATURE_SKT_CLIP_ARCHIVE` with an
absolute local archive path to verify the current original and rejection after
class mutations. Real archives remain outside Git.

The initial registry migration passed the authored clipping regression, embedded
registry validation, local original fingerprint and class-mutation checks,
`make test`, `make test-debug`, `go test -race ./internal/...`, and `go vet ./...`.
The migration preserves the previously verified clip behavior; it does not add
a new browser or full-game playthrough claim.
