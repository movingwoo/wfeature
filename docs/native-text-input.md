# Native text input

The browser composes text using the phone or PC keyboard and IME. The user opens
**문자 입력** after selecting a supported input field in the game, edits the
contents, and presses **입력** to submit the complete string. Intermediate IME
composition is local to the browser. The existing game keypad remains available.

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

Custom game-owned input interfaces are not automatically discoverable. A platform
without a proven active field reports that no supported field is active. In
particular, WIPI-C UIC lacks component creation, and game-owned native widgets
do not expose a field identity. Supporting those paths requires their guest UI
lifecycle first.

## Supported editors

KTF supports active LWC TextField and TextBox instances, including guest subclasses.
The adapter preserves field constraints and stores the complete value using the
component string-setting contract. Fields with an explicitly installed
InputMethodListener remain unsupported: its per-key composition deltas cannot
safely describe an arbitrary whole-field replacement.
KFC modal editors do not expose a live target and remain unsupported. SKT supports
TextBox, the selected Form TextField, and a focused XTextField on the visible canvas.
LGT supports a focused LWC TextField or TextBox whose parent chain reaches a
shown Shell. Shell visibility and focus changes invalidate pending edits; field
and listener revisions also reject guest changes that restore an earlier value.
Application lifecycle callbacks are dispatched by their exact method signature,
and shown/focused widget graphs remain reachable by the Java collector. As with
KTF, an installed InputMethodListener is unsupported. This adds text editing,
not a complete LWC renderer.

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

The integrated normal and debug suites, internal race checks, and `go vet ./...`
pass. Both CLI and embedded-server binaries build in debug and release profiles.
