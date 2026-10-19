# ARM execution core

`internal/armcore` is a pure-Go ARM interpreter shared by KTF and LGT. It models
the user-mode execution needed by supported archives, not a complete ARM system.
Instruction investigations, optimization decisions, profiles and measurements
are retained in [ARM history](history/armcore.md).

## Execution and threads

`Core` owns shared guest memory. Each `Thread` owns `r0`–`r15` and CPSR.
Execution is serialized per instruction quantum. At SVC the core saves the
thread context and releases its execution lock before calling the runtime, so
a handler may wait for a Host event while another guest thread runs. Nested
calls (`Core.Call`) derive a temporary context, pass arguments in registers and
a bounded stack tail, and preserve the outer registers on return.
`Thread.LiveContexts` includes every active derived call as well as the logical
thread's own context. At a parked execution boundary, collectors must scan all
of these contexts, because an object address can live only in a nested call's
register. Calls leave this set on return, error or cancellation.

Selected aligned guest words can be registered as logical-thread state
(`RegisterThreadLocalWord`). Guest loads and stores see the running thread's
private value while Host inspection sees the shared word; nested calls inherit
the state and unrelated threads keep private values. KTF uses it for Java
exception-handler heads.

Default quanta contain 1,000 instructions and the default run ceiling is
1,000,000, both configurable through `CoreOptions`. Cancellation is checked
between quanta. A thread's `SetStepBudget` and `SetLimitHook` support bounded
execution windows and parking without discarding the suspended call stack:
returning nil from the hook grants the next window, and the hook may block.
A quantum can run past the budget, because a stand-in charges every
instruction of the loop it replaced; the hook is then called once for each
window used up and the remainder is carried into the next window, so hosts
that pace threads by renewals see what an interpreted run would have shown.
Derived calls inherit the budget and hook. Without a renewing hook, exhaustion
returns `ErrStepLimit`.

`SetFastSupervisorCall` installs a handler for supervisor calls that can be
answered inside the quantum, because a crossing costs more than a call that only
reads a value the platform already holds. It runs with the memory and execute
locks held, so it must not touch guest memory, re-enter the guest, or take a
lock either could be waiting behind; returning false leaves the call to the
ordinary handler.

### Checkpoint state

Quick save on the ARM platforms is built from three records, each versioned and
bounded:

- `CaptureState` / `NewCoreFromState`: mappings, committed pages (at most
  512 MiB), registered thread-local words (at most 4,096), and step counters.
  Decode caches and Host callbacks are omitted, and the platform must stop all
  execution and memory producers before capture.
- `CaptureRootThread` / `RestoreRootThread`: a logical thread whose execution
  lives in derived calls. Its instruction budget is checked against Host policy
  on restore.
- `CaptureCalls` / `InspectParkedCalls`: the derived calls below a logical
  thread, outermost first, at most 64. Every call must be parked at a
  supervisor boundary or inside the limit hook, and the platform completes the
  pending supervisor operation before execution resumes. A call parked in the
  limit hook resumes with the window overrun the live run would have carried;
  a record claiming more overrun than one quantum can report is refused.

## Memory and instruction boundary

Memory is sparse, little-endian and 32-bit. Mappings independently validate
read, write and execute permissions; loader writes may initialize mapped
read-only code. Backing pages are committed on demand, and new storage reads as
zero. Bounds, alignment, address overflow and mapping limits produce typed
errors. Guest writes invalidate the affected decode caches, so self-modifying
code is coherent. Overlapping mappings are rejected when neither permission set
contains the other.

The interpreter implements the supported ARMv4T data processing, transfers,
branches, interworking, multiply, status operations and SVC instructions, and
the ARMv5TE additions the handsets' cores have: BLX in both forms, CLZ,
QADD/QSUB/QDADD/QDSUB, and the signed halfword multiplies (SMUL, SMLA, SMULW,
SMLAW and SMLAL with their x/y halves). Saturation sets Q, which MSR can write
with the other flags. ARM `LDR pc` and `LDM` with the PC in the list interwork
on bit zero, as Thumb `POP {pc}` does. The rest of the miscellaneous encoding
space (BKPT, BXJ, ARMv6T2 MOVW/MOVT), LDRD/STRD and PLD fault as undefined.
Selected Thumb hint encodings are also accepted. CP15 c7 cache/write-buffer
maintenance is a no-op; any other coprocessor access is an error. Thumb IT
blocks are not treated as hints.

Guest word and halfword data accesses follow the implemented ARM alignment
rules; instruction fetch and direct Host memory helpers remain strictly aligned.
Architectural exception modes, banked registers, interrupts, privileged
exception returns and cycle-accurate timing are not implemented.

A run of guest instructions holds the memory lock exclusively and keeps the
last-mapping and page caches. Host reads (`Memory.Read`, and `Memory.Holds`,
which compares spans with guest memory where the pages keep them) share the
lock and write no cache: they read the last-mapping cache and find pages
through the page directory, two loads, not through the page cache, whose
lookup refreshes it. A debugger's reads beside the session's therefore cannot
tear either cache, and a read costs a shared lock rather than an exclusive one.
The exclusive lock was most of the 12% more instructions a tick that a light
KTF title, which has its sprites read a pixel at a time, retired than on 0.5.2.
Host and guest reads copy through one loop (`readPages`), and both validations
start with one check of the last-mapping cache (`lastMappingCovers`), small
enough to be made inline, before they scan the mappings; only an exclusive
holder refreshes the caches.

## Diagnostics and performance

`CommittedRegions` exposes committed spans for bounded scanners such as the
cheat engine. `Core.Watch` records guest and Host stores to watched addresses
separately (`WriteOrigin`), up to 4,096 distinct address and PC pairs
(`WatchHitsOverflowed` reports truncation). A Host hit's PC is the last guest
instruction that ran, which is context rather than necessarily the writer.
Cheat writes use `WriteUntracked` so they do not appear as guest behavior.

`AttachDebugger` installs a hook called with the guest stopped at a breakpoint
(`SetBreakpoint`) or after `StepOnce`. Attaching runs one instruction per
quantum, which keeps the check out of the inner loop of an undebugged run.

`EnableProfile(interval)` samples by executed instruction. Stack walking is
best effort, bounded to 32 frames; distinct stacks are capped at 65,536, after
which leaf samples remain counted. `ResetProfile` starts a new observation
window. Missing-feature lists in the history predate these interfaces.

Recognized loops can use optimized implementations while charging their full
guest instruction count. A recognizer accepts a body only when the stand-in
reproduces it exactly — accesses before the pointer advances they walk, values
the body reloads read from where it reloads them, and no destination over a
word the run treats as fixed — and declines everything else. Unrecognized
shapes retain interpreter execution.

Recognizers are asked in Thumb state only, and a loop that leaves from the middle
of its body — a conditional `bx lr` before the branch back — is no shape they
read. Looked at on 2026-10-19 and left so: one LGT Clet spends 88% of its guest
instructions in two such ARM loops of its own library, a byte fill that counts
an index up to a limit and the byte tail of its `memcpy`, and costs about 11 ms
of CPU a tick on the development Mac, inside a 16 ms frame. A static scan of the
162 LGT modules in the local corpus found those loops, and any other ARM loop of
that shape, in that title alone (two copies of it). Standing in for them would
put a recognizer call on every backward branch taken in ARM state, which every
ARM title pays for: the Thumb hook costs 2 to 6% of a run where it never fires,
of which keeping refusals in the decode entry won back about half on some
titles and nothing on others (see `fill_loop.go`). A report of the title running too slow is
what would change the answer, and the answer then is a stand-in for exactly
those two loops.

`Backend` is the execution seam, and the interpreter is the conformance oracle.
Only the interpreter is registered by default. A future backend must agree on
registers, memory, stop reason and retired instruction counts at every boundary.

### Profile-guided build

Go applies `cmd/server/default.pgo` and `cmd/cli/default.pgo` to every build of
those main packages without a flag. `make pgo` regenerates both from one merged
profile of four runs, because a profile that covers one path through the engine
leaves the others at the compiler's guess:

| Run | Driver | Ticks |
|---|---|---|
| an LGT Clet (Thumb), driven by a route | `TestLGTLoadCostProbe` in `internal/platform/lgt` | `PGO_LGT_TICKS`, 20,000 |
| an LGT AOT Java title (mostly ARM), driven by a route | `TestLGTLoadCostProbe` | `PGO_LGT_TICKS`, 20,000 |
| a KTF title | `TestSessionCostProbe` in `internal/webhost` | `PGO_KTF_TICKS`, 1,500 |
| an SKT title, for `internal/jvm`, which no engine run reaches | `TestSessionCostProbe` | `PGO_SKT_TICKS`, 3,000 |

`TestSessionCostProbe` presses a fixed key cycle (`WFEATURE_PERF_KEYS=mash`) and
encodes every picture and sound batch as the server does, so the encoder is
profiled too. The archives and routes are local files under `var/` and are given
on the command line (`PGO_LGT_CLET`, `PGO_LGT_CLET_ROUTE`, `PGO_LGT_JAVA`,
`PGO_LGT_JAVA_ROUTE`, `PGO_KTF`, `PGO_SKT`); the target refuses to run when one
is unset or missing. The optional `PGO_LGT_CLET_SAVE` and `PGO_LGT_JAVA_SAVE`
save directories decide whether a route profiles the scene inside a save or the
title screen a fresh boot stops at. They are copied into `build/pgo` first,
because the run plays and a profile taken against a save it is changing differs
every time.
Each run is profiled with `GOMAXPROCS=1`: on macOS the profiling signal
otherwise lands on `pthread_cond_signal` for most samples.

Tests do not see the profile, because a test binary has no `default.pgo` beside
it; an A/B of the profile itself uses `go test -pgo=<file>`. The measured gains,
the 2026-10-11 re-take and the reasons for each run are in
[the build-flag record](history/armcore.md#implementation-the-host-profiles-next-answer-was-a-build-flag-and-it-was-12).
Historical speedups apply to their recorded machine and workload; they are not
current performance claims.

## Validation

Authored ARM/Thumb programs test instruction semantics, nested calls,
thread-local words, memory permissions, self-modifying code and instruction
charging. `internal/armcore/conformance` checks architectural expectations and
compares registered backends. Platform integration fixtures cover SVC and
Java/ARM reentry. Run [the standard gates](testing.md) after execution changes
and use a paired fixed-work benchmark before claiming a performance
improvement.
