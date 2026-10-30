# Offline authentication compatibility

Authentication compatibility applies automatically to recognized local game
archives in both the PWA and CLI, and never contacts a license server. The
settings panel (설정) has no authentication control or notice; old stored
preferences and the obsolete incoming `authentication` field do not affect new
sessions. `runktf`, `runskt` and `runlgt` use the same platform defaults.
`-no-auth` and `Options.DisableAuthentication` remain as an explicit diagnostic
comparison path, and the old `-auth` flag is still accepted for existing
scripts. A resumed session keeps its original runtime. CLI reporting is in
[the command line](cli.md#offline-authentication-compatibility).

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
| `lgt-certificate-100` | Connected embedded-certificate and options contracts match. Keep the certificate and its presence flag private while ordinary progress persists. |
| `lgt-offline-notification` | Connected Thumb or ARM notification contracts match. Acknowledge the explicit choice locally; the Thumb variant also supports its empty remote-save query. |
| `lgt-offline-authentication` | Connected packet builder, dial/socket callbacks and response dispatcher match. Answer the exact session-bound authentication request in process. |
| `lgt-save-subscriber` | A recognized native revision's save reader matches. Accept a saved subscriber identity from another handset; the original length and checksum checks still run and no file changes. |
| `lgt-certificate-message` | One of two recognized native revisions' two-stage certificate exchange matches. Answer it through the bounded local socket service so the guest sets and saves its own certificate-present flag. |
| `skt-license` | A complete normalized Java license-check shape matches a supported compiler/library layout. Adapt only its terminal license comparison in a private class-file copy. |
| `skt-native-certificate` | A recognized native executable's packaged certificate names the handset it was issued to. Accept that one comparison in the code before it is loaded; every other check still runs and no file changes. See [SKT native](skt-native.md#skt-native-identity). |
| `skt-native-save-subscriber` | A recognized native executable's packaged save names the handset it was written on. Accept that one comparison; the save's own length and checksum checks still run and no file changes. See [SKT native](skt-native.md#skt-native-identity). |

The status constants are in `internal/backend/authentication.go`. Selection is
not evidence that a game reached playable input. Unknown certificate formats,
other LGT transport/application protocols and encrypted executables are not
silently treated as one of these mechanisms. Research and acceptance evidence
are in [authentication case history](history/authentication.md#cases-authentication-case-inventory).

## KTF certificates and identity

The 23-byte certificate is selected before guest execution. The 52-byte reader
and the subscriber accessor need relocated pointers, so they are selected after
runtime initialization and before application construction. Recognition never
changes guest instructions: it checks bounded instruction windows, branch and
call targets, pointer-table locations and referenced strings, and refuses
conflicting candidates. These recognizers do not select by archive name or
digest; the receipt recovery below additionally limits a private data layout to
one compiled image revision.

The 52-byte reader decrypts 48 bytes and checks the AID at offset 10, the
subscriber identity at 36 and a 16-byte application token at 20. Recognition
also checks the decoded header, archive AID, token presence and old identity.
The cipher table comes from the archive's own decoder pointer. Its inverse
searches all 256 entries and keeps the last matching index, because the table
need not be a permutation, and every replacement byte must round-trip through
that exact decoder. The other fields and the trailing four bytes are unchanged:
this reader does not validate that trailer. This is field adaptation, not a
general certificate issuer for other 52-byte formats.

The number-length investigation first established that a short number opens an
offline path. Disassembly explains why: the accessor replaces input of at most
four digits with an embedded 11-digit number, and a fresh run given that full
number also reaches the menu. The adapter therefore uses the executable's own
fallback rather than exposing a short number to other guest APIs. Earlier
statements that *every* full-length number fails overstated the original sweep.

A saved-record reproduction found another gate in compiled image SHA-256
`390284cd703b23cd3fddd25991a290fe2a8d9f2104a61db4437fcb39cfec44fc`: its 64-byte
`prefs` can retain five zero content lengths despite complete packaged files.
For this revision only, the subscriber adapter validates the record checksum and
marker, obtains the cipher table from the archive, and restores the five 19-bit
lengths on reads. The packaged receipt must agree with all five file lengths,
excluding their 44-byte footers. Any saved data override must be byte-identical
to the packaged data; missing, removed or changed data, read errors, invalid
receipts and partially populated length tables prevent recovery. No absent
content is fabricated.

The recovery changes only length fields and their encrypted checksum. Settings,
slot files, timestamps, nonce and reserved bits stay intact. Reading does not
rewrite the backing receipt, and later guest writes persist normally through the
save boundary. This revision-scoped recovery does not claim a general receipt
format. Evidence and remaining QA are in
[the gameplay investigation](history/ktf.md#ktf-gameplay-qa-2026-09-20).

KTF, LGT and SKT snapshot the Host identity after Host configuration (the
default is set by `wipic.SetSubscriberNumber`; see
[network](network.md#the-subscriber-number-is-the-one-property-worth-changing)).
KTF's recognized fallback can change its own snapshot before application
construction. KTF's C, HandsetProperty, System and DMInfo readers agree; LGT's C
and Java property readers agree; SKT's MIN and carrier agree. No adapter changes
process-wide defaults or another running session's identity.

Both certificate formats use the same in-memory save wrapper. Certificate writes
and deletion state stay local to the run; the backing certificate and its
original deletion bit stay intact, while other saves and deletion-ledger entries
persist normally. Disabling compatibility for diagnostics exposes the original
state again. The explicit `provision` command keeps its separate, persistent
behavior.

## SKT license checks

Recognition fingerprints the complete method, including branch offsets, local
slots, stack/local limits, exception ranges, platform references and license
constants. Constant-pool indexes resolve to their meanings. Application-owned
class and member names and display strings are normalized, so obfuscation and
notice wording do not select behavior; other instructions, descriptors or
license constants produce a different shape. Parsing uses the bounded class-file
reader.

The supported layouts read `MIDlet-Key` and `MIDlet-Jar-URL`, extract a service
identifier, combine subscriber data and a library salt, and compare a computed
digest against the key. One layout selects among four key generations. The final
`String.equals` call becomes `pop2; iconst_1; nop` in a session-owned class-file
copy, keeping its length and stack effect. Earlier code, library state
initialization, digest calculation, exception handling and every other equality
comparison execute normally. The archive and JVM implementation are unchanged.

## Validation scope

The local gameplay and checkpoint evidence (2026-09-11 to 2026-09-13) was
collected in a combined working tree that also contained SKT menu-key and
repaint-queue fixes and LGT numeric input. It establishes the authentication
behavior in that tree; it does not imply that an authentication-only checkout
includes those gameplay fixes, and the 58-byte Clet may need the numeric-input
change to create a name. Off/on browser runs describe the earlier opt-in design;
the current page applies recognized compatibility automatically.

## Validation evidence

Authored tests exercise relocated addresses, decoder execution, duplicate cipher
entries, invalid identities, changed instructions, truncated input, private saves
and certificate deletions. An authored J2ME license JAR
(`internal/platform/skt/testdata/license.jar`) has the same normalized compiler
layout as a supported check, with a synthetic digest. It proves opposing session
policies, retained digest side effects, unchanged general equality, ordinary RMS
save/restart, renamed classes and methods, malformed-property exceptions and
near matches. The Go WebSocket handler reports the applied status and retains it
on resume.

Local routes under `var/acceptance/auth/2026-09-11/` take the 52-byte KTF case,
the five SKT license refusals and the number-dependent KTF case through playable
input, an ordinary guest save and a fresh release restart; four controls (three
LGT, one KTF) give matching step digests with compatibility off and on, and four
longer LGT controls reach play without a selected adapter. Chromium and WebKit
exercised each status against a real Go server. The per-case routes, byte counts
and evidence directories are in
[the acceptance records](history/authentication.md#cases-reference-acceptance-records).
All probes use private save roots, and the checked original archive SHA-256
values are unchanged.

## LGT cached authentication

A supported Clet reads and writes a 56-byte options record. Its response handler
accepts message 302, reads the result byte at offset 6 and stores the cached
success word at options offset 40. Its startup gate tests that word separately
from the following notice-state word. Recognition connects the complete file
reader and writer, bounded response and gate instruction windows, their resolved
literal pointers, the shared options buffer and the file-call trampoline. It
rejects different instructions, disconnected buffers and malformed addresses;
the filename or record length alone never selects this adapter.

The save view changes that word to one on reads of a complete recognized record.
Writes preserve the original four authentication bytes while persisting other
settings normally, including writes that truncate the record. Progress files and
creation/deletion ledgers pass through unchanged. Packaged defaults remain in the
archive, and absent records remain absent until the guest creates them, so a
game's ordinary first-run restart can still be required. No dial, socket or
application response is fabricated.

In the local case only options word 10 clears the certificate request (words 8,
9 and 12 do not), after which declining the optional remote-save recovery
reaches the title and offline mode menus. Authored Clet/JAR tests exercise guest
file open/read/write/close, opposing policies and fresh-session settings
persistence. Scanning the 109 distinct local LGT modules selects one layout; the
other 108 keep ordinary behavior. The career, batting and restore route is in
[the acceptance records](history/authentication.md#cases-reference-acceptance-records).

## LGT socket prerequisite

A successful-dial experiment reached `MC_netSocket(2, 1)` at slot `0x25a`, which
was unimplemented. Its Thumb callback checks a negative descriptor, and the
following successful path converts an address and port before socket connect.
This establishes the standard-order slot alongside the already observed `0x7d0`
variant. Both now resolve and return an ordinary socket failure; no socket
opens. The authored regression failed on the missing slot before the change and
passes for both slots afterwards, along with the focused network tests.

The controlled local replay now completes the callback and reaches the menu. The
original failed-dial route also reaches that menu, so this case does not justify
a dial-success authentication adapter. The successful dial exists only in an
isolated diagnostic overlay; production network policy is unchanged. The
comparison and callback disassembly are recorded under the research directory
and `build/auth-complete/`.

## LGT encrypted 58-byte certificate

`lgt-certificate-58` requires complete connected Thumb reader, writer, cipher
and subscriber-gate instruction contracts, with bounded relocation-aware literal
and call resolution; the filename alone never selects it. The cipher uses seed
`0x21c3` and the 32-bit recurrence `state * 0x343fd + 0x269ec3`, XORing each byte
with bits 16–23. The decoded subscriber occupies bytes 40–51. The other 46 bytes
of an existing exact-length record are unchanged. An absent or empty record
starts with zeros outside the session identity (see
[the empty-certificate regression](#empty-certificate-regression-and-automatic-defaults));
other nonzero lengths are refused.

The certificate and its guest rewrites stay in memory, as do case variants of
the authentication filename. Both filesystem ledgers keep the original
certificate membership on disk while ordinary file creation, deletion and
progress persist. A fresh session uses its own subscriber snapshot; disabling
authentication exposes the original file state. No network callback, consent
selection or executable instruction changes.

Tests cover instruction mutations, relocated pools and calls, truncation,
invalid identities and lengths, opaque-field preservation, ledger membership,
ordinary persistence, both language property readers, opposing session policies
and execution of the guest cipher against the session-generated record. The
109-module local scan selects exactly one case. The selected Clet starts with no
backing certificate, accepts a numeric character name, reaches a minigame and
restores an early checkpoint in a fresh release run; the route, file sizes and
browser checks are in
[the acceptance records](history/authentication.md#cases-reference-acceptance-records).

## LGT local notification and empty remote-save service

`lgt-offline-notification` recognizes connected notification writer/reader and
dial/socket callbacks. The Thumb variant also requires its remote-save command
writers, reply parsing and opcode dispatch. It resolves the application token
and IPv4 endpoint from those relationships; neither an archive name nor a fixed
application identifier selects it. The scan of 109 local LGT modules found one
matching the Thumb contract. The endpoint is compared as data and never
contacted. No OS socket, DNS lookup, SMS transmission, remote-save upload,
deletion, download or purchase operation is implemented.

The user still chooses Yes or No in the guest. The bounded `SMSAGREE` request
carries that explicit choice; local reply byte 3 acknowledges Yes and byte 2
acknowledges No. The guest writes its own one-byte `AGREE` receipt and asks to
restart. This is an ordinary guest save, not a record the emulator places in the
save tree, and disabling compatibility preserves it like ordinary progress. The
setting does not grant consent, transmit the subscriber number or register a
choice with a third party.

On restart, `IS_SAVEDATA_EXIST` receives an eight-byte empty-result frame:
opcode 2, zero status, zero subscriber-field length, false existence and zero
network-order data length. `FINISH_SAVEDATA` receives opcode 7, zero status and
zero variable-field length. These exchanges stay local, and their status is
available in diagnostics. The guest may show its own restart and no-remote-save
notices; they are not evidence of an external service or cloud backup.

The ARM variant uses a separate contract: its virtual tables connect the
agreement initializer, command builder, reply handler and stream decoder. The
dial and socket callbacks must share the transport state, and their methods must
appear together in the transport table. Instruction windows and connected
literal pools establish the contract; names, hashes and fixed image addresses
are not selection keys, and changed compiler layouts remain unsupported.

Its command is `SMSAGREE <identity> <application> <N-or-Y>` plus a terminating
NUL, bounded to 64 bytes rather than padded to 100. The reply contains the
choice's opcode, a one-byte text length and the local informational message
`Handled locally. Nothing sent.`, which the guest displays and dismisses through
its own UI. This variant acknowledges only the agreement and does not accept the
Thumb variant's remote-save commands; the guest writes its own receipt and
progresses after confirmation. The local reproduction and its limits are in
[the gameplay QA report](history/skt.md#gameplay-qa-2026-09-20-lgt-agreement-reproduction).

Only the recognized dial callbacks, socket callback, endpoint, session subscriber
and application token qualify. The limits are four sockets and 64 pending dials;
requests accumulate at most 100 bytes, and partial writes and reads preserve
order. Other commands, identities, endpoints, trailing bytes and invalid guest
pointers fail. Connect/read/write callbacks run between guest calls and are
one-shot; closing a socket or the network cancels pending callbacks, including
later callbacks in an already selected batch. Separate clients own their buffers
and identity.

Authored tests cover both explicit choices, malformed requests, remote-save
query ordering, relocated and disconnected recognition contracts, guest callback
arguments, short reads, memory bounds, queue limits and cancellation. Local debug
No and release Yes runs both produce the corresponding guest acknowledgement and
ordinary receipt; play and restart acceptance is in
[authentication case history](history/authentication.md#cases-additional-offline-control-routes).

## LGT local binary authentication

`lgt-offline-authentication` recognizes one connected native contract: its
packet builder writes the `KP` header and protocol version 7, its request writer
uses command 12 and a 60-byte record, its dial callback creates the socket named
by the same endpoint wrapper, and both receive branches reach the dispatcher
whose command-12 case records completion. The recognizer resolves relocated
calls, callback pointers, the IPv4 endpoint, application token and protocol
version from those relationships. It does not use an archive name, digest, fixed
application identifier or fixed code address.

The expected request is rebuilt from the session subscriber number and handset
model plus the recognized module's application and version fields. Every byte,
including padding and the trailing mode word, must match, and partial writes are
bounded to 60 bytes. The local response is the eight-byte `KP` frame for protocol
7, command 12 and status zero. Other requests, callbacks, endpoints, application
identities and trailing bytes fail without a success response. The endpoint is
never opened or resolved.

The [WIPI C network contract](https://mirusu400.github.io/wipi-wiki/c-api/network.md)
requires a zero-returning connect to report its later result through the
callback, defines socket callbacks as `(fd, error, param)`, and makes read-ready
callbacks one-shot after `M_E_WOULDBLOCK`. The responder uses that asynchronous
path. A failure callback was already reaching the guest; the wait continued
because this title deliberately keeps its progress screen on failure and expects
the packet exchange.

An authored relocated module covers connected recognition, changed code and
literals, disconnected calls, exact fragmented requests, byte mutations, length
bounds, archive binding and diagnostic opt-out. A scan of 127 parseable local
archive copies selected three copies of two distinct modules; all other modules
stayed unselected. On the reported fresh-save route, confirming the connection
completes the write, two short reads and the socket close, leaves the
authentication progress screen and reaches the guest's next explicit consent
choice; declining that choice reaches character information input. The emulator
does not choose either consent button.

## Authentication acceptance boundary

The seven earlier recognized mechanisms have local routes through playable
input, ordinary saving and a fresh release restart. The notification route
writes a 10,128-byte guest container; its populated level-one slot restores the
opening room with all three save-file hashes unchanged. Both explicit
notification choices and a fresh disabled control are checked. Chromium and
WebKit confirm the status, retained policy, disabled restart and unsupported
control through the real server. Concurrent clients with opposing
choices/identities and a disabled client are isolated in the race-tested
notification suite.

This establishes those seven adapters within the [validation scope](#validation-scope).
The binary-handshake adapter (`lgt-offline-authentication`) has a fresh-start
route through character input; ordinary save and fresh-release restart remain
outside its acceptance. `lgt-save-subscriber` and `lgt-certificate-message` are
accepted on the evidence linked from [LGT](lgt.md). Unknown schemes, cloud saves,
purchases, missing resources, later campaign behavior and quick saves are not
implied by this acceptance. Native keyboard and Hangul input have their own
boundary in [native-text-input.md](native-text-input.md). Earlier failed
hypotheses are in
[authentication case history](history/authentication.md#cases-authentication-case-inventory).

## Empty certificate regression and automatic defaults

A user report exposed a missed existing-save case in the 58-byte adapter: a
failed original connection had left a zero-byte `audio.adt`. Recognition matched
the code, but the store rejected every present file whose size was not 58, so the
session reported `unsupported` and followed the guest's connection wait.
Fresh-save acceptance had not covered this state.

For this recognized contract, an empty certificate now receives the same private
58-byte replacement as a missing one. The zero-byte backing file,
creation-ledger membership and ordinary options and progress stay intact; other
nonzero invalid lengths still fail recognition. An authored regression test
fails before the change and passes afterwards, including private rewrites and a
second session against the preserved empty file. Copies of the user's save
reproduce the connection wait before the fix and reach the game menu afterwards
in both debug and release, without any authentication flag
([case record](history/authentication.md#cases-existing-empty-certificate-and-automatic-defaults)).

Automatic defaults replaced the initial opt-in design at the user's request. The
shared session and all three platform option types use a diagnostic
`DisableAuthentication` opt-out; the browser sends no policy switch and records
the applied or unsupported result in diagnostics, without settings-panel text.
An older browser's `authentication: false` request is ignored, and a fresh
browser needs no preference.

## Authentication-only branch verification

After the input and repaint changes were separated, the authentication-only
branch passed the focused checks, the four Go/Node gates, debug/release builds
and a Chromium route; a shortened WebKit run reported a `createImageBitmap` Blob
error, which remains an open frame-decoding issue. Details are in
[the acceptance records](history/authentication.md#cases-reference-acceptance-records).

The embedded 100-byte format and first-run initialization behavior are
documented in the
[2026-09-19 startup investigation](history/ktf.md#startup-compatibility-2026-09-19-lgt-embedded-certificate-74b0ff2230f7).
