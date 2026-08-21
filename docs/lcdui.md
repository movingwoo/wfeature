# The high-level LCDUI surface

This is part of the **SKT** platform. An SKT title is a MIDlet, so the MIDP
surface it draws through lives in `internal/platform/skt` along with the rest
of that vendor's runtime — there is no vendor-neutral MIDlet platform here.

A MIDlet's screen is either a `Canvas`, which owns its pixels and paints
itself, or a `Screen` — `Form`, `List`, `TextBox`, `Alert` — which does not.
The application never draws a `Screen`; it describes one, and the platform
renders it and reads the keys. That division is the whole design of this part
of the runtime.

- The Java classes in `internal/api/midp/java/javax/microedition/lcdui` are the
  guest's view.
- `internal/platform/skt/lcdui.go` holds the state and the natives.
- `internal/platform/skt/lcdui_render.go` draws a `Screen` into the Host
  framebuffer.
- `internal/platform/skt/lcdui_input.go` decides what a key does.

## Where the state lives

Everything is on the Go side, in a map keyed by the `Displayable` object.
`Displayable`'s constructor is not native — an application subclasses `Canvas`
and the runtime never sees the allocation — so state is created on first use
rather than at construction, and a map is the only place it can live.
`Item` and `Command` are different: their constructors *are* native, so their
state hangs off the object itself.

`Screen` content is created by whichever native first needs it, and the
calling method supplies the kind. A game subclasses these classes freely, so
the class name is not a reliable way to tell a `Form` from a `List`.

## Commands, soft keys, and the menu

MIDP puts `addCommand`/`setCommandListener` on every `Displayable`, including
`Canvas`, but it never says which key runs which command — every handset chose
for itself. This runtime chooses:

- Commands are ordered by priority, stably, so equal priorities keep the order
  they were added in. Lower is more important, as MIDP specifies.
- `KeyCodeSoft1` (-6) runs the first command; `KeyCodeSoft2` (-7) runs the
  second. Those are the Nokia values, which is what a MIDlet reading raw key
  codes was written against.
- **With three or more commands the second key opens a menu instead.** Without
  it the third command would be unreachable, which is worse than an invented
  menu. The menu is drawn over the bottom of the screen, navigated with
  up/down, and confirmed with fire.
- A `Canvas` gets the soft keys and the menu-free part of this too, but no
  command labels are drawn over it: a `Canvas` owns its pixels.

## Navigating a Screen

- **List**: up/down move the highlight, fire selects. An `IMPLICIT` list
  reports the selection by firing `List.SELECT_COMMAND` (or the command set
  with `setSelectCommand`), because that is the only way a game learns a row
  was chosen. A `MULTIPLE` list toggles instead.
- **Form**: up/down move between items, skipping any that cannot be acted on —
  a `StringItem` with no commands is decoration, and stopping on it would be a
  cursor that does nothing. When the focused item is a `ChoiceGroup`,
  left/right move within its elements and fire toggles or selects one, which
  then calls the form's `ItemStateListener`. Fire on any other item runs its
  first command.
- **TextBox** and **Alert** display their content; neither is navigable. See
  "Deliberately incomplete".

## GameCanvas

A `GameCanvas` inverts the `Canvas` contract: it owns an off-screen buffer,
draws into it whenever it likes, and calls `flushGraphics` to push it. The
runtime never asks it to paint. `getKeyStates` is latched on every key event
whether or not the canvas also wants the callbacks, because polling is the
point of the class; `suppressKeyEvents` only suppresses the callbacks.

A background `GameCanvas` that keeps drawing does not reach the display —
`flushGraphics` presents only when the canvas is the current `Displayable`.

## Media

`javax.microedition.media` runs the JSR-135 state machine and plays through
the same `backend.Audio` timeline every other sound in this runtime uses, so a
Host batching ticks hears the same sequence in the same order as one running
in real time. Content is decoded at `createPlayer`, not at `start`, so an
undecodable file fails where MIDP says it should.

`Manager.playTone` is a two-event sequence on that timeline rather than a
second path into the sink. A network locator is **refused** rather than
accepted and left silent: a game told a player exists waits for events that
would never come — the same decision `docs/network.md` makes everywhere else.

## The curve calls were missing entirely

`Graphics` had `drawRect` and `fillRect` and nothing that curves:
`drawArc`, `fillArc`, `drawRoundRect` and `fillRoundRect` were not declared on
the class at all. That is not a shape drawn approximately — an undeclared
method does not resolve, so a MIDlet reaching for one ends there.

All four are now declared and bound to `internal/curve`, which walks the shape
and hands out horizontal spans that go through the same translated, clipped
fill `fillRect` uses. The angles are MIDP's: zero degrees at three o'clock,
positive counter-clockwise, the second angle an extent rather than an end, and
the arc centred in the rectangle it was given. `arcWidth` and `arcHeight` are
corner **diameters**, so one equal to the side it rounds makes the shape an
ellipse rather than overflowing it. The other two platforms draw these through
the same geometry; see `ktf.md`, "The same geometry, on five surfaces".

## Deliberately incomplete

- **Text entry is Latin and digits, not Hangul.** A `TextBox` types through
  the shared multi-tap input method
  ([`internal/textinput`](../internal/textinput/textinput.go)), the same one
  KTF's lwc text components and this vendor's own text component use, so a
  game types the same way wherever it asks. What none of them composes is
  Hangul. The pad moves the caret and only the pad: 4 and 6 are a game's left
  and right elsewhere, but in a field they are `ghi` and `mno`, and taking
  them as caret moves left both untypable — see `skvm.md`, "Two ways a title
  takes a name".
- **`Alert` has no timer.** `getTimeout` reports what was set and
  `getDefaultTimeout` answers a fixed value, but no alert dismisses itself —
  the runtime has no timer thread for screens. An alert stays until the
  application replaces it.
- **`Item.setPreferredSize` is ignored** and `getPreferredWidth/Height` report
  what the renderer will actually use, so a game laying out against those
  numbers sees the truth rather than its own request echoed back.
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
does and check both the guest-visible state and the framebuffer, because a
screen that updates its state without reaching the display is exactly the bug
this design could have.

Regenerate the fixture with:

`$stub_dir/classes` is the signature classpath from
[`testing.md`](testing.md); build it once per session.

```sh
fixture_dir="$(mktemp -d /tmp/wfeature-ui-fixture.XXXXXX)"
javac -source 1.8 -target 1.8 -g:none -cp "$stub_dir/classes" \
  -d "$fixture_dir" internal/platform/skt/testdata/src/UIMIDlet.java
mkdir -p "$fixture_dir/META-INF"
cp internal/platform/skt/testdata/UI.MF "$fixture_dir/META-INF/MANIFEST.MF"
(cd "$fixture_dir" && zip -X -q "$fixture_dir/ui.jar" META-INF/MANIFEST.MF UIMIDlet*.class)
cp "$fixture_dir/ui.jar" internal/platform/skt/testdata/ui.jar
```
