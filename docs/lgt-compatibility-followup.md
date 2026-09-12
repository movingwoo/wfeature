# LGT compatibility follow-up

This note records the compatibility decisions that followed the 2026-09-12
audit. It narrows what the API scanner proves, records the guest-stream fix,
and preserves the evidence from the current save and text-input investigations.

## Java API coverage follows dispatch

A Java member name is diagnostic metadata. It does not mean the runtime can
execute the member. Coverage now uses the same resolution rules as the call
path:

- Core Java interface functions count when the Java SVC table has a handler.
- The unnamed class entries in a class's static run count because the handler
  resolves them to the platform class object.
- A static method counts when its class, name, and descriptor select a method
  body. An unnamed static entry may also count when its descriptor leaves one
  unclaimed method body on that class.
- A virtual method first follows the baked vtable slot through the platform
  superclass chain. If no baked slot applies, it counts only when the module's
  metadata identifies a registered method body.
- The known Java auxiliary tables and the Java entry in the OEM table count as
  accepted auxiliary contracts, matching the Java dispatcher.

The same resolver supplies the method selected by execution and the name used
by tracing. An inherited baked method is consequently reported against its
declaring class while retaining the class on which it was dispatched. A named
metadata member without a body remains readable in diagnostics and remains an
unimplemented coverage result.

Regression tests cover inherited and unsupported baked slots, metadata members
with and without bodies, exact and descriptor-only static resolution, unnamed
class entries, and an accepted auxiliary table. Each case compares the
coverage result with the executable dispatch result rather than testing a
second list of expected implementations.

## What the import scan proves

The LGT import scan runs `Client.Start` and reports only the imports resolved by
the time that call returns. Some Java import stubs resolve on their first call,
and later virtual dispatch entries are built from class metadata rather than
appearing as startup import resolutions. A route can therefore add evidence
that the startup scan never sees.

This makes the report a lower bound on the linked surface reached during boot.
Zero reported gaps does not mean that every Java API used during later menus or
gameplay is implemented. Static metadata inspection and driven runtime traces
remain necessary for those paths. A startup failure narrows the result again:
only resolutions made before the failure are present.

A read-only scan of the local corpus processed 31 valid archives. Four Java
auxiliary entries that had been reported as gaps are accepted contracts and no
longer appear. The remaining startup result is C-library slot `0x32`, resolved
by four archives. Two other inputs failed archive validation before execution
because they had no application metadata. The scan did not modify an archive
or save tree.

## Guest stream mark and reset

An input wrapper can be constructed over an `InputStream` subclass supplied by
the application. The wrapper already forwarded reads to the subclass, but it
previously answered its own host-side `markSupported` flag and lost the
subclass's mark contract.

Opening a guest stream now inspects the inherited `InputStream` vtable slots for
`mark(int)`, `reset()`, and `markSupported()`. Wrapper calls invoke the guest
overrides when present. Reset discards bytes pulled from the guest's old cursor
before the next read. A block-read override is asked for only the bytes the
wrapper currently needs, capped by the existing pull limit; this keeps the
guest cursor at the wrapper cursor when mark is forwarded.

The regression test uses guest Thumb routines whose writes make the forwarded
receiver, read limit, and reset call observable. A separate test records the
length given to a guest block read, so future read-ahead cannot silently move
the guest past the wrapper's mark position. Existing byte-array mark/reset and
unsupported-reset tests continue to exercise the host-owned stream path.

This fills the specific forwarding omission reproduced by the audit. It does
not claim support for unrelated unobserved stream ABI slots.

## Save and menu evidence

The Java file implementation and the C file implementation use the same
per-title store. Existing driven evidence shows a Java title writing a small
settings file, closing it, and reading the same bytes on its next run. A second
title creates settings and empty slot files during play. These are end-to-end
proof of open, write, close, persistence, and read for paths that the titles
actually reached.

They are not proof of a progress save. One driven in-game menu has no save
entry; static analysis found its progress write behind application state rather
than another platform method. Another title reaches its save action and reports
that the current area forbids saving. Walking to a permitted area remains a
gameplay-route problem. The observed calls do not justify adding another file
or menu ABI slot.

Future save checks should start from the title's packaged help or key map, then
drive to a state where that key is active, and finally verify both the save-tree
bytes and a fresh-session load. Settings files and pre-created empty slot files
must be distinguished from saved progress.

## Native text input boundary

The browser can safely open an operating-system text editor only when the
runtime can identify one active editable field, snapshot its constraints and
contents, and commit through the field's normal notification path. The LGT
runtime does not currently retain enough state to do that.

The Java lightweight-component implementation stores text, maximum length,
input mode, child order, and visibility. It does not retain `focusNotify`, does
not identify an active child of the shown shell, and deliberately declines text
component key events because no component is drawn. Updating its stored string
from the host would bypass the title's listener and any validation or screen
transition tied to normal editing.

The WIPI-C UI component path also provides no target: class lookup and component
creation are refused, so no editable component exists. The WIPI-C input-method
calls transform one key into caller-owned completion and composition buffers;
they do not retain a field or a whole string that a later host commit can
replace. Text drawn and edited by application code is opaque guest state and
cannot be inferred from the framebuffer.

The LGT session therefore does not expose a text-input provider. The shared
host adapter should return `backend.ErrNoTextInput` for LGT, including for
application-owned custom text screens. Support can be added after a real title
proves a focus lifecycle, active component identity, constraint semantics, and
the callback or notification a completed edit must trigger. A Java lightweight
text component is the nearest candidate once those contracts are observed; a
custom UI is not a safe target without an application-specific editing API.
