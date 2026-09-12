# SGS vector arithmetic evidence

The three vector services consume `(destination, scalar, count, operation)`
for `0xb4`, `(destination, source, count, operation)` for `0xb5`, and
`(destination, leftSource, rightSource, count, operation)` for `0xb6`.
They produce no stack result. The original dispatchers are at `0x41baf0`,
`0x41bdd0`, and `0x41c110`; their twelve-entry helper tables are at
`0x4949d8`, `0x494a08`, and `0x494a38`.

Operations 0 through 11 are assignment, addition, subtraction, multiplication,
division, remainder, AND, OR, complement, XOR, arithmetic right shift, and
left shift. Assignment and complement use the scalar/source operand; the
three-address form uses its left source and does not read right-source elements.
Arithmetic truncates to signed 16-bit words. Division widens both operands
before dividing, so minus 32768 divided by minus one wraps to minus 32768.
Shift counts use the low five bits, matching the original word instructions.

Each element reads its operands immediately before writing its destination.
Overlapping ranges therefore observe earlier writes; they do not use a source
snapshot. The scalar division/remainder helpers at `0x41b9a0` and `0x41b9e0`
check a zero divisor even when count is zero or negative. Vector divisors are
checked only when their element is reached. Earlier results remain written if
a later element divides by zero.

The host validates every accessed span before mutation and reserves execution
work for the complete loop. This deliberately prevents partial writes on a
range or work-limit failure, while retaining partial results on an arithmetic
fault. Nonpositive counts access no elements; unused source spans need no
element reads. Invalid operation selectors fail explicitly instead of indexing
outside the original native function table.

An ignored local diagnostic executed the original helpers on 5,760 authored
cases and compared all resulting words and arithmetic failure states against
guest-bytecode execution in Go. It covered all 36 service/operator combinations,
five overlap layouts, counts minus one/zero/one/four, and eight scalar values
including signed extremes and shift boundaries. All cases matched after fixing
the empty-count scalar zero-divisor check. No original executable or helper code
is bundled. Tracked tests cover the operations, both address banks, aliasing of
either source, actual instruction dispatch, arithmetic overflow and partial
faults, destination bounds, stack consumption, and atomic work exhaustion.
