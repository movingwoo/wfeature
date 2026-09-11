# Offline authentication compatibility

Authentication compatibility applies automatically to recognized local game
archives in both the PWA and CLI. The Opts panel has no authentication control
or notice. Old stored preferences and the obsolete incoming `authentication`
field do not affect new sessions. `runktf`, `runskt` and `runlgt` use the same
platform defaults. `-no-auth` and `Options.DisableAuthentication` retain an
explicit diagnostic comparison path. The old `-auth` flag remains accepted by
the CLI for existing scripts. A resumed session retains its original runtime.
This is separate from the server access key and never contacts a license server.

## Implemented mechanisms

| Reported status | Recognition and adaptation |
|---|---|
| `off` | Compatibility was explicitly disabled for diagnostics. |
| `unsupported` | No implemented mechanism matched; ordinary behavior remains. |
| `ktf-certificate-23` | The packaged certificate, checksum, AID, cipher table and executable property/file names match the existing 23-byte format. Generate a session certificate for the session's subscriber number. |
| `ktf-certificate-52` | A complete Thumb reader and decoder match; resolve their file and cipher pointers from the relocated image. Replace only the certificate's 12-byte subscriber field. |
| `ktf-subscriber-fallback` | A complete Thumb subscriber accessor matches, alongside a packaged 64-byte `prefs` record and no `cert.c2s`. Resolve its embedded fallback number and use that full number throughout this session. |
| `lgt-cached-authentication` | Matching Thumb options reader/writer, authentication response handler and startup gate share a buffer. Expose its cached authentication word only within the session. |
| `lgt-certificate-58` | Complete connected Thumb reader, writer, cipher and subscriber comparison match the 58-byte format. Keep a certificate for the session identity in memory. |
| `lgt-offline-notification` | Connected Thumb notification and remote-save contracts match. Acknowledge the explicit choice locally; report no remote save and finish that query. |
| `skt-license` | A complete normalized Java license-check shape matches a supported compiler/library layout. Adapt only its terminal license comparison in a private class-file copy. |

Selection is not evidence that a game reached playable input. Unknown certificate
formats, other LGT transport/application protocols, and encrypted executables are not
silently treated as one of these mechanisms. Research and acceptance evidence
are in [authentication-cases.md](authentication-cases.md).

## KTF certificates and identity

The 23-byte certificate is selected before guest execution. The 52-byte reader and
subscriber accessor need relocated pointers; selection occurs after runtime
initialization and before application construction. Recognition never changes
guest instructions. It checks bounded instruction windows, branches/call targets,
pointer-table locations and referenced strings. Conflicting candidates are refused.
Names, archive digests and fixed instruction addresses do not select an adapter.

The 52-byte reader decrypts 48 bytes, checks AID at offset 10, subscriber identity
at 36, and a 16-byte application token at 20. Recognition also checks the decoded
header, archive AID, token presence and old identity. The cipher table comes from
the archive's own decoder pointer. Its inverse searches all 256 entries and keeps
the last matching index; it need not be a permutation. Every replacement byte must
round-trip through that exact decoder. The other fields and trailing four bytes
remain unchanged: this recognized reader does not validate that trailer. This is
field adaptation, not a general certificate issuer for other 52-byte formats.

The number-length investigation originally established that short input opens an
offline path. Disassembly explains why: the accessor replaces input of length at
most four with an embedded 11-digit number. A fresh run given that full embedded
number also reaches the menu. The adapter therefore uses the executable's own
fallback, rather than exposing a short number to other guest APIs. It does not
decode, rewrite or delete `prefs`. Earlier statements that *every* full-length
number fails were stronger than the original sweep supported.

KTF, LGT and SKT snapshot the Host identity after Host configuration. KTF's recognized
fallback can change its own snapshot before application construction. KTF's C,
HandsetProperty, System and DMInfo readers agree; LGT's C and Java property readers agree; SKT's MIN and carrier agree.
No adapter changes process-wide defaults or another running session's identity.

Both certificate formats use the same in-memory save wrapper. Certificate writes
and deletion state stay local to the run; the backing certificate and its original
deletion bit remain intact. Other saves and deletion-ledger entries persist normally.
Disabling compatibility for diagnostics exposes the original state again. Explicit `provision`
retains its separate, persistent behavior.

## SKT license checks

Recognition fingerprints the complete method, including branch offsets, local
slots, stack/local limits, exception ranges, platform references and license
constants. Constant-pool indexes resolve to their meanings. Application-owned class
and member names and display strings are normalized, so obfuscation and notice
wording do not select behavior. Other instructions, descriptors or license constants
produce a different shape. Parsing uses the bounded class-file reader.

The supported layouts read `MIDlet-Key` and `MIDlet-Jar-URL`, extract a service
identifier, combine subscriber data and a library salt, and compare a computed
digest against the key. One layout selects among four key generations. The final
`String.equals` call becomes `pop2; iconst_1; nop` in a session-owned class-file
copy. Its length and stack effect stay the same. Earlier code, library state
initialization, digest calculation, exception handling and every other equality
comparison still execute normally. The archive and JVM implementation are unchanged.

## Validation scope

This change contains authentication adapters, automatic selection and Host
reporting. Input and repaint fixes are separate changes. Earlier local gameplay
and checkpoint evidence below was collected in a combined working tree that
also contained SKT menu-key/repaint-queue fixes and LGT numeric input. Those
results establish the authentication behavior in that tree; they do not imply
that an authentication-only checkout includes those gameplay fixes. The
58-byte Clet may still need the separate numeric-input change to create a name.
Historical checkbox/off-on browser runs describe the earlier opt-in design.
The current page applies recognized compatibility automatically.

## Validation evidence

`make test`, `make test-debug`, `go test -race ./internal/...`, and `go vet ./...`
passed again after the certificate, subscriber, Java-license, SKT menu-key and
LGT cached-result changes, and again after the synchronous repaint queue fix. Chromium and
WebKit each exercised all three new statuses against a real Go server, including
per-game preferences, retained-session resume, a fresh start with the option off,
and an unsupported control. All six browser runs passed with no page errors. The new LGT status also passes
the same two-browser preference/resume/off/control route, using an isolated copy
of the ordinary first-run settings.
Evidence is in `var/acceptance/auth/2026-09-11/complete-browser/`.

Authored tests exercise relocated addresses, decoder execution, duplicate cipher
entries, invalid identities, changed instructions, truncated input, private saves
and certificate deletions. An authored J2ME license JAR has the same normalized
compiler layout as a supported check, with a synthetic digest. It proves opposing
session policies, retained digest side effects, unchanged general equality, ordinary
RMS save/restart, renamed classes/methods, malformed-property exceptions and near
matches. The Go WebSocket handler reports application and retains it on resume.

Local evidence under `var/acceptance/auth/2026-09-11/cases/` records:

- A 52-byte case with the option off stops at its authentication error. With it
  on, new-game input reaches a field, accepts movement and attack, and writes a
  1,000-byte ordinary save on return to the title. A fresh release runtime loads
  a private copy and resumes the saved stage, then accepts movement. This is an
  ordinary stage checkpoint, not an exact-position quick save.
- Five SKT startup refusals advance to titles, menus or dialogue with the option
  on. One classic layout now also completes new-game creation, skips its opening
  cutscene, accepts field movement, opens its menu and writes an ordinary
  105-byte RMS progress record plus settings. A fresh release runtime displays
  that slot, confirms loading, reaches the saved stage checkpoint and accepts
  movement. A second compiler layout also deploys units, accepts tactical movement and
  uses the game's own mid-battle save (`XFile`, three ordinary files). A fresh
  release process loads it from the title's Game Start / Continue submenu,
  restores the battle layout and accepts input. This is an existing game feature;
  it does not implement the proposed emulator quick-save feature. A third layout
  leaves its town, accepts field movement and writes an ordinary RMS save plus
  settings (537 bytes total). A fresh release process loads the saved field and
  accepts movement. A fourth layout executes a strategy training command,
  consumes all three available officers and saves five ordinary files (5,524
  bytes). A fresh release process loads the campaign with zero available officers
  and accepts another menu command. The fifth layout now completes its shooting
  training, including the two-enemy no-hit lesson, reloading and a headshot. The
  guest writes an 80-byte `save.dat`, alongside 12-byte installation and 6-byte
  settings records. A fresh production release process uses Continue to restore
  the first-mission selection without repeating training. All three hashes match
  immediately after restoration; mission and difficulty selection then reach the
  mission briefing. The debug investigation adds read-only diagnostics and drives
  ordinary key input; no guest state or save values are forced. Evidence is in
  `complete-training-diagnostics`, `complete-training-release` and
  `complete/skt-final-restore-result.json`. This completes the five observed SKT
  refusal cases' checkpoint save/restart routes; it does not prove entire campaigns.
  The first soft key needed its handset menu mapping when a Canvas has no MIDP
  commands; [the input contract](skvm.md#input) records that prerequisite. A
  second route exposed stale queued callbacks after `serviceRepaints`; the
  [queue correction](skvm.md#synchronous-repaint-queue) preserves input while
  the Host waits. The real route now remains responsive across those waits.
  During manual CLI investigation, a live worker and its clock can continue while
  the Host awaits the next command. The last route has timed tutorial goals; its
  own pause menu keeps those goals from expiring during inspection. Read-only
  guest-state diagnostics distinguish a failed tutorial from lost Host input.
- The number-dependent case completes character creation, skips the prologue,
  accepts keypad movement and writes a 320-byte ordinary save. A fresh release
  runtime lists the saved character, loads its town and accepts movement. Its
  in-game tabs and actions read numeric 4/6/5, while the opening menus also accept
  the directional and fire keys. Automatic selection uses the embedded full
  identity without changing Host defaults.

Four local controls (three LGT and one KTF) also repeat their refusal/decline
routes with the option off and on. Every captured step digest matches across
each pair. The LGT controls retain their failing transport; unknown code does
not select the cached-result adapter. Results are recorded in `complete/controls.json`.
Four longer LGT controls also reach actual play without a selected adapter: one
restarts initialized settings before character creation and combat; another
persists two explicit certificate refusals across fresh runs before opening its
board game; a third declines information sharing and optional saved-data download,
then completes the opening story and accepts movement and attack in town. The
fourth writes its own first-authentication marker after a 20-second guest timeout,
reaches town movement, and reopens the title from that marker in a fresh release
process. Their prompts alone do not justify an automatic network intervention.

All these probes use private save roots. The checked original archive SHA-256
values remain unchanged. Screenshots, commands and diagnostics distinguish the
proved routes from earlier incomplete observations.

## LGT cached authentication

A supported Clet reads and writes a 56-byte options record. Its response handler
accepts message 302, reads the result byte at offset 6 and stores the cached
success word at options offset 40. Its startup gate tests that word separately
from the following notice-state word. Recognition connects the complete file
reader and writer, bounded response and gate instruction windows, their resolved
literal pointers, the shared options buffer and the file-call trampoline. It
rejects different instructions, disconnected buffers and malformed addresses.
The filename or record length alone never selects this adapter.

The save view changes that word to one on reads of a complete recognized record.
Writes preserve the original four authentication bytes while persisting other
settings normally, including writes that truncate the record. Progress files and
creation/deletion ledgers pass through unchanged. Packaged defaults remain in the
archive; absent records remain absent until the guest creates them. A game's
ordinary first-run restart can therefore still be required. No dial, socket or
application response is fabricated.

In the local case, independently setting words 8, 9 or 12 does not clear the
certificate request. Setting word 10 does, then exposes an optional remote-save
recovery question. Declining recovery reaches the title and offline mode menus.
Automatic selection reproduces that path from an unmodified copy of the original
private settings. Authored Clet/JAR tests exercise guest file open/read/write/close,
opposing policies and fresh-session settings persistence. Scanning the 109 distinct local LGT modules selects one layout; the other 108
retain ordinary behavior. With the production adapter, a fresh career reaches
actual batting and writes 20 ordinary settings, roster and progress files. A fresh
release runtime loads a private copy, offers Continue and restores the career,
1,419 cash, ten pitchers and fifteen batters; team management accepts input.
The source copy remains byte-for-byte unchanged, and both backing options files
retain authentication word zero. Commands, screenshots and the preservation
report are recorded in `complete-adapter-play`, `complete-adapter-restore` and
`complete/lgt-restore-result.json` under the local evidence directory.

## LGT socket prerequisite

A successful-dial experiment reached `MC_netSocket(2, 1)` at slot `0x25a`, which
was previously unimplemented. Its Thumb callback checks a negative descriptor,
and the following successful path converts an address/port before socket connect.
This establishes the standard-order slot alongside the already observed `0x7d0`
variant. Both now resolve and return an ordinary socket failure; no socket opens.
The authored regression failed on the missing slot before the change and passes
for both slots afterwards, along with the focused network tests.

The controlled local replay now completes the callback and reaches the menu.
The original failed-dial route also reaches that menu, so this case does not
justify selecting a dial-success authentication adapter. The successful dial
exists only in an isolated diagnostic overlay. Production network policy remains
unchanged. The new comparison and callback disassembly are recorded under the
research directory and `build/auth-complete/`.

## LGT encrypted 58-byte certificate

`lgt-certificate-58` requires complete connected Thumb reader, writer, cipher
and subscriber gate instruction contracts, with bounded relocation-aware literal and
call resolution. The filename alone never selects it. The cipher uses seed
`0x21c3` and the 32-bit recurrence `state * 0x343fd + 0x269ec3`, XORing each byte
with bits 16–23. The decoded subscriber occupies bytes 40–51. The other 46
bytes of an existing exact-length record remain unchanged. An absent or empty record
starts with zeros outside the session identity; other nonzero lengths are refused.

The entire certificate and its guest rewrites stay in memory. Case variants of the authentication filename stay private as well. Both filesystem
ledgers retain the original certificate membership on disk while ordinary file
creation, deletion and progress persist. A fresh session uses its own subscriber
snapshot; disabling authentication exposes the original file state. No network
callback, consent selection or executable instruction is changed.

Tests cover instruction mutations, relocated pools and calls, truncation,
invalid identities and lengths, opaque-field preservation, ledger membership,
ordinary persistence, both language property readers, opposing session policies,
and execution of the guest cipher against the session-generated record.

The 109-module local scan selects exactly one case; the other 108 remain
unselected. All four required validation gates pass after this adapter and the
numeric input fix. Chromium and WebKit each pass the real page/server route for
`lgt-certificate-58`, retained resume, a fresh authentication-off start and an
unsupported control. These two runs extend the earlier eight browser checks.

The selected real Clet now starts with no backing certificate, accepts a numeric
character name, reaches a minigame, and responds to numeric action input. It
writes ordinary 345-byte and 142-byte progress records, plus a 115-byte options
file and the creation ledger. A fresh release run loads that new-character,
day-150 checkpoint through Continue without another name prompt. All four
checkpoint hashes match immediately after restoration; later play updates the
ordinary progress record normally. No `audio.adt` appears in either save tree.
This proves that early checkpoint, not a later campaign or completed minigame.
Evidence is under `complete-adapter58-confirmed`, `complete-adapter58-release`
and `complete/certificate58-restore-result.json`.

An additional initialized LGT control reaches tutorial movement and attack with
status `unsupported` (`a23f3c9fc2cb/complete-offline-final`). Its longer route
confirms that the earlier title screen can lead to actual offline input.

Two further controls extend ordinary paths: an initialized LGT restart reaches
battle movement and attack (`b90d4f003201`), and a fresh KTF run explicitly
declines SMS reception before reaching character creation and outpost input
(`49ade89578c5`). Both retain status `unsupported`.


## LGT local notification and empty remote-save service

`lgt-offline-notification` recognizes connected notification writer/reader,
dial callbacks, remote-save command writers, reply parsing and opcode dispatch.
It resolves the application token and IPv4 endpoint from those relationships;
neither an archive name nor a fixed application identifier selects it. Of 109
local LGT modules, one matches this contract. The endpoint is compared as data
and never contacted. No OS socket, DNS lookup, SMS transmission, remote-save
upload, deletion, download or purchase operation is implemented.

The user still chooses Yes or No in the guest. The bounded `SMSAGREE` request
contains that explicit choice; local reply byte 3 acknowledges Yes and byte 2
acknowledges No. The guest writes its own one-byte `AGREE` receipt and asks to
restart. This is an ordinary guest save, not a fabricated record placed in the
save tree by the emulator. Disabling compatibility for diagnostics preserves that guest-written
receipt just as it preserves ordinary progress. The setting does not grant
consent, transmit the subscriber number, or register a choice with a third party.

On restart, `IS_SAVEDATA_EXIST` receives an eight-byte empty-result frame:
opcode 2, zero status, zero subscriber-field length, false existence and zero
network-order data length. `FINISH_SAVEDATA` receives opcode 7, zero status and
zero variable-field length. These exchanges remain local; their status is
available in diagnostics.
The guest may show its own restart and no-remote-save notices; those notices are
not evidence of an external service or cloud backup.

Only the recognized dial callbacks, socket callback, endpoint, session subscriber
and application token qualify. Four sockets and 64 pending dials are the limits.
Requests accumulate at most 100 bytes; partial writes and reads preserve order.
Other commands, identities, endpoints, trailing bytes and invalid guest pointers
fail. Connect/read/write callbacks run between guest calls and are one-shot;
closing a socket or network cancels pending callbacks, including later callbacks
in an already selected batch. Separate clients own their buffers and identity.

Authored tests cover both explicit choices, malformed requests, remote-save
query ordering, relocated and disconnected recognition contracts, guest callback
arguments, short reads, memory bounds, queue limits and cancellation. The four
required Go/Node validation gates pass. Local debug No and release Yes both
produce the corresponding guest acknowledgement and ordinary receipt. Play and
restart acceptance is recorded in [authentication-cases.md](authentication-cases.md).


## Authentication acceptance boundary

The seven recognized mechanisms have local routes through playable input,
ordinary saving and a fresh release restart. The final notification route writes
a 10,128-byte guest container; its populated level-one slot restores the opening
room with all three save-file hashes unchanged. Both explicit notification
choices and a fresh disabled control are checked. Chromium and WebKit confirm
the new status, retained policy, disabled restart and unsupported control through
the real server. Concurrent clients with opposing choices/identities and a
disabled client are isolated in the race-tested notification suite.

This establishes the seven adapters within the validation scope above. Unknown schemes,
cloud saves, purchases, missing resources, later campaign behavior, Hangul input
and quick saves are not implied by this acceptance. The latter two remain
separate implementation items. Detailed evidence and earlier failed hypotheses
remain in [authentication-cases.md](authentication-cases.md).


## Empty certificate regression and automatic defaults

A user report exposed a missed existing-save case in the 58-byte adapter. A
failed original connection had left a zero-byte `audio.adt`. Recognition matched
the code, but the store rejected every present file whose size was not 58, so the
session reported `unsupported` and followed the guest's connection wait. Earlier
fresh-save acceptance did not cover this state.

For this recognized contract, an empty certificate now receives the same private
58-byte replacement as a missing certificate. The zero-byte backing file,
creation-ledger membership and ordinary options/progress remain intact. Other
nonzero invalid lengths still fail recognition of the stored certificate. An
authored regression test fails before the change and passes afterward, including
private rewrites and a second session against the preserved empty file. Copies of
the user's save reproduce the connection wait before the fix and reach the game
menu afterward in both debug and release, without any authentication flag.

Automatic defaults replace the initial opt-in product design at the user's
request. The shared session and all three platform option types use a diagnostic
`DisableAuthentication` opt-out; the browser sends no policy switch and records
the applied/unsupported result in diagnostics, without settings-panel text.
An older browser's `authentication: false` request is ignored, and a fresh browser needs no preference. Historical off/on
comparisons elsewhere in this document describe the policy controls available
when those measurements were taken.


## Authentication-only branch verification

After separating input and repaint changes, the focused platform/session/Host
checks, all four required Go/Node gates, and debug/release CLI/server builds pass.
An extended Chromium run using this branch's release server and a copy of the
existing empty-certificate save reaches the game menu. It also verifies automatic
selection despite an obsolete false preference, the absent checkbox, retained
resume, fresh start and an unsupported authored control without page errors.
Evidence is under `var/acceptance/auth/2026-09-11/authentication-only-pr/`.
This does not extend the earlier combined-tree checkpoint evidence to the
separate numeric-input or repaint fixes.

WebKit's authentication, reconnect, restart and control assertions also pass on
this branch, but its shortened run reports one Blob access-control error from
`createImageBitmap`. The final no-page-error assertion therefore fails. The
error remains an open frame-decoding issue, not a clean WebKit acceptance result;
its stack and route observations are retained beside the Chromium evidence.
