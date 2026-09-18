# Native text input

The browser composes text using the phone or PC keyboard and IME. The user
selects an input field in the game, opens **Opts → 문자 입력**, and presses
**입력** to submit the completed string. Intermediate IME composition is local
to the browser. The emulator does not implement a separate handset Hangul
layout; the existing game keypad remains available for game controls.

## Boundary

The session requests a platform snapshot of the active field. The snapshot carries
its text, length limit, multiline/password flags, keyboard hint, and a commit
closure. The platform must validate the active target and original contents again
before changing guest state. A focus or value change rejects the edit rather than
redirecting it to another field. Platform constraints and notifications remain
platform responsibilities; a browser keyboard hint does not validate text.

The WebSocket `text` message uses `open`, `commit`, and `cancel` actions. A successful
open returns `textInput` in the normal `result` envelope. Commit and cancel carry
the returned `edit` identifier; commit also carries `text`. The server binds each
pending edit to its game instance and clears it when parking or stopping the game.
Committed strings are limited to 64 KiB of valid UTF-8, independently of guest field
limits. Constraint validation preserves the pending edit for correction. A stale
edit is invalidated permanently; the browser retains its draft until the user
closes it. A guest exit during a change callback or the held-input release
performed before opening an edit ends the session and settles the outstanding
request. Password
fields use a password input and are cleared when the dialog closes.

An append snapshot is used when a platform can pass completed text to the active
guest cursor but cannot read or replace the existing value. The response carries
`append: true`, starts with an empty Host field, and labels the operation as an
insertion. The same stale-edit and transport rules apply.

Custom game-owned input interfaces are supported only where the platform exposes
a proven active target. SKT's `TextComponentHandler` identifies the title-owned
component and exposes its size, cursor, limit, constraints, and character insertion,
but no value getter. The Host therefore appends the completed string at the guest
cursor and rejects commits after component, keypad, cursor, size, limit, or constraint
changes. This character-at-a-time route accepts BMP text, including Korean, Latin,
digits, and common symbols; it rejects supplementary characters that cannot survive
separate Java `char` callbacks. LGT WIPI-C is the other bounded route: after a native widget selects or
calls the platform input method, a Host commit enters its Clet with one character-key
carrier. The widget's
normal `MC_imHandleInput` call receives the complete EUC-KR string in its
completion buffer. The carrier is substituted inside the platform and is not
composed as a keypad character. If the current Clet route no longer reaches the
input method, the edit is rejected as stale.

## Supported editors

KTF supports active LWC TextField and TextBox instances, including guest subclasses.
The adapter preserves field constraints and stores the complete value using the
component string-setting contract. Fields with an explicitly installed
InputMethodListener remain unsupported: its per-key composition deltas cannot
safely describe an arbitrary whole-field replacement.
KTF also supports a bounded non-modal KFC route: an explicitly shown GForm with
exactly one directly attached GTextField that has an EventListener. The Host
changes only that field's value. The guest listener receives keypad events and
owns acceptance and dismissal. Ambiguous or changed form contents are rejected,
and the synchronous KFC `doModal` path remains unsupported. SKT supports TextBox,
the selected Form TextField, a focused XTextField on the visible canvas, and the
title-owned TextComponent attached to its input-method handler. The last route is
append-only because the component interface cannot return its existing value.
LGT supports a focused LWC TextField or TextBox whose parent chain reaches a
shown Shell. Shell visibility and focus changes invalidate pending edits; field
and listener revisions also reject guest changes that restore an earlier value.
Application lifecycle callbacks are dispatched by their exact method signature,
and shown/focused widget graphs remain reachable by the Java collector. As with
KTF, an installed InputMethodListener is unsupported. This adds text editing,
not a complete LWC renderer.

LGT also supports game-owned WIPI-C widgets that use `MC_imHandleInput`. This
path appends at the guest cursor because WIPI-C exposes neither the field value
nor a stable component identity. It accepts up to 64 completed characters per
submission, rejects controls and text outside strict EUC-KR, and accepts only
ASCII digits while the widget has selected `N123`. The widget still enforces
its own smaller field and completion-buffer limits; an over-capacity submission
changes nothing and can be retried.

Limits count Java UTF-16 units; supplementary characters consume two units. Native
entry does not infer fields drawn by game code.

## Validation

Go tests exercise adapter constraints, stale fields, UTF-16 limits, notifications,
concurrent SKT editing/rendering, and an authored MIDlet packaged as a JAR. An actual
WebSocket test submits Korean text and a supplementary character, rejects an
oversized edit, parks and resumes the game, rejects the old edit token, and reads
back the retained guest value.

Chromium and WebKit both pass an end-to-end browser probe against the Go server and
that MIDlet at a 390 by 844 mobile viewport. It verifies composition isolation,
complete-string guest mutation, page reload/resume, stale-target rejection with
draft preservation, guest length limits, and absence of leaked game-key events or
page errors. Artifacts are in the ignored
`var/diagnostics/native-text-e2e-20260912` directory. Composition events are driven
by automation; physical-phone keyboard and IME interaction still needs a person.

A real KTF archive also passes the shared session and browser paths for its
non-modal GForm editor. A complete Korean composition updates the same
GTextField without hiding the form. The guest's FIRE listener then reads the
value, stores it, and hides the form; reopening the editor returns the accepted
string. Leaving settings with CLR also persists the name across a new runtime
session using the same save directory. Chromium and WebKit retain the game keypad and report no page errors.
Review artifacts are in the ignored
`var/diagnostics/ktf-native-browser-20260912` directory.

An authored LGT AOT archive also boots through the shared session and actual
WebSocket. Its initializer resolves the Java tables, allocates a shell and text
field, and invokes construction, attachment, visibility, length, and focus calls.
The tests reject an over-limit string, retry the same edit with Korean and a
supplementary character, and reopen the field to read the stored value. Chromium
and WebKit both pass composition isolation, game-key isolation, reload/resume,
length rejection/retry, and guest readback with this archive. Review artifacts
are in `var/diagnostics/lgt-text-e2e-20260912`. The fixture does not draw a complete
widget screen or provide a guest key route that changes focus; those lifecycle
transitions remain covered by separate runtime regressions.

An authored WIPI-C Clet additionally drives a Host composition through its real
`handleCletEvent` entry point and `MC_imHandleInput` import. Tests cover Korean,
English and digits in one completed EUC-KR string, numeric constraints, strict
encoding, stale mode snapshots, output byte lengths, atomic capacity rejection,
and retry.

The integrated normal and debug suites, internal race checks, and `go vet ./...`
pass. Both CLI and embedded-server binaries build in debug and release profiles.


The following dated investigation records how these boundaries were established.
Earlier statements about unsupported targets are superseded by the support table
and validation above, and by later observations within the record.


<a id="lifecycle-widget-input-lifecycle-follow-up"></a>

## Widget lifecycle evidence (2026-09-12)

The native keyboard transport can edit a platform field only after the runtime
identifies an active target. A text component allocated during startup is not
necessarily an editor the guest is currently using. This investigation separates
that distinction from the mechanics of sending a complete string.

<a id="lifecycle-ktf-vendor-forms"></a>

### KTF vendor forms

A bounded recursive scan of the 543 current library files found two distinct
archive hashes containing `com/ktf/kfc/GTextField` and `GMenubarForm`:

- `f07cbc7828280b886fdef164b7a1fe16fff107ea60e4086a4167b61a551aef87`
- `bfa8ec3524518147e1383ecb44560fb3b9fba72fdc3aa30e4c2fdc035e7cbfa9`

The marker scan is static evidence, not a method-call count. Three distinct
archives contain `GForm`, while 17 contain the unqualified `doModal` name; that
name also belongs to other dialog classes and does not identify a KFC caller.

A fresh 400-tick run of the first archive constructs one form and one text field,
sets the maximum length, obtains the default listener, and supplies IME modes.
The recorded call sites are `0x119bb5` through `0x119db9`. A 1,200-tick route with
nine FIRE inputs advances beyond the opening notice into the story. Extending
that route to 2,500 ticks with additional soft-key and confirmation input still
shows story dialogue. Neither route resolves or calls `doModal`. The second
archive's sampled run loads `GMenubarForm` without constructing a text field.
These observations establish startup allocation, not an active editor.

The manager's ignored `var/diagnostics/widget-lifecycle-20260912` directory
contains the hash/path map, per-route JSON diagnostics, logs, and frames. Each
route uses its own save directory under `var/savedata/widget-lifecycle-20260912`.
The library and original saves are unchanged.

<a id="lifecycle-suspension-boundary"></a>

### Suspension boundary

`runtimeGFormDoModal` currently returns zero immediately and records that no
dialog was shown. Its text-field constructor stores contents and a maximum but
does not retain a parent form. Merely exposing that allocated object through the
Host would edit an inactive field and still leave the guest's modal call closed.

The execution context matters before changing this behavior. A KTF guest worker
can park with its nested call stack intact, as `sleepCurrentWorker` already does.
A client-thread callback cannot: the Host is synchronously inside that callback,
and `client.run` protects the guest state. Blocking there would prevent the
serialized Host loop from processing the text request needed to unblock it.
A modal implementation must first establish whether the actual caller is a
worker or a client-thread callback, and then preserve dismissal, cancellation,
and shutdown behavior for that context. The sampled routes have not yet reached
the modal call, so they cannot select that design.

This is an implementation and caller-validation task. The presence of the two
archives means it must not be classified as an unavailable-archive blocker.

<a id="lifecycle-lgt-ownership-requirement"></a>

### LGT ownership requirement

An LGT adapter needs the focused text component and its visible shell, along with
normal field constraints and notification behavior. Any new shown-shell or
focus handle must also be a Java collection root; Go-side storage of a numeric
guest handle alone does not preserve the guest object. Parent/child edges must
remain traversable, and hiding, removing, or refocusing a component must
invalidate its pending Host edit. The LGT adapter now records this ownership, rejects stale edits, and traces the
shown/focused graphs. Its authored lifecycle regressions cover callback reentry,
exact inherited signatures, and parent-cycle rejection. The sampled archive
routes above still do not establish a real Java text-entry route.

<a id="lifecycle-whole-field-replacement-and-ime-callbacks"></a>

### Whole-field replacement and IME callbacks

The WIPI [InputMethodHandler contract](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lcdui/InputMethodHandler.md)
describes a per-key automaton. Its
[listener](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lcdui/InputMethodListener.md)
receives processed characters and an insertion, replacement, or deletion mode.
The handler has no initial-field-text or replacement-range argument. A Host
that composes a complete string therefore cannot assume that mode zero means
replace the entire existing field. In particular, a listener editing its current
composition fragment could apply the replacement a second time after the Host
has already stored the complete value.

Whole-field input needs the component's string-setting contract. It must not
synthesize per-key automaton events with the complete string. An explicitly
installed application listener needs a separately established contract; refusing
that unsupported target is a limitation, not evidence that custom listeners work.
The KTF and LGT adapters follow this distinction: they refuse installed delta
listeners and use the component string-setting contract for supported fields.

<a id="lifecycle-separate-native-input-method-callers"></a>

### Separate native input-method callers

The historical LGT authentication route that accepts a numeric name is a
WIPI-C, game-owned widget. It calls `MC_imHandleInput` with completed and
composing buffers, then owns its text, deletion, mode, and drawing. The existing
numeric implementation returns completed digits; that route does not expose a
Java LWC object, parent shell, or focus handle. It cannot validate a Java editor
adapter. Conversely, the historical Java `TextComponent.imHandler` initialization
failure remains evidence of Java widget demand even when newer bounded routes
do not reach the input screen. These are distinct input contracts; see the
[input-method and widget history](lgt.md).

<a id="lifecycle-a-response-dependent-lgt-editor-route"></a>

### A response-dependent LGT editor route

Static analysis of local archive SHA-256
`735a579d82ac53bb205b04250ce44586c6d9375e64c2d6f3324796b3ae24d031`
identifies a Java editor branch selected by response value 1101. The value is
read from a connection's input stream. The connection factory call uses static
import 46 (`0xb8 / 4`) from the module's static-method table; the recorded class
metadata resolves that import to `org/kwis/msf/io/URL.find(String)`. Its address
constant uses the `BillSocket` scheme. The endpoint is not needed for this
finding and is omitted.

The current runtime implements that factory with `javaURLFind`, which throws
`SchemeNotFoundException` instead of opening a connection. This particular
response-dependent branch therefore cannot establish real editor acceptance
under the current offline runtime. Focused socket-refusal and exception-inheritance
tests pass. This is static reachability evidence, not a recorded editor execution,
and it does not rule out other entry paths in the same archive. A synthetic
response would test a different acceptance path and must not be counted as real
service behavior. See [network limitations](network.md).

<a id="lifecycle-ktf-text-field-virtual-dispatch"></a>

#### KTF text-field virtual dispatch

A KTF archive calls `TextFieldComponent.setString(String)` with a `GTextField`
receiver when opening the name editor from its settings menu. `GTextField` must
inherit `TextFieldComponent`: inheriting directly from `TextComponent` placed a
constructor in the virtual slot resolved by that caller. The call then failed
while decoding constructor arguments, before the editor could open.

The corrected hierarchy preserves the parent method slot. A regression test
checks the actual generated receiver table against the parent declaration. A
fresh-save replay of the settings route reaches `GTextField.setString` without
the previous argument-decoding failure. The captured display remains on the
settings menu; editor visibility, host text submission and persistence are still
separate acceptance requirements. General, debug, race and vet checks pass.

<a id="lifecycle-ktf-non-modal-form-visibility"></a>

#### KTF non-modal form visibility

The same settings route invokes `GForm.show()` after setting the field text.
The captured route attaches one GTextField through
`ContainerComponent.addComponent`, installs its EventListener, and invokes
`GForm.show()` after setting the field text. It makes no focus call. The WIPI
[FormComponent contract](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lwc/FormComponent.md)
describes explicit child focus traversal, so the runtime does not infer a
general rule that every shown field is active. It selects a Host target only
for this observed vendor shape: an explicitly shown form with exactly one
directly attached, listened GTextField. A missing listener, multiple eligible
fields, removal, or a later lifecycle change prevents or invalidates editing.

The field's real EventListener receives `eventNotify` with the WIPI KEY_NOTIFY
tuple. On FIRE it reads `GTextField.getString`, copies that value into guest
state, and calls `GForm.hide`; it returns false. Host composition therefore
changes only the field value and leaves the form shown. The form owns each
key press through its matching release even if the callback hides or replaces
the form. A release whose press belonged to the underlying card is kept there,
so the release that follows the opening FIRE cannot immediately dismiss the
new form.

Authored tests cover ambiguous and null children, listener and child changes,
value restoration, hide and re-show generations, key ownership, and replacement
forms. A fresh-save session replay accepts a complete Korean string through the
guest FIRE callback and reads it back after reopening. Chromium and WebKit pass
the same whole-string composition and keypad-isolation route without page
errors. This support does not add a complete KFC renderer or change the
synchronous `doModal` stub.

<a id="lifecycle-ktf-name-persistence-acceptance"></a>

#### KTF name persistence acceptance

Closing the runtime while still inside settings does not persist the newly
accepted name. The real archive writes its settings when the user leaves that
menu with CLR. The acceptance probe commits Korean text, confirms with FIRE,
leaves settings with CLR, closes the session, and starts a new session against
the same isolated save directory. Reopening the name field then returns the
previously accepted text. This verifies guest-owned persistence, rather than
only reopening an editor in the same runtime. The final normal, debug, internal
race and vet checks pass.

A key press also captures the selected field, child-list revision and event
listener state. Replacing the listener during its press callback consumes the
old press's trailing release without delivering it to the replacement listener.
The same form and visibility generation alone are not sufficient ownership.
A regression reproduces this callback replacement before the correction.

Release ownership is cleared before invoking the guest release callback. If
that callback hides the form and throws, the next card press is still delivered
normally; an error must not leave the previous press captured. A regression
covers this callback failure.

<a id="lifecycle-remaining-synchronous-modal-references"></a>

### Remaining synchronous modal references

The first archive's static helper at `0x1136cc` constructs a GMsgBox from two
strings and fixed bounds, calls `doModal` at `0x113716`, discards its return
value, and returns. It does not construct or read a GTextField. In the saved
image its body pointer occurs in its method record, and that record is listed
in its class method table; the bounded scan found no ordinary direct caller
or separate symbolic reference. This does not exclude external/native calls.

The other decoded modal path constructs a label-based DialogComponent. The
second archive carrying GTextField has no literal `doModal` reference. These
observations do not establish a reachable synchronous text-entry route. The
proven settings editor uses non-modal show and a guest EventListener instead.

A future modal investigation needs a concrete caller or external callback to
exercise, with a PC-hit trace at the helper entry. Directly invoking the helper
would verify modal mechanics but would not prove user-visible reachability.
Synchronous modal support remains unimplemented; current evidence does not
make it a prerequisite for the verified name-entry path.
