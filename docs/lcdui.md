# The high-level LCDUI surface

This is part of the **SKT** platform. An SKT title is a MIDlet, so the MIDP
surface it draws through lives in `internal/platform/skt` with the rest of that
vendor's runtime; there is no vendor-neutral MIDlet platform.

A MIDlet's screen is either a `Canvas`, which owns its pixels and paints
itself, or a `Screen` — `Form`, `List`, `TextBox`, `Alert` — which does not.
The application describes a `Screen`; the platform renders it and reads the
keys. That division is the design of this part of the runtime.

- The classes declared in Go in `internal/api/midp` (`definitions.go`, with
  bodies in `lcdui.go`) are the guest's view.
- `internal/platform/skt/lcdui.go` holds the state and the natives;
  `lcdui_register.go` binds them to the declared methods.
- `internal/platform/skt/lcdui_render.go` draws a `Screen` into the Host
  framebuffer.
- `internal/platform/skt/lcdui_input.go` decides what a key does.
- `internal/platform/skt/lcdui_game.go` holds the `GameCanvas` buffer.

## Where the state lives

State is on the Go side, in a map keyed by the `Displayable` object.
`Displayable`'s constructor is not native — an application subclasses `Canvas`
and the runtime never sees the allocation — so state is created on first use
rather than at construction, and a map is the only place it can live. `Item`
and `Command` constructors are native, so their state hangs off the object.

`Screen` content is created by whichever native first needs it, and the calling
method supplies the kind. A game subclasses these classes freely, so the class
name does not reliably tell a `Form` from a `List`.

A title edits a `List`, `ChoiceGroup`, `Form`, `Item` or `Alert` from its own
threads as freely as from a callback, while the Host paints that content and
moves through it on a key. The natives that read or change it therefore run
under the same lock (`textMu`) the painter and the key handlers hold, and the
key handlers release it before calling back into the title.

## Commands, soft keys, and the menu

MIDP puts `addCommand`/`setCommandListener` on every `Displayable`, including
`Canvas`, but does not say which key runs which command; every handset chose
for itself. This runtime chooses:

- Commands are ordered by priority, stably, so equal priorities keep the order
  they were added in. Lower is more important, as MIDP specifies.
- `KeyCodeSoft1` (-6) runs the first command; `KeyCodeSoft2` (-7) runs the
  second. These are the Nokia values, which MIDlets reading raw key codes were
  written against.
- **On a `Screen` with three or more commands the second key opens a menu
  instead.** Without it the third command would be unreachable, which is worse
  than an invented menu. The menu is drawn over the bottom of the screen,
  navigated with up/down, and confirmed with fire or the first soft key; the
  second soft key closes it.
- **On an `IMPLICIT` List a command from a soft key or the menu first selects
  the focused row.** MIDP defines `IMPLICIT` as the choice whose focused element
  is selected when a command is initiated, so `getSelectedIndex` in
  `commandAction` names the row the player is on. A row the title selects
  itself with `setSelectedIndex` or `setSelectedFlags` becomes the highlighted
  one, so the next command keeps it.
- A `Canvas` gets the two soft keys but no menu and no command labels: a
  `Canvas` owns its pixels.

The browser reaches `KeyCodeSoft1` and `KeyCodeSoft2` through the keypad's
`메뉴` and `우상단` buttons, which send -6 and -7 — the numbers `MH_KEY_SOFT1`
and `MH_KEY_SOFT2` carry on the WIPI platforms, so each button serves every
vendor ([`cli.md`](cli.md#key-names), "Key names"). A Displayable with commands
receives the command, never the key, and which command is this runtime's choice
rather than the title's, since the original handset also chose for it. On a
`Canvas` with no commands the two keys differ: the first reaches `keyPressed` as
129 (`KeyCodeMenu`), this vendor's menu-key code, because a local title switches
on that value to open its in-game menu; the second reaches nothing, because no
title here has shown what this vendor numbered it, and passing -7 on would be a
guess. A Jlet's card receives both soft keys as ordinary WIPI key codes.

## Navigating a Screen

- **List**: up/down move the highlight, fire selects. An `IMPLICIT` list
  reports the selection by firing `List.SELECT_COMMAND` (or the command set
  with `setSelectCommand`), because that is the only way a game learns a row
  was chosen. A `MULTIPLE` list toggles instead. Deleting rows keeps the
  highlight on a row that exists, the way `Form.delete` keeps its focus.
- **Form**: up/down move between items, skipping any that cannot be acted on —
  a `StringItem` with no commands is decoration, and a cursor stopping on it
  would do nothing. When the focused item is a `ChoiceGroup`, left/right move
  within its elements (an empty one keeps its element focus at 0) and fire
  toggles or selects one, which then calls the form's `ItemStateListener`.
  Fire on any other item runs its first command. A `TextField` item is not
  typed on the keypad; Host text input edits the selected one
  ([native text input](native-text-input.md#supported-editors)).
- **TextBox** takes keypad typing, with left/right moving the caret; see
  "Deliberately incomplete". A `NUMERIC`, `PHONENUMBER` or `DECIMAL` box types
  the digit on each key rather than its letters, and an `UNEDITABLE` box takes
  no keypad edits, the same rules the Host text input applies.
- **Alert** displays its content and is not navigable.

## GameCanvas

A `GameCanvas` inverts the `Canvas` contract: it owns an off-screen buffer,
draws into it whenever it likes, and calls `flushGraphics` to push it. The
runtime never asks it to paint. `getKeyStates` is recorded on every key event
whether or not the canvas also takes the callbacks, because polling is the
point of the class; `suppressKeyEvents` suppresses only the callbacks. As MIDP
requires, a key reads as pressed if it is down or was pressed since the
previous call, so a tap that starts and ends between two polls is reported
once; a quicksave keeps that latch beside the held keys.
`flushGraphics` presents only when the canvas is the current `Displayable`, so
a background `GameCanvas` that keeps drawing does not reach the display. When
one is painted the ordinary way, its inherited `paint` blits the buffer at the
`Graphics` origin, inside its clip and under the lock every other drawing call
takes.

## Media

`javax.microedition.media` runs the JSR-135 state machine and plays through
the same `backend.Audio` timeline as every other sound in this runtime, so a
Host batching ticks hears the same sequence in the same order as one running in
real time. Content is decoded at `createPlayer`, not at `start`, so an
undecodable file fails where MIDP says it should.

`Manager.playTone` is a two-event sequence on that timeline rather than a
second path into the sink. `Manager.createPlayer(String)` accepts the tone
device locator and `resource:` or `/` paths into the archive; any other
locator, including a network one, raises `MediaException` rather than yielding
a silent player, because a game told a player exists waits for events that
never come. [Network](network.md) makes the same decision everywhere else.

## The curve calls were missing entirely

`drawArc`, `fillArc`, `drawRoundRect` and `fillRoundRect` were once not
declared on `Graphics` at all. An undeclared method does not resolve, so a
MIDlet calling one stopped there rather than drawing an approximate shape.

All four are declared and bound to `internal/curve`, which walks the shape and
emits horizontal spans through the same translated, clipped fill `fillRect`
uses. The angles are MIDP's: zero degrees at three o'clock,
positive counter-clockwise, the second angle an extent rather than an end, and
the arc centred in the rectangle it was given. `arcWidth` and `arcHeight` are
corner **diameters**, so one equal to the side it rounds makes the shape an
ellipse rather than overflowing it. The other two platforms draw these through
the same geometry; see
[the same geometry, on five surfaces](history/ktf.md#implementation-the-same-geometry-on-five-surfaces).
The walk is bounded by the clip met with the surface (`curve.Bounds` and the
`...Within` forms), so a box as large as an int draws what lands and returns;
the pixels are those the whole walk drew.

## Deliberately incomplete

- **Keypad text entry is Latin and digits, not Hangul.** Hangul reaches a
  `TextBox` through the Host text-input dialog
  ([native text input](native-text-input.md)). On the keypad a `TextBox` types
  through the shared multi-tap editor
  ([`internal/textinput`](../internal/textinput/textinput.go);
  see [the keypad editor](native-text-input.md#the-keypad-editor)), the same
  one KTF's LWC text components and this vendor's text component use, so a game
  types the same way wherever it asks; none of them composes Hangul. Only the
  directional pad moves the caret: 4 and 6 are a game's left and right
  elsewhere, but in a field they are `ghi` and `mno`, and taking them as caret
  moves would leave both untypable — see
  [two ways a title takes a name](history/skt.md#implementation-two-ways-a-title-takes-a-name).
- **`Alert` has no timer.** `getTimeout` reports what was set and
  `getDefaultTimeout` answers 2,000 ms, but no alert dismisses itself: the
  runtime has no timer thread for screens. An alert stays until the application
  replaces it.
- **`Ticker` does not scroll.** Its text is drawn once under the title,
  trimmed to the screen width.
- **`Item.setPreferredSize` is ignored**, and `getPreferredWidth/Height` report
  what the renderer uses, so a game laying out against those numbers sees the
  real extent rather than its own request echoed back.
- **Per-element fonts in a choice are not honored**; `getFont` answers the
  default rather than a font the renderer would not use.
- **`AlertType.playSound` answers false.** The Host sink plays decoded media,
  not device tones, and answering true would report a sound the user cannot
  hear.
- `ChoiceGroup` with `POPUP` renders like `EXCLUSIVE`; there is no popup
  window.
- No `Gauge`, `DateField`, `Spacer`, or `CustomItem`.

## Testing

`internal/platform/skt/testdata/ui.jar` is a newly authored MIDlet covering
the surface. The tests drive real key events through the same queue a Host
uses and check both the guest-visible state and the framebuffer, because a
screen that updates its state without reaching the display is the defect this
design is prone to.

Regenerate the fixture as follows. `$stub_dir/classes` is the signature
classpath built by
[the fixture guide](history/testing.md#implementation-compiling-a-java-fixture);
build it once per session.

```sh
fixture_dir="$(mktemp -d /tmp/wfeature-ui-fixture.XXXXXX)"
javac -source 1.8 -target 1.8 -g:none -cp "$stub_dir/classes" \
  -d "$fixture_dir" internal/platform/skt/testdata/src/UIMIDlet.java
mkdir -p "$fixture_dir/META-INF"
cp internal/platform/skt/testdata/UI.MF "$fixture_dir/META-INF/MANIFEST.MF"
(cd "$fixture_dir" && zip -X -q "$fixture_dir/ui.jar" META-INF/MANIFEST.MF UIMIDlet*.class)
cp "$fixture_dir/ui.jar" internal/platform/skt/testdata/ui.jar
```
