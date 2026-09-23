# Startup compatibility investigation, 2026-09-19

Three local archives were investigated through the shared session path used by
the CLI and browser server. Archive digests identify evidence, never select
runtime behavior. All probes used isolated saves below the ignored
`var/savedata/qa-20260919/`; original user saves were not edited. Raw traces and
screenshots are under `var/acceptance/qa-20260919/` and are not distribution assets.

## KTF startup exception (`1379b56de66e`)

The first failure was a compiled protected-field access check. It compared the
receiver's decoded class address against the canonical class record in the
image. A dispatch alias referred to the same class but failed that raw address
comparison, producing a guest `java/lang/Error` during construction.

For ordinary images the JVM context and dispatch aliases now occupy a bounded
4 MiB region immediately after the mapped image and BSS. Object headers name
canonical image records when their signed 27-bit displacement is representable.
Distant runtime records still use aliases. Earlier native modules retain their
existing context layout. Large images can still require aliases for records
outside the displacement range; this is not evidence of universal raw-address
identity support for every possible image layout.

The next startup failures exposed three independent runtime omissions:

- Fieldless runtime subclasses did not retain their parent's instance size.
  Guest subclass fields could overlap `TextComponent.imHandler`.
- The constructor stored that handler under an unqualified field key, while
  the guest-field publisher read its canonical declaring-class key.
- `Kernel.getExecNames` and `InputMethodHandler.getCurrentMode` were missing.
  The former lists the current session application with descriptor filters;
  the latter returns the input mode already maintained by the handler.

The API contracts were checked against the WIPI specification for
[Kernel](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msf/core/Kernel.md)
and [InputMethodHandler](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lcdui/InputMethodHandler.md).
Both debug and release 1,200-tick runs reached the title menu with 1,200
frame flushes.
This proves startup and menu input, not campaign completion or progress restore.

## LGT embedded certificate (`74b0ff2230f7`)

The notice requests a certificate on the first connection. It does not describe
a twelve-hour renewal interval. Ordinary offline transport correctly delivers a
connection failure, but the archive requires a separate application certificate
to pass its startup gate. Its identity/options file reads already succeeded.

The main save contains a 100-byte encrypted options header, a 100-byte
certificate at offset 100, and progress records from offset 200. The header's
first 48 plaintext bytes carry a byte-sum checksum at offset 48; byte 27 is the
certificate-presence flag. The certificate reader compares the decoded string
against the session subscriber number followed by the application identifier
and an archive-owned token. The writer and reader share those pointers and the
cipher seed. The cipher uses ciphertext feedback and two module-owned factors.

`lgt-certificate-100` recognizes connected reader, writer, startup gate,
header reader, subscriber accessor and encoder/decoder instruction contracts.
It resolves filenames, tokens, state addresses and cipher parameters from those
contracts. It rejects ambiguous matches, invalid paths, mismatched application
identity, invalid header checksums, inconsistent live state and open target
files. No game name, archive digest or absolute guest instruction address
selects this adapter. No guest instruction is patched and no server is contacted.

After Clet initialization has created or loaded the normal save, the session
exposes the certificate and presence flag privately. The matched live presence
flag stays coherent if the guest initializes its settings again after the first
notice. Publishing the flag requires that the rest of the live options match
the header being saved. Writes restore the original certificate and flag,
recompute the header checksum, and persist ordinary settings and progress.
Malformed nonempty target writes are refused rather than persisting the private
certificate in an invalid record. File deletion ledgers keep ordinary behavior.
Disabling compatibility exposes the backing state again.

Release reached save-slot selection from a fresh save in 2,200 ticks; debug
reached the opening story on a subsequent 2,200-tick session. A copy of the
release save run with `-no-auth` returned to the original certificate prompt,
confirming that compatibility did not persist a generated certificate.

A separately modified diagnostic save first proved the certificate format by
reaching the menu. It was never installed over a user's save. Automated tests
use invented application tokens and newly assembled instruction fixtures.

## LGT restart notice (`e934256c238a`)

This archive deliberately exits once before proceeding. Its initial `option`
record is 18 bytes, with a restart flag in byte 17. The notice asks for the OZ
confirmation key, represented by `fire`/OK, rather than the numeric zero key.
After its input delay, the confirmation handler clears the flag, writes and
closes the options file, then requests guest exit. Restarting the emulator
before that handler runs leaves the flag set and repeats the notice.

A fresh isolated run confirmed at ticks 50 and 150 (five-tick holds) exited at
tick 152 and persisted the cleared flag. A new session with the same saves
passed the notice; confirming the following startup notices reached the title.
Debug and release both reproduced the exit at tick 152 and reached the title
on the next run. No emulator change or save deletion is required for this case. Let the game
exit through its own confirmation action, then launch it again.

## Validation boundary

Regression tests cover canonical class identity, inherited instance payload,
handler publication, executable filtering, input mode readback, class arena
bounds, relocated certificate recognition, changed instructions, ambiguous
contracts, cipher feedback, private certificate persistence, malformed writes
and first-run reinitialization. Real archive checks exercise native CLI sessions;
a fresh browser or physical-device playthrough is outside this evidence.

The following checks passed: `make test`, `make test-debug`,
`go test -race ./internal/...`, and `go vet ./...`. Both `make debug` and
`make release` succeeded, as did `make server` and `make server-release`. Additional truncated-contract tests passed in the
focused LGT certificate suite after those full checks.

## Follow-up: entering a campaign

Evidence is under `var/acceptance/qa-20260919-followup/`. The original user's
saves remain untouched; KTF uses a fresh isolated save directory and LGT reuses
the isolated restart-check save described above.

For `1379b56de66e`, Continue failed while painting because the compiled caller
could not resolve `java/lang/Character.isDigit(C)Z`. The JVM already implements
that CLDC operation; publishing its KTF runtime class metadata connects the
compiled caller to the existing implementation. A regression test checks both
AOT resolution and digit boundaries. Continue now loads the packaged level-five
slot and draws its field scene. This does not prove campaign completion or a
save/restore cycle.

New character creation is a separate unresolved path: declining its connection
prompt returns to character customization; accepting displays the guest's
server-disconnected message and returns to the title menu. The captured run
contains `Network.disconnect`, but no `Network.connect` or `URL.find` call.
Therefore the message alone does not establish a host transport refusal or a
working remote service. Do not describe this path as fixed or substitute a
successful connection result without establishing the guest's required state
and response contract.

For `e934256c238a`, New Game stopped at the unimplemented LGT graphics slot
`0xed`. The caller passes a framebuffer, crop coordinates, dimensions and a
length pointer, then supplies the result to `CreateImage`. This identifies
`MC_grpEncodeImage`, consistent with the
[WIPI graphics contract](https://mirusu400.github.io/wipi-wiki/c-api/graphics.md).
The implementation synchronizes guest-written RGB565 pixels, clips the region,
encodes BMP with the existing shared encoder, and returns a caller-owned buffer
from the normal guest heap. Tests cover round-trip pixels, cropping, overflow
boundaries, invalid pointers and release through the ordinary free operation.
New Game now passes the opening story, reaches the controls tutorial, and
accepts character movement in the first playable scene.

After these changes, `make test`, `make test-debug`,
`go test -race ./internal/...`, and `go vet ./...` passed. Debug and release CLI
and server builds succeeded. The interactive checks above used the debug CLI;
a fresh browser playthrough remains outside this evidence.

### New-character dependency and transport investigation, 2026-09-20

The first failure is now localized. The archive declares
`REQLIB=01039AD6|01.00.08` and calls `Kernel.getExecNames` with the required
identifier as its name filter. The current application has a different AID.
The lookup returns no installed match; the guest caches this as a false flag.
Its connection routine reads that flag at `0x1c6e6a` and enters its own error
path before calling `Network.connect`. The displayed server-disconnected
message therefore hides a missing dependency check. A scan of 608 local ZIPs
under `var/games` and `var/ext` found the reference only in this archive's
descriptor and no archive providing the required application.

A diagnostic-only experiment changed that flag in an isolated session. It
then reached the emulator's ordinary `Network.connect` refusal. A second
experiment supplied connection success, which reached
`URL.find("socket://wipiwicgsfg.magicn.com:17096")`. DNS resolution of that
hostname failed on this machine on 2026-09-20. This observation does not prove
when or why the remote service stopped resolving.

A third experiment supplied an in-memory socket with an empty input stream
and a capture-only output stream. The guest requested both streams, then raised
a caught array-bounds exception before sending bytes. The initial attribution
to incoming data was incorrect: the follow-up below locates the exception in
the outgoing identity header. Empty input alone did not establish peer order.
Neither an installed-name alias nor blanket connection success establishes a
playable offline new-character path. The compatible service response and
subsequent character-creation protocol remain unimplemented.

All three experiments were confined to a temporary test and isolated saves.
Their injected success values, guest-memory edits, and socket stand-ins are
not present in product code. Evidence, screenshots, memory dumps and the local
probe source are under `var/acceptance/qa-network-20260920/`. No game payload,
subscriber data or save was sent to the remote host; the only external check
was DNS resolution.

`Kernel.getExecNames` now records its filters and match count through the
existing debug diagnostic boundary, so a future saved log exposes the missing
lookup instead of only the guest's generic connection message. A `REQLIB`
declaration does not fabricate an installed application. The regression suite
explicitly preserves that distinction. Release diagnostics remain disabled.

For this diagnostic-only follow-up, ordinary, debug and race KTF package tests
and `go vet ./internal/platform/ktf` passed. Both CLI and server debug/release
builds succeeded. An unmodified archive startup with the rebuilt debug CLI
recorded `kernel executable lookup name="01039AD6" version=* vendor=* matches=0`.
The earlier full-repository gates describe the preceding gameplay API fixes;
they were not rerun for this lookup diagnostic addition.

### Relay framing and worker progress, 2026-09-20

Compatibility has three distinct owners. Handset properties and worker grants
belong to the common platform runtime. The recovered relay envelope belongs
to a protocol module in `internal/platform/ktf/relay_protocol.go`. Application
commands and service state require a separate local service contract; recognizing
an envelope is not enough to claim that service is installed or successful.
CLI and browser hosts must use the same runtime path. No title-name condition,
archive digest dispatch, or guest-address patch implements product behavior.

The outgoing header builder reads `WIPISTANDARDVERSION`, trims it, encodes it,
and marks its first byte with `0x80`. The previously empty property caused the
array-bounds exception at `0x1cdeac`. Both Java property entry points now expose
the shared value `1.2.1`. This is the emulator's chosen baseline, not a claim of
complete WIPI conformance or a recovered handset's exact version string. The
WIPI 1.2.1 property documentation consulted does not specify this property.
The regression test fails with the previous empty value and exercises the
caller-visible byte conversion after the change.

With diagnostic dependency and socket stand-ins, the guest emits a 72-byte
frame and later a 71-byte frame. Both have 52 extension bytes; the payloads
have 13 and 12 bytes respectively. Their command semantics remain unresolved.
The codec consumes and reproduces both captured frames exactly. The prefix is:

| Offset | Width | Meaning |
| --- | --- | --- |
| 0 | 1 | Zero marker |
| 1 | 4 | Big-endian total frame length, including the prefix |
| 5 | 2 | Big-endian extension length |
| 7 | variable | Extensions, followed by application payload |

The compiled writer at `0x1cdd00` establishes the prefix, and the reader at
`0x1cf340` consumes its one-, four-, and two-byte fields. Extension and payload
contents stay opaque in the codec. Tests use authored bytes, cover every
truncated prefix/body, adjacent frames, malformed lengths and the one-MiB
emulator resource limit. The codec is not yet connected to a product socket
or local service, and does not enable network success.

The initial capture also exposed a scheduler defect. A UI callback requeues
itself each round. `ServiceThreads(ctx, 1)` counted that serial callback as
the only worker grant, so a started receiver never entered `run()`. The common
scheduler now grants the requested worker slice independently of the client
thread's serial callback. Each queue retains its existing bounds and ordering.
An authored test proves both make progress on successive rounds and that a
completed worker retires; it fails before the change with `serial=1 worker=0`.
The archive then enters both its receiver (`0x1cc204`) and relay input reader.
With immediate EOF it reports disconnection and retires the receiver. This is
an expected diagnostic transport failure, not successful character creation.
A second probe supplies an extension-free envelope containing three authored
big-endian words (`12, 0, 10`). It reaches the application receive queue at
`0x1b5300`; this proves frame delivery, not the meaning or validity of a
character-creation response.

All dependency flags, connection successes, and socket stand-ins remain in the
ignored probe only. Evidence is under `var/acceptance/qa-relay-20260920/`, using
isolated saves under `var/savedata/qa-relay-20260920/`. No service payload was
sent externally. The bounded local service now implements the recovered
handshake, confirmation text, and displayable EUC-KR slot name. It is exposed
only when the archive declares the matching provider contract; unrelated
applications retain the offline refusal. The original save tree is never
modified by acceptance runs.

The first implementation returned a binary digest in the slot-name field. The
guest treated those bytes as a bitmap-font string, so selecting the new slot
raised `ArrayIndexOutOfBoundsException` from its paint callback. The service now
returns a short EUC-KR display name, and its regression test checks that the
field round-trips through the same encoding. A copied save with the corrupted
field was repaired for reproduction only; no user save was changed.

After the property, framing and scheduler changes, `make test` (including
219 browser-client tests), `make test-debug`, `go test -race ./internal/...`,
and `go vet ./...` passed. Debug and release CLI/server builds succeeded.
The temporary probe was retained as text under ignored evidence, outside Go
package discovery. These checks do not establish offline new-character
completion or a browser playthrough.

The later [slot creation and save overlay check](ktf-save-slots-2026-09-23.md)
validates sequential creation and restart of all six slots in both profiles,
including the zero-based first-slot request and a save/resource path collision.
