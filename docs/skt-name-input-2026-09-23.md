# SKT name input with a placeholder Canvas

## Reproduction and cause

The two SKT reports saved at 07:42 UTC on 2026-09-23 show working key
delivery and active sessions. Replaying their final sessions with isolated
saves reaches both name editors. Before the correction, `Runtime.TextInput`
returns `no supported text field is active` in both.

Both archives construct `com.xce.lcdui.XTextField` with an empty text value,
an eight-character limit, unconstrained input, and a placeholder Canvas.
The placeholder has an empty `paint` body. The visible game Canvas paints
the focused field itself. The Host adapter incorrectly requires the
constructor's Canvas to be the current display, so it rejects these editors.

The archive SHA-256 values are
`2d66945008c1be8a749abd91d54e98c622268ab3c53e2a7c07cbe108a4d5780d` and
`2f5246006bd86d43e3ea85901208082b8eae8b47ca8831d28879d74301e15392`.
The routes and images remain ignored local artifacts.

This is vendor-specific observed behavior. A search of the WIPI specification
site and public documentation found no XTextField contract. The correction
does not change `m.SK_VM` or infer a different constructor argument order.

## Correction and boundaries

A focused field painted directly through this runtime's screen Graphics records
the current display and its revision. That evidence also permits Host text
input when the constructor supplies a different Canvas. An offscreen image
does not establish a visible target. Focus calls clear the evidence, and
display transitions invalidate it. Existing field, value, constraint, focus,
menu, and display revision checks still apply at commit.

After a Host commit, the visible Canvas is queued for repaint. The vendor
field's current `repaint` stub alone cannot present the changed name.
The game still handles OK, validates its name policy, and dismisses the editor.
The Host adds no handset Hangul keypad automaton.

## Validation

`TestHostXTextFieldPaintedOnCurrentCanvas` reproduces the rejected field with
an authored Canvas and the real XTextField constructor. Restoring the original
ownership check makes it fail at opening the painted field. The corrected
path checks Korean commit and readback, immediate Canvas repaint, stale edits,
focus restoration, and a display round trip without repainting the field.
`TestHostXTextFieldRejectsOffscreenPaint` rejects an image-only field.

Both local archives accept a two-character Korean name and return the same
text on reopening in release and debug probes. Debug probes additionally
confirm with the game's ordinary OK key; both dismiss the editor and remain
active. This proves the reported name-entry route, not complete gameplay or
name persistence across a new runtime.

Chromium and WebKit also pass both original archives through the served page:
open the native-text dialog, submit Korean, reopen and read it back, then use
OK to reach the next setup screen. The dialog reports unavailable after the
game closes the field. Neither browser reports a page error. The runs use
isolated saves and do not establish physical-phone IME behavior. Results and
screenshots are under `var/acceptance/name-input-chromium-1790150858291` and
`var/acceptance/name-input-webkit-1790150937937`.

`make test` (including 227 Node tests), `make test-debug`,
`go test -race ./internal/...`, `go vet ./...`, and `make server` pass.
