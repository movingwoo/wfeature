# KTF compatibility follow-up

This note preserves the evidence needed to resume two compatibility investigations
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
`Component.setFocus` and `FormComponent.setFocus`. That object, its current string,
and its constraints must be captured under the client run lock. A later whole-text
commit must reacquire the lock and reject the edit if either the focus object or
the captured string changed.

The [text component
specification](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lwc/TextComponent.md)
defines constraints 0 through 5 for unrestricted, numeric, numeric password,
email, URL, and phone input. A text box is multiline; a text field is
single-line. The maximum length counts Java UTF-16 code units. The LWC input
listener receives a whole replacement through
`notifyTextChanged(char[], int, int)` with replacement mode zero. A vendor text
field instead reports the completed change through `textChanged(GTextField)`.

The vendor modal path has an additional lifecycle gap: its text field is not
associated with the form's active focus, and `doModal` returns synchronously.
There is therefore no live vendor editor for a Host to commit to after the call.
That modal lifetime needs its own design before the common snapshot-and-commit
adapter can support it honestly.
