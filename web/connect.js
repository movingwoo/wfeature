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
  if (!response.ok) throw new Error(`api/connect answered ${response.status}`);
  const answer = await response.json();
  return typeof answer?.url === "string" ? answer.url : "";
};

// noAddress is what the panel says when the server has no address to give: a
// machine with nothing but loopback, or one behind a proxy that owns the
// address a phone would use. It is a sentence about this machine rather than
// an error, because nothing has failed.
export const noAddress =
  "이 컴퓨터의 네트워크 주소를 찾지 못했습니다. 서버가 프록시 뒤에 있거나, 이 컴퓨터가 네트워크에 연결되어 있지 않습니다.";

// initConnect wires the panel. It asks the server only when the panel is
// opened, and only once: the address does not change while the server runs.
export const initConnect = ({ document, fetcher = fetch, clipboard } = {}) => {
  const panel = document.getElementById("connect-panel");
  const toggle = document.getElementById("connect-toggle");
  if (!panel || !toggle) return;

  const canvas = document.getElementById("connect-qr");
  const label = document.getElementById("connect-url");
  const copy = document.getElementById("connect-copy");
  let loaded = null;

  const show = async () => {
    if (!loaded) loaded = fetchConnectURL(fetcher).catch(() => "");
    const url = await loaded;
    if (!url) {
      // A failed ask is answered the same way as no address at all. The
      // difference matters to a developer and not to the person looking at
      // this panel, and the log has the other half.
      if (label) label.textContent = noAddress;
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
    const opening = !panel.classList.contains("visible");
    panel.classList.toggle("visible", opening);
    if (opening) void show();
  });
  document.getElementById("connect-close")?.addEventListener("click", () =>
    panel.classList.remove("visible"));

  copy?.addEventListener("click", async () => {
    const url = await loaded;
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
