# Keypad rapid fire

The keypad editor offers a local **연사** switch in any assignable cell. It is
not a handset key and is not part of keyboard mapping. Type1, Type2 and Type3
include it between Menu and Call by default; Type4 remains empty. Saved custom
layouts are preserved. Resetting a layout restores the new default, or the
switch can be assigned manually. Its placement uses the existing per-layout
storage; its mode is never persisted.

Type1 now uses the former Type2 default (named directions, OK and the full
number pad); Type2 uses the former Type1 numeric-direction default. Only the
shipped key tables are exchanged. Saved custom tables, size settings and the
selected type keep their existing storage keys and are not migrated.

Each activation cycles `off`, `manual`, `auto`, then `off`. Manual repeats only
held `5` and `OK`, including their keyboard bindings. Both may repeat together.
Auto initially waits for a key. Pressing `5` or `OK` toggles that key's
continuous repetition; releasing it does not stop repetition. Pressing the same
key again stops it. Pressing the other target releases the old target first,
then starts the new one, so only one target repeats at a time. Other keys
retain ordinary behavior. Entering auto with a target already physically held
does not select it; a new press is required.
Duplicate cells, multiple fingers and keyboard input share ownership of a key.
Sliding across a switch does not activate it.

The page schedules alternating press/release edges 50 ms apart, targeting ten
presses per second per key. These are ordinary session key messages, not the
handset's native long-press repeat event. No server protocol or core change is
needed. Late browser timers slow the cadence; they never generate catch-up
bursts. This is a nominal rate, not a real-time guarantee under browser load.

Input loss releases keys and resets to off: window blur, hidden/pagehide,
connection/session transitions, new or resumed games, keypad layout changes or
editing, and opening native text input. There is no timer while off, or while
manual has no eligible key held. Auto must be enabled again after returning.

## Server cost measured on 2026-09-19

Machine: Apple M1, darwin/arm64, Go 1.27.1, release profile. The authored
`canvas-skt.zip` fixture repaints the entire 240x320 image on every key edge.
Scale was 1. These measurements are not real-game or physical-phone acceptance.

The live probe uses Node's WebSocket client against a separate server process,
consumes frames, and sends alternating edges every 50 ms. Two rounds each
measure five seconds of off, automatic OK, and simultaneous manual 5/OK.
`ps` cumulative process CPU time measures the server alone, including socket
handling, guest execution, GC and PNG output. One fully occupied CPU core is
100%; these percentages are not fractions of all eight cores.

| Mode | Input messages per 5 s | Frames per 5 s | Server CPU, rounds 1 / 2 |
| --- | ---: | ---: | ---: |
| off | 0 | 0 | 0.60% / 0.60% |
| auto OK | 96 | 96 | 9.80% / 10.00% |
| manual 5 and OK | 192 | 191 | 13.19% / 18.39% |

There were no protocol errors. The achieved rate was 9.6 presses/s/key because
host timers ran slightly late. The input JSON measured 826 B/s for auto and
1,632 B/s for both keys, excluding WebSocket, TCP and frame traffic. At the
nominal rate these are 860 and 1,700 B/s. Input volume is small; a game that
redraws on every edge adds appreciable rendering work. This fixture did not
saturate one core, but the result cannot bound arbitrary guest callbacks or
four simultaneous games. No universal claim of negligible server load follows.

A separate fixed-work benchmark decodes input, dispatches through the actual
web handler, executes the fixture's callbacks/repaints and encodes each result
to PNG. Three runs gave medians of 1.181 ms per OK press/release pair and
2.359 ms per pair for both keys. Allocations were approximately 1.34 MB and
2.72 MB per operation, including framebuffer copies. JSON decode alone was
1.200 microseconds and 1,746 bytes per pair. These microbenchmarks exclude
socket scheduling and are not substitutes for the live process CPU result.

Reproduce from the repository root:

```sh
make server-release
node web/acceptance/rapid-fire-load.mjs build/release/wfeature-server 5
go test ./internal/webhost -run '^$' -bench BenchmarkRapidFire -benchmem -count=3
```

The live probe requires Node 22+ and `ps` on macOS/Linux. Generated fixture
copies and reports remain in ignored `var/games`, `var/ext`, `var/savedata`
and `var/acceptance`. No user saves are reused.

## Input verification

Deterministic Node tests check ten press/release pairs per second, mode
transitions while held, non-target keys, multiple holders, timer cancellation,
stored switch placement and ordinary `GameSession` packet serialization.
The Go socket test drives both keys at the nominal rate and receives the
ordered park acknowledgement after the input sequence.

Chromium and WebKit browser routes exercise editor assignment, keyboard and
pointer holds, automatic OK, focus loss, editing, and restart with mode off.
The WebKit one-second samples observed 10 manual and 9 automatic presses.
Headless Chromium on this machine observed only 3–5 presses in comparable
samples because timers were delayed. A separate blank, visible Chromium page
also needed 3,540.5 ms for twenty sequential 50 ms timers (nominally 1,000 ms),
reproducing the slowdown without the application or server. Browser assertions therefore establish
repetition, key identity, bounded rate and cancellation; the deterministic
clock test establishes the nominal cadence. Actual phone cadence and long
real-game play remain unmeasured.

```sh
PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs \
  node web/acceptance/rapid-fire.mjs build/release/wfeature-server chromium
PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs \
  node web/acceptance/rapid-fire.mjs build/release/wfeature-server webkit
```

The ordinary and debug test gates, internal race tests and `go vet ./...`
passed. The original Node suite passed 217 tests. The release server was rebuilt
with the new shell, and both browser routes also passed against embedded assets.

## Auto toggle correction

Auto now selects its sole target on a key press, rather than starting OK when
the mode is selected. Tests cover idle entry, both target directions, same-key
stop, duplicate downs, switching while keys are held, and mode/reset cleanup.
The browser route uses pointer and keyboard activation and checks both target
switches and both stops. The load probe still sends the same ordinary key
packets for an active OK stream; its earlier measurements remain historical
and were not repeated for this client-only correction.

Validation of the correction: `make test` passed, including 219 Node tests.
The rebuilt release server passed the updated Chromium and WebKit routes using
embedded assets. Both routes verified idle auto entry, continued repetition
after release, switching in both directions, and stopping each selected key.
