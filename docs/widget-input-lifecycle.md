# Widget input lifecycle follow-up

The native keyboard transport can edit a platform field only after the runtime
identifies an active target. A text component allocated during startup is not
necessarily an editor the guest is currently using. This investigation separates
that distinction from the mechanics of sending a complete string.

## KTF vendor forms

A bounded recursive scan of the 543 current library files found two distinct
archive hashes containing `com/ktf/kfc/GTextField` and `GMenubarForm`:

- `f07cbc7828280b886fdef164b7a1fe16fff107ea60e4086a4167b61a551aef87`
- `bfa8ec3524518147e1383ecb44560fb3b9fba72fdc3aa30e4c2fdc035e7cbfa9`

The marker scan is static evidence, not a method-call count. Three distinct
archives contain `GForm`, while 17 contain the unqualified `doModal` name; that
name also belongs to other dialog classes and does not identify a KFC caller.

A fresh 400-tick run of the first archive constructs one form and one text field,
sets the maximum length, obtains the default listener, and supplies IME modes.
The recorded call sites are `0x119bb5` through `0x119db9`. A 1,200-tick route with
nine FIRE inputs advances beyond the opening notice into the story. Extending
that route to 2,500 ticks with additional soft-key and confirmation input still
shows story dialogue. Neither route resolves or calls `doModal`. The second
archive's sampled run loads `GMenubarForm` without constructing a text field.
These observations establish startup allocation, not an active editor.

The manager's ignored `var/diagnostics/widget-lifecycle-20260912` directory
contains the hash/path map, per-route JSON diagnostics, logs, and frames. Each
route uses its own save directory under `var/savedata/widget-lifecycle-20260912`.
The library and original saves are unchanged.

## Suspension boundary

`runtimeGFormDoModal` currently returns zero immediately and records that no
dialog was shown. Its text-field constructor stores contents and a maximum but
does not retain a parent form. Merely exposing that allocated object through the
Host would edit an inactive field and still leave the guest's modal call closed.

The execution context matters before changing this behavior. A KTF guest worker
can park with its nested call stack intact, as `sleepCurrentWorker` already does.
A client-thread callback cannot: the Host is synchronously inside that callback,
and `client.run` protects the guest state. Blocking there would prevent the
serialized Host loop from processing the text request needed to unblock it.
A modal implementation must first establish whether the actual caller is a
worker or a client-thread callback, and then preserve dismissal, cancellation,
and shutdown behavior for that context. The sampled routes have not yet reached
the modal call, so they cannot select that design.

This is an implementation and caller-validation task. The presence of the two
archives means it must not be classified as an unavailable-archive blocker.

## LGT ownership requirement

An LGT adapter needs the focused text component and its visible shell, along with
normal field constraints and notification behavior. Any new shown-shell or
focus handle must also be a Java collection root; Go-side storage of a numeric
guest handle alone does not preserve the guest object. Parent/child edges must
remain traversable, and hiding, removing, or refocusing a component must
invalidate its pending Host edit. These are requirements for the executable
slice, not claims that the current runtime already satisfies them.

## Whole-field replacement and IME callbacks

The WIPI [InputMethodHandler contract](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lcdui/InputMethodHandler.md)
describes a per-key automaton. Its
[listener](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lcdui/InputMethodListener.md)
receives processed characters and an insertion, replacement, or deletion mode.
The handler has no initial-field-text or replacement-range argument. A Host
that composes a complete string therefore cannot assume that mode zero means
replace the entire existing field. In particular, a listener editing its current
composition fragment could apply the replacement a second time after the Host
has already stored the complete value.

Whole-field input needs the component's string-setting contract. It must not
synthesize per-key automaton events with the complete string. An explicitly
installed application listener needs a separately established contract; refusing
that unsupported target is a limitation, not evidence that custom listeners work.
The current follow-up reviews the KTF adapter against this distinction and uses
it as a requirement for the LGT adapter.

## Separate native input-method callers

The historical LGT authentication route that accepts a numeric name is a
WIPI-C, game-owned widget. It calls `MC_imHandleInput` with completed and
composing buffers, then owns its text, deletion, mode, and drawing. The existing
numeric implementation returns completed digits; that route does not expose a
Java LWC object, parent shell, or focus handle. It cannot validate a Java editor
adapter. Conversely, the historical Java `TextComponent.imHandler` initialization
failure remains evidence of Java widget demand even when newer bounded routes
do not reach the input screen. These are distinct input contracts; see the
[input-method and widget history](lgt.md).
