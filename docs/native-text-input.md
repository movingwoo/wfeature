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
particular, the current LGT widget implementation does not retain the focus and
notification state needed for a safe adapter; WIPI-C UIC also lacks component
creation. Supporting those paths requires their guest UI lifecycle first.

## Supported editors

KTF supports active LWC TextField and TextBox instances, including guest subclasses.
The adapter preserves field constraints and stores the complete value using the
component string-setting contract. Fields with an explicitly installed
InputMethodListener remain unsupported: its per-key composition deltas cannot
safely describe an arbitrary whole-field replacement.
KFC modal editors do not expose a live target and remain unsupported. SKT supports
TextBox, the selected Form TextField, and a focused XTextField on the visible canvas.
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

The integrated normal and debug suites, internal race checks, and `go vet ./...`
pass. Both CLI and embedded-server binaries build in debug and release profiles.
