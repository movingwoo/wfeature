# KTF dialogue clipping and deferred text input

## Scope and evidence

The reported failures concern two local archives, identified without titles:

| Case | Archive SHA-256 | Reported path |
| --- | --- | --- |
| A | `016d344f7c328e6713c3e553555069472e82ecae9123eb25dc304bc6d1e2c890` | Dialogue borders and a four-character Korean name field |
| B | `c36cbe78011f1a794341e4a250aeb1a2d23466c0b9dfdb68e802aa5ee96a5a7e` | A five-character Korean name field |

The newer report for case B supersedes the initial authentication diagnosis.
Both archives reach their name editor. The failure occurs when the Host submits
text, before the guest has processed its queued key. Authentication changes are
not needed for this path.

The 2026-09-23 debug logs and isolated-save replays establish the failures. The
[WIPI graphics and input-method specification](https://mirusu400.github.io/wipi-wiki/c-api/graphics.md)
defines clip coordinates and completed/composing input buffers. The graphics
record is opaque in that specification; the vendor layout and deferred key
delivery below come from the actual callers.

## Graphics correction

Case A's SDK helper at `0x1017d8` examines the leading graphics-context word
before reading the clip through `GetContext`. Zero selects the full framebuffer.
`SetContext` previously stored the four clip coordinates without setting this
word. Nested drawing consequently restored full bounds and painted past the
dialogue's right and bottom edges.

Setting a clip now sets the leading flag; a null clip clears it. An invalid
source pointer leaves the context unchanged. Explicitly empty intersections
must also draw nothing: treating every empty rectangle as an absent clip let
widget border fragments escape the name editor. The renderer now distinguishes
an enabled empty clip from a disabled clip. Nonempty bounds written directly by
a caller remain supported.

The reproduced dialogue now stays within its requested horizontal bounds
`[34, 206)`, and the name editor no longer draws stray border fragments.
This is evidence for those widgets, not a whole-game rendering certification.

## Input correction

Both cards queue their numeric-key notification and consume it in a later C
timer callback through `MC_imHandleInput`. The Host previously required that
call during `keyNotify` itself. It returned `ErrTextInputChanged`, discarded the
pending completed character, and disabled input availability. The timer then
received a carrier key with no completed text.

The input adapter records the C timer that activates or handles the editor. A
commit waits for that timer's deadline before injecting each complete EUC-KR
character, then runs the ordinary timer scheduler under the existing guest
execution lock. A manual probe clock advances to the deadline; an interactive
Host waits cancellably. A missing timer or a deadline more than one second away
rejects delivery before queuing a key. The existing 64-character submission
limit, encoding checks, and nested Host service budget remain in force.

Card, focus, mode and revision checks also run when the timer consumes text.
Canceled queued carriers are discarded, and an accepted prefix invalidates
the old edit token. A new synchronous editor clears the previous timer owner.
Generic guest event loops remain unsupported; this change handles the observed
C timer path whose consumption can be checked before returning.

CLR needs the same deferred treatment. Its timer may only flush composition,
which does not close the field. The adapter recognizes that flush only for the
pending CLR, its owning timer and the same visible card. A key that dismisses
the editor without further input-method activity still disables availability.

The guest retains its cursor, value, confirmation and length policy. In
particular, case A rejects a five-character name on confirmation and accepts
four; case B accepts five. These limits are not encoded in the emulator.

## Validation and limits

Authored regressions cover SDK clip readback, invalid clip pointers, explicitly
empty clipping for blits and rectangle fills, five-character timer delivery
with manual and wall clocks, CLR and replacement input, stale snapshots,
dismissal, mode/card changes, removed or distant timers, cancellation, queued
carrier cleanup, and replacement of the timer owner. The timer fixture uses
the platform's ARM/SVC callback path; no JVM interpreter behavior changed.

Local debug and release probes use the real archives with isolated saves under
`var/savedata/ktf-qa-20260923/`. Screenshots, diagnostics and command transcripts
are retained under ignored `var/diagnostics/ktf-qa-20260923/`. The route inserts
Korean text, rejects reuse of the consumed edit, deletes with CLR, reopens the
Host editor, inserts a replacement and confirms through the guest's normal key.
Both cases continue into the next story scene displaying the submitted name.
Case A was checked in debug and case B in release. No game files or original
saves were modified.

`make test` (including all 227 Node tests), `make test-debug`,
`go test -race ./internal/...`, and `go vet ./...` pass. Both server profiles
were rebuilt with `make server server-release`.

Physical iPhone keyboard interaction and saved-name restoration are outside
this replay. The platform text provider is shared by the CLI and browser Host;
the session protocol and page implementation are unchanged.
