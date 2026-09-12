# KTF compatibility follow-up

This note preserves the evidence needed to resume compatibility investigations
without relying on a local working tree or an archive label.

## Native loading stall

The September 7 acceptance record names `var/games/KTF 1.1-BREW` as the old
corpus and records these SHA-256 values:

- `56d6865251cc6e3bb2e07065d5d460591478b8c7175e2798ce597d2534794cc3`
- `1d5831e42a8a41d4c875b81e2606e779c5559b051f536686a0bd08e20ac9e71e`
- `98db83c3bda6cdff6b1096374e8a2197fdf18445397bba7cdab24f2c0fb8d0db`

Files with all three hashes remain in that directory. Their contents are ordinary
WIPI packages with an `__adf__` descriptor and a Java archive, however; none is a
native package containing the `.mif` and `.mod` pair required by the BREW loader.
A recursive ZIP-entry scan of all 543 files currently under `var/games` found no
archive containing that pair. The canonical native-package acceptance probe skips
for the same reason.

Consequently, the previously recorded stall after eighteen resources could not be
reproduced. The saved evidence still says that those resources loaded, the target
surface remained empty, and no later trap or surface write explained where the
resource data should have been copied. A matching native archive is required to
trace the resource destinations and the blit source. The current files do not
support a loader change.

## Requested landscape packages

The two current archives requested for a 320 by 240 check are identified by these
SHA-256 values, independent of their local filenames:

| SHA-256 | `DisplaySize` | `ResizeType` | bytes |
| --- | ---: | ---: | ---: |
| `93d5b6b8ceb5cf4ce15784e369a7af9cd8ef7a49d6f15816d665aedadc487a4a` | `176*220` | `0` | 985530 |
| `df5f0261a2f9315a745d181a52eb76ea6a89a0ab4bd4554a78e19cca076f4012` | `240*320` | `0` | 1326663 |

All current copies of either requested package are byte-identical to those
archives. A scan of all 543 files in the local library found 296 readable WIPI
descriptors: 108 declare `176*220`, 184 declare `240*320`, one declares
`240*400`, and three omit `DisplaySize`. None declares `320*240`.

The available archives therefore provide no package evidence for automatically
rotating either title or overriding its declared handset. The CLI's explicit
`-screen 320x240` remains suitable for a diagnostic run, but selecting it in the
runtime would require a matching archive or handset trace that demonstrates the
landscape contract.

A fresh 400-tick diagnostic confirms that the override itself is executable. The
automatic runs report 240 by 320 for both archives. The first hash paints 258
flushes and 76630 non-black pixels in either automatic or forced mode; the second
paints 400 flushes, with 51795 non-black pixels automatically and 51340 when
forced to 320 by 240. Both forced runs produce a populated 320 by 240 frame
without a start failure. This proves that a person can inspect the landscape
layout; it does not identify that layout as the handset contract.

## Unconditional Jlet teardown

The [WIPI 1.2.1 Jlet lifecycle
contract](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lcdui/Jlet.md)
says that a transition to the destroyed state invokes `destroyApp`, and that a
true `unconditional` argument means an exception cannot prevent termination.
Previously, closing a KTF session stopped audio and guest workers without
invoking that callback.

The current canonical directory has 35 KTF WIPI archives after one package for a
different platform is excluded by content. All 35 declare a non-native
`destroyApp(Z)V` override. Existing runtime evidence also records a guest exit
through `Jlet.notifyDestroyed`, which reaches the same Host close path.

Session close now invokes `destroyApp(true)` while guest entry is still safe, then
stops audio and unwinds workers. A callback failure is counted in lifecycle
diagnostics but cannot refuse the unconditional teardown. The callback shares the
bounded Host-service instruction and time budgets used by other guest callbacks.
Its transition is marked before guest entry, so an exit, exception, exhausted
budget, or repeated close cannot enter it twice.

The regression fixture records the boolean callback argument in the Jlet object.
Its focused normal and debug tests prove that the callback receives true and that
workers still stop. The full KTF package tests pass in both profiles.

## Host-composed text

Guest focus is recorded in the runtime's `lwc:focus` object by
`Component.setFocus` and `FormComponent.setFocus`. `Session.TextInput` now captures
that object, its current string, and its constraints under the client run lock. A
later whole-text commit reacquires the lock and rejects the edit if the focus
object, string value, constraint, or maximum length changed. Guest subclasses of
an LWC text field or text box follow the same path.

The [text component
specification](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lwc/TextComponent.md)
defines constraints 0 through 5 for unrestricted, numeric, numeric password,
email, URL, and phone input. A text box is multiline; a text field is
single-line. The maximum length counts Java UTF-16 code units. The
[input-method listener
specification](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lcdui/InputMethodListener.md)
defines per-key insert, replace, and delete operations. It has no cursor or
range argument with which to describe an arbitrary whole-field replacement. A
vendor text field instead reports the completed change through
`textChanged(GTextField)`.

The adapter validates the Host transport, applies the active constraint, rejects
line breaks for a text field, and enforces the maximum without truncating a
composition. It stores the completed Java string as `setString` would and resets
the legacy keypad editor. A field with an explicitly installed
`InputMethodListener` is not offered to the Host because sending the completed
value as a guessed replacement operation could corrupt that listener's active
composition. Installing a listener after the Host takes its snapshot invalidates
the pending commit. Exact field snapshots do not detect a listener that starts
explicitly null, is installed, and is restored to the same null value before
commit; detecting that history requires a mutation generation.

The vendor modal path has an additional lifecycle gap: its text field is not
associated with the form's active focus, and `doModal` returns synchronously.
There is therefore no live vendor editor for a Host to commit to after the call.
That modal lifetime needs its own design before the common snapshot-and-commit
adapter can support it honestly.
