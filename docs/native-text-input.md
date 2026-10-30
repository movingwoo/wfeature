# Native text input

The browser composes text with the phone or PC keyboard and IME. The user
selects an input field in the game, opens **설정 → 🔤 문자 입력** (shown while a
game is playing), and presses **입력** to submit the completed string.
Intermediate IME composition stays in the browser. The emulator does not
implement a handset Hangul keypad layout; the game keypad remains available for
game controls and for the [keypad editor](#the-keypad-editor).

The dialog (`web/text-input.js`) checks the actual target when it opens and
shows only a green **입력 가능** or red **입력 불가능**, with no explanatory
text; the status stays hidden while checking or submitting. An unsupported
screen shows an empty, disabled field. A stale target keeps the draft for
copying, makes it read-only and disables resubmission until the dialog is
reopened. A constraint failure shows the red status while keeping editing and
retry available. Append-only targets label the action **삽입**. Availability
describes the snapshot taken at opening, not a continuously polled claim about
the game's focus; the platform validates again at commit.

**Availability errs toward offering.** A field the player can see and cannot
type into is the failure that matters; a screen that offers input it has no
field for costs a commit that reports failure. Every platform therefore keeps
its strict rules first, so the field they pick is still the one that gets the
text, and asks relaxed rules only when the strict ones name nothing. A relaxed
rule never chooses between two candidates it cannot tell apart. On the routes
that deliver text through a carrier key, a commit on a screen without a field
reaches the game as one press of that key, followed by its release, and then
fails. That failure is evidence: the route offers nothing more until something
new says a field may be up, so retrying does not press the key again
([2026-10-20](history/lgt.md#lgt-text-input-2026-10-20),
[review follow-ups](history/lgt.md#lgt-text-input-review-2026-10-20)). A carrier
never types into a field it was not composed for.

## Boundary

The session requests a platform snapshot of the active field
(`backend.TextInput`): its text, length limit, multiline and password flags, a
browser keyboard hint and a commit closure. Before changing guest state, the
platform validates the active target and the original contents again. A focus
or value change rejects the edit rather than redirecting it to another field.
Platform constraints and notifications stay platform responsibilities; the
keyboard hint does not validate text.

The WebSocket `text` message has `open`, `commit` and `cancel` actions. A
successful open returns `textInput` in the normal `result` envelope, with
`edit`, `text`, `maxLength`, `multiline`, `password`, `inputMode` and, for an
append target, `append: true`. Commit and cancel carry the returned `edit`
identifier; commit also carries `text`. The server binds each pending edit to
its game instance and clears it when the game is parked or stopped. Opening an
edit first releases held input. Committed strings are limited to 64 KiB of valid
UTF-8 (`backend.MaxTextInputBytes`), independently of guest field limits.
Constraint validation keeps the pending edit for correction. An edit proven
stale (`the active text field changed; open text input again`) is invalidated
permanently, even if the guest later restores the same focus and contents; the
browser keeps its draft until the user closes the dialog. A guest exit during a
change callback, or during the held-input release before an edit opens, ends the
session and settles the outstanding request. Password fields use a password
input, never the multiline area, and are cleared when the dialog closes.

An append snapshot is used when a platform can pass completed text to the active
guest cursor but cannot read or replace the existing value. It starts with an
empty Host field; the same stale-edit and transport rules apply.

Game-owned input interfaces are supported only where the platform can name the
target the text goes to; the rules below say how each platform names one.

## Supported editors

### KTF

**Two passes.** Six routes can name an editor, in this order: a title's own
`InputMethodHandler`, the vendor KFC form, the LWC component with explicit
focus, a shown shell's text field, a field the title draws, and the C input
method. They are asked first under the rules below, and the first that names an
editor its rule accepts wins; a candidate that cannot take the edit, such as a
focus left on a button or a field behind a hidden shell, hands the question to
the next route instead of ending it. Only when every route comes up empty are
they asked again, in the same order, under the relaxed rule each describes. So
what the first rules pick they still pick, and the relaxed rules only answer
where the first had nothing. A commit asks again and must find the same editor;
a route ahead of it that now names another editor makes the edit stale, while a
route that still names nothing does not.

**LWC fields.** Active LWC `TextField` and `TextBox` instances are supported,
including guest subclasses. The adapter keeps field constraints and stores the
complete value through the component string-setting contract.

- *Explicit focus* follows the recorded parent chain to the currently shown
  shell, including nested containers and shell subclasses. Detached children and
  hidden ancestors are unavailable. A field that never belonged to a container
  stays supported for a card that draws it directly. Moving focus away and back
  through either the component or the form API invalidates the earlier edit.
  *Relaxed:* the first shell in the chain needs only to be shown, so a field in
  a shell that is still shown after a later shell was hidden qualifies, and a
  chain that ends at a container nothing holds is treated as a field the card
  draws. Detached children, a parent that no longer lists its child and cycles
  are still refused.
- *Without explicit focus*, the most recently shown `ShellComponent` exposes its
  sole direct text child (or sole work component). This covers an observed name
  editor that calls `addComponent` and `show` without `setFocus`. It is a bounded
  Host compatibility path, not automatic keypad focus: the WIPI
  `ContainerComponent` contract requires explicit focus for key delivery. Nested
  or ambiguous layouts are not inferred. Hiding or reopening the shell, replacing
  it, or changing its children or work component invalidates a pending edit, and
  explicit guest focus takes precedence. *Relaxed:* the newest shell still shown
  (the platform keeps up to sixteen shown shells, newest first, so hiding a
  dialog uncovers the shell beneath it) is searched through nested containers,
  without entering nested shells, and offers its focused field, else its first
  field told it has focus, else its first field. A field it cannot write — one
  with a composition listener — offers nothing rather than another field.
- *With no explicit focus, shell or vendor form*, a field the title draws
  itself is offered: a text component told it has focus through
  `focusNotify(true)` whose text the top card read during its last paint. One
  title's character-name screen keeps such a field from start-up, never calls
  `setFocus` and never adds it to a container; its card reads `getString` on
  every paint while the name screen is up and on no other screen, so the read,
  not the focus, is the evidence. Exactly one focused field must be read: two, an
  unfocused one, or a failed paint offer nothing. One screen that does not show
  the field is still offered: that title's network-failure dialog is painted by
  the same card, which still reads the field under it. *Relaxed:* the set the
  last paint that read any text published is kept through paints that read none
  or fail, and of its LWC fields the only focused one, or else the only one, is
  offered; several unfocused fields still offer nothing, and a paint that reads
  more than sixteen texts empties the set. The set belongs to the card that
  painted it and is offered only while that card is on top, so a card pushed
  over the name screen, or one that replaced it, draws its own screen. A commit
  through this route does not need the latest paint to have read the field.
- A field with an explicitly installed `InputMethodListener` is unsupported:
  per-key composition deltas cannot safely describe an arbitrary whole-field
  replacement ([whole-field replacement and IME callbacks](#lifecycle-whole-field-replacement-and-ime-callbacks)).

**KFC form.** A bounded non-modal route covers an explicitly shown `GForm` with
exactly one directly attached `GTextField` that has an `EventListener`. The Host
changes only that field's value; the guest listener receives keypad events and
owns acceptance and dismissal. Ambiguous or changed form contents are rejected,
and the synchronous KFC `doModal` path is unsupported
([lifecycle evidence](#lifecycle-ktf-non-modal-form-visibility)). *Relaxed:* the
form's contents are read when the Host asks rather than when the form was shown,
so a field added or given a listener afterwards counts; with several listened
fields the focused one is taken, and a form with no listened field offers its
only `GTextField`. Keys still go where they went before.

**A title's own `InputMethodHandler`.** A Java title may construct a handler,
give it its own listener, and hand it the keys its card receives. The Host
appends up to 64 Java chars per submission, one character per carrier key the
title forwards, so the title's own length limit and redraw apply to each; the
field's constraint is applied and controls are rejected. Under the first rule
the field is available from the listener's registration until a press does not
reach the handler, except for a field the title rebuilds while handling a key,
and it survives a quick load. *Relaxed:* it stays offered through presses that
miss it, until the title removes the listener or a commit's carrier does not
reach it: a title whose field is still on its screen hands that field every key,
so a carrier it keeps from the field shows the field closed, and the offer ends
until the title hands the field a key again. A press that misses it still spends
an outstanding edit, and the first character of a commit reopening it does not.
A carrier the title hands to another of its handlers types nothing there and
makes that handler the open field; one it hands to a text component types
nothing either. It is not offered while another card is on top or a vendor form
owns the keypad. The keypad itself types digits only in the title's digit mode.
Titles that run their own event loop are unsupported. See
[whole-field replacement and IME callbacks](#lifecycle-whole-field-replacement-and-ime-callbacks).

**C input method.** A C-backed card editor that activates the WIPI-C input
method receives completed Host text through the guest's existing key callback and
completion buffer. Input-mode selection describes the handset keypad automaton,
not a Java-style field constraint; Host text must be KSC5601 without controls. A
submission is delivered as complete EUC-KR characters through successive guest
callbacks, so a small completion buffer does not restrict the field to one or
two characters, and the guest keeps its own field-length policy. CLR invalidates
the old edit snapshot while keeping an editor that flushes its composition
available for another submission. Key and editor lifecycle changes invalidate
pending edits. Cards that defer keys to the C timer owning their input method are
supported: the commit waits for that timer and verifies consumption before
returning, and deferred CLR flushes keep the active editor. General guest event
loops are unsupported. A recognized C controller also keeps availability after
an ignored empty confirmation; its live state blocks input while a validation
message covers the editor, permits a fresh edit on return, and blocks input after
acceptance. For an unknown caller the first rule needs input-method activity
since the last key on the card that showed the editor. *Relaxed:* an unknown
caller is offered on any card once the title has made any input-method call,
start-up queries included; after a load, recorded calls, activations or a built
mode table say so. A recognized controller keeps its live-state rules in both
passes. A carrier that reaches no input method is followed by its release, the
commit fails as stale, the editor is no longer active, and the relaxed rule
offers nothing until the title calls its input method again or the player
presses a key. See the [initial implementation](history/ktf.md#ktf-c-input-bitmap-investigation),
the [timer delivery and clipping regression](history/ktf.md#ktf-graphics-input-qa-2026-09-23)
and the [controller lifetime regression](history/ktf.md#ktf-input-lifetime-2026-09-23).

Each carrier, for the `InputMethodHandler` route and the C editor alike, is a
key: an uncaught guest exception from the title's `keyNotify` ends that key as
it does for a key a person presses, and whether the field took the character
decides whether the submission continues.

### SKT

SKT supports a `TextBox`, the selected `Form` `TextField`, a focused vendor
`XTextField` on the visible Canvas, and the title-owned `TextComponent` attached
to its input-method handler, in that order. SKT tracks revisions for screens,
command overlays, selected Form children, field values, limits and constraints,
and vendor focus; restoring an earlier state does not revive a pending Host edit.

When none of those names a field, three fallbacks are asked, and none while a
`TextComponent` is attached, since that component is where the handset's keys
type. A Form whose selection is not on an editable `TextField` offers its only
editable `TextField`; a Form's selection starts on its first item and only the
pad moves it, so a Form that opens on a label above its one field refused the
page until the player pressed Down. Two editable fields with the selection on
neither offer nothing. On a Canvas, the focused `XTextField` is offered when it
belongs to that Canvas: built for it, or drawn while it is up — painted into any
Graphics, or its text read for the title to draw — so a field left focused on a
Canvas the title has moved on from is not. With no field focused, the one
`XTextField` painted into any Graphics since the Displayable came up is offered;
two painted fields offer nothing, and a focus call that changes a field's focus
starts that record again, while one that repeats it does not. A fallback's
commit checks that the same rule still picks the same field.

An `XTextField` built with a placeholder constructor Canvas is placed by the
strict route after the focused field paints directly onto the current screen,
and by the fallback when it paints into a back buffer or the title reads its
text to draw it. Focus and display changes invalidate that evidence. Host
commits repaint the visible Canvas. See the [SKT name-input regression](history/skt.md#skt-name-input-2026-09-23).

The `TextComponent` route is append-only. `TextComponentHandler` identifies the
title-owned component and exposes its size, cursor, limit, constraints and
character insertion, but no value getter. The Host appends the completed string
at the guest cursor and rejects commits after component, keypad, cursor, size,
limit or constraint changes. Delivery is one character at a time and accepts
KSC5601 text — its Hangul syllables and symbols, plus printable ASCII — and
refuses everything else, including supplementary characters that could not
survive separate Java `char` callbacks. Each callback is followed by an
ownership and cancellation check before another character or repaint is
delivered. A detached or replaced target, a queued screen change or a
cancellation stops delivery; any accepted prefix stays guest-owned, and the
consumed snapshot cannot be retried.

**Native programs.** An SKT native program is a Clet ([SKT native](skt-native.md#skt-native-text-entry)),
and its only route is the WIPI-C input method: the program draws its own field,
keeps the value and composes through `MC_imHandleInput`, which SKT's graphics
table carries at 37 to 41 (`internal/platform/skt/clet/text_input.go`). A commit
is appended one complete EUC-KR character at a time. Each character is a carrier
key — a press and then its release — handed to the program's event entry, and
the field's own `MC_imHandleInput` call receives the character as its completed
text. The rest follows the KTF C input method's rules:

- Text is offered once the program has used its input method at all — set a
  mode, asked for its modes or handed it a key — on every screen, since a Clet
  names no widget the platform could see. A carrier that reaches no input method
  fails the commit as stale, and text is not offered again until the program
  calls its input method or the player presses a key.
- A field that took its keys on the timer that called the input method is given
  its carrier when that timer is due — the platform waits for it, at most one
  second, moving a Host's own clock instead of waiting where it has one — and
  the timer is then run, so the queued carrier reaches the field.
- A submission holds up to 64 characters; controls and text outside KSC5601 are
  refused. A field that refuses a character keeps what it took before it and
  the commit reports the refusal; a consumed character spends the snapshot.

### LGT

**LWC fields.** LGT supports an LWC `TextField` or `TextBox` whose parent chain
reaches a shown Shell, or ends without reaching a shell at all, which is a field
the title draws itself (as a field that never joined a container is on KTF). The
focused field comes first; a focused field on the screen that cannot take a Host
edit keeps any other from being offered in its place. Without a focused one, the
only field under a shown Shell is taken, and when no shown Shell holds one, the
only field in no shell whose text the pushed card read during its last paint
that read text; a field the title built and never drew, or dropped and has not
had collected, is not one. Two at the same level get nothing. A title can build
its name dialog without giving the field focus, or tell the field focus has gone
while the dialog stays up, and the field is still the one the player means.
Shell visibility and focus changes invalidate pending edits, and field and
listener revisions also reject guest changes that restore an earlier value; a
notification that repeats what a component already knows, and an input-mode
selection, do not, since a title can send them on every frame. Application
lifecycle callbacks are dispatched by their exact method signature, and
shown/focused widget graphs stay reachable by the Java collector. As on KTF, a
text component whose handler has an installed `InputMethodListener` is
unsupported. This adds text editing, not a complete LWC renderer.

**WIPI-C widgets.** Game-owned widgets that use `MC_imHandleInput` are an append
route, because WIPI-C exposes neither the field value nor a stable component
identity. While a widget is taking the player's keys through the platform input
method, a Host commit enters its Clet with character-key carriers, and the
widget's normal `MC_imHandleInput` call receives the completed EUC-KR text in its
completion buffer, as much as the buffer holds each time. The carrier is
substituted inside the platform and is not composed as a keypad character.

- A submission holds up to 64 completed characters; controls and text outside
  KSC5601 are rejected. Input-mode selection describes the keypad automaton, not
  a field constraint, so Host composition accepts Korean even when the widget
  selected `N123`; numeric keypad events still produce digits in that mode.
- Characters go to every caller in batches that fit its completion buffer,
  through successive carriers. Only a buffer too small for the first character
  rejects the submission, atomically. One recognized SDK caller uses output-only
  lengths with five-byte local buffers, so its batches are at most four bytes;
  recognition requires the instruction sequence and matching live stack
  arguments, not a game name or a zero size value. Other callers keep their
  declared capacities.
- The widget enforces its own smaller limits: a widget that takes part of the
  text and stops asking for more has applied its limit, and the commit succeeds.
- A widget that queues the carrier and reads it in a timer is served by running
  the Clet's due timers, at most eight rounds and one second of guest time
  (`maxDeferredCarrierRounds`, `maxDeferredCarrierWait`), until the input method
  is called.
- Consuming any completion bytes invalidates the snapshot, even if a later guest
  callback faults; a fresh edit is required after consumed input. Errors before
  delivery and atomic capacity rejections keep the retry path. If the Clet route
  no longer reaches the input method, the edit is rejected as stale.

Input is offered from the title's first input-method call on, start-up
configuration included, on every screen. A commit on a screen whose widget does
not route the carrier to the automaton fails as a stale edit; the title has
received one carrier press, and the platform queues the matching release so the
key does not stay down. Input is then not offered until the title calls its
input method again or the player presses a key, which is what can bring a field
up, so a retry does not hand the game another press. See
[what a title sees after a commit](#what-a-title-sees-after-a-commit).

### Limits common to every route

Java field limits count UTF-16 units in both Host composition and the keypad
editor. Host text never holds a supplementary character (see
[the handset's encoding](#the-handsets-encoding)), but a title's own text can,
and such a character consumes two units. Keypad caret movement, backspace and
truncation keep complete characters. Beyond the routes above — a KTF field a
card reads while painting, a KTF title's own `InputMethodHandler`, and WIPI-C
input-method widgets — native entry does not infer fields drawn by game code.

## The keypad editor

Typing on the game keypad, as opposed to the Host dialog, goes through the
shared multi-tap editor in `internal/textinput`. SKT `TextBox` and `XTextField`
and KTF LWC text components use it, and the SKVM text component handler runs the
same key table and timing against a component it cannot read, so a game types
the same way wherever it asks. It composes no Hangul.

- Keys 2–9 cycle their letters and then the digit (`abc2` … `wxyz9`), key 1
  cycles punctuation (`.,?!'"1-()@/:_`), and key 0 cycles a space and `0`.
- A second press of the same key within `CommitDelay` (900 ms of guest time, so
  a Host batching ticks types the same text as one running live) replaces the
  character it produced; any other key, a pause, or a caret move starts a new
  character.
- `*` cycles the mode: lowercase, uppercase, numeric (labels `abc`, `ABC`,
  `123`); numeric mode inserts the key's digit. `#` deletes the character before
  the caret.
- Only presses type. KTF LWC fields also take CLR as backspace; SKT
  `XTextField` takes left/right as caret moves and CLR as backspace, and SKT
  `TextBox` takes left/right as caret moves. 4 and 6 stay letters inside a field
  ([LCDUI](lcdui.md#deliberately-incomplete)).
- A Java field's limit counts UTF-16 units, and insertion that would exceed it
  is refused.
- SKT `TextBox` and `XTextField` follow the field's constraint as the Host
  dialog does: an `UNEDITABLE` field takes no key at all, caret moves
  included, and a `NUMERIC`, `PHONENUMBER` or `DECIMAL` field is held in
  numeric mode, so a key types its digit and `*` cannot leave it. That is the
  handset's digit mode, so the keypad types no `-` or `.` there; the page's
  text input does, where the constraint allows them (a leading `-` for
  `NUMERIC`, and a `.` for `DECIMAL`).

An editor's state, including an unfinished multi-tap cycle, is captured in
quick saves (`textinput.StateSnapshot`) and restored without sending keys.

## What a title sees after a commit

A handset changed a field only through keys, so a title's own code after a key
is often what puts new text on screen. A Host commit has no key, and three
consequences were reported as "the name appears only after I press another
button" and "only the first letter appeared until I deleted it and typed it
again".

**Whole-value fields ask for a frame.** A KTF LWC or KFC commit sets
`repaintPending` (and posts the repaint event to a guest event loop); an LGT
Java commit marks the pushed card dirty. That is the request a component makes
when its contents change. Without it, a KTF card whose worker owns its frame
cadence, and every LGT Java card, kept the old text until the next key.

**Title-owned SKT fields get one key.** After a commit to an `XTextField` or a
title's `TextComponent`, the runtime delivers a press and release of key code 0
(`hostTextRedrawKey`) to the current Canvas. Two local titles draw such a field
only in their own key handling, after passing the key to the field: one title's
`TextComponent.repaint` is empty and its game thread redraws after it takes a
queued key; another's `paint` draws the field only on the pass its key handler
requested. Repainting the Canvas was therefore not enough. Code 0 is no handset
key, neither vendor input method edits with it, `getGameAction(0)` answers 0,
and the key travels the title's own path, so a title that queues keys for a game
thread redraws when that thread takes it. One title clears its name field on the
first key after the name prompt appears; a commit made before that key is
cleared by the redraw key exactly as it would be by the player's next key, and a
commit made after it is kept and shown.

**A widget's own input-method calls are part of the delivery.** KTF and LGT
WIPI-C commits hand text to the widget through a carrier key. A widget may call
the input method again while handling that key: one LGT name widget flushes the
automaton and routes the same key a second time, another selects its mode again
after every key. Each such call used to advance the snapshot revision, which is
how a player's key is detected. On KTF, where text goes one character per
carrier, delivery stopped after the first character and the page reported the
field as changed. On LGT the first widget had the whole string and the page
still reported failure; the second is fed four bytes per carrier and kept only
its first two Korean characters. Calls made while the Host's own carrier is
being processed no longer move the revision. A key between snapshot and commit,
a mode change or a different event handler still makes the edit stale. A
checkpoint is refused while a carrier is in flight.

**A vendor field draws in the title's colour.** `XTextField.paint` draws with
the colour the title set on its Graphics, as every other drawing call does. It
used to draw black regardless: two titles that draw a black or dark input bar
and then the field over it showed nothing, and with the title's colour both show
the name in white. A third leaves a dark outline colour set from its own
lettering, and its name stays hard to read; no evidence says what the vendor
field drew instead.

**LGT C input is offered on every screen.** LGT WIPI-C input was available from
the first input-method call. Several titles select a mode while starting and on
every screen change, so eleven local archives offered input on splash, menu and
gameplay screens, and a commit there delivered a stray `0` key. Availability was
then made to follow the keys: calls before the first key did not count, and a key
the Clet handled without the input method closed input. One title's name screen
showed that this refuses a field that is up. It calls the input method once while
starting, and after that only from its name screen's key handler; the key that
opens the screen is handled by the class screen, so on arrival the empty field
had received no call and the page showed 입력 불가능 until the player pressed some
other key there. A field the player cannot type into is the failure that matters,
and an offer on a screen without one costs a commit that reports failure, so
input is offered again from the first input-method call on, and a failed carrier
is followed by its release. A load of a record written while input followed the
keys offers input to a title whose record shows input-method calls, a mode table
or a selected mode other than the first. One title's
ranking name widget reads its keys in a timer and holds eight bytes per call; it
takes the whole name ([batches](#lgt)).

**Not a text-input defect, and deliberately not built:** one KTF title draws its
name field as an LWC component the platform is expected to paint
(`ShellComponent` with a `TextBoxComponent` and foreground and background
colours). This runtime does not paint LWC components, so the field stays empty
whether the name arrives from the Host or the keypad; the value is there and
reads back. A renderer would be a widget toolkit whose look nothing here
records — no capture of the original runtime's components exists — for a
surface few titles reach: four titles showed a shell in a 3,000-tick sweep of
the local corpus, and about twenty-six name `ShellComponent.show` at all. What
it drew would be a guess presented as the original. Reopen this when a capture
of the original runtime's LWC components turns up.

## The handset's encoding

Host text reaches a title only if every character has a code in KSC5601, on all
three platforms and every route: KTF's LWC, vendor and input-method fields and
its C input method, SKT's fields, and LGT's Java fields and C widgets. The page
shows a refusal as 입력 불가능 and keeps the dialog open, so the text can be
changed and submitted again.

The WIPI specification names the encoding — a String becomes a C string as
ISO8859 for English and KSC5601 for Hangul — and the handsets of all three
carriers were EUC-KR. Go's `korean.EUCKR`, which this runtime encodes with, is
the Microsoft extension (code page 949), which also gives codes to the 8,822
modern Hangul syllables KSC5601 lacks. A title that draws text with its own font
indexes the glyph table by those bytes. One KTF title saved a character name
holding such a syllable and then drew it on its slot screen: the font raised
`ArrayIndexOutOfBoundsException` from the paint and the slot showed blank, and
with the name in the save it would have stayed so. The same name drew correctly
on the name screen, which uses the platform's font, so nothing at entry time
showed the problem.

Text the handset could not have typed is therefore refused before it reaches a
title, rather than handed over in an encoding the title was not written for.
Emoji, the extension syllables (`똠`, `햏`), Latin-1 letters and conjoining jamo
are refused; KSC5601's 2,350 syllables, its jamo and symbols, and printable
ASCII pass. The rule is `backend.EncodeKSC5601`, shared by every platform: a
lead or trail byte below `0xA1` is refused.

Only Host commits are checked; a title still reads whatever its saves hold. A
name an earlier build stored with such a syllable reads back unchanged, but a
whole-field commit that keeps the syllable is refused, so the syllable must be
removed from the dialog's text before the edit is accepted.

## Validation

The [2026-09-23 exception audit](history/testing.md#text-input-audit-2026-09-23) records five
additional defect categories, their authored reproductions, supported-route
coverage and an acceptance matrix that separates unsupported and unreached
editors from regressions. All five categories have fixes and ordinary Go
regression tests, including restoration, nested ownership, callback
interruption, consumed versus unconsumed retries, and mixed Host/keypad UTF-16
limits; the audit's resolution section lists the remaining manual acceptance
limits.

Go tests exercise adapter constraints, stale fields, UTF-16 limits,
notifications, concurrent SKT editing and rendering, and an authored MIDlet
packaged as a JAR (`internal/platform/skt/testdata/text-input.jar`). A WebSocket
test submits Korean and ASCII text, rejects an oversized edit, parks and resumes
the game, rejects the old edit token, and reads back the retained guest value.
An authored WIPI-C Clet drives a Host composition through its real
`handleCletEvent` entry point and `MC_imHandleInput` import, covering Korean,
English and digits in one completed EUC-KR string across all keypad modes,
strict encoding, stale mode snapshots, output byte lengths, atomic capacity
rejection and retry. The integrated normal and debug suites, internal race
checks and `go vet ./...` pass, and CLI and embedded-server binaries build in
debug and release profiles. An authored SKT native package
(`internal/testfixture.SKTCletArchive`) drives the native route through the
shared session: a field that takes keys in its event entry and one that takes
them on its frame timer, no offer before the input method is used, a carrier the
program does not route withdrawing the offer until a key, encoding refusals, a
spent snapshot, digits the player types completing through the input method, and
an offer right after a quick load (`TestTextInputSKTClet*`).

Dated end-to-end records, each with its artifacts in an ignored directory:

- **2026-09-12, SKT MIDlet:** Chromium and WebKit pass a browser probe against
  the Go server and the authored MIDlet at a 390 by 844 viewport, verifying
  composition isolation, complete-string guest mutation, page reload/resume,
  stale-target rejection with draft preservation, guest length limits, and no
  leaked game-key events or page errors
  (`var/diagnostics/native-text-e2e-20260912`). Composition events are driven by
  automation; physical-phone keyboard and IME interaction still needs a person.
- **2026-09-12, KTF GForm:** a real archive passes the shared session and
  browser paths for its non-modal editor. A complete Korean composition updates
  the `GTextField` without hiding the form; the guest's FIRE listener reads the
  value, stores it and hides the form; reopening returns the accepted string;
  leaving settings with CLR persists the name across a new runtime session with
  the same save directory. Chromium and WebKit keep the game keypad and report no
  page errors (`var/diagnostics/ktf-native-browser-20260912`).
- **2026-09-12, LGT AOT:** an authored archive boots through the shared session
  and WebSocket. Its initializer resolves the Java tables, allocates a shell and
  text field, and invokes construction, attachment, visibility, length and focus
  calls. The tests reject an over-limit string and a character outside KSC5601,
  retry the same edit with Korean and ASCII text, and reopen the field to read
  the stored value. Chromium and WebKit pass composition isolation, game-key
  isolation, reload/resume, length rejection/retry and guest readback
  (`var/diagnostics/lgt-text-e2e-20260912`). The fixture draws no complete widget
  screen and has no guest key route that changes focus; those transitions are
  covered by separate runtime regressions.
- **2026-09-19, KTF Shell:** a regression reproduces a direct `TextBox` child
  opened without explicit focus. Authored tests cover complete-string commit,
  hidden and reopened shells, changed and restored children, replacement work
  components, ambiguous layouts, installed delta listeners and explicit-focus
  precedence. A local archive probe in isolated saves reached the reported name
  editor, committed a two-character Korean name, and used the guest's OK route to
  close the shell and continue gameplay; the isolated save held the submitted
  UTF-8 name. No renderer or key-dispatch change was added. `make test`
  (including Node tests), `make test-debug`, `go test -race ./internal/...`,
  `go vet ./...` and the debug server build passed
  (`var/diagnostics/ktf-shell-input-20260919/`). The probe did not recheck a
  physical iPhone keyboard; the user later confirmed that input works with the
  fix.

Specification: [ShellComponent](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lwc/ShellComponent.md)
and [ContainerComponent](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lwc/ContainerComponent.md).

The dated investigation below records how these boundaries were established.
Its earlier statements about unsupported targets are superseded by the support
description and validation above, and by later observations within the record.

<a id="lifecycle-widget-input-lifecycle-follow-up"></a>

## Widget lifecycle evidence (2026-09-12)

The native keyboard transport can edit a platform field only after the runtime
identifies an active target. A text component allocated during startup is not
necessarily an editor the guest is using. This investigation separates that
question from the mechanics of sending a complete string.

<a id="lifecycle-ktf-vendor-forms"></a>

### KTF vendor forms

A bounded recursive scan of the 543 library files then present found two
distinct archive hashes containing `com/ktf/kfc/GTextField` and `GMenubarForm`:

- `f07cbc7828280b886fdef164b7a1fe16fff107ea60e4086a4167b61a551aef87`
- `bfa8ec3524518147e1383ecb44560fb3b9fba72fdc3aa30e4c2fdc035e7cbfa9`

The marker scan is static evidence, not a method-call count. Three distinct
archives contain `GForm`, and 17 contain the unqualified `doModal` name, which
other dialog classes share and which therefore does not identify a KFC caller.

A fresh 400-tick run of the first archive constructs one form and one text
field, sets the maximum length, obtains the default listener and supplies IME
modes, at call sites `0x119bb5` through `0x119db9`. A 1,200-tick route with nine
FIRE inputs advances beyond the opening notice into the story; extending it to
2,500 ticks with more soft-key and confirmation input still shows story
dialogue. Neither route resolves or calls `doModal`. The second archive's sampled
run loads `GMenubarForm` without constructing a text field. These observations
establish startup allocation, not an active editor.

The ignored `var/diagnostics/widget-lifecycle-20260912` directory holds the
hash/path map, per-route JSON diagnostics, logs and frames. Each route used its
own save directory under `var/savedata/widget-lifecycle-20260912`; the library
and original saves are unchanged.

<a id="lifecycle-suspension-boundary"></a>

### Suspension boundary

`runtimeGFormDoModal` returns zero immediately and counts that no dialog was
shown. Its text-field constructor stores contents and a maximum but does not
retain a parent form. Exposing that allocated object through the Host would edit
an inactive field and still leave the guest's modal call closed.

The execution context decides the design. A KTF guest worker can park with its
nested call stack intact, as `sleepCurrentWorker` does. A client-thread callback
cannot: the Host is synchronously inside that callback, and `client.run`
protects the guest state, so blocking there would stop the serialized Host loop
from processing the text request needed to unblock it. A modal implementation
must first establish whether the caller is a worker or a client-thread callback,
then preserve dismissal, cancellation and shutdown behavior for that context. The
sampled routes never reached the modal call, so they cannot select that design.
This is an implementation and caller-validation task; the two archives exist, so
it is not an unavailable-archive blocker.

<a id="lifecycle-lgt-ownership-requirement"></a>

### LGT ownership requirement

An LGT adapter needs the focused text component and its visible shell, with
normal field constraints and notification behavior. Any shown-shell or focus
handle must also be a Java collection root; storing a numeric guest handle on
the Go side does not preserve the guest object. Parent/child edges must stay
traversable, and hiding, removing or refocusing a component must invalidate its
pending Host edit. The LGT adapter records this ownership, rejects stale edits
and traces the shown/focused graphs; its authored lifecycle regressions cover
callback reentry, exact inherited signatures and parent-cycle rejection. The
sampled archive routes above did not establish a real Java text-entry route.

<a id="lifecycle-whole-field-replacement-and-ime-callbacks"></a>

### Whole-field replacement and IME callbacks

The WIPI [InputMethodHandler contract](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lcdui/InputMethodHandler.md)
describes a per-key automaton. Its
[listener](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lcdui/InputMethodListener.md)
receives processed characters and an insertion, replacement or deletion mode.
The handler has no initial-field-text or replacement-range argument, so a Host
that composes a complete string cannot assume that mode zero replaces the whole
field; a listener editing its current composition fragment could apply the
replacement a second time after the Host has stored the complete value.

Whole-field input therefore uses the component's string-setting contract and
never synthesizes per-key automaton events with the complete string. An
explicitly installed application listener needs a separately established
contract; refusing that target is a limitation, not evidence that custom
listeners work. The KTF and LGT adapters refuse installed delta listeners and use
the string-setting contract for supported fields.

Per-key edits are that separate contract, and on KTF three titles that build
their own handler settle it: an insertion appends the whole array, a deletion
cuts `len` characters off the end, and a replacement is ignored. That makes a
title's own field a Host target. While the field is open — a listener registered
on a handler the title built, with the presses the card receives reaching it —
the Host offers an append-only edit, and a commit reaches the listener as one
insertion per character, each carried by a key press the title forwards, so the
title applies its own limit and redraw to each. A field closes on a press it does
not see, except a field the title rebuilt while handling a key, which stays open
until it takes a key itself. The open field is a runtime object, so it survives a
quick load. A text component whose handler the title gave its own listener stays
unsupported, as above. See
[the KTF history](history/ktf.md#implementation-three-members-a-sweep-that-held-keys-down-reached).

<a id="lifecycle-separate-native-input-method-callers"></a>

### Separate native input-method callers

The historical LGT authentication route that accepts a numeric name is a WIPI-C,
game-owned widget. It calls `MC_imHandleInput` with completed and composing
buffers, then owns its text, deletion, mode and drawing. That route exposes no
Java LWC object, parent shell or focus handle, so it cannot validate a Java
editor adapter. Conversely, the historical Java `TextComponent.imHandler`
initialization failure remains evidence of Java widget demand even when newer
bounded routes do not reach the input screen. These are distinct input
contracts; see the
[input-method and widget history](history/lgt.md#implementation-character-input).

<a id="lifecycle-a-response-dependent-lgt-editor-route"></a>

### A response-dependent LGT editor route

Static analysis of local archive SHA-256
`735a579d82ac53bb205b04250ce44586c6d9375e64c2d6f3324796b3ae24d031`
identifies a Java editor branch selected by response value 1101, read from a
connection's input stream. The connection factory call uses static import 46
(`0xb8 / 4`) from the module's static-method table; the class metadata resolves
it to `org/kwis/msf/io/URL.find(String)`. Its address constant uses the
`BillSocket` scheme; the endpoint is not needed for this finding and is omitted.

The runtime implements that factory with `javaURLFind`, which throws
`SchemeNotFoundException` instead of opening a connection, so this branch cannot
establish real editor acceptance offline. Focused socket-refusal and
exception-inheritance tests pass. This is static reachability evidence, not a
recorded editor execution, and it does not rule out other entry paths in the
same archive. A synthetic response would test a different acceptance path and
must not be counted as real service behavior. See [network limitations](network.md).

<a id="lifecycle-ktf-text-field-virtual-dispatch"></a>

#### KTF text-field virtual dispatch

A KTF archive calls `TextFieldComponent.setString(String)` with a `GTextField`
receiver when opening the name editor from its settings menu. `GTextField` must
inherit `TextFieldComponent`: inheriting directly from `TextComponent` placed a
constructor in the virtual slot that caller resolves, and the call failed while
decoding constructor arguments, before the editor could open.

The corrected hierarchy keeps the parent method slot, and a regression test
checks the generated receiver table against the parent declaration. A fresh-save
replay of the settings route reaches `GTextField.setString` without the
argument-decoding failure. The captured display stays on the settings menu;
editor visibility, Host text submission and persistence were separate acceptance
requirements. General, debug, race and vet checks pass.

<a id="lifecycle-ktf-non-modal-form-visibility"></a>

#### KTF non-modal form visibility

The same settings route attaches one `GTextField` through
`ContainerComponent.addComponent`, installs its `EventListener`, sets the field
text and invokes `GForm.show()`. It makes no focus call. The WIPI
[FormComponent contract](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lwc/FormComponent.md)
describes explicit child focus traversal, so the runtime does not infer that
every shown field is active. It selects a Host target only for this observed
vendor shape: an explicitly shown form with exactly one directly attached,
listened `GTextField`. A missing listener, multiple eligible fields, removal or a
later lifecycle change prevents or invalidates editing.

The field's `EventListener` receives `eventNotify` with the WIPI `KEY_NOTIFY`
tuple. On FIRE it reads `GTextField.getString`, copies the value into guest
state, calls `GForm.hide` and returns false. Host composition therefore changes
only the field value and leaves the form shown. The form owns each key press
through its matching release, even if the callback hides or replaces the form. A
release whose press belonged to the underlying card stays there, so the release
after the opening FIRE cannot immediately dismiss the new form.

Authored tests cover ambiguous and null children, listener and child changes,
value restoration, hide and re-show generations, key ownership and replacement
forms. A fresh-save session replay accepts a complete Korean string through the
guest FIRE callback and reads it back after reopening. Chromium and WebKit pass
the same whole-string composition and keypad-isolation route without page
errors. This support adds no complete KFC renderer and does not change the
synchronous `doModal` stub.

<a id="lifecycle-ktf-name-persistence-acceptance"></a>

#### KTF name persistence acceptance

Closing the runtime while still inside settings does not persist a newly accepted
name: the archive writes its settings when the user leaves that menu with CLR.
The acceptance probe commits Korean text, confirms with FIRE, leaves settings
with CLR, closes the session, and starts a new session against the same isolated
save directory; reopening the name field returns the accepted text. This verifies
guest-owned persistence, not only reopening an editor in the same runtime. The
normal, debug, internal race and vet checks pass.

A key press also captures the selected field, child-list revision and event
listener state. Replacing the listener during its press callback consumes the
old press's trailing release without delivering it to the replacement listener;
the same form and visibility generation alone are not sufficient ownership. A
regression reproduces this callback replacement.

Release ownership is cleared before the guest release callback runs. If that
callback hides the form and throws, the next card press is still delivered
normally; an error must not leave the previous press captured. A regression
covers this callback failure.

<a id="lifecycle-remaining-synchronous-modal-references"></a>

### Remaining synchronous modal references

The first archive's static helper at `0x1136cc` constructs a `GMsgBox` from two
strings and fixed bounds, calls `doModal` at `0x113716`, discards its return
value and returns. It does not construct or read a `GTextField`. In the saved
image its body pointer occurs in its method record, and that record is listed in
its class method table; the bounded scan found no ordinary direct caller or
separate symbolic reference, which does not exclude external or native calls.

The other decoded modal path constructs a label-based `DialogComponent`. The
second archive carrying `GTextField` has no literal `doModal` reference. These
observations do not establish a reachable synchronous text-entry route; the
proven settings editor uses non-modal show and a guest `EventListener` instead.

A future modal investigation needs a concrete caller or external callback to
exercise, with a PC-hit trace at the helper entry. Invoking the helper directly
would verify modal mechanics but not user-visible reachability. Synchronous modal
support is unimplemented, and current evidence does not make it a prerequisite
for the verified name-entry path.
