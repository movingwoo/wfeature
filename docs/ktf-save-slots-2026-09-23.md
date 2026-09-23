# KTF slot creation and save overlay reads

## Report and cause

The local 15:15:40 debug report ends in
`FileSystem.exists("/ev/0/f000")` with `read save fs/ev/0/f000: ... not a directory`.
The archive SHA-256 is
`1379b56de66ee4f90ae53aea9a7a43117d19ab61eb1c015d1fc16c2e304251b4`.
The report's duplicate at 15:15:44 contains the same failure.

The guest writes a save named `ev` and also reads packaged resources beneath
`ev/`. These are separate entries in the mounted resource table. The host
save store maps slashes to real directories, so probing `fs/ev/0/f000` beneath
the saved regular file returns `ENOTDIR`. The resource lookup still finds
the archive entry, but the save reader retains the host error and the native
call ends the session. Clearing saves alone cannot fix this: the guest creates
the conflicting save again.

`DirectorySaveStore` now treats `ENOTDIR` on a read as an absent save entry,
allowing the existing archive fallback. Other read failures still propagate.
Writes beneath a regular file still fail and preserve that file. No save
format, archive contents, resolution order, or platform entry point changes.

The [WIPI FileSystem contract](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/io/FileSystem.md)
defines existence queries and I/O failures. It does not define this host save
overlay; the correction belongs to the backend storage adapter.

## First-slot creation

A fresh-save route also found a separate local-service error: selecting the
first slot reaches request kind 5, command 1410, but the service rejects its
zero-based index 0. The display-name generator already adds one to the index.
The service now accepts 0 through 127, retaining the signed-byte bound,
message-length validation, and conversation ordering. Missing or extra slot
bytes remain invalid. This is an observed application protocol, not a WIPI API
contract. The provider remains restricted to its existing dependency and
endpoint checks; it does not contact an external service.

## Evidence and scope

The original 18-file debug save was copied under ignored
`var/savedata/backups/ktf-save-slots-20260923/PD006309/` and verified by per-file
SHA-256 before any reset. Acceptance uses separate save trees under
`var/savedata/ktf-save-slots-fixed-{debug,release}-20260923/`.
An empty host save directory still exposes a packaged first-slot record;
the route replaces it through the game's New Game confirmation.

Both build profiles created all six slots in order, committed local-service
receipts, and restored the slots and their opening scene in new processes.
Commands, screenshots, and diagnostics are retained under ignored
`var/diagnostics/ktf-save-slots-20260923/`. These are shared-platform-path
checks, not a physical iPhone or complete campaign playthrough.

Recreating the third slot with the reported character-selection sequence
reaches the exact `/ev/0/f000` lookup. Replaying identical commands against
copies of the same saves fails with `ENOTDIR` in the old binary and passes
in the fixed release binary. Debug also passes that lookup during creation
and a subsequent restart. Both fixed runs retain `exists /ev/0/f000 found=true`
without an uncaught callback or retained save-read error.

The original save separately reproduces a slot-name paint exception in both
old and fixed binaries when the second slot is selected. This is consistent
with the earlier [invalid service label](startup-compatibility-2026-09-19.md#relay-framing-and-worker-progress-2026-09-20)
record; it is not repaired automatically. The requested reset replaces these
old records with the newly created slots, while retaining the verified backup.

After validation, only the reported game's active debug save tree was replaced
with the verified fresh slots. Other games and the active release save tree
were preserved. The debug server was restarted with the rebuilt binary.
At the user's subsequent request to test creation personally, the six-slot
debug save was moved to the backup tree and the active game save directory
was left absent. The packaged first-slot record remains part of the unchanged
archive and can still appear before New Game replaces it.

Backend and KTF regression tests fail before the correction and pass afterward.
They cover packaged descendants of a saved file, missing descendants, writes
after a resource lookup, restart, preservation of the parent file, all six
zero-based slots, and malformed service messages. Existing read-error and
transaction-failure tests remain enabled.

`make test`, `make test-debug`, `go test -race ./internal/...`, and
`go vet ./...` passed. Both server profiles built successfully; the backend
tests also cross-compiled for Windows amd64 with cgo disabled.

## Continuous creation and collection roots

The 15:52:36 report, duplicated at 15:52:42, follows creation in slot order
6, 2, 3, 4, then 5 without restarting. Its fifth service conversation stops
after phase 1. `RelayInputStream.read([B)I` receives address `0x3015cfd0`,
which no longer has a JVM object binding. The trace previously allocated that
address as a two-byte array. The server collected guest objects immediately
before the failure. The earlier six-slot acceptance started a new process
for each slot; it did not cover this continuous allocation history.

`Core.Call` derives a temporary ARM thread whose registers differ from its
logical parent. The collector scanned only the service thread and worker
parents, missing registers of active derived calls, including callers beneath
another nested call. An array held only there could lose its strong JVM
binding, be reclaimed by Go, and fail at the next native boundary.

The execution layer now tracks active calls until they unwind and exposes
their saved registers through `Thread.LiveContexts`. Collection scans all
these contexts while execution is parked. This includes suspension inside a
native call and a forced step-budget yield. Normal returns, errors, and
cancellation remove the temporary roots. Stack scanning, Go graph retention,
collection thresholds, and the save format remain unchanged.

An authored ARM regression holds different arrays only in an outer and an
inner call register, runs collection across a Go GC, and checks both bindings.
It fails before the fix for service and worker threads and passes afterward;
the arrays become collectible after normal and error unwinds. An execution
test separately parks nested calls at the step limit, checks their register
snapshots from the host, and verifies cleanup after cancellation.

The user's new 24-file save tree was copied to ignored
`var/savedata/backups/ktf-slot-order-20260923/PD006309/` and verified by SHA-256.
Acceptance uses separate save trees; the active saves are preserved. Commands,
screenshots, diagnostics, and local collection probes are under ignored
`var/diagnostics/ktf-slot-order-20260923/`.

Debug and release both complete 6, 2, 3, 4, 5 in one process, with all five
service phases recorded five times and the fifth opening scene rendered.
The ordinary replay also passes in the old binary, so creation order alone
is not a deterministic reproducer. Running Go GC every ten ticks likewise
does not guarantee the guest collection lands in the vulnerable window.

A local build overlay forces guest collection and Go GC while phase 1 has
completed and phase 2 is pending. With the old register scan, the first
creation fails at the same `RelayInputStream.read([B)I` boundary: a two-byte
array allocated at guest site `0x1d5ac7` loses its binding. With the fix, both
profiles complete all five creations under the identical forced window,
including 33 nonempty collection cycles, with no failed service or uncaught
callback. The overlay and forced-GC probes are local evidence only; they are
not present in the server builds.

`make test`, `make test-debug`, `go test -race ./internal/...`, and
`go vet ./...` pass with the new regression tests. Both server profiles build.
The active debug server is restarted with the fix, preserving all 24 save
files byte for byte. Physical iPhone replay remains a user confirmation.
