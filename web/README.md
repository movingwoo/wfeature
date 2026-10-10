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
- **Keypad** — `설정`, `메뉴`, `통화`, `우상단` and `취소` (CLR) above a
  direction pad and the twelve phone keys; the buttons are labelled in Korean,
  the way the key settings list them. Which of the four keypads is drawn is a setting
  rather than a button here: Type1 drives the direction pad with the arrow keys
  plus `OK`, Type2 with `2/4/6/8`, and Type3 is Type2 with `1` and `3` moved
  off the number pad into the direction pad's top row, which is where a game
  that walks diagonally wants them. Type4 starts empty. Every type supports
  cell assignment and keeps its own edits. `통화` is the handset's send key: a
  game that answers it usually answers with a quick save, and no other button
  reaches it.
  `메뉴` is the handset's left soft key, the one its own screen labelled 메뉴
  and the one a title of this era puts its in-game menu on. It sends -6, which
  is what `MH_KEY_SOFT1` and a MIDlet's soft key are both numbered, so no
  platform has to translate it. `우상단` is the right soft key and sends -7,
  `MH_KEY_SOFT2`, for the same reason. It is named for where it sat because
  that is the only name the titles give it: their help screens say 우측상단키
  and never what was printed on the key. It is not 확인 either, because a help
  screen's 확인키 is the centre key ("5번 및 확인키"), the one the pad prints
  as `OK`. Measured on four titles across KTF and LGT, the function a help
  screen puts on 우측상단키 — a minimap, a world map — answers -7 and the one
  on 좌측상단키 or 메뉴키 answers -6; about twenty-eight local titles name the
  right key in plain text, and more keep their help text compressed. The third
  soft key (`EZ`) is on no button and no shortcut, and stays reachable only
  from the CLI's `key soft3` (alias `ez`).
  The keyboard mirrors the keypad by default: `1 2 3 / Q W E / A S D`
  for `1`-`9`, `Z X C` for `* 0 #`, `Backspace` for `취소`, `\` for `통화`,
  `M` for `메뉴`, `,` for `우상단`, arrows and `Space` for the direction pad.
  Any of those can be moved from the settings panel; `keybindings.js` holds
  the table and the one rule it has, and a binding a user changed is
  remembered. A key this build adds takes its default only where the user has
  not already spent that keyboard key, and a keypad a user edited is not
  given the new button — it is in the editor's list.
  Quick save and quick load are local actions in that same cell editor. Type1,
  Type2 and Type3 place them in top-row columns 5 and 6 by default; Type4 leaves
  them unassigned. Assigned buttons act immediately during a session the server
  reports as `can_checkpoint` — KTF, LGT and SKT titles —
  with brief inline feedback instead of a popup. They send
  no handset key, never activate during a slide or while editing, and are disabled
  during another checkpoint operation. An absent slot disables quick load during
  play while leaving its cell editable. The game picker has no restore button;
  after a server restart, start the game and use the assigned quick-load key.
  A load brings the game back and leaves its saves as they are, and the status
  line says both: that the game went back to the quick-save moment and that
  saves were not reverted. It does not say the saves are unchanged, because a
  load first stores what the running game had written and not yet stored. A
  refusal shows the server's reason after `퀵로드 실패:` or `퀵세이브 실패:`; the
  server words the three a person can act on in Korean, and a slot from an
  earlier build keeps quick load enabled so that pressing it says why.
  The grid editor opens from `설정` over the game screen and leaves the keypad
  visible. It assigns keys, merges adjacent cells into buttons, splits a button,
  clears its action, and undoes the last edit. All types use the same grid;
  sizing sliders are retired. See [Grid keypad](#grid-keypad) for geometry,
  input, and storage contracts.
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
- **Status** — session and settings messages: a session that
  would not open, a game that exited, an error the server sent, a report that
  was written. It is a popup over the screen with a `확인` to dismiss it, and it
  is fixed rather than part of the column, because it used to be a line between
  the screen and the keypad and any message pushed the keypad down out from
  under a thumb. It dims the page at every width and sits above the panels: it
  is never something the game is read alongside, and a message can arrive while
  a panel is open. Dismissing it leaves whatever was open behind it open.
  Nothing hides on a timer; the empty string is what clears it, which is how
  every caller already cleared the old line.
  Checkpoint feedback uses a separate status line over the bottom of the game
  screen. It expires automatically and neither dims the page nor captures input.
- **Run log** — the page's own log as it is written: session events, key
  presses, the server's frame statistics and anything the page logged or threw.
  These are the lines a saved report carries, so what is on screen during a run
  and what is read back afterwards cannot disagree. Wide windows and debug
  builds only.
- **Settings (`설정`)** — MIDI and effect volume, magnification, speed, keypad
  type and grid editing, keyboard bindings, cheats, debug reports, and restart.
  Both type lists select the same stored type and show whether it was edited.
  The keyboard binding panel appears after a physical keyboard key is used.
  The dark-red settings button starts at the first grid cell and can move or
  merge, but exactly one group always retains it, including in Type4.
  Debug reports join the server's runtime evidence with the page's own log.
- **Cheat panel** — a progressive memory search over the running game: type and
  endianness, the value filters, undo/reset, a freeze list whose values stay
  editable, write watching, and saving or loading a cheat table. Candidate
  values refresh twice a second while the panel is open and the search has
  narrowed to 1000 or fewer hits. Every platform has one: the two ARM platforms
  sweep the guest's own address space, and the MIDP runtime sweeps a synthetic
  one built over its object graph, where the regions are named by class and the
  write watch is not offered. Whether a session has a panel is the session's
  answer rather than the platform's, and the page removes the toggle where the
  answer is no.
- **Sound** — the engine's MIDI and PCM events are synthesised in the page from
  oscillators rather than a soundfont; see the head of `audio.js` for why.
  The page negotiates `sound=resume&timing=1&pcm=1&phase=1`: MIDI channels and PCM belong to individual
  clips, and stopping one cancels its voices and tails independently. Queue
  recovery, reconnect and quick load reconstruct current output and held-note
  envelope ages and fractional PCM positions within the first remaining frame.
  MIDI volume/expression/pan changes affect sounding notes and
  release tails through independent channel controls. Sustain, pitch-bend range
  and controller reset preserve their per-clip state through reconstruction.
  Presentation timestamps preserve intervals inside one server batch; a late
  batch requests fresh output. Scheduled clip stops keep earlier audio connected
  until its deadline and isolate the next restart's output nodes. Immutable PCM
  definitions reuse a bounded buffer cache while each trigger keeps its own
  source, gain and cursor. A complete batch must fit 512 retained sources and
  128 MiB of PCM float payload per live reference, including scheduled and
  retired sources; pressure invokes the existing output recovery. ATR groups
  also preserve live squared volume/expression
  and cosine/sine pan independently of MIDI. Group nodes are reclaimed after
  their final source; control state remains for subsequent waves until stop/reset.
  See [PCM controls](../docs/audio.md#live-pcm-track-controls),
  [source bounds](../docs/audio.md#live-source-bounds),
  [buffer retention](../docs/audio.md#decoded-pcm-buffer-reuse) and
  [the audio contract](../docs/audio-ownership.md).
- **What is remembered, and where** — every setting the page keeps goes through
  `storage.js`, which is `localStorage` and `sessionStorage` behind a boundary
  that cannot throw. The boundary is not tidiness: a browser told to block site
  data throws on the *property*, so `globalThis.localStorage` raises before any
  key is named, and a browser that allows storage still throws
  `QuotaExceededError` on a write once the origin is full. Both used to reach
  the page — one of the unguarded reads sat inside the block that starts a
  game, so a denied browser answered the start button with an error instead of
  a game. A value the browser refuses is kept in memory for the life of the
  page, so the control still works and only its memory is lost, and the run log
  says so once at load. `localStorage` holds the last game, the per-game screen
  size and speed, the magnification, music and effects volume, vibration, the
  keypad type, grid arrangements and key bindings;
  `sessionStorage` holds the resume token, which belongs to one tab.

The run log and the report button are the developer's half of the page, and a
release build shows neither: the session's `ready` message says which profile
answered, and the release drops the log column — the layout closes up rather
than leaving a gap — and takes the report button out of the settings panel.
They are the same files either way; the binary serving them is the one thing
that knows which build this is.

The page receives pictures at the game's own size on the socket — complete
ones, and updates that draw only what changed, sometimes after moving the held
picture for a scrolling field — decodes them with `createImageBitmap`, and
composes them in order on a retained canvas (`frame-stream.js`). The newest
picture is drawn on the next animation frame, magnified here with hqx at the
scale the server names (`magnify.js`, `hqx.js`, and `hqx-patterns.js`, which is
generated from the Go tables). Game execution remains on the server. Keys go up
as JSON; sound arrives as compact binary MIDI and PCM events, each sample
carried once (`audio-stream.js`), and is played by the synthesiser here; the
cheat panel's operations are a request and an answer.

## Grid keypad

`keypad-layout.js` owns a complete partition of fourteen columns and nine rows.
A group stores its first cell as its stable identifier, all owned cell coordinates,
one action, and its `press` or `slide` activation policy. Empty cells are groups
as well. Every cell has one owner, each group is connected through shared edges,
and exactly one group holds settings. Diagonal contact alone is insufficient.
The supported shapes include rectangles, L and T shapes, and rings around empty
cells or other buttons. Keys may be duplicated in separate groups.

Each former top-row cell is divided horizontally into two columns. The previous
top row maps to row 1, and previous pad row `r` maps to rows `2*r` and `2*r+1`.
Previous column `c` maps to columns `2*c-1` and `2*c`. Migrated top-row buttons
start as two-cell groups and assigned pad keys as four-cell groups. Version-1
grid buttons and custom merged shapes retain their owned area through the same
paired-column mapping, including empty merged shapes. Former single empty cells
become independent empty halves. This preserves assignments and topology while
allowing finer edits through splitting and merging.

Type1, Type2 and Type3 presets keep settings in merged columns 1–2 and clear in
merged columns 13–14 of row 1. Columns 3–12 are single cells: columns 5–10 hold
quick save, quick load, rapid fire, menu, call and the right soft key, respectively;
columns 3, 4, 11 and 12 are empty. Type4 keeps its settings-only preset. These
defaults apply to new or reset layouts; saved custom groups and legacy migration
keep their recorded assignments and shapes.

The keypad fills the game column's safe width, up to 480 px, without decorative
side or bottom padding. Game and keypad share the dynamic viewport height after
all four safe-area insets and the desktop's 16 px top margin. CSS uses `100dvh`
where supported and falls back to `100vh`. It reserves the smaller of 284 px or
55% of usable height for the keypad, fits the 3:4 game wrapper into the remaining
budget, then gives all space below that wrapper to the keypad. The board can grow
beyond the reservation on tall or narrow screens. The normal 4 px gaps also shrink
in extremely short or narrow windows; the renderer reads the computed gap when
building visible shapes and hit targets.

Every row and column scales equally on its axis. Coordinates and cell ownership
never change with device, OS, browser chrome, rotation or resize. Larger screens
enlarge cells rather than adding logical rows; smaller screens retain all cells.
Small targets can be enlarged by merging. The old size, split and band preferences
remain stored for rollback but no longer size the page. Safe areas deliberately
remain clear of buttons. Dynamic browser chrome and physical touch still require
the device observations listed in [testing](../docs/testing.md#grid-keypad).

`keypad-editor.js` renders one native button per group and preserves one accessible
name and focus target. `keypad-geometry.js` computes one union of cell rectangles
for both clipping and the visible outline. Shared seams are filled; missing
corners and holes stay outside the hit target. The label occupies the largest
filled rectangle, so it cannot float inside a hole. Labels stay on one line;
the renderer measures the actual text with a DOM range and reduces the font size
when it exceeds that rectangle's available width. Fitting starts from the normal
CSS size on each resize, merge or assignment, so a larger button can restore it.
The rapid-fire mode label is fitted again when its text changes. Resize updates
geometry without rewriting storage and releases held input.

The editor has assignment and multiple-selection modes. Taps toggle selection;
dragging adds each crossed group once. Existing merged groups are selected as a
whole. A successful merge returns to assignment mode with the resulting button
selected, so the next key choice immediately updates it. A failed merge retains
multiple-selection mode and its selected groups. A merge with conflicting
assignments requires the resulting action to be chosen. A merge containing
settings retains settings. Assigning settings elsewhere
moves it. Clearing preserves the group's shape; splitting keeps its action in
its first cell and empties the rest. Undo restores the last successful edit in
the current editor and type, including reset. Reset affects only the selected
type. Opening the editor or changing type starts a new undo boundary.

Input continues through `key-holds.js`, rapid fire, and the existing session key
codes. A slide inside a group stays one press. The last pointer or keyboard
holder releases the guest key, and duplicate buttons share pressed feedback.
Native Space/Enter activates a focused gameplay button; local controls retain
the browser's native click path. During editing, keyboard and pointer input
selects controls without reaching the guest, rapid fire or checkpoint requests.

Migration retains old band cells' direct-press behavior and old pad cells' slide
behavior even for custom assignments. New action assignments use sliding for
direction/number gameplay keys and direct press for menu, call, soft-key and
clear controls. A merge retaining a key retains the first selected matching
group's policy. Settings, rapid fire and checkpoint actions always require direct
activation. Sliding across those actions never triggers them.

The selected type remains `wfeature:keypadLayout`. `wfeature:keypadGridV2` stores
`{version: 2, layouts: {type1, type2, type3, type4}}`, where an entry is either a
validated `{columns, rows, groups}` grid or `null` for that type's current preset.
Explicit default entries prevent reset from reviving legacy edits. Migration
reads version 1 from `wfeature:keypadGrid` only when the new slot is absent. If
both grid slots are absent, it reads `wfeature:keypadKeys`. All old records,
including `wfeature:keypadSize`, remain unchanged. Separate storage prevents an
already-open coarse editor from overwriting refined edits. Older clients still
read their own records and do not see subsequent grid edits. Once version 2 has
been saved, including an explicit reset, later old-tab changes are not imported.

Stored text is bounded to 65,536 code units before parsing, and each type is
bounded to 126 groups and 126 unique owned cells. Invalid dimensions, actions,
connectivity, ownership or settings counts reject that type independently.
Valid siblings survive. Before an edit replaces a damaged record, its source
is retained as `wfeature:keypadGridV2Backup`; a failed backup keeps the edit only
in memory. A damaged old-slot source needs no copy because migration never writes
that slot. Future versions and oversized records are not overwritten. Failed
writes retain a working layout for the current tab and show an inline notice.
The service worker carries all three grid modules in `wfeature-pwa-v43`.

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
  (`var/games` by default), listing the `.zip` and `.jar` files it holds. Like
  the page's own files it carries a validator and is revalidated rather than
  re-sent, and it is gzip-compressed where the browser accepts that.
- `GET /games/<group>/<archive>` — the archive itself, revalidated with an
  `ETag` rather than re-sent, since these are tens of megabytes.
- `GET /api/saves/<owner>` — `{ saves: { "<key>": "<base64>" } }` read from
  `-saves` (`var/savedata/<profile>/ktf` by default), in the same layout the
  native CLI's `DirectorySaveStore` writes, so both Hosts boot from one set of
  saves. `/api/saves/<platform>/<owner>` reaches another platform's tree.
- `PUT /api/saves/<owner>/<key>` — persists one entry from the raw request body.
- `POST /api/debug-log` — the page's own log, written under `-logs`
  (`var/logs`) beside the session report the server writes itself. **Debug
  builds only**: a release answers 404 here and its page collects nothing to
  post. What a debug server does keep is bounded by report size, by a rolling
  rate, and by the age and total size of the directory; see
  `../docs/architecture.md`, "Debug run logs".
- `GET /api/session` (WebSocket) — one controlling connection per game. Browser
  clients request `?protocol=2` for lossless picture updates at the game's own
  size and binary sound, and add `pictures=webp` when their browser decodes
  lossless WebP, which then replaces the updates' PNGs; clients without the
  protocol value receive complete, magnified PNGs and JSON sound. Tokens, retention, explicit takeover, the wire formats,
  frame composition and recovery are described in
  [server sessions](../docs/session.md).

The emulator and the save tree are on the same machine, so nothing is preloaded
and no save crosses the network. The save API remains for the CLI's layout,
which both Hosts read.

The service worker caches the app shell for offline launches. It does not cache
the save API or the game archives. The shell list is hand written and nothing at
runtime complains when it falls behind — a module missing from it is fetched
over the network like any other file, so the page works everywhere except
offline, where it comes up and then fails on an import — so
`service-worker.test.mjs` compares the list against what `index.html` names and
what the module graph from `app.js` reaches. Two modules had already drifted out
of it. A change to the list wants the cache name bumped with it.
Shell version 47 uses `wfeature-pwa-v47`. The prefix introduced in version 35 prevents already-installed
workers from deleting it through their broad `wfeature-shell-*` cleanup.
Activation removes legacy shell caches and strictly older `wfeature-pwa-vN`
versions, preserving future versions and unrelated caches. A controlled navigation
also retires old caches recreated by a replaced worker's late response. Offline
fallback reads only the current shell. Cache writes extend the fetch event lifetime.

Game-key presses and supported canvas touches ask the existing audio boundary
to resume a suspended AudioContext. Actual audible recovery still needs a
physical-device observation. The optional two-version browser route and manual
PWA checklist are in [PWA acceptance](../docs/pwa-acceptance.md).

## Known gaps

- **Which titles run is not this page's question.** Every platform starts games
  here over the same shared layer the CLI uses, so the page has no per-platform
  limitation of its own: a title that runs under `wfeature runktf`/`runlgt`/
  `runskt` runs here, and one that does not, does not.
- The cheat panel appears on every platform. The two ARM ones search guest
  memory; the MIDP runtime searches a synthetic address space over its object
  graph, where the region labels are class names and the write watch is not
  offered.
- The keypad carries two soft keys, `메뉴` for the handset's left one (`-6`)
  and `우상단` for its right one (`-7`). The third (`EZ`) is on no button and
  no shortcut, and stays reachable only from the CLI's `key soft3`.
