import { clearLog, recordEvent, saveReport, subscribeLog } from "./debug-log.js";
import { PageAudio } from "./audio.js";
import { createKeyHolds } from "./key-holds.js";
import { GameSession, playAudioEvents, sessionAvailable } from "./session.js";
import {
  assign,
  bindable,
  clear as clearBinding,
  codeLabel,
  codeLookup,
  heldBy,
  keyLabel,
  keyOrder,
  loadBindings,
} from "./keybindings.js";

// The sink is registered once the session is up and before any game starts, so
// a game's very first sound has somewhere to go. The AudioContext behind it
// still waits for a user gesture, which starting a game supplies.
let pageAudio = null;

// The emulator runs on the server and this page draws the pictures it sends.
// The page ran it itself once, in WebAssembly; the same build measured about
// fifteen times slower on a phone than on a desktop, and the cost was Go's
// WebAssembly backend rather than the emulator, so a phone that emulates
// cannot play. Nothing here emulates any more.
let session = null;

const canvas = document.getElementById("canvas");
const canvasContext = canvas.getContext("2d", { alpha: false });
const statusMessage = document.getElementById("status-message");
const statusText = document.getElementById("status-text");

// A window this wide has an empty margin beside a 3:4 phone screen, and the
// stylesheet spends it on the settings and cheat panels. Narrower than this the
// two are modals instead, which several behaviours turn on, so the question is
// asked in one place. The width matches the breakpoint in style.css, where a
// second margin at 1180px takes the run log as well.
const dockedPanels = window.matchMedia("(min-width: 900px)");

// Browser key names to the MIDP-style codes the server translates into WIPI key
// values. The keypad buttons carry the same names in their data-key attribute.
const keyCodes = new Map([
  ["1", 49],
  ["2", 50],
  ["3", 51],
  ["4", 52],
  ["5", 53],
  ["6", 54],
  ["7", 55],
  ["8", 56],
  ["9", 57],
  ["0", 48],
  ["*", 42],
  ["#", 35],
  ["CLR", 8],
  // The send key. A game that answers it at all usually answers with a quick
  // save, and no other button on the keypad reaches it.
  ["CALL", 10],
  ["UP", 141],
  ["DOWN", 146],
  ["LEFT", 142],
  ["RIGHT", 145],
  ["OK", 148],
]);

// Which physical key sends which phone key. The table itself lives in
// keybindings.js, phone key first, because that is the direction the settings
// panel lists it and the direction its one rule is stated in; keyboardMap is
// the same table the way a keydown asks it. Both are state rather than
// constants: the user can move any key, and rebinding rewrites the pair.
const KEY_BINDINGS_KEY = "wfeature:keyBindings";

const storedBindings = () => {
  try {
    return JSON.parse(localStorage.getItem(KEY_BINDINGS_KEY) ?? "null");
  } catch {
    // Private browsing denies storage and a half-written entry parses as
    // nothing. Either way the defaults are the answer.
    return null;
  }
};

let keyBindings = loadBindings(storedBindings());
let keyboardMap = codeLookup(keyBindings);

// Set while the settings panel is waiting for a key to bind. It takes the press
// instead of the game, which is the whole of the difference between changing a
// key and pressing it.
let pendingCapture = null;

// The status popup is raised by having something to say and lowered by the
// empty string, which is how every caller already cleared the old status line.
// Nothing dismisses it on a timer: everything it carries — a session that
// died, a game that exited, a report that was written — is worth reading at
// the reader's pace, and it covers nothing that moves while it is up.
const setStatus = message => {
  statusText.textContent = message;
  statusMessage.classList.toggle("visible", message !== "");
};

const statusVisible = () => statusMessage.classList.contains("visible");

const initStatus = () => {
  document.getElementById("status-close")?.addEventListener("click", () => setStatus(""));
};

const reportError = error => {
  setStatus(error instanceof Error ? error.message : String(error));
  console.error(error);
};

// Restarting is a page reload rather than a teardown: the server holds one
// session per connection and a fresh document is the cheapest way back to a
// clean one. Saves live on the server, so nothing but unsaved progress is lost.
const LAST_GAME_KEY = "wfeature:lastGame";
const FRAME_SCALE_KEY = "wfeature:frameScale";
// The screen is remembered per game rather than once for the page: it is a
// property of the title — which artwork its archive carries — and not a taste.
const SCREEN_KEY_PREFIX = "wfeature:screen:";
const DEFAULT_SCREEN = "240x320";
// The range the server accepts, mirrored so a stored value outside it is
// treated as absent here rather than sent on and refused there. See
// `validScreen` in internal/webhost/session.go.
const MIN_SCREEN = 32;
const MAX_SCREEN = 1024;

const screenKey = path => `${SCREEN_KEY_PREFIX}${path}`;

// storedScreen answers the size stored for a game, or the default. A stored
// value that is not two numbers is treated as absent rather than sent on: it
// can only come from an older page or from somebody editing storage.
const storedScreen = path => {
  let stored = DEFAULT_SCREEN;
  try {
    stored = localStorage.getItem(screenKey(path)) ?? DEFAULT_SCREEN;
  } catch {
    // Private browsing denies storage; the default is the answer.
  }
  const [width, height] = String(stored).split("x").map(Number);
  const inRange = value =>
    Number.isInteger(value) && value >= MIN_SCREEN && value <= MAX_SCREEN;
  if (!inRange(width) || !inRange(height)) {
    return { width: 240, height: 320 };
  }
  return { width, height };
};

const rememberScreen = (path, value) => {
  if (!path) return;
  try {
    localStorage.setItem(screenKey(path), value);
  } catch {
    // Private browsing denies storage; the setting then lasts one session.
  }
};

// chosenGame is the game the screen setting is about: whatever the list is
// showing, and the last game played once the list is gone.
const chosenGame = () => document.getElementById("game-select")?.value || lastGame() || "";

const rememberGame = path => {
  try {
    localStorage.setItem(LAST_GAME_KEY, path);
  } catch {
    // Private browsing denies storage; preselecting is a convenience only.
  }
};

const lastGame = () => {
  try {
    return localStorage.getItem(LAST_GAME_KEY);
  } catch {
    return null;
  }
};

// The gear and its panel keep their places for the whole run, so starting a
// game only takes the picker off the canvas.
const hideGameSelect = () => {
  const prestart = document.getElementById("prestart");
  prestart?.classList.add("hidden");
  setTimeout(() => prestart?.remove(), 300);
};

let gameRunning = false;
// Names the saved report after the game it came from.
let currentGameLabel = "";
// The platform the running session belongs to, as the engine detected it.
// Empty until a game has been started.
let currentPlatform = "";

// Whether this platform can say what wrote an address. The panel asks before it
// offers the control, because the poll that keeps the panel live shares a call
// with the write hits: on a platform that cannot record them the poll threw
// every interval, the error reached the page, and the candidate refresh behind
// it never ran — the whole panel read as broken over a feature it never had.
let canWatchWrites = false;

// The cheat panel is wired once and re-read per game. What it may offer is a
// property of the session — which platform, and whether that platform can
// watch writes — but the listeners and the poll belong to the page. Running
// the whole of initCheat per game stacked a second click handler and a second
// poller on the same panel every time a game was started.
let cheatWired = false;
let resetCheatPanel = () => {};

// A dropped connection no longer ends the game: the server parks it for a few
// minutes under a token it hands over when the game starts, and a page that
// comes back sends the token to get it back. That is what makes this playable
// on a phone — switching to another app suspends this page, the browser drops
// the socket behind it, and without this the game was over.
//
// The token lives in sessionStorage rather than in a variable because the phone
// is also where the page itself gets discarded and reloaded while it is in the
// background: the reload is the case the game most needs to survive. Restarting
// deliberately clears it, so the restart button still starts a game over.
const RESUME_TOKEN_KEY = "wfeature.resume-token";
// RESUME_ATTEMPTS x RESUME_DELAY covers the server's window with room to spare;
// past it the game the token names has been closed and there is nothing to
// reconnect to.
const RESUME_DELAY = 3000;
const RESUME_ATTEMPTS = 120;
let reconnecting = false;

const rememberResumeToken = token => {
  try {
    if (token) sessionStorage.setItem(RESUME_TOKEN_KEY, token);
    else sessionStorage.removeItem(RESUME_TOKEN_KEY);
  } catch {
    // Private browsing denies storage. Resuming then works for as long as the
    // page itself lives, which is every case except the page being discarded.
    inMemoryResumeToken = token ?? "";
  }
};

let inMemoryResumeToken = "";

const storedResumeToken = () => {
  try {
    return sessionStorage.getItem(RESUME_TOKEN_KEY) ?? "";
  } catch {
    return inMemoryResumeToken;
  }
};

const sendKey = (eventType, name) => {
  const code = keyCodes.get(name);
  if (code === undefined || !gameRunning) return;
  // Input lands in the log too, so a live watcher can line up what the guest
  // did against what was actually pressed.
  if (eventType !== "repeat") recordEvent(`key ${eventType} ${name} (${code})`);
  // Nothing waits for an acknowledgement: a key is worth one packet, and a
  // round trip per press would put the network in the input path.
  session?.sendKey(eventType, code);
};

const initInput = () => {
  // A key can be printed on more than one button — the type-2 layout keeps the
  // digits the type-1 direction pad uses, and type 3 prints 1 and 3 on the
  // direction pad as well — so lighting a key lights every button carrying it,
  // of which at most one is on screen.
  const buttonsByKey = new Map();
  for (const button of document.querySelectorAll("button[data-key]")) {
    const named = buttonsByKey.get(button.dataset.key) ?? [];
    named.push(button);
    buttonsByKey.set(button.dataset.key, named);
  }

  // A phone has no hover and the keypad is drawn rather than native, so this
  // class is the whole of the answer a finger gets. It is held for exactly as
  // long as the key is, which is why the keyboard raises it too: what is lit
  // is what the game is being sent.
  const showPressed = (name, down) => {
    for (const button of buttonsByKey.get(name) ?? []) button.classList.toggle("pressed", down);
  };

  const holds = createKeyHolds({
    press: name => {
      showPressed(name, true);
      sendKey("press", name);
    },
    release: name => {
      showPressed(name, false);
      sendKey("release", name);
    },
  });

  // Typing into the cheat panel must not reach the game.
  const isTextEntry = target =>
    target instanceof HTMLElement && (target.tagName === "INPUT" || target.tagName === "SELECT");

  // The pad is what a finger slides across: the two key blocks and the row of
  // *, 0 and # under them. The keypad's top row is not part of it. Opts, the
  // layout toggle, Call and CLR are aimed at one at a time — a slide that woke
  // one of them on its way past would be a surprise, and CLR in the middle of a
  // game is an expensive one — so they are pressed the way every button here
  // was before sliding: held until the finger lifts, wherever it wanders.
  const padKey = element => {
    const button = element?.closest?.("button[data-key]");
    return button?.closest(".keypad-main, .keypad-footer") ? button.dataset.key : null;
  };

  // Where a slide may begin. A finger that goes down on the screen or in the
  // margins is on a handset and takes the keys it reaches, which is the whole
  // point of the gesture. Three places are not that: the run log and the cheat
  // panel are a document, and they scroll and are selected; any other button is
  // a control that was aimed at; and the keypad's own frame is where the top
  // row sits, the strip this gesture is deliberately kept out of.
  const slidesFrom = target =>
    target instanceof Element &&
    !isTextEntry(target) &&
    !target.closest(".log-view, .cheat-panel") &&
    !target.closest("button") &&
    !target.matches(".button-container");

  // The keys are not bound one by one, because a slide has to be able to begin
  // where there is no key: pressing the pad is one gesture and the finger that
  // arrives from the screen or the margin is the same finger.
  document.addEventListener("pointerdown", event => {
    const target = event.target instanceof Element ? event.target : null;
    const button = target?.closest("button[data-key]");
    if (button) {
      event.preventDefault();
      // Capture keeps the moves and the release coming once the finger leaves
      // the button it started on. A finger that started beside the keys needs
      // nothing of the sort: its events already belong to no button, and the
      // window hears them wherever it goes.
      button.setPointerCapture(event.pointerId);
      const name = button.dataset.key;
      if (padKey(button)) holds.moveTo(event.pointerId, name);
      else holds.latch(event.pointerId, name);
      return;
    }
    if (slidesFrom(target)) holds.track(event.pointerId);
  });

  // Capture also means the event no longer says what is under the finger — it
  // is still addressed to the button the press started on — so the point is
  // asked instead. Off the pad the answer is null, and the key that was held is
  // let go until the finger comes back — the top row counts as off the pad.
  const keyUnder = event => padKey(document.elementFromPoint(event.clientX, event.clientY));

  window.addEventListener(
    "pointermove",
    event => {
      if (!holds.tracking(event.pointerId)) return;
      holds.moveTo(event.pointerId, keyUnder(event));
    },
    { passive: true },
  );

  const release = event => holds.lift(event.pointerId);
  window.addEventListener("pointerup", release);
  window.addEventListener("pointercancel", release);

  // Resting on a key or smearing across the pad is a gesture the browser reads
  // as its own: Android offers to translate or search the word under the
  // finger, iOS offers to copy it, and either way the menu lands on top of the
  // game. Nothing outside the panels is text anybody wants to select, so the
  // menu is refused everywhere the panels are not.
  document.addEventListener("contextmenu", event => {
    const target = event.target;
    if (isTextEntry(target) || target?.closest?.(".cheat-panel, .log-view")) return;
    event.preventDefault();
  });

  // What is held, and which phone key it was sent as. The name is kept rather
  // than looked up again on the way out because the binding can move while the
  // key is down — the settings panel is open during the same run — and a
  // release has to name the key the press did.
  const keysDown = new Map();

  document.addEventListener("keydown", event => {
    if (isTextEntry(event.target) || event.ctrlKey || event.metaKey || event.altKey) return;
    // A key arriving at all is the proof that there is a keyboard, which is
    // what puts the key settings in the panel.
    noticeKeyboard();
    if (pendingCapture) {
      // The panel is waiting for this press, so the game does not get it.
      event.preventDefault();
      pendingCapture(event.code);
      return;
    }
    const name = keyboardMap[event.code];
    if (!name) return;
    event.preventDefault();
    keysDown.set(event.code, name);
    showPressed(name, true);
    sendKey(event.repeat ? "repeat" : "press", name);
  });
  // A release answers what is held rather than what is bound: a modifier
  // pressed between the two, or a rebinding, would otherwise lose the keyup and
  // leave the key down in the game for the rest of the run.
  document.addEventListener("keyup", event => {
    const name = keysDown.get(event.code);
    if (name === undefined) return;
    event.preventDefault();
    keysDown.delete(event.code);
    showPressed(name, false);
    sendKey("release", name);
  });

  // A window that loses focus stops being told about keyup, so a key held
  // across the switch would stay down in the game and lit on the keypad for
  // the rest of the run. Letting go on the way out is what actually happened.
  window.addEventListener("blur", () => {
    for (const name of keysDown.values()) {
      showPressed(name, false);
      sendKey("release", name);
    }
    keysDown.clear();
  });
};

// drawFrame paints one picture from the server. Frames can arrive faster than
// the display refreshes, so the newest one wins and the rest are dropped
// undrawn: showing a picture the game has already replaced costs a phone the
// budget it needs for the one after it.
let pendingBitmap = null;
let drawScheduled = false;
const drawFrame = bitmap => {
  pendingBitmap?.close();
  pendingBitmap = bitmap;
  if (drawScheduled) return;
  drawScheduled = true;
  requestAnimationFrame(() => {
    drawScheduled = false;
    const frame = pendingBitmap;
    pendingBitmap = null;
    if (!frame) return;
    // The magnification filter runs on the server, so the frame's size is the
    // server's answer and the canvas follows it. Its CSS size is unchanged, so
    // the picture stays the same size on screen and gains detail.
    if (frame.width !== canvas.width || frame.height !== canvas.height) {
      canvas.width = frame.width;
      canvas.height = frame.height;
    }
    canvasContext.drawImage(frame, 0, 0);
    frame.close();
  });
};

// openSession connects to the server's emulator. It answers null rather than
// throwing so the caller can say what a page with no session can do, which is
// nothing: the emulator runs on the other end of this socket.
const openSession = async () => {
  if (!sessionAvailable()) return null;
  const opening = new GameSession({
    onFrame: drawFrame,
    onAudio: events => playAudioEvents(pageAudio, events),
    onExited: () => {
      gameRunning = false;
      // A game that ended has nothing to come back to.
      rememberResumeToken("");
      recordEvent(`${currentPlatform} session exited`);
      setStatus("게임이 종료되었습니다. 🔄 재시작으로 다시 시작할 수 있습니다.");
    },
    onError: message => {
      recordEvent(`session error: ${message}`);
      setStatus(message);
    },
    onStats: stats => recordSessionStats(stats),
    onClosed: () => {
      if (!gameRunning) return;
      // The game outlives the connection now. Input goes nowhere until there
      // is a socket again, so the page stops sending it and starts trying to
      // get its game back.
      gameRunning = false;
      recordEvent("session connection lost");
      void reconnectSession();
    },
  });
  try {
    await opening.open();
  } catch (error) {
    console.warn("wfeature session unavailable", error);
    return null;
  }
  return opening;
};

const wait = millis => new Promise(resolve => setTimeout(resolve, millis));

// reconnectSession opens a new socket and asks for the game the server parked.
// It keeps trying for as long as the server would hold one, because the reason
// the socket went is usually that the phone stopped running this page at all —
// and the moment it comes back is the moment an attempt succeeds.
const reconnectSession = async () => {
  const token = storedResumeToken();
  if (!token || reconnecting) return;
  reconnecting = true;
  setStatus("서버와 다시 연결하는 중…");
  try {
    for (let attempt = 0; attempt < RESUME_ATTEMPTS; attempt++) {
      const next = await openSession();
      if (next) {
        session = next;
        if (await resumeStoredGame()) return;
        // The socket is back but the game is not: the window ran out, or the
        // server was restarted. Nothing is left to reconnect to.
        return;
      }
      await wait(RESUME_DELAY);
    }
    setStatus("서버 연결이 끊어졌습니다. 새로고침해서 다시 시작하세요.");
  } finally {
    reconnecting = false;
  }
};

// resumeStoredGame asks the freshly opened session for the parked game and puts
// the page back the way it was. It answers whether the game came back.
const resumeStoredGame = async () => {
  const token = storedResumeToken();
  if (!token) return false;
  let answer;
  try {
    answer = await session.resume(token);
  } catch (error) {
    recordEvent(`session resume failed: ${error.message}`);
    setStatus("서버 연결이 끊어졌습니다. 새로고침해서 다시 시작하세요.");
    return false;
  }
  if (!answer?.started) {
    rememberResumeToken("");
    recordEvent("session could not be resumed");
    setStatus(answer?.message || "이어서 진행할 게임이 없습니다. 새로고침해서 다시 시작하세요.");
    return false;
  }
  currentPlatform = answer.started.platform ?? currentPlatform;
  rememberResumeToken(answer.started.token ?? token);
  document.getElementById("restart")?.classList.remove("hidden");
  // The audio graph belongs to this page rather than to the game, so a page
  // that came back has to build one again. A browser still holds it silent
  // until a gesture, which the first key press supplies.
  pageAudio?.ensure();
  hideGameSelect();
  // The speed setting lives in this page rather than in the parked game, so it
  // is applied again on the way back in.
  applyStoredSpeed();
  sessionStarted(answer.started);
  recordEvent("session resumed");
  return true;
};

// initResumeOnReturn reconnects the moment the page is looked at again. A
// backgrounded tab has its timers throttled to the point where a retry loop may
// not run at all, so the event that says the phone is back is worth more than
// any interval: it fires exactly when a reconnection can succeed.
const initResumeOnReturn = () => {
  const tryAgain = () => {
    if (document.hidden || gameRunning || reconnecting) return;
    if (!storedResumeToken()) return;
    void reconnectSession();
  };
  document.addEventListener("visibilitychange", tryAgain);
  window.addEventListener("online", tryAgain);
  window.addEventListener("pageshow", tryAgain);
};

// The server's own numbers, which are the only ones there are: the page is not
// emulating, so it cannot measure what emulation costs. They go to the log
// rather than to a variable, because the log is what a report reads back.
const recordSessionStats = stats => {
  if (!stats) return;
  // The speed and the tick rate are what make the rest readable. A frame rate
  // below the game's own says nothing on its own — titles differ in how many
  // ticks they take per picture — and the tick cost only says whether the
  // server ran out of time once the rate says how many of them there were.
  const speed = stats.speed > 0 ? `, speed ${stats.speed.toFixed(2)}x` : "";
  const shed = stats.shed > 0 ? `, shed ${stats.shed}` : "";
  recordEvent(`session ${stats.fps.toFixed(1)}fps, tick ${stats.tick_ms.toFixed(1)}ms` +
    ` x${(stats.tick_rate ?? 0).toFixed(1)}/s${speed}, ` +
    `frame ${stats.frame_bytes}B, dropped ${stats.skipped}${shed}`);
};

// startServerGame runs a game on the server and draws what comes back. The
// archive never reaches the page — it is already on the machine that will run
// it, which is also why there is nothing to upload and nothing to preload:
// saves are read and written on the same side as the emulator.
const startServerGame = async (path, scale) => {
  document.getElementById("restart")?.classList.remove("hidden");
  // Starting a game is the user gesture browsers require before an
  // AudioContext may run, so this is where the audio graph comes up.
  pageAudio?.ensure();
  const answer = await session.start(path, scale, storedScreen(path));
  currentPlatform = answer.started?.platform ?? "";
  // The token names this game while the page is away from it; keeping it is
  // the whole of what a reconnect needs.
  rememberResumeToken(answer.started?.token ?? "");
  hideGameSelect();
  sessionStarted(answer.started ?? {});
};

// A session is running from here on: reveal the panels that only mean
// something against a live game and say which platform answered.
const sessionStarted = info => {
  gameRunning = true;
  canWatchWrites = info.can_watch === true;
  recordEvent(`${currentPlatform} session started: ${info.main_class || info.name || ""}`);
  setStatus("");
  initCheat();
};

const initGameSelect = async () => {
  const select = document.getElementById("game-select");
  const startButton = document.getElementById("game-start");

  try {
    const response = await fetch("games.json", { cache: "no-store" });
    if (!response.ok) throw new Error(`게임 목록을 불러오지 못했습니다 (${response.status})`);
    const games = await response.json();
    select.replaceChildren();

    for (const group of [...new Set(games.map(game => game.group))]) {
      const optionGroup = document.createElement("optgroup");
      // An archive sitting in the game root has no platform directory to name
      // it, and a blank optgroup label reads as a glitch.
      optionGroup.label = group ? group.toUpperCase() : "기타";
      for (const game of games.filter(candidate => candidate.group === group)) {
        const option = document.createElement("option");
        option.value = game.path;
        option.textContent = game.name;
        optionGroup.appendChild(option);
      }
      select.appendChild(optionGroup);
    }

    if (games.length === 0) {
      select.add(new Option("등록된 게임이 없습니다", ""));
      return;
    }

    const previous = lastGame();
    if (previous && [...select.options].some(option => option.value === previous)) {
      select.value = previous;
    }

    select.disabled = false;
    startButton.disabled = false;
    startButton.addEventListener("click", async () => {
      const path = select.value;
      if (!path) return;

      select.disabled = true;
      startButton.disabled = true;
      startButton.textContent = "불러오는 중...";

      try {
        rememberGame(path);
        currentGameLabel = select.options[select.selectedIndex]?.textContent ?? "";
        recordEvent(`starting ${path} on the server`);
        setStatus("게임을 시작하는 중입니다. 수십 초 걸릴 수 있습니다.");
        await startServerGame(path, Number(localStorage.getItem(FRAME_SCALE_KEY) ?? "1"));
      } catch (error) {
        reportError(error);
        select.disabled = false;
        startButton.disabled = false;
        startButton.textContent = "실행";
      }
    });
  } catch (error) {
    reportError(error);
    select.replaceChildren(new Option("게임 목록을 불러올 수 없습니다", ""));
  }
};

const initRestart = () => {
  document.getElementById("restart")?.addEventListener("click", () => {
    if (!confirm("게임을 처음부터 다시 시작할까요? 저장하지 않은 진행은 사라집니다.")) return;
    // Restarting is the one reload that must not come back to the same game,
    // so the token goes before the page does.
    rememberResumeToken("");
    location.reload();
  });
};

// The one button cycles the layouts rather than naming them in a list: there
// are few enough of them that pressing it again is quicker than opening a menu,
// and it reads as the layout it is showing, not the one it would move to.
const KEYPAD_LAYOUTS = ["type1", "type2", "type3"];

const initKeypadLayout = () => {
  const container = document.querySelector(".button-container");
  const toggle = document.getElementById("keypad-layout-toggle");

  toggle?.addEventListener("click", () => {
    const next = KEYPAD_LAYOUTS.indexOf(container.dataset.layout) + 1;
    const layout = KEYPAD_LAYOUTS[next % KEYPAD_LAYOUTS.length];
    container.dataset.layout = layout;
    toggle.textContent = `Type${KEYPAD_LAYOUTS.indexOf(layout) + 1}`;
  });
};

// initDebugLog reveals the report button when there is a report to take, which
// is when a debug server answered: the reports are a developer's tool and a
// release has no use for one, so the button is taken out of the panel rather
// than left there to press. The server is the side with the numbers and writes
// its own half, instead of shipping one to the page to post back.
const initDebugLog = debugBuild => {
  const button = document.getElementById("debug-log");
  if (!button) return;
  if (!debugBuild) {
    button.remove();
    return;
  }
  button.classList.remove("hidden");
  button.addEventListener("click", async () => {
    button.disabled = true;
    const previous = button.textContent;
    button.textContent = "저장 중...";
    try {
      // Two halves, saved together: the server knows what the guest did, and
      // only the page knows what became of the frames and the socket.
      const answer = await session.report(currentGameLabel);
      const pageLog = await saveReport(currentGameLabel).catch(() => null);
      setStatus(pageLog
        ? `세션 보고서를 ${answer.message} 에, 페이지 로그를 var/logs/${pageLog} 에 저장했습니다.`
        : `세션 보고서를 ${answer.message} 에 저장했습니다.`);
    } catch (error) {
      reportError(error);
    } finally {
      button.disabled = false;
      button.textContent = previous;
    }
  });
};

// initLogView shows the page log in the left rail. The lines are the same ones
// a saved report carries, so what is on screen during a run and what is read
// back afterwards cannot disagree. It is a debug build's rail: a release drops
// the whole column, and the stylesheet then centres the page on what is left.
const LOG_VIEW_LINES = 400;

const initLogView = debugBuild => {
  const view = document.getElementById("log-view");
  if (!view) return;
  if (!debugBuild) {
    document.querySelector(".rail-left")?.remove();
    return;
  }
  document.body.classList.add("debug-build");

  // Every line starts with a full ISO stamp. The date is the same all run, so
  // the rail keeps the clock and drops the rest.
  const shorten = line => line.replace(/^\d{4}-\d{2}-\d{2}T(\d{2}:\d{2}:\d{2})\.\d+Z /, "$1 ");

  let pinned = true;
  view.addEventListener("scroll", () => {
    pinned = view.scrollHeight - view.scrollTop - view.clientHeight < 24;
  });

  subscribeLog(line => {
    if (line === null) {
      view.replaceChildren();
      pinned = true;
      return;
    }
    const row = document.createElement("div");
    row.className = "log-line";
    row.textContent = shorten(line);
    view.appendChild(row);
    while (view.childElementCount > LOG_VIEW_LINES) view.firstElementChild.remove();
    // Following the tail is the useful default, but a user who has scrolled up
    // to read something is not dragged back down.
    if (pinned) view.scrollTop = view.scrollHeight;
  });

  document.getElementById("log-clear")?.addEventListener("click", () => clearLog());
};

// Tapping the dimmed page behind a modal closes it, the same gesture that
// closes the settings panel. The status popup is the one thing that can be on
// top of a panel, so it goes first and alone: a tap meant to put an error away
// must not also shut the panel that was open behind it.
const initModalBackdrop = () => {
  document.getElementById("modal-backdrop")?.addEventListener("click", () => {
    if (statusVisible()) {
      setStatus("");
      return;
    }
    document.getElementById("settings-panel")?.classList.remove("visible");
    document.getElementById("cheat-panel")?.classList.remove("visible");
  });
};

const initSettings = () => {
  const toggle = document.getElementById("settings-toggle");
  const panel = document.getElementById("settings-panel");

  toggle?.addEventListener("click", () => panel?.classList.toggle("visible"));
  document.getElementById("settings-close")?.addEventListener("click", () =>
    panel?.classList.remove("visible"));

  // Clicking away closes a modal, which is what the panel is on a narrow
  // screen. Docked in the rail it is part of the page, and closing it because
  // the user pressed a key on the keypad would be a surprise. So is dismissing
  // an error that landed on top of the panel and taking the panel with it,
  // which is why the status popup is not "away".
  document.addEventListener("click", event => {
    if (dockedPanels.matches) return;
    if (statusMessage.contains(event.target)) return;
    if (!toggle?.contains(event.target) && !panel?.contains(event.target)) {
      panel?.classList.remove("visible");
    }
  });

  // The rail has room to hold the settings open, so it starts open there and
  // closes again on the way back to a narrow window, where an open panel would
  // be a modal nobody asked for.
  const followLayout = wide => panel?.classList.toggle("visible", wide);
  followLayout(dockedPanels.matches);
  dockedPanels.addEventListener("change", event => {
    followLayout(event.matches);
    if (!event.matches) document.getElementById("cheat-panel")?.classList.remove("visible");
  });

  // The sliders trade the music against the sound effects. They apply
  // immediately and also when audio comes up later, since the graph is built
  // on the first user gesture rather than at load.
  const midiSlider = document.getElementById("volume-midi");
  const waveSlider = document.getElementById("volume-pcm");
  const applyVolumes = () => {
    pageAudio?.setMIDIVolume(Number(midiSlider.value) / 100);
    pageAudio?.setWaveVolume(Number(waveSlider.value) / 100);
  };
  midiSlider?.addEventListener("input", applyVolumes);
  waveSlider?.addEventListener("input", applyVolumes);
  applyVolumes();

  // The magnification filter costs real time per presented frame, so the
  // choice is the user's and it is remembered.
  const scale = document.getElementById("frame-scale");
  const applyScale = value => {
    // The filter runs where the frame is made. A phone receiving magnified
    // pixels has nothing left to do but draw them. The panel is wired before
    // the socket is open, so an early change is stored and carried into the
    // session by the scale that start sends.
    session?.setScale(Number(value));
    localStorage.setItem(FRAME_SCALE_KEY, String(value));
  };
  if (scale) {
    scale.value = localStorage.getItem(FRAME_SCALE_KEY) ?? "1";
    scale.addEventListener("change", () => applyScale(scale.value));
  }

  // Speed is a multiple of the game's own pace, so it applies to a running
  // session and to whatever starts next. Restarting reloads the page, which is
  // why the choice is stored rather than kept in memory.
  // The panel is wired before the socket is open, so the stored choice is only
  // shown here; applyStoredSpeed pushes it down once there is something to
  // push it to.
  const speed = document.getElementById("game-speed");
  speed?.addEventListener("change", () => {
    applySpeed(speed.value);
    rememberSpeed(speed.value);
  });
  if (speed) speed.value = String(storedSpeed());

  // The screen belongs to the game rather than to the page, so the menu shows
  // what the game about to start is set to and changing it is a decision about
  // that game. It cannot apply to a running one — a MIDlet reads its screen
  // once, at startup — so the panel says restart rather than pretending.
  const screen = document.getElementById("screen-size");
  const screenNote = document.getElementById("screen-note");
  if (screen) {
    const showStored = () => {
      const { width, height } = storedScreen(chosenGame());
      screen.value = `${width}x${height}`;
    };
    showStored();
    // The list is what says which game the setting is about, so the menu
    // follows it: picking another game shows that game's screen.
    document.getElementById("game-select")?.addEventListener("change", showStored);
    screen.addEventListener("change", () => {
      const path = chosenGame();
      if (!path) return;
      rememberScreen(path, screen.value);
      if (screenNote) {
        screenNote.textContent = gameRunning ? "다시 시작해야 새 화면 크기로 적용됩니다." : "";
        screenNote.classList.toggle("hidden", !gameRunning);
      }
    });
  }
};

// Whether this browser has a keyboard, which is what decides whether the key
// settings appear at all: on a phone they would be a list of keys nobody can
// press, in the panel that has to stay short enough to use with a thumb.
//
// A media query is the wrong instrument for it. `(pointer: fine)` asks about a
// mouse — it misses a tablet with a keyboard case and answers yes to a desktop
// with a touchscreen and no keyboard at all. The only sound evidence is a key
// actually arriving, so that is what reveals the section, and it is remembered
// so that a later visit does not have to press one again. The media query is
// kept as the opening guess alone, which spares a desktop user the first press.
const KEYBOARD_SEEN_KEY = "wfeature:keyboardSeen";
const keyboardLikely = window.matchMedia("(hover: hover) and (pointer: fine)");

const keyboardSeen = () => {
  try {
    return localStorage.getItem(KEYBOARD_SEEN_KEY) === "1";
  } catch {
    return false;
  }
};

let keyboardNoticed = false;
// Assigned by initKeyBindings; a page without the section still counts presses.
let revealKeyBindings = () => {};

const noticeKeyboard = () => {
  if (keyboardNoticed) return;
  keyboardNoticed = true;
  try {
    localStorage.setItem(KEYBOARD_SEEN_KEY, "1");
  } catch {
    // Private browsing denies storage; the section then has to be earned once
    // per visit, which is one keypress.
  }
  revealKeyBindings();
};

// initKeyBindings draws the key list and takes the presses that change it.
const initKeyBindings = () => {
  const section = document.getElementById("key-bindings");
  const list = document.getElementById("key-bindings-list");
  if (!section || !list) return;

  revealKeyBindings = () => section.classList.remove("hidden");
  if (keyboardSeen() || keyboardLikely.matches) revealKeyBindings();

  // The phone key whose row is waiting for a press, empty when none is.
  let capturing = "";

  const stopCapture = () => {
    capturing = "";
    pendingCapture = null;
    render();
  };

  const apply = next => {
    keyBindings = next;
    keyboardMap = codeLookup(keyBindings);
    try {
      localStorage.setItem(KEY_BINDINGS_KEY, JSON.stringify(keyBindings));
    } catch {
      // Private browsing denies storage; the change then lasts one session.
    }
    render();
  };

  const startCapture = name => {
    capturing = name;
    pendingCapture = code => {
      if (code === "Escape") {
        stopCapture();
        return;
      }
      if (!bindable(code)) {
        // Still waiting: refusing a key the browser needs is not a reason to
        // make the user open the row again.
        setStatus("이 키는 쓸 수 없습니다. 다른 키를 누르세요.");
        return;
      }
      // Asked before the assignment, which is what empties the row.
      const loser = heldBy(keyBindings, code, name);
      capturing = "";
      pendingCapture = null;
      apply(assign(keyBindings, name, code));
      if (loser) {
        setStatus(`${codeLabel(code)} 키를 ${keyLabel(loser)}에서 가져왔습니다. ${keyLabel(loser)}은 이제 비어 있습니다.`);
      }
    };
    render();
  };

  // Nineteen rows are rebuilt rather than patched: there is nothing on them to
  // lose — no text being typed, no scroll of their own — and one path that
  // draws every state is one path to be wrong in.
  const render = () => {
    list.replaceChildren(
      ...keyOrder.map(name => {
        const row = document.createElement("div");
        row.className = "key-binding";

        const label = document.createElement("span");
        label.className = "key-binding-key";
        label.textContent = keyLabel(name);

        const bound = keyBindings[name];
        const code = document.createElement("button");
        code.className = "key-binding-code";
        code.classList.toggle("capturing", capturing === name);
        code.classList.toggle("unbound", !bound && capturing !== name);
        code.textContent = capturing === name ? "키를 누르세요" : codeLabel(bound) || "없음";
        code.addEventListener("click", () => (capturing === name ? stopCapture() : startCapture(name)));

        const drop = document.createElement("button");
        drop.className = "key-binding-clear";
        drop.textContent = "✕";
        drop.setAttribute("aria-label", `${keyLabel(name)} 키 비우기`);
        drop.disabled = !bound;
        drop.addEventListener("click", () => {
          if (capturing === name) stopCapture();
          apply(clearBinding(keyBindings, name));
        });

        row.append(label, code, drop);
        return row;
      }),
    );
  };

  document.getElementById("key-bindings-reset")?.addEventListener("click", () => {
    capturing = "";
    pendingCapture = null;
    apply(loadBindings(null));
  });

  // A row left waiting swallows the next key the game was meant to get, so
  // anything that says the user has moved on disarms it: folding the section
  // away, and touching anything outside it — the keypad, the canvas, the button
  // that closes the panel.
  section.addEventListener("toggle", () => {
    if (!section.open && capturing) stopCapture();
  });
  document.addEventListener("pointerdown", event => {
    if (capturing && !section.contains(event.target)) stopCapture();
  });

  render();
};

const applyStoredSpeed = () => {
  applySpeed(document.getElementById("game-speed")?.value ?? storedSpeed());
};

const SPEED_KEY = "wfeature:speed";

const storedSpeed = () => {
  try {
    const stored = Number(localStorage.getItem(SPEED_KEY));
    return Number.isFinite(stored) && stored > 0 ? stored : 1;
  } catch {
    return 1;
  }
};

const rememberSpeed = value => {
  try {
    localStorage.setItem(SPEED_KEY, String(value));
  } catch {
    // Private browsing denies storage; the setting then lasts one session.
  }
};

const applySpeed = value => {
  const multiplier = Number(value);
  if (!Number.isFinite(multiplier) || multiplier <= 0) return;
  session?.setSpeed(multiplier);
};

// Re-reading every candidate costs a pass over the list, so live refresh only
// kicks in once a search has actually narrowed.
const REFRESH_LIMIT = 1000;
const REFRESH_INTERVAL = 500;

const hex = value => `0x${(value >>> 0).toString(16).padStart(8, "0")}`;

// The address a user types into the watch box, which is hex with or without
// the prefix. null means it was not an address.
const parseAddressInput = text => {
  const trimmed = (text ?? "").trim();
  if (trimmed === "") return null;
  const body = /^0[xX]/.test(trimmed) ? trimmed.slice(2) : trimmed;
  if (!/^[0-9a-fA-F]+$/.test(body)) return null;
  const value = Number.parseInt(body, 16);
  return Number.isFinite(value) ? value >>> 0 : null;
};

// A saved cheat table names the game it was made against; the last started
// archive is the best answer the page has.
const currentGameName = () => (lastGame() ?? "").split("/").pop()?.replace(/\.(zip|jar)$/i, "") ?? "";

const parseNumber = text => {
  const trimmed = text.trim();
  if (trimmed === "") return null;
  const negative = trimmed.startsWith("-");
  const body = negative ? trimmed.slice(1) : trimmed;
  const value = /^0[xX]/.test(body) ? Number.parseInt(body.slice(2), 16) : Number(body);
  return Number.isFinite(value) ? (negative ? -value : value) : null;
};

// cheatEngine answers the panel's operations against the session. The shapes
// are Go's, from internal/cheat, and every call is a promise because every one
// of them is a round trip.
const cheatEngine = () => {
  const call = (op, fields = {}) =>
    session.ask({ kind: "cheat", op, ...fields }).then(message => message.cheat ?? {});
  // A scanning operation answers a count and a list, and both are meaningful
  // at zero — a reset narrows to nothing on purpose. Rendering a missing
  // field would put `undefined` on screen instead of `0`, so the shape is
  // completed here as well as sent complete.
  const candidates = answer => ({ count: answer.count ?? 0, items: answer.items ?? [] });
  return {
    available: () => currentPlatform === "ktf" || currentPlatform === "lgt" || currentPlatform === "skt",
    scan: (type, filter, operand) => call("scan", { type, filter, operand }).then(candidates),
    refresh: () => call("refresh").then(candidates),
    undo: () => call("undo").then(candidates),
    reset: () => call("reset").then(candidates),
    freeze: (address, value, type) => call("freeze", { address, operand: value, type }).then(r => r.frozen ?? []),
    unfreeze: address => call("unfreeze", { address }).then(r => r.frozen ?? []),
    frozen: () => call("frozen").then(r => r.frozen ?? []),
    watch: address => call("watch", { address }),
    unwatchAll: () => call("unwatch", { all: true }),
    hits: () => call("hits").then(r => r.hits ?? { items: [], total: 0, overflowed: false }),
    saveTable: () => call("saveTable").then(r => r.table ?? ""),
    loadTable: text => call("loadTable", { table: text }).then(r => ({ applied: r.applied ?? 0 })),
  };
};

const initCheat = () => {
  const panel = document.getElementById("cheat-panel");
  const toggle = document.getElementById("cheat-toggle");
  if (!panel || !toggle) return;

  // Every platform this emulator runs has an engine now: the two ARM ones
  // search guest memory, and the MIDP runtime searches a synthetic space laid
  // over its object graph. The toggle is still asked rather than assumed,
  // because a platform added later may not have one — and it is hidden rather
  // than removed, because which platform is running changes with the game and
  // a session without an engine must not take the panel away from the next
  // session that has one.
  const cheat = cheatEngine();
  const usable = cheat.available();
  toggle.classList.toggle("hidden", !usable);
  if (!usable) {
    panel.classList.remove("visible");
    return;
  }

  // Write watching is the session's answer too. Not every platform can say
  // what wrote an address, and the ones that cannot say so before the panel
  // opens rather than by failing the first poll.
  for (const id of ["cheat-watch-section", "cheat-watch-row", "cheat-hits"]) {
    document.getElementById(id)?.classList.toggle("hidden", !canWatchWrites);
  }

  // Everything below is the page's, not the session's, so a second game
  // re-reads the answers above and stops here.
  if (cheatWired) {
    resetCheatPanel();
    return;
  }
  cheatWired = true;


  const typeSelect = document.getElementById("cheat-type");
  const bigEndian = document.getElementById("cheat-be");
  const valueInput = document.getElementById("cheat-value");
  const countLabel = document.getElementById("cheat-count");
  const results = document.getElementById("cheat-results");
  const frozenList = document.getElementById("cheat-frozen");

  let lastCount = 0;

  const valueType = () => typeSelect.value + (bigEndian.checked ? "be" : "");

  // Every operation is a promise, because in a session it is a round trip.
  // Failures land in the same place they always did.
  const run = async action => {
    try {
      await action();
    } catch (error) {
      reportError(error);
    }
  };

  // The panel's own status line, used by the controls that have something to
  // report but no list of their own to render it into.
  const setCheatStatus = text => {
    const status = document.getElementById("cheat-status");
    if (status) status.textContent = text;
  };

  const renderResults = result => {
    lastCount = result.count;
    countLabel.textContent = `${result.count.toLocaleString()}개`;
    results.replaceChildren();

    for (const item of result.items) {
      const row = document.createElement("div");
      row.className = "cheat-item";

      const address = document.createElement("span");
      address.className = "cheat-address";
      address.textContent = hex(item.address);

      const value = document.createElement("span");
      value.className = "cheat-value";
      value.textContent = String(item.value);

      const freeze = document.createElement("button");
      freeze.textContent = "고정";
      freeze.addEventListener("click", () => run(async () => {
        renderFrozen(await cheat.freeze(item.address, item.value, valueType()));
      }));

      row.append(address, value, freeze);
      results.appendChild(row);
    }

    if (result.count > result.items.length) {
      const more = document.createElement("div");
      more.className = "cheat-more";
      more.textContent = `... 외 ${(result.count - result.items.length).toLocaleString()}개. 더 좁혀보세요.`;
      results.appendChild(more);
    }
  };

  const renderFrozen = entries => {
    frozenList.replaceChildren();

    for (const entry of entries) {
      const row = document.createElement("div");
      row.className = "cheat-item";

      const address = document.createElement("span");
      address.className = "cheat-address";
      address.textContent = hex(entry.address);

      const input = document.createElement("input");
      input.className = "cheat-freeze-value";
      input.value = String(entry.value);
      input.addEventListener("change", () => run(async () => {
        const value = parseNumber(input.value);
        if (value === null) return;
        await cheat.freeze(entry.address, value, entry.type);
      }));

      const remove = document.createElement("button");
      remove.textContent = "해제";
      remove.addEventListener("click", () => run(async () => {
        renderFrozen(await cheat.unfreeze(entry.address));
      }));

      const type = document.createElement("span");
      type.className = "cheat-type-tag";
      type.textContent = entry.type;

      row.append(address, input, type, remove);
      frozenList.appendChild(row);
    }
  };

  const scan = operation => run(async () => {
    const operand = parseNumber(valueInput.value) ?? 0;
    renderResults(await cheat.scan(valueType(), operation, operand));
  });

  for (const button of panel.querySelectorAll("button[data-filter]")) {
    button.addEventListener("click", () => scan(button.dataset.filter));
  }

  document.getElementById("cheat-undo")?.addEventListener("click", () =>
    run(async () => renderResults(await cheat.undo())));

  document.getElementById("cheat-reset")?.addEventListener("click", () =>
    run(async () => renderResults(await cheat.reset())));

  toggle.addEventListener("click", () => {
    // The toggle lives inside the settings panel, which as a modal would
    // otherwise stay open on top of the cheat panel. Docked in the rail the two
    // sit one above the other and both stay.
    if (!dockedPanels.matches) document.getElementById("settings-panel")?.classList.remove("visible");

    panel.classList.toggle("visible");
    if (panel.classList.contains("visible")) {
      run(async () => renderFrozen(await cheat.frozen()));
    }
  });

  document.getElementById("cheat-close")?.addEventListener("click", () => panel.classList.remove("visible"));

  // Write watching. A scan says where a value is; this says what changes it,
  // which is the part that still means something in the next session.
  const hitsList = document.getElementById("cheat-hits");
  const watchAddress = document.getElementById("cheat-watch-address");
  const renderHits = value => {
    if (!hitsList || !value) return;
    hitsList.textContent = "";
    if (value.items.length === 0) {
      hitsList.textContent = "아직 기록된 쓰기가 없습니다.";
      return;
    }
    for (const hit of value.items) {
      const row = document.createElement("div");
      row.className = "cheat-item";
      const address = document.createElement("span");
      address.className = "cheat-address";
      address.textContent = hex(hit.address);
      const detail = document.createElement("span");
      // A host write's pc is the last guest instruction, not the writer, so it
      // is labelled rather than shown as an address to go and disassemble.
      // A platform whose code is not in the address space names the writer
      // instead of addressing it, and sends that name as `site`.
      const where = hit.site ? hit.site : `pc ${hex(hit.pc)}`;
      const writer = hit.origin === "host" ? `호스트 · 직전 ${where}` : where;
      detail.textContent = `${writer} · ${hit.count}회 · ${hex(hit.value)}`;
      row.append(address, detail);
      hitsList.append(row);
    }
    if (value.overflowed) {
      const note = document.createElement("div");
      note.className = "cheat-more";
      note.textContent = "쓰기 지점이 너무 많아 일부가 빠졌습니다.";
      hitsList.append(note);
    }
  };

  document.getElementById("cheat-watch-add")?.addEventListener("click", () => run(async () => {
    const address = parseAddressInput(watchAddress?.value);
    if (address === null) {
      setCheatStatus("주소를 0x… 형식으로 입력하세요.");
      return;
    }
    await cheat.watch(address);
    renderHits(await cheat.hits());
  }));

  document.getElementById("cheat-watch-clear")?.addEventListener("click", () => run(async () => {
    await cheat.unwatchAll();
    renderHits(await cheat.hits());
  }));

  // Cheat tables. A cheat is found once and used forever, so what the session
  // holds is written to a file the user keeps.
  document.getElementById("cheat-save")?.addEventListener("click", () => run(async () => {
    const table = await cheat.saveTable();
    if (!table) return;
    const blob = new Blob([table], { type: "application/json" });
    const link = document.createElement("a");
    link.href = URL.createObjectURL(blob);
    link.download = `${currentGameName() || "cheats"}.cheats.json`;
    link.click();
    URL.revokeObjectURL(link.href);
  }));

  const tableInput = document.getElementById("cheat-load-file");
  document.getElementById("cheat-load")?.addEventListener("click", () => tableInput?.click());
  tableInput?.addEventListener("change", async () => {
    const file = tableInput.files?.[0];
    if (!file) return;
    const text = await file.text();
    tableInput.value = "";
    run(async () => {
      const loaded = await cheat.loadTable(text);
      if (!loaded) return;
      setCheatStatus(`${loaded.applied}개 값을 적용했습니다.`);
      renderFrozen(await cheat.frozen());
      renderHits(await cheat.hits());
    });
  });

  // A new game gets a new engine on the server, so what is on screen from the
  // last one is not a narrower search — it is a list of addresses in somebody
  // else's memory. The panel is emptied rather than left to be refreshed into
  // agreement.
  resetCheatPanel = () => {
    lastCount = 0;
    renderResults({ count: 0, items: [] });
    renderFrozen([]);
    renderHits({ items: [], total: 0, overflowed: false });
    setCheatStatus("");
  };

  // Keep candidate values and recorded writes live while the panel is open.
  // A refresh still in flight must not be asked for again: over a socket the
  // requests would queue up behind a game that is busy, and the panel would
  // fall further behind the longer it stayed open.
  let refreshing = false;
  setInterval(() => {
    if (!panel.classList.contains("visible") || !gameRunning || refreshing) return;
    refreshing = true;
    void run(async () => {
      // The refresh is the half every platform has, so it goes first: an
      // optional feature must not be able to stop the one the panel is for.
      if (lastCount > 0 && lastCount <= REFRESH_LIMIT) {
        renderResults(await cheat.refresh());
      }
      if (canWatchWrites) renderHits(await cheat.hits());
    }).finally(() => { refreshing = false; });
  }, REFRESH_INTERVAL);
};

const main = async () => {
  initStatus();
  initInput();
  initKeypadLayout();
  initRestart();
  initModalBackdrop();
  initSettings();
  initKeyBindings();

  session = await openSession();
  if (!session) {
    setStatus("서버 세션에 연결하지 못했습니다. 서버가 실행 중인지 확인하고 새로고침하세요.");
    // Nothing said which build this is, so the page keeps the release face:
    // the developer's parts are only ever shown on a debug server's word.
    initLogView(false);
    initDebugLog(false);
    return;
  }
  // Nothing registers a sink here: sound arrives as events over the socket and
  // is played by the synthesiser in this page.
  pageAudio = new PageAudio();
  recordEvent("server session opened");
  applyStoredSpeed();
  // Which build is running is the server's answer, given with "ready". The log
  // rail and the report button belong to a debug run; a release is the same
  // page without them.
  const debugBuild = session.profile === "debug";
  initLogView(debugBuild);
  initDebugLog(debugBuild);
  initResumeOnReturn();
  // A page that was already playing does not ask which game to start: the
  // phone discarding and reloading it in the background is exactly the case
  // the parked game is there for.
  if (storedResumeToken() && (await resumeStoredGame())) return;
  await initGameSelect();
};

void main();

if ("serviceWorker" in navigator) {
  window.addEventListener("load", () => {
    navigator.serviceWorker.register("./service-worker.js").catch(error => {
      console.warn("wfeature service worker registration failed", error);
    });
  });
}
