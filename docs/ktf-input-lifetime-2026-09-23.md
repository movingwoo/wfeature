# KTF input availability after rejected confirmation

## Reproduction

The 14:52:49 local debug report exposed an omitted route in the earlier
[timer-input acceptance](ktf-graphics-input-qa-2026-09-23.md). Case A, archive
SHA-256 `016d344f7c328e6713c3e553555069472e82ecae9123eb25dc304bc6d1e2c890`,
keeps its name editor open when OK is pressed with an empty name. Opening Host
text input afterward returned `no supported text field is active`.

The report contains two input-mode selections and no character-processing calls.
An isolated-save replay reproduced the failure with the current source: the
editor remains visible, but one empty confirmation makes `Session.TextInput`
unavailable. Selecting another keypad mode or entering a digit reactivates the
input method. The earlier acceptance opened Host input immediately after the
editor appeared, so it missed this intermediate key sequence.

## Cause and bounded correction

`dispatchKeyToCards` invalidated pending edits on external keys and also disabled
C input whenever a key produced no input-method activity. That second inference
was too broad: an ignored confirmation does not call the automaton and does not
close the editor. Keeping every C input activation alive would instead expose
closed editors, so the correction requires evidence of the controller lifetime.

The [WIPI input-method contract](https://mirusu400.github.io/wipi-wiki/c-api/graphics.md)
provides composition buffers and mode selection, but no game-owned C widget
identity or visibility query. The observed Thumb caller writes its controller
state immediately before initializing the editor and selecting the input mode.
That state remains unchanged after empty confirmation, changes while a length
error message covers the editor, returns when the message is dismissed, and
changes when a valid name is accepted.

The adapter recognizes that bounded call sequence at mode activation. It checks
the mode wrapper, saved static base, initializer and controller call target;
then resolves the state pointer through the caller's PC-relative literal and
static-base table. Code/data addresses and the state value are derived from the
running guest. There is no archive-name, hash, absolute-address or image-content
shortcut. Invalid instructions, inconsistent frames and unreadable or overflowing
pointers leave the existing conservative behavior in place.

For this recognized controller, availability follows its live state. Every
external key still invalidates the old edit. A validation message blocks input;
returning to the editor permits a fresh snapshot. The commit also checks the
controller before delivery and inside the timer's input-method call, preventing
delivery after the controller changes. Unknown C callers still require observed
input-method activity after keys.

Debug diagnostics count `C input controller lifetime recognized` when this
bounded ownership path is established. Input contents are not logged.

## Validation

`wipic_input_owner_test.go` authors a relocated Thumb call layout and saved frames
with different addresses and a different controller value. Regressions cover
rejected confirmation, stale snapshots, hidden/restored controllers, controller
changes during timer delivery, mode changes, malformed instructions, changed call
targets, signed table offsets and invalid pointers. Existing unrecognized-caller
and timer-input tests retain their behavior.

The real-archive replay uses isolated saves and records screenshots and commands
under ignored `var/diagnostics/ktf-input-recheck-20260923/`. It presses empty
confirmation three times, inserts five Korean characters, verifies the guest's
length-error message blocks Host input, dismisses the message, deletes the name,
inserts four characters and confirms. The next story scene displays the submitted
name, and Host input is unavailable after the editor closes.

That route passed in both debug and release builds. The second archive's
previous insertion/deletion/replacement/confirmation route also passed again
in release. On the isolated PR branch, `make test`, `make test-debug`,
`go test -race ./internal/...`, `go vet ./...`, and `make server server-release`
passed. The local debug and release server binaries were rebuilt as well.

This is shared-platform-path acceptance. The original saves and the running
user session are preserved. Physical iPhone IME interaction is not automated.
