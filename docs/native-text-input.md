# Native text input

The browser composes text using the phone or PC keyboard and IME. For a Java
field, the user opens **문자 입력** after selecting the field in the game. An SGS
native dialog opens automatically when the script requests it. The user edits
the contents and presses **입력** to submit the complete string; cancellation is
reported where the platform contract requires it. Intermediate IME composition
is local to the browser. The existing game keypad remains available.

## Boundary

The session requests a platform snapshot of the active field. The snapshot carries
its text, optional prompt, character or guest-encoded byte limit,
multiline/password flags, keyboard hint, and commit plus optional cancel
closures. The platform must validate the active target and original contents again
before changing guest state. A focus or value change rejects the edit rather than
redirecting it to another field. Platform constraints and notifications remain
platform responsibilities; a browser keyboard hint does not validate text.

The WebSocket `text` message uses `open`, `commit`, and `cancel` actions. A successful
open returns `textInput` in the normal `result` envelope. A standalone
`textInput` server event asks the page to open a pending platform-native dialog;
the subsequent `open` exchange still owns the snapshot and edit token. Commit and cancel carry
the returned `edit` identifier; commit also carries `text`. The server binds each
pending edit to its game instance and clears the Host snapshot when parking or
stopping the game. A platform request may remain pending across parking and is
announced once to the resumed page.
Committed strings are limited to 64 KiB of valid UTF-8, independently of guest field
limits. Constraint validation preserves the pending edit for correction. A stale
edit is invalidated permanently; the browser retains its draft until the user
closes it. Duplicate open events retain the active edit token and draft, while a
late cancel for an older token cannot consume a newer platform request. A guest exit during a change callback or the held-input release
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
The adapter preserves field constraints and invokes the guest text-change callback.
KFC modal editors do not expose a live target and remain unsupported. SKT Java
supports TextBox, the selected Form TextField, and a focused XTextField on the
visible canvas. Those limits count Java UTF-16 units; supplementary characters
consume two units. Native entry does not infer fields drawn by game code.

The SGS service `0x8f` is a separate native modal contract. It supplies prompt
and initial/destination resources, limits committed text to 32 bytes in EUC-KR,
and invokes system callback 6 with parameter 2 after either commit or cancel.
The page shows the guest prompt through `textContent`. It does not apply an HTML
`maxlength`, whose UTF-16 units would enforce the wrong boundary.

## Validation

Go tests exercise adapter constraints, stale fields, UTF-16 limits, notifications,
concurrent SKT editing/rendering, and authored Java and SGS archives. The SGS
WebSocket test checks automatic presentation, duplicate-open ownership,
synchronous next dialogs, stale cancellation, callback exit, and park/resume
reannouncement. Platform tests cover exact EUC-KR byte limits, embedded NUL and
unencodable input, cancel preservation, timer/key blocking, and one-shot
completion. The Java WebSocket test submits Korean text and a supplementary
character, rejects an oversized edit, parks and resumes the game, rejects the old
edit token, and reads back the retained guest value.

Chromium and WebKit both pass an end-to-end browser probe against the Go server and
that MIDlet at a 390 by 844 mobile viewport. It verifies composition isolation,
complete-string guest mutation, page reload/resume, stale-target rejection with
draft preservation, guest length limits, and absence of leaked game-key events or
page errors. Artifacts are in the ignored
`var/diagnostics/native-text-e2e-20260912` directory.

Chromium also passes an authored SGS archive on the current debug server at the
same mobile viewport. Without using the settings control, the first prompt opens,
a complete Korean value commits, and callback 6 opens the next prompt. Parking
closes only the page editor; resume announces and presents the same pending guest
dialog without cancelling it. Escape, the cancel button, and a click outside the
dialog each complete cancellation and open the following request, while a click
on dialog padding does not. The run has no page or server errors. Its artifacts are in the ignored
`var/diagnostics/sgs-input-e2e-20260912` directory. Composition events are driven
by automation; physical-phone keyboard and IME interaction still needs a person.

The integrated normal and debug suites, internal race checks, and `go vet ./...`
pass. Both CLI and embedded-server binaries build in debug and release profiles.
