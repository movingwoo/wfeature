# ARM execution core

`internal/armcore` is a pure-Go ARM interpreter shared by KTF and LGT. It models
the user-mode execution needed by supported archives, not a complete ARM system.
Instruction investigations, optimization decisions, profiles, and measurements
are retained in [ARM history](history/armcore.md).

## Execution and threads

`Core` owns shared guest memory. Each `Thread` owns `r0`–`r15` and CPSR.
Execution is serialized per instruction quantum. At SVC, the core saves the
thread context and releases its execution lock before calling the runtime.
Nested calls derive a temporary context, pass arguments in registers and a
bounded stack tail, and preserve the outer registers on return.
`Thread.LiveContexts` includes every active derived call as well as the
logical thread's own context. At a parked execution boundary, collectors must
scan all of these contexts: an object address can live only in a nested
call's register. Calls leave this set on return, error, or cancellation.

Selected aligned guest words can be registered as logical-thread state.
Nested calls inherit that state; unrelated threads keep private values. KTF
uses it for Java exception-handler heads.

Default quanta contain 1,000 instructions and the default run ceiling is
1,000,000, configurable through `CoreOptions`. Cancellation is checked between
quanta. A thread's `SetStepBudget` and `SetLimitHook` support bounded execution
windows and parking without discarding the suspended call stack. Derived calls
inherit the budget and hook. Without a renewing hook, exhaustion returns
`ErrStepLimit`.

## Memory and instruction boundary

Memory is sparse, little-endian, and 32-bit. Mappings independently validate
read, write, and execute permissions. Backing pages are committed on demand;
new storage reads as zero. Bounds, alignment, address overflow, and mapping
limits produce typed errors. Guest writes invalidate relevant decode caches.
Overlapping mappings are rejected when neither permission set contains the other.

The interpreter implements the supported ARMv4T data processing, transfers,
branches, interworking, multiply, status operations, and SVC instructions.
Observed BLX forms and selected Thumb hint encodings are also accepted. CP15 c7
cache/write-buffer maintenance is a no-op; unsupported coprocessor behavior
remains an error. Thumb IT blocks are not treated as hints.

Guest word/halfword data accesses follow the implemented ARM alignment rules;
instruction fetch and direct Host memory helpers remain strictly aligned.
Architectural exception modes, banked registers, interrupts, privileged
exception returns, and cycle-accurate timing are not implemented.

## Diagnostics and performance

`CommittedRegions` exposes committed spans for bounded scanners. Memory watches
record guest and Host stores separately. A Host hit's last guest PC is context,
not necessarily the instruction that performed the write. Cheat writes use
`WriteUntracked` so they do not appear as guest behavior.

`EnableProfile(interval)` samples by executed instruction. Stack walking is
best effort, bounded to 32 frames; distinct stacks are capped at 65,536, after
which leaf samples remain counted. `ResetProfile` starts a new observation
window. Sampling, watches, and diagnostics exist; older missing-feature lists
in the history are not the current interface.

Recognized loops can use optimized implementations while charging their full
guest instruction count. Unrecognized shapes retain interpreter execution.
`Backend` is the execution seam, and the interpreter is the conformance oracle.
Only the interpreter is registered by default. A future backend must agree on
registers, memory, stop reason, and retired instruction counts at every boundary.

Go uses the committed `cmd/*/default.pgo` profiles automatically. `make pgo`
regenerates them from explicitly supplied local LGT Clet, LGT Java, and KTF
archives and routes. See the Makefile's `PGO_*` parameters and
[profile regeneration evidence](history/armcore.md). Historical speedups apply
to their recorded machine and workload; they are not current performance claims.

## Validation

Authored ARM/Thumb programs test instruction semantics, nested calls, thread-local
words, memory permissions, self-modifying code, and instruction charging.
`internal/armcore/conformance` checks architectural expectations and compares
registered backends. Platform integration fixtures cover SVC and Java/ARM
reentry. Run [the standard gates](testing.md) after execution changes and use a
paired fixed-work benchmark before claiming a performance improvement.
