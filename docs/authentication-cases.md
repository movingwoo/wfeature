# Authentication case inventory

Research date: 2026-09-11. Source revision: `6d62c35`. The initial investigation preceded
[the common feature](authentication.md). Later sections
record the implementation and acceptance that followed;
[authentication.md](authentication.md) describes current production support. The question is which guest state transition prevents
local play, and what evidence distinguishes it from a similar-looking screen.

## Scope and evidence

The later gameplay and checkpoint records were collected in a combined working
tree with separate input/repaint fixes. Those fixes are excluded from the
authentication-only change; see [validation scope](authentication.md#validation-scope).
These dated records must not be read as proof that this change supplies numeric
name input, menu-key handling or repaint fixes.

The local census recursively reads every file under `var/games`, including the
unsorted directories. It records the complete SHA-256, content-based platform
classification, nested archive entries, static signals, and scan failures.
Short identifiers below are the first twelve hexadecimal digits of that hash,
not recognition rules for a future implementation. File names and per-archive
records remain in ignored `var/acceptance/auth/2026-09-11/`.

The census contains 543 files, with 492 distinct byte contents. Supported
platform classifications account for 465 distinct archives: 277 KTF, 109 LGT,
and 79 SKT. The other 27 distinct files are inventoried too, rather than counted
as authentication failures. Exact duplicates do not multiply the case count;
archives with different bytes remain separate even if their displayed titles
look alike.

Evidence has four different strengths:

- **Current controlled observation:** the same archive and command sequence,
  separate save roots, and one stated change. Screens, responses and logs are
  retained. This establishes the observed branch, not all later gameplay.
- **Current observation:** a fresh run, a verified source contract, or an
  archive structure. A dialog alone does not establish its cause.
- **Historical investigation:** earlier traces and experiments in platform
  documents. These explain a mechanism but are not new acceptance results.
- **Unresolved candidate:** a symptom with a named next discriminating check.
  Unknown does not mean that a generic success response is appropriate.

The initial opening probes used newly built debug executables. Original archives and existing user
save roots are preserved. Research saves are isolated under
`var/savedata/auth-research`. The LGT diagnostic executable uses a Go build
overlay that changes only the C dial callback result and Java `Network.connect`
result to success. Socket operations remain refused. Production network behavior was unchanged during those opening probes. Later
local-protocol work is recorded separately below. Parallel run durations are not performance
measurements.

The shared opening probe advances 400 ticks, then takes four 200-tick steps
separated by confirm keys; the last also presses left. Five screen captures
make choices and transitions inspectable. Each process has a 30-second wall
limit. This is a discovery route, not a game-specific acceptance route: it can
choose an unwanted option, miss a delayed logo, or time out after reaching a
menu. Separate case routes test the relevant alternatives. OCR helps find
notices; its output is fallible and decisive screens are inspected visually.

All 465 classified archives received that opening probe: 418 processes exited
with status zero, three with status one, and 44 hit the wall limit. A zero
process status is not a successful game or authentication result. All 44
timeouts received a separate fresh-save, 100-tick-step probe with a 20-second
limit; 32 completed and twelve timed out again. These bounds and every partial
capture remain in the register. They are not silently counted as passed or
classified as authentication failures.

All 109 LGT archives also received the dial-success comparison: 94 processes
exited zero, four exited one, and eleven timed out. Eleven pairs with all five
captures differ in their pixel sequences. This includes animation and failures,
so it is not an eleven-game regression count. The decisive negative control is
the refusal-notice-to-menu transition becoming a connection wait, inspected
in both builds. The new socket-import failure below was reproduced separately.

## Case families

### Failure permits play

**A failed authentication attempt is an intended offline branch.** Several LGT
Clets show a notice saying authentication failed and will be tried next time;
confirming it reaches the title and menu. Current archive `01f05f8231f4`
provides a direct control: ordinary refusal reaches the menu, while the same
keys in the dial-success build leave it displaying a connection-in-progress
notice. `2a8a3dcd07eb` and `3cc7a9b4cb15` show the same direction of change.
The failure's timing and delivery matter as much as its value.

[The earlier LGT investigation](lgt.md#network-authentication-is-a-path-the-titles-already-handle)
traced negative transport results through this state machine and compared
three originals with two-byte-patched copies. The originals already played.
Do not count a patched copy as proof that this emulator needs that patch.

**A certificate offer can be an optional route rather than a certificate
reader.** [The recorded LGT route](network.md#a-certificate-is-the-same-gate-under-another-name-and-it-is-not-a-wall)
chooses Yes, receives a connection failure, dismisses it, and reaches menu and
difficulty selection. The default No exits. Its trace opened five ordinary
files over 1,500 ticks and no certificate file. A screen that mentions a
certificate is insufficient evidence for generating one.

**Declining the request is a separate case.** Some archives offer authentication
before attempting a connection. Another selection reaches their offline path.
Current `dddac9077962` offers a certificate before dialling; accepting it leaves
an authentication notice with `Connect CB Error [-1]`. Granting the dial alone
still leaves the notice on the shared opening route. This must not be merged
with the historical case where granting an initial dial exposes a *later*
question. Record which question came first and which answer was selected.
Selecting No in a separate current run reaches its title; the offer can recur
on a subsequent attempt to start, so reaching the title is not proof of play.
For `393d359d0815`, choosing right then confirm *at the certificate prompt*
reaches the menu and an empty save slot. Sending right earlier during a logo
does not select that option and instead reproduces the authentication wait.
The older route cannot be copied without checking which scene receives its keys.
The shorter targeted routes also decline requests in `a23f3c9fc2cb` and
`468a0d52e618` and reach their titles. These are additional title-access
controls; they do not prove every later start or network menu works.

Two more current controls prevent misclassification. `7f2c396bced5` displays a
user-authentication failure, but another confirm reaches menu and character
selection. Its executable is identical to `3d91c2217bad`'s, although the outer
archives differ. `a2cb4a23f300` says play requires authentication when the
request is declined; explicitly accepting it, then dismissing failure, reaches
the title. The wording is not proof that a server response is mandatory.

### Failure prevents the guest's own cleanup

**An LGT Java save-backup check never dismisses its notice on `-1`.** The
[guest branch analysis](lgt.md#the-gate-and-what-holds-it-open) identifies the
notice flag, its setters, the paint branch, and the sleeping Jlet method.
`Network.connect` failure jumps past the notice teardown. Returning success
instead reaches `URL.find`; the guest catches its `SchemeNotFoundException`,
clears the notice, and reaches the title. The failure belongs at the second
operation for this caller. This is not evidence that a server needs to send
backup data, nor a reason to rewrite the scheduler.

**An LGT Clet has an offline choice behind a successful dial callback.** The
[historical comparison](network.md#one-title-does-not-report-its-own-error-it-parks)
records `Connect CB Error [-1]` holding the authentication screen, then callback
success exposing a certificate question whose No answer reaches play. Other
archives with that log text do not necessarily share its transition graph.
The once-missing socket slot `0x7d0` is now implemented and refused normally;
old results that ended on an unimplemented import must not be copied forward.
Current controls still show that unconditional dial success can lose an
already-working offline branch.

**Current additional ABI prerequisite (`af7d82e5e239`).** The dial-success
experiment calls unimplemented LGT slot `0x25a` from `0xd41b`, with arguments
2 and 1, inside callback `0xd409`. The baseline refuses the dial without that
import failure. A separate pair of fresh-save runs with live network tracing
reproduces it. Those arguments and its place after dial completion are evidence
for the socket-creation operation, but the slot differs from the implemented
`0x7d0`. Identify the applicable interface variant before adding a mapping;
do not assume the old socket prerequisite covers every archive. This is a
concrete implementation prerequisite discovered by this investigation.

The implementation follow-up confirmed the descriptor test and subsequent
address/port/socket-connect sequence. Slot `0x25a` now resolves and refuses normally,
alongside `0x7d0`. A repeated dial-success probe completes and reaches the menu;
the original failed-dial route reaches it too. This repairs the missing API but
does not establish an authentication gate or justify changing this case's dial
policy. See [current evidence](authentication.md#lgt-socket-prerequisite).

### Complete local content, but identity or certificate prevents entry

**KTF number-length branch (`93d5b6b8ceb5`).** The archive has five data files,
plus a 64-byte `prefs` record. The
[earlier trace and number sweep](ktf.md#a-second-download-gate-and-the-answer-that-opened-it)
found that a number of length 0–4 skips the download receipt path; length 5–11
asks to fetch the data again. The files are read and their footers checked only
after that branch. Current separate-save runs reproduce a 600 KB download
request and connection failure with `01000000000`, versus the menu and save-slot
selection with `0100`. This is evidence of an existing offline branch, not of
the `prefs` cipher having been decoded. The implementation follow-up found the
accessor's embedded full-length fallback: input of length at most four is replaced
with that number. Giving the full fallback directly also reaches the menu. The
current adapter resolves that number from the recognized accessor and uses it only
in this session; see [the corrected mechanism](authentication.md#ktf-certificates-and-identity).

**KTF 23-byte certificate (`974e0df9ab1e`).** The archive carries its resource
databases and `cert.c2s`. Current ordinary startup reaches error 3001; using the
existing `provision` command in a separate research save root reaches the title
and scenario menu with the same keys. Provisioning for the full number and
running with a different, short number is a negative control. The
[decoded contract](ktf.md#the-certificate-behind-it) binds application identity
and subscriber number. `internal/platform/ktf/provision.go` implements the
recognized 23-byte structure and its checksum checks. It is currently a CLI
operation, not a session policy.

**Other files named `cert.c2s` are distinct candidates.** Current archives
`500502d493e1` and `f9fdd6476e3a` package 52 and 28 bytes respectively; both are
rejected by provisioning's format check. The 28-byte case reads the certificate
and reaches an item-selection screen on the current opening route, so it is
not an observed startup authentication failure. `2dab850c3aa2` contains a code reference to the
name and no packaged file, yet reaches a menu. Both filename-only recognition
and assuming that every missing certificate blocks play are disproved.

**KTF 52-byte certificate (`500502d493e1`).** This is a second currently
confirmed mandatory certificate mechanism, not merely a filename candidate.
The packaged record reaches error 5001. In isolated saves, an empty record
changes it to 3002 and 52 zero bytes change it to 3004. Disassembly of the
relocated image explains those controls: `0x130f1c` opens and reads 52 bytes,
transforms the first 48 through a guest substitution table, then compares
three fields. Application ID is eight bytes at offset 10; a separate application
token is sixteen bytes at offset 20; subscriber identity is twelve bytes at
offset 36. The caller obtains that last value from `PHONENUMBER`.

The observed return codes are 3001 for open failure, 3002 for a short read,
3004 for application-ID mismatch, 5001 for subscriber mismatch, and 5003 for
the sixteen-byte token mismatch. These numbers belong to this guest contract;
they do not have universal meaning across certificate versions.

Replacing only the encoded subscriber field in a research copy, preserving
every other byte, reaches the title menu with the same commands. This isolates
the current block to the subscriber comparison. The substitution table has 255
distinct byte values rather than being a permutation; the inverse search's
behavior matters. The experiment changes only values whose round trip was
checked and does not establish a production encoder for arbitrary inputs.
The writer also produces four trailing bytes; this startup read path does not
validate them. The diagnostic copy preserved them, so a future codec still
needs writer-format and malformed-input tests. No table or proprietary asset
is added to tracked implementation code by this investigation.

**SKT guest license checks.** The
[historical five failures](skvm.md#five-titles-check-their-licence-against-the-handsets-number)
hash subscriber identity, a service-ID slice from `MIDlet-Jar-URL`, and a
constant before comparing `MIDlet-Key`. Four exit during startup; another
checks after a logo. The earlier instruction trace reached the comparison
without entering the exception handler. Number reconstruction also depends on
`m.CARRIER`, which the current implementation derives from the number.

The current class scan finds license-related strings in 41 distinct SKT
archives, far more than five. That is a candidate count, not a failure count.
Some checks are obfuscated; others have different `SecureUtil` versions or
are embedded in a larger class. Method metadata is retained in
`java-methods.ndjson`. A common variant first tests
`System.getProperty("com.xce.wipi.version")` and returns true when non-null.
Current SKT properties already include that key with an empty string, which
is a non-null Java object. Other validators do not have that entry branch;
class or method names alone cannot identify a behavior. One version even has
a different boolean return convention behind the property test. Do not replace
all `isValid` methods, string comparisons, digest methods, or property results.

Current exit logs identify five startup license refusals: `eefc947d8337`,
`c33090c12755`, `44b6356d13f8`, `a42f77f44955`, and `0ed66634d3dc`.
Their notices and `System.exit(-1)` evidence are retained. The CLI's opening
response can continue reporting ticks after that guest exit, so tick counts
alone would miss this case; read the lifecycle log and frame together.

### Authentication-looking platform and persistence problems

**KTF errors 1001 and 2001/2002 precede the certificate.** The
[startup investigation](ktf.md#one-titles-startup-gate) separates capability
mask, executable listing, and certificate. The current platform grants the API
groups and supplies the identity listing; neither implies usable networking.
A related free-space failure counted packaged data against the save budget and
is already fixed. Similar screen styling does not make these one auth adapter.

**LGT application identity.** Slot `0x97` returns the archive's declared
application-ID string. A caller compared it with its compiled identity before
showing an illegal-download notice. The current answer follows archive metadata;
this was a platform contract correction, not a fabricated authorization.
See [lgt.md](lgt.md#0x97-answers-the-programs-application-id-and-a-titles-copy-check-reads-it).

**Packaged Java database not loaded.** A KTF caller already carried
`P/op_save.idx` and `P/op_save.db`. Treating its Java database as empty sent the
guest to a download prompt. The current loader reads those packaged records;
saved records, including deliberately empty ones, take precedence. Archive
`5aa438fbdc87` still contains this pair. See the
[database investigation](ktf.md#the-java-database-never-looked-at-the-archive-and-that-is-a-download-gate).

**An older saved record causes re-download.** The
[controlled persistence investigation](ktf.md#a-download-prompt-that-one-saved-record-will-not-let-go-of)
found that fresh saves and a second run of newly written saves worked. Copying
one older record reproduced the prompt while the other three records did not.
The data files were opened at their correct sizes. Clock shifts ruled out the
simple timestamp-expiry explanation. The remaining record-key mechanism is
unresolved. Preserve the original save; compare copies and fresh roots instead
of deleting user progress or claiming the download server is the cause.

**Restart, consent and informational screens.** LGT investigations found a
missing `fsRename`, a legitimate request to restart after writing initial data,
and an ordinary press-any-key notice. Another save-failure message occurred
without any filesystem, database or free-space call. Current opening captures
also contain SMS consent, customer-information consent and optional ranking
requests. Separate declining consent, accepting a request, restarting with the
new save, and dismissing a notice. A white frame or a held dialog is not itself
an authentication diagnosis.

**BREW certificate and file-interface status.** Later native packages load a
74-byte `gbxcerti.dat` through their resource loader and pass it to ClassID
`0x103028a`; the current `nativeAnswerCertificate` returns accepted status zero.
Earlier experiments showed status one inviting network authentication. A
separate file-existence result had opposite polarity between runtime
generations; the version-gated implementation now preserves both. Incorrect
file status made a locally present marker look absent. These existing behaviors
are controls, not new work for a WIPI network callback adapter. A native package
still stopping on loading must be investigated at its drawing/resource boundary.
See [ktf.md](ktf.md#a-certificate-and-the-object-both-later-archives-create).

### Content and container prerequisites

**Missing resource containers (`689c491586b9`).** An early investigation inferred
completeness from 857 KB of packaged resources. That inference was disproved:
`5/ZipTable`, `7/ZipTable` and `9/ZipTable` refer to twelve absent containers,
`5/0`–`5/1`, `7/0`–`7/5`, and `9/0`–`9/3`. The current entry inventory confirms
all twelve are absent. Historical number changes, fourteen-byte `Config.dat`
variants and forcing `8/StartMenu` did not supply the missing resources.
[The full investigation](ktf.md#a-third-download-gate-and-the-lever-that-did-not-open-it)
ends at missing content. Aggregate size and absence of host errors were both
insufficient proof that the archive was complete.

**Encrypted payload (`2c53da7f921f`).** Detection identifies a locked KTF inner
container. Guest code cannot run yet, so returning authentication success from
a runtime API cannot help. The prerequisite is executable content and its
container/key contract, separate from an in-game license check.

**Unsupported or incomplete packaging.** The census also has twelve legacy
`.SGS` packages, two unclassified bare content archives, one archive of archives,
and eleven distinct non-ZIP inputs or auxiliary files. These remain cataloged
with detection reasons. They must not inflate the count of runnable games
blocked by authentication. Static strings inside a bundled desktop utility
are not evidence about the game's executed path.

## Consequences for common logic

The common layer should select and describe an evidenced strategy; it cannot
collapse the preceding cases into one boolean result. Record, per session:
identity, recognized certificate/library/protocol variant, execution phase,
expected refusal or success point, and the save changes it owns. Application
identity, transport availability, server response, and authorization are
separate facts. A diagnostic should state which fact was changed.

The [WIPI C contract](https://mirusu400.github.io/wipi-wiki/c-api/network.md)
distinguishes accepting a dial request from the later callback outcome. An
immediate error produces no callback; application close cancels callbacks and
connections. The
[Java contract](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msf/io/Network.md)
defines 0 as existing access, 1 as newly obtained access and -1 as failure.
Neither defines the publisher's license protocol. Specification pages were
fetched directly on the research date.

Before enabling a scheme, its case must have a route beyond the gate and
positive and negative recognition evidence. Preserve refusal-dependent games,
opposing subscriber identities in concurrent sessions, original save recovery,
and ordinary operation when the feature is disabled. When bytes from a server
are truly required, identify the consumed response fields first; a successful
dial with no bounded response can simply move the infinite wait. Missing game
data and encrypted executables remain separate prerequisites.

This inventory supports selecting the first implementation slice. It does not
claim arbitrary authentication is solved, that static matches all execute, or
that an opening probe proves saving and sustained gameplay. The acceptance
requirements for the eventual feature remain in
[authentication.md](authentication.md#validation-scope).

## Boundaries still to prove before enabling an adapter

| Case | What is established | What must not be assumed / next discriminating check |
|---|---|---|
| `9acb5e0387eb` | A longer normal-refusal run writes its own marker after a 20-second guest timeout, reaches town movement, and restarts at the title in release. | Preserve the guest timeout path. The earlier wall limit did not establish an authentication gate; see the follow-up below. |
| `caf9d76ffd13` | Declining authentication offers exit; accepting with normal network refusal leaves the connection notice. Its cached gate reads an encrypted 58-byte record. The production adapter reaches playable input and restores an ordinary early checkpoint. | The complete reader/writer/cipher/subscriber gate is now recognized by the production adapter. Numeric name input, minigame action and a fresh release Continue from the ordinary early checkpoint are verified. |
| `a23f3c9fc2cb` | Accepting the certificate offer reaches a connection notice; a targeted No route reaches the title. Some longer routes time out. | The targeted route now reaches tutorial movement and attack with status `unsupported`; preserve ordinary behavior. |
| `dddac9077962` | Declining once reaches the title but can still leave Game Start waiting. A fresh process and a second decline persist the existing opt-out count and reach actual board movement with normal network refusal. | Preserve that explicit choice. The follow-up below identifies the count reader and writer; no response adapter is needed for the verified offline route. |
| `49ade89578c5`, `580a66c32fff`, `69bf82f53f2d` | Explicit No reaches ordinary KTF/LGT input in the first two cases. The third needs the recognized local notification/empty-save responder. | Both explicit choices retain their guest acknowledgement; the new responder reaches ordinary play and saving. No consent choice is automatic. |
| Restart notices | The initial shared probe did not restart after initialization. Follow-ups now establish the recognized cached-result adapter and career save restoration for `acc4215b7ec0`, and ordinary offline combat for `619d98bc8f64`. Other routes still need individual checks. | Reopen newly written saves and exercise the choices before diagnosing a server dependency. The historical `fsRename` case is not proof every restart notice shares its cause. |
| Historical Java backup and pre-question Clet dial cases | Earlier documents contain guest branch evidence and controlled changes. | Those observations have not been bound to a verified current archive digest in this census. Retain them as distinct mechanisms, without claiming a new replay or using their old corpus counts. |
| Older rejected saved record | Historical copying and clock experiments isolate one record; fresh and newly written saves worked. | The rejected key's derivation is still unknown. Do not replace all save records or presume age expiry. |
| Twelve repeated short-probe timeouts | Eleven have an earlier splash, menu or other rendered scene; `6103e87874c6` times out after `startApp` but before the first requested capture. | A wall limit is an observation limit. These runs do not establish an authentication cause, and no automatic bypass should be selected from them. |

Three baseline process errors are also separate from authentication:
`1b107b96bf4e` reaches an unsupported `BackLight.before` call,
`26f1bf941f2e` an unsupported `Display.removeCard` dispatch, and
`73f3a21e981c` an unsupported `Stack` virtual dispatch. Their original errors
remain in the logs. The successful-dial-only `0x25a` error is different because
that intervention exposes it directly.

## Reproducible local evidence

The report directory contains:

- `files.ndjson` and `end-files.ndjson`: the beginning/end census. All 543 paths
  and SHA-256 values match; no original input changed.
- `inventory.ndjson`: one record per original path, with nested entries and
  static signals. ZIP readers must accept prefixed JARs; checking only whether
  the bytes begin with `PK` misses some SKT class contents. The final inventory
  uses the reader's ZIP check and includes the resulting scan errors.
- `case-register.ndjson`: one record per distinct input digest, joining static
  signals, per-variant process results, captures, OCR, exit and callback signals.
  Its explicit evidence scope prevents a text match being read as a diagnosis.
- `executable-digests.ndjson`, `java-checks.ndjson`, `java-methods.ndjson`:
  executable relationships and Java method/constant-pool observations. An outer
  ZIP difference need not imply a different executable, and an identical
  executable does not imply identical certificates or saves.
- `baseline/`, `dial-success/`, `short-opening/`, and `cases/`: commands, results,
  stdout, stderr and captures. KTF/SKT diagnostics are recorded where supported;
  LGT's expected refusal of `diag` is not a missing authentication API. Specific
  LGT investigations use `-trace-live net` instead.
- `500502d493e1-certificate-disassembly.txt` and the subscriber-mutation metadata:
  the 52-byte format's field comparisons and the single-field control. Raw game
  data and derived images remain ignored, alongside the original archives.
- `metadata.json` and `reproduce/`: executable/source identities, scripts, and
  the exact diagnostic overlay difference. Repeats require a new result and save
  label; existing results are preserved rather than reported as a fresh run.

This is a census and a bounded investigation of available startup cases,
combined with the repository's prior deeper investigations. It does not exhaust
all key sequences, later purchase/ranking/backup menus, absent historical
archives, or every possible saved state. Those limits are part of the evidence,
not implicit authentication successes. The future implementation must expand
its acceptance routes as each recognized scheme is added.


## Cached LGT result follow-up

A fresh process after the ordinary first-run save clears the restart notice in
four previously unresolved LGT cases. This is not itself authentication success:
one then asks separately for optional news consent and a required certificate,
one still waits for remote data, one remains in its opening notices/logos, and
one dismisses a connection error into a new-game menu. Keep those stages separate.
The new routes use copies of the previous private saves and preserve their hashes.

For the certificate case, selecting the actual numeric consent/certificate keys
reaches a socket refusal under both normal failed dial and the successful-dial
probe. Neither dial result supplies the application certificate. Disassembly
connects its 56-byte options reader/writer to a reply handler and startup gate:
reply type 302, result byte at offset 6, cached word at options offset 40. A
controlled one-word sweep changes words 8, 9, 10 and 12 independently; only word 10
clears the certificate request. Declining the subsequent optional remote-save
recovery reaches the title, practice-mode setup and an actual batting screen.
The production adapter and its limits are documented in
[authentication.md](authentication.md#lgt-cached-authentication). The executable
scan selects one of 109 distinct local LGT modules. The production adapter now
also completes career creation, actual batting and ordinary saves. A fresh release
runtime restores the career, cash and roster from a private copy while preserving
the original stored authentication word. This separate production route, rather
than the diagnostic modified-settings route, supplies the save/restart proof.

A longer restart replay for `619d98bc8f64` reaches the title, creates an empty
slot, declines optional custom naming and skips the prologue into a combat field.
Movement and attack input are accepted with normal failing network behavior and
`authentication: unsupported`. The earlier server-data/error captures therefore
do not establish an unavoidable authentication gate. The isolated
`complete-dismiss` route records the full sequence and screenshots.

The current `dddac9077962` replay confirms that declining the initial certificate
reaches its title but Game Start enters authentication again. Normal dial failure
leaves that screen unchanged. An isolated successful-dial replay reaches the
socket refusal and cleanup callback, including its two-second guest-clock delay,
then returns to the same authentication screen. Neither transport result supplies
the application certificate. `complete-decline-play` and `complete-dial-control`
record those routes; the delay completed normally and is not a clock deadlock.

A second fresh run of that same private save changes the diagnosis: declining
again writes `initconfirm.dat` from one to two, after which Game Start reaches
the story, mode selection and an actual board game with the ordinary network
policy. The route selects a map and accepts dice input. The
reader at `0x2a7c4` returns the four-byte count; the two startup gates require a
value greater than one. The declined path reads the count, adds one and calls
the writer at `0x2a80c`; an accepted response uses three. Thus this is an existing
offline opt-out path, not evidence that a response adapter is required. The
`complete-decline-restart` route preserves the original three-file seed.

A read-only filename sweep finds that counter filename in only this one of the
109 distinct LGT modules. It is a case-specific observation, not a generic
rule for every certificate prompt or an automatic save rewrite.

The initialized `b44b5fbe29e6` follow-up also reaches ordinary offline play. After
the startup notices, the player declines information sharing and the optional
saved-data download. Game Start then offers empty slots and character selection.
The opening story continues through several scenes before handing over town
movement and attack. The `complete-offline-play` route records that handover and
the visible response to both controls. It uses the production debug binary,
normal failing network behavior and a private copy of the initialized save;
the authentication status remains `unsupported`. Neither the long story nor the
optional download is evidence that this route requires an authentication adapter.

For `9acb5e0387eb`, `complete-offline-check` declines information sharing with
Right/Fire, acknowledges the first-authentication notice with Clear, and retains
normal dial failure. A subsequent 1,200-tick batch completes in 46.039 wall seconds
and reaches the title. The guest timeout branch at `0x3afda` waits more than
20,000 guest milliseconds, then `0x3aff8..0x3b014` writes a four-byte value of one
to `Certification.dat`. Startup at `0x3aa68..0x3aa76` checks that file's existence.
The route continues through the story, declines the tutorial, obtains its first
quest and accepts town movement. A fresh release process in
`complete-offline-restart` reaches the title with the persisted marker. Its
four-file source seed remains unchanged. No response or file adaptation is used.

The `caf9d76ffd13` probe identifies a different cached format. `audio.adt` stores
58 bytes XORed with successive high bytes of the 32-bit recurrence
`state = state * 0x343fd + 0x269ec3`, initially `0x21c3`. The decoded layout holds
40 opaque bytes, a 12-byte subscriber field, a four-byte attempt field and two
trailing bytes. The writer at `0x164cc` and reader at `0x165b0` both use the cipher
at `0x1649c`. The gate at `0x16674` first requires a readable record; it accepts
a signed first attempt byte greater than nine, or an exact 12-byte match with
the current `PHONENUMBER`. That counter shortcut is observed guest behavior, not a
general authentication rule. The response branch for type `0x103` consumes
40 opaque bytes and a mode byte before calling the same writer with the current
subscriber field.

Normal dial failure reaches `0x196f8`, closes the connection, and invokes the
listener's empty failure callback. It does not supply the cached record. A
separate private probe constructs a zero-filled opaque field and attempt field
with the configured subscriber number, encrypts the 58-byte record, and reaches
the title and game menu using unchanged production code. This does not yet prove
ordinary save restoration or justify a broad file-name-only adapter. Evidence
and the constructed save stay in `complete-certificate58-probe`; original
archives and user saves are untouched.

The constructed-record route exposed a mandatory name input defect. An input
trace confirms that the widget selects numeric mode 3 and calls `imHandleInput`
with digits and event type 502; the old implementation always returned empty
buffers. Numeric completion now enters a four-digit name, passes confirmation,
and reaches the town route after explicitly declining the optional tutorial.
English and Hangul composition remain separate work. The guest rejects a
short two-byte name using its own minimum-length check.

The production adapter now matches the full connected reader, writer, cipher
and subscriber gate, preserving the original certificate and both ledger
memberships. Its property string is `PHONENUMBER`, not `MIN`; both platform
identity APIs use the same session snapshot. The local 109-module scan selects
only this case. Mutation, relocation, truncation, executed cipher, guest file,
identity, both-profile and Chromium/WebKit checks pass. The production run `complete-adapter58-confirmed` accepts the numeric name,
reaches the minigame and visibly responds to numeric 2. Its ordinary progress
records are 345 and 142 bytes. `complete-adapter58-release` loads the saved
day-150 checkpoint through Continue without re-entering the name; all four
checkpoint file hashes match before further play. Neither save tree contains
the session certificate. Later minigame completion and campaign progress are
not part of this early-checkpoint evidence.

## Additional offline control routes

`a23f3c9fc2cb` now extends its initialized, targeted title-access route through
New Game, an empty slot, the opening events and tutorial dialogue. Numeric 6
moves the character and scrolls the field; numeric 5 visibly attacks. The current
production build reports `unsupported` throughout. Evidence is in
`complete-offline-final`, particularly `tutorial-move` and `tutorial-action`.
No new authentication adapter is needed for that observed offline route.

Before the local responder, `69bf82f53f2d` with the default highlighted No choice produces a normal network
refusal and the guest's forced-exit error notice. Reopening the resulting save
and selecting Yes produces the same error. The screenshot named
`complete-consent-restart/decline-selected` actually shows Yes selected after
Right; its name is not evidence of a second No attempt. Neither choice establishes
a usable offline route. The first run leaves only a deletion ledger; no consent
response is synthesized and no consent selection is automatic. The following tracing identifies its consumed response fields and exit condition.

Further static tracing identifies the first exchange as an `SMSAGREE` notification,
not a certificate comparison. Both explicit choices set request kind 2 and send
the chosen value; response byte 2 or 3 completes that request. The guest then
writes a one-byte `AGREE` record in its own record container. Startup tests only
whether that record can be read, not the stored byte. Normal connection refusal
sets message state 110; dismissing it exits while startup remains in state 5 or
6. The deletion ledger contains only `debug`, not a deleted authentication file.

An isolated diagnostic container with a zero-valued `AGREE` record confirms this
boundary: startup passes the notification screen and reaches a separate saved-data
lookup. That lookup also refuses its connection normally; its error acknowledgement
exits the guest. Evidence is `complete-consent-marker-control`, with the synthetic
receipt explicitly recorded in `lgt-consent/marker-control.json` under the local
investigation build directory. This is not an ordinary save, evidence of consent,
a production adapter, or a completed play route. Handling the notification alone
would still leave the saved-data response contract unresolved at that stage.

The private local-socket probe then receives the literal `IS_SAVEDATA_EXIST`
request after notification restart. An eight-byte empty-result response advances
the guest to `FINISH_SAVEDATA`; refusing this final command leaves a data-management
error. Returning its zero-status completion instead reaches the no-remote-save
notice, title and ordinary menu. The finish receiver also consumes byte 2 as a
variable-field length, so production supplies all three header bytes. The
notification protocol preserves the caller's explicit N/Y value; reply 2 produces
the guest's refusal acknowledgement and reply 3 its acceptance acknowledgement.

Production recognition now selects this case alone in 109 LGT modules.
`complete-notification-debug-no` and `complete-notification-release-yes` each
start from empty saves and write the guest's own 656-byte record container after
the explicit choice. Both guest acknowledgements request restart.
`complete-notification-debug-play` restarts the actual No receipt, completes the
empty-save lookup and starts a new character. It reaches the opening room,
moves with numeric 6, opens System through Clear and the direction keys, and
saves through the guest menu. The ordinary container grows to 10,128 bytes;
the 5-byte deletion ledger and 7-byte creation ledger accompany it. No emulator
code writes a fabricated `AGREE` record into the backing store.

`complete-notification-release-restore` opens that populated level-one slot and
restores the opening room. All three save-file hashes match immediately after
restoration, and the debug source checkpoint remains unchanged. Numeric movement
and attack still work after restoration. The original archive hash also matches.
`complete/notification-restore-result.json` records these comparisons. This is an
early ordinary checkpoint, not a campaign-completion or quick-save claim.
`complete-notification-release-off` confirms that disabling compatibility still
reaches the original connection-refusal notice from an empty save.

The four required validation gates pass. Chromium and WebKit both exercise the
real Go server and page with applied status, retained resume, a new off start
and an unsupported control. Reports are `complete/checks-notification.json`
and `complete-browser/<engine>/69bf82f53f2d/result.json`. The separate
`complete-local-*` runs remain diagnostic evidence, not production acceptance.

`b90d4f003201/complete-offline-final` extends the initialized restart route
through default party names and its opening scenes. It reaches a battle,
accepts numeric movement and attacks with visible damage. The production status
remains `unsupported`. This is another normal offline control, not a newly
recognized authentication format.

`49ade89578c5/complete-consent-check` starts from fresh saves, explicitly
chooses No for SMS reception, and reaches the title, an empty slot and character
creation. After skipping the opening dialogue it reaches the first quest and
outpost; numeric movement and action continue there with status `unsupported`.
No SMS or authentication response is synthesized. The SMS preference is an
ordinary guest choice, not a certificate mechanism.

`580a66c32fff/complete-consent-check` explicitly declines customer-information
sharing and reaches New Game and the opening tutorial with status `unsupported`.
The menu lesson is a skippable presentation: its visible `#Skip` advances the
explanation. After the remaining explanation pages, Clear opens the ordinary
menu, numeric 3 changes Character to Keyword, and Fire opens the keyword panel.
The screenshots `continue-after-menu-lesson`, `continue-ordinary-menu`,
`continue-menu-selection` and `continue-menu-open` record that interaction at
7,820–7,915 ticks. Earlier soft-key trials were still inside the presentation;
they did not establish an input defect. This offline control now reaches normal
menu input without a transport or consent response being synthesized.


## Final SKT checkpoint acceptance

`44b6356d13f8` completes its training through normal keypad input: aiming,
shooting, kills on both sides, crouching and standing, weapon pickup, two kills
without taking damage, reloading and a headshot. A private read-only diagnostics
overlay distinguishes the script goal, health, weapon and target positions while
a continuous input driver avoids spending the guest's timer on manual inspection.
The no-hit goal advances with health 100. Death animation must finish before
some subsequent tutorial goals advance; a shot alone is not their completion.

The guest's training completion writes ordinary `fs/save.dat` (80 bytes),
`fs/install.dat` (12 bytes) and `fs/option.dat` (6 bytes). A fresh unmodified
production release process loads a private copy with Continue and restores the
first-mission selection instead of the training scenario. The checkpoint frame
digest matches the debug run, and all three file hashes match immediately after
restore. Selecting the mission and difficulty reaches the next briefing.
Evidence is `complete-training-diagnostics`, `complete-training-release` and
`complete/skt-final-restore-result.json`. This proves the ordinary checkpoint
route, not an emulator snapshot or completion of the following mission.

## Existing empty certificate and automatic defaults

Case `caf9d76ffd13` exposed an existing-save regression: a failed connection had
left an empty `fs/audio.adt`, which the 58-byte adapter rejected. The private
`default-user-before` save copy reproduces the connection wait. The same input
route reaches the game menu in `default-user-debug` and `default-user-release`
after the fix, with no authentication flag. The original empty file, creation
ledger and options are unchanged; hashes also match in both new private copies.
These directories are under `var/acceptance/auth/2026-09-11/cases/caf9d76ffd13`.

Browser runs use copies of that existing save and an obsolete false local
preference. Both Chromium and WebKit select `lgt-certificate-58` automatically,
show no checkbox, retain the result after reconnect, apply it on a fresh start,
and report `unsupported` for the authored control. The extended Chromium run
passes all assertions and records the game menu in
`var/acceptance/auth/2026-09-11/default-browser-final/chromium/caf9d76ffd13`.
The server test also accepts and ignores an older page's false policy field.

The longer WebKit run reaches the same menu and passes those route assertions,
but does not pass the assertion that no page errors occurred. A quiet rerun
records twelve Blob access-control errors from `createImageBitmap` in
`web/session.js`, with matching frame-decode warnings. Its evidence is
`var/acceptance/auth/2026-09-11/default-browser-rerun/webkit/caf9d76ffd13/observations.json`.
The shorter WebKit run passed without errors. The frame decoder was unchanged
by this fix; the longer run's error cause and affected lifecycle stage remain
unresolved, so it is not recorded as a fully passing browser check.

The required `make test`, `make test-debug`, `go test -race ./internal/...` and
`go vet ./...` checks pass. Debug and release CLI/server binaries are rebuilt.
