# Web host

This directory contains the browser PWA, the emulator's primary interface: a
phone screen over a phone keypad, laid out like the reference implementation's
web front end so muscle memory carries over.

**The page does not emulate.** It opens a session on the server, which runs the
game and sends back finished frames; `session.js` is that client and
[`../docs/session.md`](../docs/session.md) is the protocol. The page once ran
the emulator itself in WebAssembly; a phone could not run it fast enough to
play, and that engine is gone.

**A game survives the page going away.** Switching to another app suspends the
page and the browser drops the socket; the server keeps the game, with no time
limit, under a browser token the page keeps in `localStorage`
(`wfeature.browser-token`, which replaced the per-tab `sessionStorage`
`wfeature.resume-token`), until it is stopped, replaced, pushed out by the
four-game limit, or the server shuts down. The page reconnects and asks for it
back when it is looked at again, retrying with a delay that doubles up to 15
seconds and starts again from one second after any answer from the server. A
page that finds its game controlled by another page shows
`다른 화면에서 게임을 진행 중입니다.` with **이 화면에서 이어하기**, an explicit
takeover. **🔄 재시작** asks first ("게임을 처음부터 다시 시작할까요? 저장하지 않은
진행은 사라집니다.", **다시 시작**), then stops the game and returns to the
picker.

## Page

- **Layout** — one column on a phone, three on a window at least 1180px wide.
  The screen and the keypad are a 3:4 shape in the middle either way; a wide
  window spends the margins on the run log (left) and on settings and cheats
  (right), read beside the game rather than over it. Narrower, the same panels
  are centred modals over a dimmed page, because the body cannot scroll and
  anything anchored below the keypad would be unreachable. The rails hold the
  panels in both cases; no node moves at runtime, and `display: contents` takes
  the rails out of the layout when they are not wanted. The page is dark
  throughout, because a lit game screen on white is not what a dark room wants;
  `color-scheme: dark` also turns the browser's own sliders, checkboxes,
  dropdown lists and scrollbars dark.
- **Game picker** — the archives under `var/games/<group>/`, grouped by the
  directory holding them and offered by `games.json`; archives directly in
  `var/games/` are grouped as `기타`, and page-added ones as 추가한 게임. The
  last game played is preselected from `localStorage`. The directory is a label
  for the reader; the engine decides the platform from the bytes, so a title in
  the wrong folder still runs. Below the list, **＋ 게임 추가** uploads one or more
  `.zip`/`.jar` files (each reported separately) and **게임 삭제**, enabled only
  for page-added games, removes one after a confirmation (`add-game.js`; the
  route is in [Server](#server)). **세이브 내보내기** downloads the selected game's
  saves as `<game>.wfs`, or hands them to the phone app's own save picker, and
  **세이브 가져오기** restores a `.wfs` over them after a confirmation
  (`save-backup.js`). The server's refusals are shown as it words them; a
  request that never reaches the server says `서버에 연결하지 못했습니다.`
  rather than the browser's own English error (`reach.js`, also used by the
  game list and the upload and delete buttons). The buttons sit on the picker
  because an import replaces a save and the game is not running there.
- **Screen** — the game's own screen size, chosen per game under `📱 화면`:
  128×160, 176×220, 240×320 (the default), 240×400 or 320×480. A change takes
  effect on the next start, and the note under the setting says so. The picture
  is fitted into the 3:4 hole (`object-fit: contain`), so a 3:5 or 2:3 screen
  leaves bezel on one axis; a touch on the canvas is mapped back to the game's
  own pixels (`touch.js`), and a touch on the bezel is ignored.
- **Keypad** — `설정`, `메뉴`, `통화`, `우상단` and `취소` (CLR) above a direction pad
  and the twelve phone keys; the buttons are labelled in Korean, as the key
  settings list them. Which of the four keypads is drawn is a setting: Type1
  drives the direction pad with the arrow keys plus `OK`, Type2 with
  `2/4/6/8`, and Type3 is Type2 with `1` and `3` moved into the direction pad's
  top row, where a game that walks diagonally wants them. Type4 starts with
  only `설정`. Every type supports cell assignment and keeps its own edits.
  - `통화` is the handset's send key: a game that answers it usually answers
    with a quick save, and no other button reaches it.
  - `메뉴` is the handset's left soft key, the one its own screen labelled 메뉴
    and the one titles of this era put their in-game menu on. It sends -6, which
    is what `MH_KEY_SOFT1` and a MIDlet's soft key are both numbered, so no
    platform translates it. `우상단` is the right soft key and sends -7,
    `MH_KEY_SOFT2`, for the same reason. It is named for its position because
    that is the only name the titles give it: their help screens say
    우측상단키, never what was printed on the key. It is not 확인, because a help
    screen's 확인키 is the centre key ("5번 및 확인키"), which the pad prints as
    `OK`. On four titles across KTF and LGT, the function a help screen puts on
    우측상단키 — a minimap, a world map — answers -7 and the one on 좌측상단키 or
    메뉴키 answers -6; about twenty-eight local titles name the right key in
    plain text, and more keep their help text compressed.
  - The third soft key (`EZ`) is on no button and no shortcut; only the CLI's
    `key soft3` (alias `ez`) reaches it.
  - Keys are sent as press/release over the session socket; translating the
    code for a platform is the engine's business. A held key lights its button,
    by pointer and by keyboard alike, for exactly as long as the game is sent
    it — a phone has no hover, so the highlight is a finger's only feedback. It
    is a class rather than `:active` because the keypad captures the pointer,
    which leaves the browser's active state behind when a finger slides.
    Resting on a key raises no browser menu: the long-press offer to translate
    or search is refused everywhere outside the panels.
  - The grid editor (`🎮 키패드 세부 조정`) opens over the game screen and leaves
    the keypad visible. It assigns keys, merges adjacent cells into buttons,
    splits a button, clears its action, and undoes the last edit. See
    [Grid keypad](#grid-keypad).
- **Keyboard** — mirrors the keypad by default: `1 2 3 / Q W E / A S D` for
  `1`-`9`, `Z X C` for `* 0 #`, `Backspace` for `취소`, `\` for `통화`, `M` for
  `메뉴`, `,` for `우상단`, arrows and `Space` for the direction pad and `확인`.
  Any of them can be moved under `⌨️ 키보드 매핑 변경`, which appears after a
  physical keyboard key is used; `keybindings.js` holds the table and its one
  rule — a physical key belongs to one phone key, so taking a key in use empties
  its old row. `Tab`, `Escape`, `Enter`, `NumpadEnter`, `F5`, `F11`, `F12` and
  the modifier keys cannot be bound. A changed binding is remembered. A key a
  newer build adds takes its default only where the user has not already spent
  that keyboard key, and a keypad the user edited is not given the new button —
  it is in the editor's list.
- **Quick save and quick load** — page actions placed in the cell editor. Type1,
  Type2 and Type3 place them in top-row columns 5 and 6 by default; Type4 leaves
  them unassigned. Assigned buttons act immediately during a session the server
  reports as `can_checkpoint` (KTF, LGT and SKT titles), with brief feedback in
  the checkpoint status line rather than a popup. They send no handset key,
  never activate during a slide or while editing, and are disabled during
  another checkpoint operation. Without a slot (`has_checkpoint`), quick load is
  disabled during play while its cell stays editable. The picker has no restore
  button; after a server restart, start the game and use the quick-load key.
  A successful load shows `퀵세이브 시점으로 돌아갔습니다.`: the game is back at the
  quick save, and its ordinary saves are left as they are (the load first
  stores what the running game had written and not yet stored). A refusal shows
  `퀵로드 실패:` or `퀵세이브 실패:` and the server's reason for 8 seconds. The
  server words the refusals a person can act on in Korean — a slot from an
  earlier build (`이전 버전의 퀵로드 형식`), saves the disk would not take, saves
  a load could not read — and a slot from an earlier build keeps quick load
  enabled so that pressing it says why.
- **Rapid fire (`연사`)** — a page control, never a handset key
  (`rapid-fire.js`). Pressing it cycles `연사 off`, `manual` and `auto`. In
  `manual`, holding `5` or `OK` repeats it, toggling every 50 ms; in `auto`,
  pressing `5` or `OK` starts a continuous repeat of that key and pressing it
  again stops it. The mode resets to off on input loss, editing and session
  changes, but not when the keypad only resizes (전체화면, rotation, a window
  resize).
- **Game controller** — one more way to hold a phone key. Its presses go
  through the same holds as the keypad and the keyboard (`key-holds.js`), so a
  key held two ways is one press, a held key lights its button, rapid fire
  repeats it and focus loss releases it. The server is sent phone keys and
  never learns a controller exists, so every platform takes it alike. A browser
  sends no controller events — it answers what a controller looks like when
  asked — so the page reads `navigator.getGamepads()` once an animation frame
  while one is connected (`gamepad.js`).
  - **The default layout is positional**, because the standard mapping numbers
    buttons by position (the bottom face button is A on one make, ✕ on another,
    B on a third). The direction pad and the left stick are the direction key,
    one direction at a time like the handset's; the right stick is the number
    block around 5 in eight sectors, up-left `1` to down-right `9`; the bottom
    face button is `확인` and the right one `취소`, since titles draw `BACK:CLR`
    on every screen that can be left; the left and top ones are `5` and `0`;
    the shoulders are `메뉴` and `우상단`, which help screens name by corner; the
    triggers are `*` and `#`, the outer keys of the handset's bottom row; View
    is `통화`, Menu opens and closes `설정`, pressing the left stick cycles rapid
    fire and pressing the right one switches `전체화면`. The home button is the
    operating system's. Quick save and quick load start unbound because either
    is one stray press from putting the game back.
  - **Thresholds.** A button presses at 0.5 and releases below 0.3, so a trigger
    under a resting finger does not chatter; a stick engages at half its
    travel, releases below 0.35, and keeps its sector until the thumb is 10°
    past the edge. A control held across a reset — focus loss, a hidden page, a
    session change — stays dead until it is released, so nothing presses by
    itself when input returns. While the keypad editor, a confirmation, the
    text input dialog, or a panel drawn over the game on a narrow screen is up,
    controller keys do not reach the game; the button on `설정` still closes the
    settings.
  - **A question takes the controller.** While a confirmation or the text
    input dialog is up, the controls bound to the directions move the focus
    between its answers and stop at either end — the safe answer is first and
    has the focus, so going round sent LEFT from it to the answer that goes
    through — the one bound to `확인` presses the answer in focus (or focuses the
    first when none is), and the one bound to `취소` presses the first, which is
    always the safe one. The focus the controller moves is drawn
    (`[data-pad]` in `style.css`); the mark goes with a pointer or a key in
    the dialog, and with the question itself, however it was answered.
  - **Remapping.** `🕹️ 게임패드 매핑 변경` appears in the settings once a
    controller has been used here (a browser shows a page no controller until
    one of its buttons is pressed) and lists a row per control with a menu of
    phone keys and page controls. Several controls may send one key, and a row
    lights while its control is held, which is how a person finds the row under
    their thumb.
  - **Unrecognised controllers.** One with an empty `mapping` is listed as
    numbered buttons and axis directions and keeps a table of its own, starting
    empty, under the name the browser gives it; an axis already at an end when
    first seen is taken as its rest rather than a press. Its direction pad may
    be a HID hat switch, which Chrome and Firefox hand over as one axis naming
    one of eight directions — -1 up, each step of 2/7 a further 45° clockwise —
    and resting past its end (1.29, or 3.29). An axis that rests past its end
    exactly on one of those steps is read as four rows, `십자 ↑ (축 10)` and its
    three siblings, and a diagonal holds two of them; it reverts to a lever the
    first time it reads between steps, which a hat never does, so a lever that
    overshoots its range is not mistaken for one. Read as a lever, the hat held
    one end at rest and never reached right or down. A four-position hat steps
    differently and stays a lever. Safari hands the same pad over as four
    buttons.
  - **Rumble.** The controller's motor takes the guest's vibration under the
    same `📳 진동` switch, with the guest's level as its strength
    (`dual-rumble`); the switch appears once such a controller is connected,
    even where the browser has no vibrator of its own. Otherwise
    `navigator.vibrate` is used, which has no strength, so the level only
    decides whether to vibrate (`vibrate.js`); the row is removed on a browser
    with neither.
  - Browsers offer controllers only to a secure context: `localhost`,
    `127.0.0.1`, the phone apps and an HTTPS proxy, not `http://192.168.x.x`.
- **전체화면** — the keypad and the rails put away, and the game screen as large
  as the window allows: the same 3:4 hole, centred, at the largest width that
  leaves room by its top-right corner for the way back, a `[ ]`-cornered button
  drawn only in this layout. The button sits 8px above that corner when the
  picture has room above it and 8px beside it when the picture takes the whole
  height, so on a tall phone it is not a hand's width above the game. On a
  window far from 3:4 the margin on the other axis already holds the button;
  near 3:4 the picture gives up 48px on one axis. It is the page's own layout
  rather than the browser's fullscreen, which a browser grants only to a click
  or a key (a controller button is neither) and an iPhone grants to no page;
  the browser's own F11, or an app that already fills the screen, still stacks
  on top. In: `⛶ 전체화면` in the settings, a keypad cell carrying it (the editor
  offers it beside quick save), or a controller's R3. Out: the corner button,
  Escape when nothing is open over the game, the same settings button (the
  panels are modals in this layout at every width, so a controller's Menu still
  reaches them), or opening the keypad editor, which brings back the keypad it
  edits. The page returns in the layout it was left in
  (`wfeature:fullscreen`). Keys, controllers and touches work as before.
- **Text input (`🔤 문자 입력`)** — shown while a game is playing. It opens a
  dialog that types a whole string into the game's active text field with the
  device's own keyboard and IME, and submits it with **입력**
  (`text-input.js`). [`../docs/native-text-input.md`](../docs/native-text-input.md)
  has the supported fields and limits.
- **Status** — session and settings messages: a session that would not open, a
  game that exited, an error the server sent, a report that was written. It is a
  popup over the screen with a `확인` to dismiss it, fixed rather than part of
  the column, because as a line between the screen and the keypad any message
  pushed the keypad out from under a thumb. It dims the page at every width and
  sits above the panels, since a message can arrive while a panel is open;
  dismissing it leaves whatever was open behind it open. Nothing hides on a
  timer; setting the empty string clears it. Checkpoint feedback uses a
  separate status line over the bottom of the game screen, which expires by
  itself and neither dims the page nor captures input. A failure keeps its
  time through a dropped connection and reconnect; returning to the picker
  clears it.
- **Confirmations** — every question (deleting a game, importing a save,
  restarting, replacing a game) is asked by the page's own dialog
  (`confirm.js`), not `window.confirm`: in the Android app a JavaScript confirm
  is the web view client's decision and can answer "no" with nothing on screen,
  and the iOS app's web view answers "no" without showing anything. The verb is
  on the button that carries it out. While a question is open, neither the
  keyboard nor a controller reaches the game, and Space and Enter press the
  focused button of the question.
- **Run log** — the page's own log as it is written: session events, key
  presses, the server's frame statistics and anything the page logged or threw,
  kept as a ring of 2000 lines. These are the lines a saved report carries, so
  the screen during a run and the report afterwards cannot disagree. Wide
  windows and debug builds only.
- **Settings (`설정`)** — `🎵 배경음` and `🔊 효과음` (MIDI and sampled-sound
  volume), `🔍 화질` (원본, hq2x, hq3x or hq4x magnification), `📱 화면`,
  `📳 진동`, `⏩ 속도` (0.25배 to 4배, per game), `🎮 키패드 타입` and
  `🎮 키패드 세부 조정`, `⛶ 전체화면`, `⌨️ 키보드 매핑 변경`, `🕹️ 게임패드 매핑 변경`,
  `🎯 치트`, `🐞 디버그 로그 저장`, `🔤 문자 입력` and `🔄 재시작`. Both type lists
  (settings and editor) select the same stored type and show whether it was
  edited. The dark-red settings button starts at the first grid cell and can
  move or merge, but exactly one group always holds it, including in Type4.
  Debug reports join the server's runtime evidence with the page's own log.
- **Cheat panel (`🎯 치트`)** — a progressive memory search over the running
  game: type and endianness, the value filters, undo/reset, a freeze list whose
  values stay editable, write watching, and saving or loading a cheat table.
  Candidate values refresh twice a second while the panel is open and the
  search has narrowed to 1000 or fewer hits. Every platform has one: the two ARM
  platforms sweep the guest's own address space, and the MIDP runtime sweeps a
  synthetic one built over its object graph, where the regions are named by
  class and the write watch is not offered. Whether a session has a panel is the
  session's answer rather than the platform's, and the page removes the toggle
  where the answer is no.
- **Sound** — the engine's MIDI and PCM events are synthesised in the page from
  oscillators rather than a soundfont; the head of `audio.js` says why. The page
  negotiates `sound=resume&timing=1&pcm=1&phase=1`: MIDI channels and PCM belong
  to individual clips, and stopping one cancels its voices and tails
  independently. Queue recovery, reconnect and quick load reconstruct current
  output and held-note envelope ages and fractional PCM positions within the
  first remaining frame. MIDI volume/expression/pan changes affect sounding
  notes and release tails through independent channel controls. Sustain,
  pitch-bend range and controller reset preserve their per-clip state through
  reconstruction. Presentation timestamps preserve intervals inside one server
  batch; a late batch re-anchors a lead ahead when it holds only the clock
  frontier or is at most 200 ms late, and a later one requests fresh output.
  Scheduled clip stops keep earlier audio connected until its deadline and
  isolate the next restart's output nodes. Immutable PCM definitions reuse a
  bounded buffer cache while each trigger keeps its own source, gain and cursor.
  A complete batch must fit 512 retained sources and 128 MiB of PCM float
  payload in total over every live reference, including scheduled and retired
  sources; pressure invokes the existing output recovery. ATR groups also
  preserve live squared volume/expression and cosine/sine pan independently of
  MIDI. Group nodes are reclaimed after their final source; control state
  remains for subsequent waves until stop/reset. Game-key presses and supported
  canvas touches ask the audio boundary to resume a suspended `AudioContext`.
  See [PCM controls](../docs/audio.md#live-pcm-track-controls),
  [source bounds](../docs/audio.md#live-source-bounds),
  [buffer retention](../docs/audio.md#decoded-pcm-buffer-reuse) and
  [the audio contract](../docs/audio-ownership.md).
- **What is remembered, and where** — every setting goes through `storage.js`,
  which wraps `localStorage` and `sessionStorage` behind a boundary that cannot
  throw. A browser told to block site data throws on the *property*, so
  `globalThis.localStorage` raises before any key is named, and a browser that
  allows storage still throws `QuotaExceededError` once the origin is full; an
  unguarded read inside the block that starts a game once answered the start
  button with an error. A value the browser refuses is kept in memory for the
  life of the page, so the control still works and only its memory is lost, and
  the run log says so once at load. `localStorage` holds:

  | Key | What |
  |---|---|
  | `wfeature:lastGame` | the last game played |
  | `wfeature:screen:<game>`, `wfeature:speed:<game>` | the per-game screen size and speed (the old shared `wfeature:speed` is no longer read) |
  | `wfeature:frameScale` | `🔍 화질` |
  | `wfeature:volume:midi`, `wfeature:volume:pcm` | the two volumes |
  | `wfeature:vibrate` | `📳 진동` |
  | `wfeature:keypadLayout`, `wfeature:keypadGridV2` (and the older grid keys) | keypad type and grid arrangements ([Grid keypad](#grid-keypad)) |
  | `wfeature:fullscreen` | `전체화면` |
  | `wfeature:keyBindings`, `wfeature:keyboardSeen` | keyboard bindings, and whether a keyboard was used |
  | `wfeature:padBindings`, `wfeature:padBindingsRaw`, `wfeature:padSeen` | controller bindings (the second for controllers the browser does not recognise), and whether a controller was used |
  | `wfeature.browser-token` | the browser token a retained game is resumed with |

The run log and the report button are the developer's half of the page, and a
release build shows neither: the session's `ready` message says which profile
answered, and against a release server the page stops its log capture, drops
the log column (the layout closes up rather than leaving a gap) and removes the
report button. The files are the same either way; the binary serving them is
the one thing that knows which build this is.

The page receives pictures at the game's own size on the socket — complete
ones, and updates that draw only what changed, sometimes after moving the held
picture for a scrolling field — decodes them with `createImageBitmap`, and
composes them in order on a retained canvas (`frame-stream.js`). The newest
picture is drawn on the next animation frame, magnified here with hqx at the
scale the server names (`magnify.js`, `hqx.js`, and `hqx-patterns.js`, which is
generated from the Go tables). Keys go up as JSON; sound arrives as compact
binary MIDI and PCM events, each sample carried once (`audio-stream.js`), and
is played by the synthesiser here; the cheat panel's operations are a request
and an answer.

## Grid keypad

`keypad-layout.js` owns a complete partition of fourteen columns and nine rows.
A group stores its first cell as its stable identifier, all owned cell
coordinates, one action, and its `press` or `slide` activation policy. Empty
cells are groups as well. Every cell has one owner, each group is connected
through shared edges (diagonal contact alone is insufficient), and exactly one
group holds settings. The supported shapes include rectangles, L and T shapes,
and rings around empty cells or other buttons. Keys may be duplicated in
separate groups.

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
quick save, quick load, rapid fire, menu, call and the right soft key,
respectively; columns 3, 4, 11 and 12 are empty. Type4 keeps its settings-only
preset. These defaults apply to new or reset layouts; saved custom groups and
legacy migration keep their recorded assignments and shapes.

The keypad fills the game column's safe width, up to 480 px, without decorative
side or bottom padding. Game and keypad share the dynamic viewport height after
all four safe-area insets and the desktop's 16 px top margin. CSS uses `100dvh`
where supported and falls back to `100vh`. It reserves the smaller of 284 px or
55% of usable height for the keypad, fits the 3:4 game wrapper into the
remaining budget, then gives all space below that wrapper to the keypad. The
board can grow beyond the reservation on tall or narrow screens. The normal
4 px gaps also shrink in extremely short or narrow windows; the renderer reads
the computed gap when building visible shapes and hit targets.

Every row and column scales equally on its axis. Coordinates and cell ownership
never change with device, OS, browser chrome, rotation or resize. Larger screens
enlarge cells rather than adding logical rows; smaller screens retain all cells.
Small targets can be enlarged by merging. The old size, split and band
preferences remain stored for rollback but no longer size the page. Safe areas
deliberately remain clear of buttons. Dynamic browser chrome and physical touch
still require the device observations listed in
[testing](../docs/testing.md#grid-keypad).

`keypad-editor.js` renders one native button per group and preserves one
accessible name and focus target. `keypad-geometry.js` computes one union of
cell rectangles for both clipping and the visible outline. The outline is two
paths along that union: the edge, and a highlight shown for pressed, picked and
focused buttons. The two trade places through `stroke-opacity` alone, because
Chromium lays an SVG out again when its stroke width or opacity changes — a
press that thickened the edge cost two layouts, one on press and one on
release, which `web/acceptance/keypad-grid.mjs` rules out. Shared seams are
filled; missing corners and holes stay outside the hit target. The label
occupies the largest filled rectangle, so it cannot float inside a hole. Labels
stay on one line; the renderer measures the text with a DOM range and reduces
the font size when it exceeds that rectangle's width. Fitting starts from the
normal CSS size on each resize, merge or assignment, so a larger button can
restore it. The rapid-fire mode label is fitted again when its text changes.
Resize updates geometry without rewriting storage and releases held input.

The editor has assignment and multiple-selection (`여러 칸 선택`) modes, with
`선택 해제`, `합치기`, `나누기`, `비우기`, `되돌리기` and `기본값으로 되돌리기`. Taps
toggle selection; dragging adds each crossed group once. Existing merged groups
are selected as a whole. A successful merge returns to assignment mode with the
resulting button selected, so the next key choice updates it. A failed merge
retains multiple-selection mode and its selected groups. A merge with
conflicting assignments requires the resulting action to be chosen. A merge
containing settings retains settings, and assigning settings elsewhere moves
it. Clearing preserves the group's shape; splitting keeps its action in its
first cell and empties the rest. Undo restores the last successful edit in the
current editor and type, including reset. Reset affects only the selected type.
Opening the editor or changing type starts a new undo boundary.

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
group's policy. Settings, rapid fire, checkpoint and fullscreen actions always
require direct activation; sliding across them never triggers them.

The selected type remains `wfeature:keypadLayout`. `wfeature:keypadGridV2`
stores `{version: 2, layouts: {type1, type2, type3, type4}}`, where an entry is
either a validated `{columns, rows, groups}` grid or `null` for that type's
current preset. Explicit default entries prevent reset from reviving legacy
edits. Migration reads version 1 from `wfeature:keypadGrid` only when the new
slot is absent. If both grid slots are absent, it reads `wfeature:keypadKeys`.
All old records, including `wfeature:keypadSize`, remain unchanged. Separate
storage prevents an already-open coarse editor from overwriting refined edits.
Older clients still read their own records and do not see subsequent grid
edits. Once version 2 has been saved, including an explicit reset, later
old-tab changes are not imported.

Stored text is bounded to 65,536 code units before parsing, and each type is
bounded to 126 groups and 126 unique owned cells. Invalid dimensions, actions,
connectivity, ownership or settings counts reject that type independently.
Valid siblings survive. Before an edit replaces a damaged record, its source is
retained as `wfeature:keypadGridV2Backup`; a failed backup keeps the edit only
in memory. A damaged old-slot source needs no copy because migration never
writes that slot. Future versions and oversized records are not overwritten.
Failed writes retain a working layout for the current tab and show an inline
notice. The service worker's shell cache carries all three grid modules.

## Text

Every sentence and label a player reads on the page is in
[`text.properties`](text.properties): the markup's buttons and headings, what
the scripts write into the page — key names, status lines, the notes over the
game screen, a question and its buttons — and what the server answers the page
with, such as why an upload or a save import was refused. Rewording the page is
an edit to that file and nothing else. A server serving this directory (`-web
web`, which is what a checkout does) shows the edit on the next reload; a
release carries the file in its binary. The words quoted elsewhere in this
document are the file's current ones.

The format is a strict subset of Java's: UTF-8, one `key = value` per line, and
`#` starting a comment line. The value is the rest of the line without the
spaces and tabs around it; there are no escapes and no continuation lines. A
`{name}` in a value is filled in when the sentence is shown — `{count}개`,
`{name} 을(를) 지웠습니다.` — from what the code that shows it passes, and a
value without one is shown as written. Keys are named for where their text
appears: `picker.*` the game picker, `key.*` the phone keys and the page's own
controls, `pad.*` the game controller list, and so on, each group under a
comment that says what it is.

The server reads the file (`internal/webhost/text.go`) and refuses to start on
a line that is neither a comment nor `key = value`, on a key written twice, and
on a placeholder in `index.html` the file does not have. The markup names each
of its words as `{{key}}`, which the server fills, escaped, before the page is
sent, and `{{@table}}` is the whole table as JSON in `#page-text`: `app.js`
installs it before anything else runs, and every script reads a sentence
through `phrase(key, values)` in `text.js`, when it shows one rather than when
it loads. The server words its own answers from the same table, so a sentence
the page and the server both say is said once. A key the table does not have
shows as the key itself rather than as nothing.

`text.test.mjs` keeps it that way. It reads the file the way the server does —
the cases in `testdata/text-format.json`, which `internal/webhost` runs against
its own reader — and fails on a key the page or the server asks for that the
file lacks, on a key in the file that nothing asks for, and on Korean written
into a script, the markup or the stylesheet instead of the file; the Go tests
fail on Korean written into the server or into `internal/cheat`, whose panel
refusals the server words. The page's tests and the browser routes in
`acceptance/` compare with `phrase(...)` rather than with copied sentences, so
a reworded file does not fail them.

What the emulated phone itself shows is the game's, not the page's: a soft-key
label a platform draws on the game screen and the replies a title's network
service is answered with stay with the platform that produces them.

## Server

`go run ./cmd/server` serves this directory, the game archives and the save API
on `-addr` (`:11541` by default, every interface, so a phone on the same
network can reach it); its flags and the data root are in
[`../docs/running.md`](../docs/running.md#the-local-server). It is Go because
the emulator is Go and the server hosts emulation sessions in the same process;
`internal/webhost` holds the routes. A released binary carries the files in
this directory inside it (`embed.go`) and serves a directory instead when
`-web` names one that exists, which is what a checkout does so an edit shows up
on a reload. The page itself is sent with its words filled in from
`text.properties` (see [Text](#text)). The profile served is the binary that is
running, not a flag.

- `GET /games.json` — `[{ group, name, path, added? }]` read from `-games`
  (`var/games`) and `-ext` (`var/ext`) on every request, listing the `.zip` and
  `.jar` files. Like the page's own files it carries a validator and is
  revalidated rather than re-sent, and it is gzip-compressed where the browser
  accepts that.
- `GET /games/<group>/<archive>`, `GET /ext/<archive>` — the archive itself,
  revalidated with an `ETag` rather than re-sent, since these are tens of
  megabytes. Page-added archives are served from `/ext/` and their `games.json`
  entries carry `added: true`.
- `POST /api/games?name=<file>` / `DELETE /api/games?game=<picker path>` — add
  an archive from the page (at most 32 MiB, written atomically), or remove one
  the page added. Nothing under `-games` can be removed this way. See
  [`../docs/running.md`](../docs/running.md#adding-a-game-from-the-page-and-taking-it-back-off).
- `GET` / `POST /api/savepack?game=<path>` — export one game's saves as a `.wfs`
  container, or import one over them (a body of at most 64 MiB).
- `GET /api/saves/<owner>` — `{ saves: { "<key>": "<base64>" } }` read from
  `-saves` (`var/savedata/<profile>/ktf` by default), in the layout the native
  CLI's `DirectorySaveStore` writes, so both Hosts boot from one set of saves.
  `/api/saves/<platform>/<owner>` reaches another platform's tree.
- `PUT /api/saves/<owner>/<key>` (or `/api/saves/<platform>/<owner>/<key>`) —
  persists one entry from the raw request body.
- `GET /api/status` — what this server is (`server`, `profile`, `version`,
  `pid`); `POST /api/shutdown` stops it, from this machine only. See
  [`../docs/running.md`](../docs/running.md#apistatus).
- `GET /licenses` — this project's licence and the bundled notices.
- `POST /api/debug-log` — the page's own log, written under `-logs`
  (`var/logs`) beside the session report the server writes itself. **Debug
  builds only**: a release answers 404 and its page collects nothing to post.
  A debug server bounds what it keeps by report size, by a rolling rate, and by
  the age and total size of the directory; see
  [`../docs/architecture.md`](../docs/architecture.md#debug-run-logs).
- `GET /api/session` (WebSocket) — one controlling connection per game. Browser
  clients request `?protocol=2` for lossless picture updates at the game's own
  size and binary sound, and add `pictures=webp` when their browser decodes
  lossless WebP, which then replaces the updates' PNGs; clients without the
  protocol value receive complete, magnified PNGs and JSON sound. Tokens,
  retention, explicit takeover, the wire formats, frame composition and
  recovery are described in [server sessions](../docs/session.md).

The emulator and the save tree are on the same machine, so nothing is preloaded
and no save crosses the network. The save API remains for the CLI's layout,
which both Hosts read.

The service worker caches the app shell for offline launches. It does not cache
the save API or the game archives. The shell list is hand written, and nothing
at runtime complains when it falls behind: a module missing from it is fetched
over the network like any other file, so the page works everywhere except
offline, where it comes up and then fails on an import. `service-worker.test.mjs`
therefore compares the list against what `index.html` names and what the module
graph from `app.js` reaches; two modules had already drifted out of it. A
change to the list wants the cache name bumped with it. The shell cache is
`wfeature-pwa-v61` (the constant in `service-worker.js`). The `wfeature-pwa-`
prefix, introduced in version 35, keeps already-installed workers from deleting
it through their broad `wfeature-shell-*` cleanup. Activation removes legacy
shell caches and strictly older `wfeature-pwa-vN` versions, preserving future
versions and unrelated caches. A controlled navigation also retires old caches
recreated by a replaced worker's late response. Offline fallback reads only the
current shell. Cache writes extend the fetch event lifetime.

Audible recovery after resuming a suspended `AudioContext` still needs a
physical-device observation. The optional two-version browser route and manual
PWA checklist are in [PWA acceptance](../docs/pwa-acceptance.md).

## Known gaps

- **Which titles run is not this page's question.** Every platform starts games
  here over the same shared layer the CLI uses, so the page has no per-platform
  limitation of its own: a title that runs under `wfeature runktf`/`runlgt`/
  `runskt` runs here, and one that does not, does not.
- Game controllers are checked against a stand-in for `navigator.getGamepads`
  in Chromium and WebKit, in the Android app on an emulator with a virtual HID
  controller, and in the iOS app in the simulator with a virtual controller —
  not against a physical controller, which waits on a device (see
  [`../docs/mobile.md`](../docs/mobile.md#what-is-not-answered-yet)).
- The third soft key (`EZ`) is on no button and no shortcut; see
  [Keypad](#page).
