# KTF gameplay QA, 2026-09-20

This investigation covers a rope-scrolling report, two additional-content
prompts, and four reports of excessive gameplay speed. Baseline revision is
`5a9321f`. Archive identities are SHA-256 prefixes. Real archives and probe
output remain ignored; original archives and user saves are unchanged, and all
writes use `var/savedata/ktf-qa-20260920/`.

## Missing additional content

Archive `689c491586b9` shows the additional-content offer after 400 CLI
service rounds. Accepting with key 1 and advancing 200 rounds displays the
network-connection failure. The runtime reports missing `Config.dat` and
`MixTable`; neither exists in the outer package or the payload JAR, and a
similarly named menu resource is not the required data file.

The three packaged `ZipTable` resources independently prove the content is
missing. Each has a little-endian 16-bit count followed by nine-byte records:
container number, 32-bit offset, 16-bit packed length, and 16-bit unpacked
length. The maximum offset plus packed length for each container gives:

| Index | Records | Absent containers | Required packed bytes |
| --- | ---: | --- | ---: |
| `5/ZipTable` | 28 | `5/0`–`5/1` | 97,292 |
| `7/ZipTable` | 62 | `7/0`–`7/5` | 279,584 |
| `9/ZipTable` | 29 | `9/0`–`9/3` | 166,375 |

All twelve containers are absent from the payload JAR; their combined extent
is **543,251 bytes**. The outer package contains only the JAR, descriptor,
metadata, and icons. This repeats the historical completeness check against
the current archive rather than inferring completeness from archive size. The
title requires an original complete package; accepting a download without
supplying those bytes cannot establish gameplay.

Local evidence: `build/ktf-qa-273-{start,download}.{json,png,log}` where
present. The initial `start.log` contains an unsuccessful screenshot command;
the later live probe uses the supported `shot` command and captures the prompt.

## Packaged data rejected with existing saves

Archive `93d5b6b8ceb5`, save owner `PD005362`, contains five complete data
files — `char.dat` (45,677 bytes), `map.dat` (163,179), `mon.dat` (182,527),
`pattern.dat` (76,964), and `tile.dat` (155,061) — alongside a 64-byte
`prefs`. Each data file in the current debug save is byte-identical to its
packaged copy.

A fresh isolated runtime selects `ktf-subscriber-fallback`, reads all five
files, and reaches the menu, scenario selection, empty slots, character
selection, and initial attribute selection.

Copying the existing debug save to an isolated root reproduces the 600 KB
download offer. Only `prefs` is opened before the offer; the five data files
are not consulted. A second isolated copy that changes only `db/prefs` to the
packaged 64 bytes reaches the menu, with all progress files, including
`db/save0.dat`, still present. The subscriber fallback alone therefore does
not handle the existing saved receipt. The packaged record's SHA-256 prefix is
`2a702cdf2db4`; the rejected saved record's is `ab7dbef36a21`. Replacing the
whole preference record is not an acceptable production fix: a correction
must preserve user progress and distinguish receipt fields from settings.

Evidence: `build/ktf-qa-248-{copied,prefs-only}.{json,png,log}` and the
`menu`, `scenario`, `slot`, and `character` PNGs. Copied saves are under
`copied/PD005362` and `prefs-only/PD005362` in the isolated QA root.

### Receipt field diagnosis and correction

The guest decoder accepts the rejected record's encrypted checksum. Decoding
with its own substitution table and session identity yields marker 75,
settings 3 and 2, five zero 19-bit content lengths, two zero 32-bit fields,
and a final zero two-bit field. The packaged record has the same marker and
settings, lengths 45,633 / 163,135 / 182,483 / 76,920 / 155,017, and final
field 1. Each length is the corresponding file size minus its 44-byte footer.

Changing only those five lengths in an isolated receipt, retaining the final
field and all other bits, passes the download prompt and exposes the existing
level-one slot, so the unknown final field need not change. The decoder probe
is `build/ktf_qa_receipt_decode_test.txt` with log `build/ktf-qa-decode.log`;
the field-only comparison is `build/ktf-qa-248-fields-{menu,scenario,slot}.png`.

The production adapter applies the same field-only recovery on reads of
`db/prefs`, only for the identified compiled-image digest and recognized
subscriber accessor. Packaged receipt lengths must match all five resources;
saved overrides must match packaged bytes; deletion ledgers or read errors
prevent recovery. Existing settings and save slots are preserved, and startup
writes no replacement record. Other compiled revisions keep the existing
behavior until their private format is established.

### Production acceptance

The production CLI, without diagnostic overlays or pre-edited receipts, reads
a fresh copy of the failing user save, passes the prompt, exposes the existing
level-one slot, and loads it into a visible town scene. The route completes
2,508 service ticks without error. Captures are
`build/ktf-qa-248-production-{menu,slot,play}.png`; diagnostics and console
output are `build/ktf-qa-248-production.{json,log}`. Both the production copy
and the original user tree retain receipt hash prefix `ab7dbef36a21` and slot
hash prefix `fd1d01fb8ed7` after the run; no original receipt was deleted or
overwritten.

Authored regressions cover exact field preservation, checksum regeneration,
word-crossing bit order, ordinary save/restart, incomplete or deleted content,
changed data overrides, invalid records, read failures, and rejection of other
executables. The opt-in local archive recognition test passes. No original
archive or cipher table is included in the tests.

## Rope-scrolling reproduction

The retained server report at `2026-09-20T08:27:40.666Z` corresponds to
archive `db8ef04a6504`, save owner `PD006654`. It reports guest speed 1.00x,
4,168 rounds, 1,581 flushes, and 853 `Clet$CletCard.paint` calls; timers
account for 99% of measured service work. The page report supplies 212 key
edges from the last session, beginning at `08:27:05.874Z`.

The first replay passed page key codes directly to the platform and is invalid
as reproduction evidence. The corrected replay applies the Host's key
conversion, runs 50.002 seconds, advances guest time 50.048 seconds, delivers
all 212 edges, and records 999 flushes over 6,722 service calls. Captures
progress from opening dialogue to the tutorial map with a rope visible, but
not to the reported held-rope scene; the on-time clock does not establish
scrolling correctness.

Evidence: `build/ktf_qa_replay.go`, `build/ktf-qa-rope-events.json`,
`build/ktf-qa-rope-mapped.{json,log}`, and sampled
`build/ktf-qa-rope-mapped-NNN.png` frames. The corrected replay exits normally.

### Held-rope reproduction and rejected paint hypothesis

The manual replay must allow at least one service frame after releasing a key
before pressing the next; without that gap the game's input logic drops
actions. The route `build/ktf-qa-rope-route.txt` reaches the rope, grabs it
with two separate UP presses, waits two guest seconds, then climbs in two
350 ms segments, capturing every service frame. No original save is changed.

The stationary hanging segment reproduces the defect. Matching a fixed 30×10
pixel patch of the island above the player over frames 649–888 places it at
Y=114 or Y=130, with **40 transitions in two guest seconds**. Character and
terrain move together: a 16-pixel camera oscillation without input.

A diagnostic experiment suppressed all automatic paints after a timer directly
flushed its screen. It reduced paint calls to four startup calls, but the held
rope produced the same 114/130 coordinates and 40 transitions. Unsolicited
paint explains the four speed reports, not this defect, so the experiment was
removed from production; it is retained only under ignored build paths.

Evidence: `build/ktf-qa-rope-{before,after}.log`,
`build/ktf-qa-rope-{before,after}-frame-*.png`,
`build/ktf-qa-rope-camera.json`, and
`build/ktf-qa-rope-direct-flush-experiment.json`. The production memory probe
reuses the after-frame filenames; the camera coordinates are unchanged.

### Camera state and viewport mismatch

Snapshots at held-rope frames 651 and 657 differ in only 15 aligned words of
the relocated client image. The camera Y field at `0x1a2500` changes from 0 to
-16. Its map object has width 320 and height 304; the viewport object has
width 240 and height 320.

A hardware-style write watch identifies the camera smoothing store at
`0x11db9a` and the clamp store at `0x11dbe2`. The clamp first sets a negative
camera Y to zero; otherwise, when `cameraY + viewportHeight > mapHeight`, it
sets Y to `mapHeight - viewportHeight`, which is -16 for a 304-pixel map and a
320-pixel viewport. Smoothing followed by these two branches alternates the
visible camera position. This is guest code, not a fluctuating Host clock or a
screen-conversion offset.

A startup watch identifies viewport initialization at `0x139464`: the guest
copies width and height from its screen framebuffer record at offsets 8 and
12, and the platform supplies its default 240×320. This identifies the
geometry mismatch without establishing a different handset resolution.

Evidence: `build/ktf-qa-rope-image-{651,657}.bin`,
`build/ktf-qa-rope-watch.json`, and `build/ktf-qa-rope-viewport-watch.json`.
The image snapshots are ignored diagnostics, never bundled fixtures.

### Revision-scoped camera correction and acceptance

The WIPI [screen framebuffer contract](https://mirusu400.github.io/wipi-wiki/c-api/graphics.md)
returns the display's framebuffer and gives no rule for subtracting 16 pixels
because one map is shorter. The correction therefore preserves the selected
240×320 screen, framebuffer stride, graphics APIs, input coordinates, and save
format, and adapts the identified guest clamp instead.

Only compiled-image SHA-256
`9ba7fbd6552f8176dfe9411ba8815a33dcf463ff50418e9f8f92707244f99b9c`
qualifies, and the relocated clamp routine must also match its own digest
before one Thumb store is replaced in emulator memory. At that instruction,
negative camera Y stores become zero; nonnegative values, all registers and
flags, the horizontal clamp, smoothing and return path keep their behavior.
The on-disk archive is never patched, and unknown images or changed routines
receive no correction. This adapts guest logic; it does not claim that ARM
stores or WIPI framebuffer heights have different semantics.

The unchanged recorded route reaches the same rope, hangs, climbs twice, and
reaches its top. Tracking the same island patch gives:

| Route segment | Samples | Island Y | Position changes |
| --- | ---: | --- | ---: |
| Hang, two seconds | 240 | 114 | 0 |
| First climb, 350 ms | 42 | 114 | 0 |
| First pause, one second | 120 | 114 | 0 |
| Second climb, 350 ms | 42 | 114 | 0 |
| Second pause, one second | 120 | 114 | 0 |

The baseline hanging segment switches between Y=114 and Y=130 forty times.
The corrected route advances 61.415 guest seconds, flushes 1,280 frames, and
records one installed adaptation and 584 corrected negative stores. The player
moves upward while the island stays still, so rendering is not frozen.
Evidence: `build/ktf-qa-rope-camera-fixed.log`,
`build/ktf-qa-rope-camera-fixed.json`,
`build/ktf-qa-rope-camera-fixed-frame-*.png`, and
`build/ktf-qa-rope-camera-acceptance.json`.

Authored Thumb regressions run the original and corrected clamp for 400 steps
on maps shorter than, equal to, and taller than the viewport: the short-map
oscillation disappears and the other bounds and ordinary scrolling are
unchanged. Further tests preserve neighboring memory, every register and
condition flag; cover signed extremes; reject unknown executables and calls
from another instruction; and reject unmapped or wrapping field addresses.

## Excessive speed

The four reported archives are `1d18b7cc06d1`, `8fdd8f52b5bf`,
`f286e817508f`, and `3b72d4676d5d`. Existing timing code applies a 16.667 ms
frame-period floor and an instruction cost model; these are emulator policy,
not measured handset rates. The WIPI 1.2.1
[kernel specification](https://mirusu400.github.io/wipi-wiki/c-api/kernel.md)
specifies timer delays in milliseconds and no universal game frame rate, so
neither a global speed change nor a fourfold timing defect is claimed without
tracing each archive's actual loop.

### Asynchronous repaint cadence

The WIPI [Card contract](https://mirusu400.github.io/wipi-wiki/java-api/org/kwis/msp/lcdui/Card.md)
distinguishes asynchronous `repaint` from synchronous `serviceRepaints`, and
the [graphics contract](https://mirusu400.github.io/wipi-wiki/c-api/graphics.md)
queues `MC_grpRepaint` rather than calling `paintClet` inline.

The Host respected a live worker calling `serviceRepaints` but not one calling
only `repaint`, so it painted again while the worker slept. The C entry queued
an event only for a guest-owned event loop and dropped requests in
Host-driven sessions; automatic Host painting masked that omission.

Asynchronous Java requests now give their live worker the same card
ownership. C requests set the pending flag in both event-loop modes. A C timer
requesting repaint retains ownership only through a rearm of the same record
and callback; timer cancellation, callback replacement, worker exit, and
another visible card leave automatic fallback eligible. No global speed
multiplier or timer delay changed, and bootstrap-only automatic painting
remains supported.

A manual-clock probe runs 1,200 session ticks with the same input schedule:

| Archive | Paint calls before | After | Repaint requests after | Guest seconds after |
| --- | ---: | ---: | ---: | ---: |
| `1d18b7cc06d1` | 670 | 143 | 137 | 10.660 |
| `8fdd8f52b5bf` | 1,200 | 244 | 239 | 19.200 |
| `f286e817508f` | 1,035 | 157 | 150 | 18.012 |
| `3b72d4676d5d` | 851 | 401 | 401 | 16.092 |

The extra initial calls precede the first worker/timer ownership. Stages
differ because paint can advance application state, so before/after images are
not paired speed measurements. This proves removal of unsolicited paint, not a
handset-equivalent speed for all four campaigns. Probe and outputs:
`build/ktf_qa_timing_probe.txt`, `build/ktf-qa-speed-baseline.log`,
`build/ktf-qa-speed-after.log`, and the per-archive diagnostics JSON files.

Authored regressions reproduce 100 world steps from one asynchronous request
before the correction. A Thumb callback requests repaint and rearms a 120 ms
timer through the actual platform stubs; repeated idle paint service does not
advance the world. Cancelling that timer restores fallback even while an
unrelated timer remains queued.

### Gameplay cadence after correction

A second manual-clock route runs 6,000 ticks, pressing OK every 120 ticks and
releasing two ticks later. All four archives reach visible gameplay. Deltas
over the last 1,200 ticks, excluding startup:

| Archive | Guest seconds | Repaint requests | Paint calls | Paints per second |
| --- | ---: | ---: | ---: | ---: |
| `1d18b7cc06d1` | 10.667 | 133 | 133 | 12.47 |
| `8fdd8f52b5bf` | 19.200 | 240 | 240 | 12.50 |
| `f286e817508f` | 16.934 | 240 | 240 | 14.17 |
| `3b72d4676d5d` | 15.995 | 399 | 399 | 24.95 |

Each requested repaint produces exactly one paint in these windows. The two C
loops declare 70 ms and 40 ms gameplay timers respectively. This establishes
guest-requested pacing at default speed without the former extra Host frames;
it does not measure graphics-call cost on a physical handset. Evidence:
`build/ktf-qa-play-window.log`, `build/ktf-qa-play-*-window.json`,
`build/ktf-qa-play-*.json`, and the four `build/ktf-qa-play-*-5999.png`
captures.

## Validation boundary

The missing-content question is resolved for the identified archive. The
receipt correction preserves existing progress and loads the saved slot into
play. All four speed cases reach gameplay with exactly one paint per requested
repaint. The rope route reproduces the old oscillation and verifies stable
hanging and climbing after the revision-scoped correction.

`make test`, `make test-debug`, `go test -race ./internal/...`, and
`go vet ./...` pass with the final implementation; the Node gate has 226
passing tests. The opt-in real-archive receipt recognition and authored
compatibility tests also pass. Final gate logs are
`build/ktf-qa-final-{test,debug,race,vet}.log` and
`build/ktf-qa-final-compatibility.log`; the speed acceptance rerun is
`build/ktf-qa-final-speed.log`. `git diff --check` passes.

These are local shared-session/CLI measurements with isolated saves. They do
not establish physical-handset performance or support for unidentified
compiled revisions, and missing game data still requires a complete original
package. No server restart, real-phone test, commit, or publication was done.
