// The screen that hands a phone the address to open.
//
// Everything a person had to do before this was a step they could get wrong:
// find the machine's address with `ipconfig`, read four numbers off a console,
// retype them into a phone keyboard with a port on the end. That is the step
// most people gave up on, and none of it was ever the user's to do — the
// server knows its own address on the network, and a camera moves it better
// than a thumb does.
//
// The drawing is deliberately dumb: black squares on white, a four-module
// quiet zone, and an integer scale so no module lands on half a pixel. A QR a
// camera cannot read is worse than no QR, and every trick that would make this
// prettier costs sharpness.

import { encodeQr } from "./qr.js";

// quietZone is four modules on every side, which the specification asks for
// and phone cameras genuinely need: a code that runs to the edge of a bright
// panel is one a camera hunts for.
const quietZone = 4;

// targetPixels is how big the code should end up, near enough. The scale is
// then floored to a whole number of pixels per module, because a fractional
// one is a blurred edge and a blurred edge is a code that takes three tries to
// scan.
const targetPixels = 240;

// drawQr paints one code onto a canvas and returns what it drew, so a test can
// check the module grid without a browser.
export const drawQr = (canvas, text) => {
  const { modules, size } = encodeQr(text, { level: "M" });
  const scale = Math.max(2, Math.floor(targetPixels / (size + quietZone * 2)));
  const pixels = (size + quietZone * 2) * scale;

  canvas.width = pixels;
  canvas.height = pixels;
  const context = canvas.getContext("2d");
  if (context) {
    // White first, including the quiet zone: the panel behind this may be any
    // colour, and a code on a dark background does not scan.
    context.fillStyle = "#ffffff";
    context.fillRect(0, 0, pixels, pixels);
    context.fillStyle = "#000000";
    for (let row = 0; row < size; row++) {
      for (let column = 0; column < size; column++) {
        if (!modules[row][column]) continue;
        context.fillRect(
          (column + quietZone) * scale,
          (row + quietZone) * scale,
          scale,
          scale,
        );
      }
    }
  }
  return { size, scale, pixels };
};

// fetchConnectURL asks the server where a phone should look. The page cannot
// work this out for itself: its own location is whatever was typed to reach it,
// which on this machine is 127.0.0.1 — an address that means the phone when a
// phone opens it — and on a server that asks for a key, the key is in a cookie
// this page is not allowed to read.
export const fetchConnectURL = async (fetcher = fetch) => {
  const response = await fetcher("api/connect", { cache: "no-store" });
  // A server older than this page has no such route, and that is a different
  // thing from a server with no address to give. Saying so is the difference
  // between "restart the server" and "your network is the problem" — the first
  // real use of this panel was a stale server wearing the second message.
  if (response.status === 404) throw new OldServer();
  if (!response.ok) throw new Error(`api/connect answered ${response.status}`);
  const answer = await response.json();
  return typeof answer?.url === "string" ? answer.url : "";
};

// OldServer names the one failure with a fix the user can carry out.
export class OldServer extends Error {
  constructor() {
    super("the server does not serve /api/connect");
    this.name = "OldServer";
  }
}

// noAddress is what the panel says when the server has no address to give: a
// machine with nothing but loopback, or one behind a proxy that owns the
// address a phone would use. It is a sentence about this machine rather than
// an error, because nothing has failed.
export const noAddress =
  "이 컴퓨터의 네트워크 주소를 찾지 못했습니다. 서버가 프록시 뒤에 있거나, 이 컴퓨터가 네트워크에 연결되어 있지 않습니다.";

// What a stale server looks like from here. The page is served from disk while
// the binary serving it can be an older build, so this is a state a developer
// meets often and a user meets after updating only half of an archive.
export const oldServer =
  "서버가 이 페이지보다 오래된 빌드입니다. 서버를 다시 시작하면 주소가 나옵니다.";

// And what a server that did not answer at all looks like.
export const noAnswer =
  "서버에 주소를 물어보지 못했습니다. 서버가 아직 실행 중인지 확인하세요.";

// initConnect wires the panel. It asks the server only when the panel is
// opened, and only once: the address does not change while the server runs.
export const initConnect = ({ document, fetcher = fetch, clipboard, docked } = {}) => {
  const panel = document.getElementById("connect-panel");
  const toggle = document.getElementById("connect-toggle");
  if (!panel || !toggle) return;

  const canvas = document.getElementById("connect-qr");
  const label = document.getElementById("connect-url");
  const copy = document.getElementById("connect-copy");
  // The answer is kept, because the address does not change while the server
  // runs. A failure is not: the fix for both of them is on the other side of
  // this button, so pressing it again after restarting the server asks again.
  let loaded = null;
  const ask = () => fetchConnectURL(fetcher)
    .then(url => ({ url }))
    .catch(error => {
      loaded = null;
      return { url: "", problem: error instanceof OldServer ? oldServer : noAnswer };
    });

  const show = async () => {
    loaded ??= ask();
    const { url, problem } = await loaded;
    if (!url) {
      if (label) label.textContent = problem ?? noAddress;
      if (canvas) canvas.classList.add("hidden");
      if (copy) copy.classList.add("hidden");
      return;
    }
    if (label) label.textContent = url;
    if (canvas) {
      canvas.classList.remove("hidden");
      drawQr(canvas, url);
    }
    if (copy) copy.classList.remove("hidden");
  };

  toggle.addEventListener("click", () => {
    // The button lives inside the settings panel, which is a modal on a narrow
    // layout and would otherwise stay open on top of this one. Docked in the
    // rail the two sit one above the other and both stay. This is the rule the
    // cheat panel already follows, for the same reason.
    if (!docked?.matches) {
      document.getElementById("settings-panel")?.classList.remove("visible");
    }
    const opening = !panel.classList.contains("visible");
    panel.classList.toggle("visible", opening);
    if (opening) void show();
  });
  document.getElementById("connect-close")?.addEventListener("click", () =>
    panel.classList.remove("visible"));

  copy?.addEventListener("click", async () => {
    const { url } = (await loaded) ?? {};
    if (!url) return;
    const previous = copy.textContent;
    try {
      await (clipboard ?? navigator.clipboard).writeText(url);
      copy.textContent = "복사했습니다";
    } catch {
      // Clipboard access is refused often enough — an insecure origin, a
      // browser that wants a gesture it did not see — that the address stays
      // on screen to be read out instead of the failure being reported.
      copy.textContent = "복사할 수 없습니다";
    }
    setTimeout(() => { copy.textContent = previous; }, 1500);
  });
};
