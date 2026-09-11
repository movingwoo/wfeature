# SGS resource formatting and diagnostic trace

The original runtime has two formatting protocols. Numeric formatting accepts a
fixed number of signed words. String formatting substitutes one resource string,
reusing that same string for every `%s`. Neither protocol calls a general C
variadic formatter. The function at `0x44d8e0`, previously mistaken for one, is a
bounded byte search used to locate NUL within a requested precision.

This document describes contracts observed by inspecting the original runtime.
It contains no original implementation, font or resource data. Native addresses
identify evidence locations only.

## Stack arguments and destinations

Arguments below are ordered oldest first, with the last argument on top. Each
service consumes exactly these arguments and returns no operand-stack value.

| Opcode | Original entry | Arguments | Destination |
|---|---|---|---|
| `0x89` | `0x41a050` | destination resource, format resource, string resource | Destination resource |
| `0xf0` | `0x41db80` | format resource, signed word 1, signed word 2, signed word 3 | Host diagnostic trace |
| `0xf1` | `0x41dc10` | format resource, string resource | Host diagnostic trace |

For `0x89`, the three IDs are read before formatting starts. A temporary result
is formed before resizing or writing the destination. Consequently destination
aliasing either source must preserve the input until the entire result is ready.
The resize request includes the final NUL. Existing resource allocation policy
may retain a slightly larger capacity; capacity and string length are distinct.

For `0xf0`, the three signed words are sign-extended to 32-bit arguments and
passed to the shared numeric formatter at `0x41a500`. The conversions are `d`,
`x`, `X` and `c`; `s` is not a numeric conversion. Hexadecimal formatting of a
negative word uses its sign-extended 32-bit representation. Unknown conversion
characters are copied without the preceding percent sign and consume no numeric
argument. The original argument-count check has an off-by-one boundary; a safe
host must reject a conversion that exceeds the supplied three words.

For `0xf1`, the string formatting loop duplicates `0x89`, but its result is sent
to `0x41daf0` instead of a guest resource. That routine prefixes the message with
`Trace: ` and appends it to the original PC player's diagnostic text control
through `0x40a360` and `0x406630`. It neither draws on the guest framebuffer nor
opens a guest dialog. Its request for a 100-byte string buffer is a minimum
capacity request, not a 100-byte truncation. In this project, the corresponding
output belongs at the backend logging boundary, with normal build-profile
filtering and without stdout calls in the execution core.

## String conversion protocol

Both `0x89` and `0xf1` scan the format as bytes until NUL, preserving EUC-KR bytes.
They have a single substitution conversion, lowercase `s`. Each `%s` begins at
the start of the same source resource; the source cursor does not carry over to
the next conversion.

| Syntax | Behavior |
|---|---|
| `%s` | Copy the source string up to NUL |
| `%5s` | Pad on the left with spaces to at least five bytes |
| `%-5s` | Pad on the right with spaces |
| `%05s` | Pad on the left with zero bytes represented by ASCII `0` |
| `%-05s` | Left alignment takes precedence over zero padding |
| `%%` | Emit one percent sign |
| `%d`, `%x`, `%c` | Emit the conversion letter; do not substitute the source |
| `%+s`, `%#s` | `+` and `#` are unknown conversion characters, so the remaining `s` is literal |

Only `-`, `0`, decimal width digits and `.` reach special parser branches. In
particular, numeric-format sign and alternate-base flags do not apply to this
string protocol. Width measures bytes, not Unicode characters or glyph cells.
Output truncation can therefore split a multibyte character; a host should keep
the resource bytes intact and decode only when presenting text.

## Precision: a bounded compatibility divergence

The original string formatter contains a non-advancing precision branch.
The dispatch maps `.` to `0x41a126` (`0x41dcd7` in the trace copy). That branch
tests the current dot for a decimal digit before advancing, fails the test,
moves its cursor back by one and returns to a loop that advances to the same
dot. The original character-class table marks dot as punctuation (`0x10`), not
digit (`0x04`), confirming the lack of progress. The shared numeric formatter
has the same pattern at `0x41a5d5`.

The host deliberately uses bounded conventional precision instead of reproducing
this hang, consistently with its existing numeric formatter. `%.3s` copies at
most three source bytes; `%.s` copies none; `%.0s` copies none. Width still pads
the shortened string, and the string protocol retains zero-padding when selected.
This is a documented compatibility choice, not evidence that the original
runtime correctly implements precision.

## Bounds and safe implementation requirements

All three original services cap the temporary result at **255 bytes plus one
NUL**, testing the cap after every appended byte. Reaching the cap stops further
formatting. Embedded NUL in a format or source terminates that input.

The original temporary-output cap does not make its input access safe: it uses
unbounded string scans, unchecked resource pointers and overflowing decimal
width accumulation. A host implementation must validate resource IDs and input
termination, bound format scanning and charge guest work before searching long
resources. Width and precision may saturate above the output cap without
allocating large padding buffers. Every loop must make progress.

Resource writes must happen only after successful formatting and checked resizing.
Invalid stack arguments, invalid resource IDs and unterminated resources must not
partially change a destination. Pure formatting produces bytes and does not send
messages or perform host I/O; the trace services choose the logging destination.

Focused acceptance cases include repeated substitutions, destination/source
aliasing, unknown conversions, left/zero-padding precedence, bounded precision,
truncation through an EUC-KR byte pair, huge widths, resource termination, stack
underflow and diagnostic formatting without framebuffer presentation.
