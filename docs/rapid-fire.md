# Keypad rapid fire

The keypad editor offers a local **연사** switch (`RAPID_FIRE` in
`web/rapid-fire.js`) for any assignable cell. It is not a handset key and is not
part of keyboard mapping; a game controller button can be mapped to it as a
page action (`padActions` in `web/gamepad.js`). Type1, Type2 and Type3 place it
in top-row column 7, between quick load and menu; Type4 leaves it unassigned.
Saved custom layouts are preserved. Resetting a layout restores the default, or
the switch can be assigned manually. Its placement uses the per-layout storage;
its mode is never persisted.

When rapid fire was added (2026-09-19), Type1 and Type2 exchanged their shipped
key tables: Type1 now has named directions, OK and the full number pad, and
Type2 the numeric-direction layout. Saved custom tables, size settings and the
selected type kept their storage keys and were not migrated.

Each activation cycles `off`, `manual`, `auto`, then `off`.

- **Manual** repeats only held `5` and `OK`, including through their keyboard and
  game controller bindings; both may repeat together.
- **Auto** waits for a key. Pressing `5` or `OK` starts that key's continuous
  repetition, and releasing it does not stop repetition; pressing the same key
  again stops it. Pressing the other target releases the old target first and
  then starts the new one, so only one target repeats at a time. Entering auto
  with a target already physically held does not select it; a new press is
  required.

Other keys keep ordinary behavior. Duplicate cells, multiple fingers and
keyboard input share ownership of a key. Sliding across a switch does not
activate it.

The page schedules alternating press/release edges 50 ms apart, targeting ten
presses per second per key. They are ordinary session key messages, not the
handset's native long-press repeat event, so no server protocol or core change
is involved. Late browser timers slow the cadence and never generate catch-up
bursts; the rate is nominal, not a real-time guarantee under browser load.

Input loss releases keys and resets the mode to off: window blur,
hidden/pagehide, connection and session transitions, new or resumed games,
keypad layout changes or editing, and opening native text input. No timer runs
while off, or while manual has no eligible key held. Auto must be enabled again
after returning.

## Server cost measured on 2026-09-19

Machine: Apple M1, darwin/arm64, Go 1.27.1, release profile. The authored
`canvas-skt.zip` fixture repaints the entire 240x320 image on every key edge.
Scale was 1. These measurements are not real-game or physical-phone acceptance.

The live probe uses Node's WebSocket client against a separate server process,
consumes frames, and sends alternating edges every 50 ms. Two rounds each
measure five seconds of off, automatic OK, and simultaneous manual 5/OK. `ps`
cumulative process CPU time measures the server alone, including socket
handling, guest execution, GC and PNG output. One fully occupied CPU core is
100%; the percentages are not fractions of all eight cores.

| Mode | Input messages per 5 s | Frames per 5 s | Server CPU, rounds 1 / 2 |
| --- | ---: | ---: | ---: |
| off | 0 | 0 | 0.60% / 0.60% |
| auto OK | 96 | 96 | 9.80% / 10.00% |
| manual 5 and OK | 192 | 191 | 13.19% / 18.39% |

There were no protocol errors. The achieved rate was 9.6 presses/s/key because
host timers ran slightly late. The input JSON measured 826 B/s for auto and
1,632 B/s for both keys (860 and 1,700 B/s at the nominal rate), excluding
WebSocket, TCP and frame traffic. Input volume is small; a game that redraws on
every edge adds appreciable rendering work. This fixture did not saturate one
core, but the result cannot bound arbitrary guest callbacks or four
simultaneous games, so no universal claim of negligible server load follows.

A separate fixed-work benchmark decodes input, dispatches through the web
handler, executes the fixture's callbacks and repaints, and encodes each result
to PNG. Three runs gave medians of 1.181 ms per OK press/release pair and
2.359 ms per pair for both keys, with about 1.34 MB and 2.72 MB allocated per
operation, including framebuffer copies. JSON decode alone was 1.200
microseconds and 1,746 bytes per pair. These microbenchmarks exclude socket
scheduling and do not substitute for the live process CPU result.

Reproduce from the repository root:

```sh
make server-release
node web/acceptance/rapid-fire-load.mjs build/release/wfeature-server 5
go test ./internal/webhost -run '^$' -bench BenchmarkRapidFire -benchmem -count=3
```

The load probe's second argument is seconds per phase (default 10, minimum 2).
It requires Node 22+ and `ps` on macOS or Linux. Generated fixture copies and
reports stay in the ignored `var/games`, `var/ext`, `var/savedata` and
`var/acceptance`; no user saves are reused.

## Input verification

Deterministic Node tests (`web/rapid-fire.test.mjs`) check ten press/release
pairs per second, mode transitions while held, non-target keys, multiple
holders, timer cancellation, stored switch placement and ordinary `GameSession`
packet serialization. Auto-mode tests cover idle entry, both target directions,
same-key stop, duplicate downs, switching while keys are held, and mode/reset
cleanup. The Go socket test drives both keys at the nominal rate and receives
the ordered park acknowledgement after the input sequence.

Chromium and WebKit browser routes exercise editor assignment, keyboard and
pointer holds, idle auto entry, continued repetition after release, switching
targets in both directions, stopping each selected key, focus loss, editing,
and restart with the mode off. The WebKit one-second samples observed 10 manual
and 9 automatic presses. Headless Chromium on the measuring machine observed
only 3–5 presses in comparable samples because timers were delayed; a separate
blank, visible Chromium page needed 3,540.5 ms for twenty sequential 50 ms
timers (nominally 1,000 ms), reproducing the slowdown without the application
or server. The browser assertions therefore establish repetition, key identity,
bounded rate and cancellation, and the deterministic clock test establishes the
nominal cadence. Phone cadence and long real-game play are unmeasured.

```sh
PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs \
  node web/acceptance/rapid-fire.mjs build/release/wfeature-server chromium
PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs \
  node web/acceptance/rapid-fire.mjs build/release/wfeature-server webkit
```

<a id="auto-toggle-correction"></a>

At introduction the ordinary and debug test gates, internal race tests and
`go vet ./...` passed, the Node suite passed 217 tests, and both browser routes
passed against a release server rebuilt with the embedded shell. The auto mode
first started OK as soon as it was selected; it was corrected the same day to
select its target on a key press. After that client-only correction `make test`
passed with 219 Node tests and both browser routes passed again against
embedded assets; the load measurements above were not repeated, since the load
probe still sends the same key packets for an active OK stream.
