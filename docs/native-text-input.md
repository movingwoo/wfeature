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
