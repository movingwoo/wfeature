# Web host

This directory contains the browser PWA. The page is the emulator's primary
interface: a phone screen over a phone keypad, laid out to match the original
project's web front end so muscle memory carries over.

**The page does not emulate.** It opens a session on the server, which runs the
game and sends back finished frames; `session.js` is that client and
[`../docs/session.md`](../docs/session.md) is the protocol. The page ran the
emulator itself once, in WebAssembly; a phone could not run it fast enough to
play, and that engine is gone.

**A game survives the page going away.** Switching to another app suspends this
page and the browser drops the socket; the server holds the game for five
minutes under a token the page keeps in `sessionStorage`, and the page
reconnects and asks for it back when it is looked at again. Restarting clears
the token, so the restart button still starts a game over.

## Page

- **Layout** — one column on a phone, three on a wide window. The screen and the
  keypad are a 3:4 shape in the middle either way; a window wider than 1180px
  spends the margins on the run log (left) and on settings and cheats (right),
  which are then read beside the game rather than on top of it. Narrower than
  that the same panels are centred modals over a dimmed page: the body cannot
  scroll, so anything anchored below the keypad would be unreachable. The rails
  hold the panels in both cases — no node is moved at runtime, `display:
  contents` just takes the rails out of the layout when they are not wanted.
  The page is dark throughout, because a lit game screen on white paper is what
  a room with the lights off does not want; the panels were already dark, so it
  is the surround that changed. `color-scheme: dark` comes with it, which is
  what turns the browser's own sliders, checkboxes, dropdown lists and
  scrollbars around — those are drawn by the browser, not by the stylesheet.
- **Game picker** — the archives under `var/games/<group>/`, grouped by the
  directory holding them and offered by `games.json`. The last game played is
  preselected from `localStorage`. The directory is a label for the human
  reading the list; which platform loads a file is the engine's answer from the
  bytes, so a title in the wrong folder still runs.
- **Screen** — a 240x320 Host framebuffer presented on the canvas, scaled to the
  viewport with a 3:4 wrapper.
- **Keypad** — `CLR`, `Call`, a direction pad, the twelve phone keys, and a
  layout toggle that cycles Type1 → Type2 → Type3. Type1 drives the direction
  pad with `2/4/6/8`, Type2 with the arrow keys plus `OK`, and Type3 is Type1
  with `1` and `3` moved off the number pad into the direction pad's top row,
  which is where a game that walks diagonally wants them. `Call` is the
  handset's send key: a game that answers it usually answers with a quick save,
  and no other button reaches it.
  The three soft keys are not here at all, by neither button nor shortcut: a
  game that wants one — one LGT title hangs its party screen off a soft key and
  the choice that leaves the screen off `EZ` — is reachable only from the CLI's
  `key soft1|soft2|ez`.
  The keyboard mirrors the keypad by default: `1 2 3 / Q W E / A S D`
  for `1`-`9`, `Z X C` for `* 0 #`, `Backspace` for `CLR`, `\` for `Call`,
  arrows and `Space` for the direction pad. Any of those can be moved from the
  settings panel; `keybindings.js` holds the table and the one rule it has, and
  a binding a user changed is remembered.
  Keys are sent as press/release over the session
  socket; the platform the code is translated for is the engine's business, not
  the page's. A held key lights its button, by pointer and by keyboard alike,
  for exactly as long as the game is being sent it — a phone has no hover, so
  that highlight is the only answer a finger gets. It is a class rather than
  `:active` because the keypad captures the pointer, which leaves the browser's
  own active state behind the moment a finger slides. Resting on a key raises
  no browser menu: the long-press offer to translate or search the word under
  the finger is refused everywhere outside the panels, where selecting text is
  the point.
- **Status** — everything the page has to say to the reader: a session that
  would not open, a game that exited, an error the server sent, a report that
  was written. It is a popup over the screen with a `확인` to dismiss it, and it
  is fixed rather than part of the column, because it used to be a line between
  the screen and the keypad and any message pushed the keypad down out from
  under a thumb. It dims the page at every width and sits above the panels: it
  is never something the game is read alongside, and a message can arrive while
  a panel is open. Dismissing it leaves whatever was open behind it open.
  Nothing hides on a timer; the empty string is what clears it, which is how
  every caller already cleared the old line.
- **Run log** — the page's own log as it is written: session events, key
  presses, the server's frame statistics and anything the page logged or threw.
  These are the lines a saved report carries, so what is on screen during a run
  and what is read back afterwards cannot disagree. Wide windows and debug
  builds only.
- **Settings (`Opts`)** — MIDI and effect volume, the magnification filter, the
  speed multiplier, the key settings, the cheat panel toggle, the debug report
  button, and a restart that reloads the page. The key settings appear only
  where a key has actually been pressed — a list of keyboard keys is nothing a
  phone can use, and only a keypress proves there is a keyboard to use them
  with. `Opts` is one of the keypad's own buttons,
  opposite `CLR`, and it is there from the first paint: it does not start over
  the canvas and move when a game is chosen. The report is written by the server, which is
  the side with the numbers, and the page's own log is saved beside it — a
  dropped socket or a draw failure shows up in no other place.
- **Cheat panel** — a progressive memory search over the running game: type and
  endianness, the value filters, undo/reset, a freeze list whose values stay
  editable, write watching, and saving or loading a cheat table. Candidate
  values refresh twice a second while the panel is open and the search has
  narrowed to 1000 or fewer hits. KTF and LGT only — they are the platforms with
  a flat guest address space to sweep — so the panel is removed for a session on
  any other.
- **Sound** — the engine's MIDI and PCM events are synthesised in the page from
  oscillators rather than a soundfont; see the head of `audio.js` for why.

The run log and the report button are the developer's half of the page, and a
release build shows neither: the session's `ready` message says which profile
answered, and the release drops the log column — the layout closes up rather
than leaving a gap — and takes the report button out of the settings panel.
They are the same files either way; the binary serving them is the one thing
that knows which build this is.

The page draws and nothing else: frames arrive as PNGs on the socket, are
decoded with `createImageBitmap` — off the main thread — and the newest one is
drawn on the next animation frame. Keys go up as JSON, sound arrives as MIDI and
PCM events and is played by the synthesiser here, and the cheat panel's
operations are a request and an answer.

## Server

`go run ./cmd/server` serves this directory, the game archives, and the save API
on `-addr` (`:11541` by default, every interface, so a phone on the same network
can reach it). It is Go rather than Node because the emulator is Go and the
server will host emulation sessions in the same process; `internal/webhost` is
where the routes live. A released binary carries the files in this directory
inside it — `embed.go` — and serves a directory instead when one is given with
`-web`, which is what a checkout does so an edit shows up on a reload.

Which profile is served is the binary that is running, not a flag: the server is
built per profile like every other binary here.

- `GET /games.json` — `[{ group, name, path }]` built from `-games`
  (`var/games` by default), listing the `.zip` and `.jar` files it holds.
- `GET /games/<group>/<archive>` — the archive itself, revalidated with an
  `ETag` rather than re-sent, since these are tens of megabytes.
- `GET /api/saves/<owner>` — `{ saves: { "<key>": "<base64>" } }` read from
  `-saves` (`var/savedata/<profile>/ktf` by default), in the same layout the
  native CLI's `DirectorySaveStore` writes, so both Hosts boot from one set of
  saves. `/api/saves/<platform>/<owner>` reaches another platform's tree.
- `PUT /api/saves/<owner>/<key>` — persists one entry from the raw request body.
- `POST /api/debug-log` — the page's own log, written under `-logs`
  (`var/logs`) beside the session report the server writes itself.
- `GET /api/session` (WebSocket) — one emulation session per connection. This
  is the path a game runs on and the reason the rest exists.

The emulator and the save tree are on the same machine, so nothing is preloaded
and no save crosses the network. The save API remains for the CLI's layout,
which both Hosts read.

The service worker caches the app shell for offline launches. It does not cache
the save API or the game archives.

## Known gaps

- **LGT titles do not start yet.** The page names them and finds their saves,
  and they now run their entry point and initializer, but they stop at an
  import table the runtime does not know. The native `wfeature runlgt` stops in
  the same place, so this is the platform rather than the browser.
- **SKT titles start and paint one frame.** All three local archives get that
  far and then present nothing over the next 200 ticks, so they are running but
  not yet progressing; whether one plays is unverified.
- The cheat panel appears on every platform. The two ARM ones search guest
  memory; the MIDP runtime searches a synthetic address space over its object
  graph, where the region labels are class names and the write watch is not
  offered.
- There are no soft-key buttons, matching the original layout.
