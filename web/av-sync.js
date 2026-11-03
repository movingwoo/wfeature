// Moving the picture and the sound against each other.
//
// The page draws a picture as soon as it has composed it, and schedules a
// sound one lead (100–250 ms, from what the browser reports of its output)
// ahead of its render clock (audio.js). An output the browser cannot see
// into — a Bluetooth headset adds a further 150–300 ms — leaves the sound
// behind the picture by an amount nothing on the page can measure, and a
// display that processes its picture can do the opposite. So the person
// listening sets it: a positive value holds each picture back that many
// milliseconds, a negative one holds the sound back.
//
// Holding a picture back means keeping it. The receiver composes every update
// into one canvas it lends to the page (frame-stream.js), so a picture shown
// later is a copy taken when it arrived. The copies are bounded by bytes; past
// that, the oldest waiting copy is dropped and what it changed is kept in the
// next one's dirty area, so the magnifier still redraws it.
//
// Holding the sound back is the audio module's: the delay joins its lead, so
// every timed sound is scheduled that much later than it would have been.

import { unionRect } from "./magnify.js";
import { local } from "./storage.js";

export const AV_SYNC_KEY = "wfeature:avSync";
export const AV_SYNC_MIN = -500;
export const AV_SYNC_MAX = 1000;
export const AV_SYNC_STEP = 10;

// What is held at most: over three seconds of 240x320 pictures at sixty a
// second. A larger screen holds fewer; past the bound the oldest waiting copy
// is dropped, so the picture skips a frame and the delay stays what was set.
const MAX_HELD_BYTES = 64 * 1024 * 1024;
// Copies kept for reuse once shown, so a steady delay allocates nothing.
const MAX_SPARE = 4;

// clampAVSync answers a stored or typed value as a whole step within range,
// and 0 for anything that is not a number.
export const clampAVSync = value => {
  if (value === null || value === undefined || value === "") return 0;
  const number = Number(value);
  if (!Number.isFinite(number)) return 0;
  const stepped = Math.round(number / AV_SYNC_STEP) * AV_SYNC_STEP;
  return Math.min(AV_SYNC_MAX, Math.max(AV_SYNC_MIN, stepped)) + 0;
};

export const storedAVSync = (storage = local) => {
  try {
    return clampAVSync(storage?.getItem(AV_SYNC_KEY));
  } catch {
    return 0;
  }
};

export const rememberAVSync = (value, storage = local) => {
  try {
    return storage?.setItem(AV_SYNC_KEY, String(clampAVSync(value))) !== false;
  } catch {
    return false;
  }
};

// The picture's share and the sound's share of one setting, in milliseconds
// and seconds: only one of them is ever nonzero.
export const pictureDelay = value => Math.max(0, clampAVSync(value));
export const soundDelay = value => Math.max(0, -clampAVSync(value)) / 1000;

// A dirty area of null is the whole picture, which absorbs any other.
const joinDirty = (first, second) => (first === null || second === null ? null : unionRect(first, second));

const defaultCanvas = (width, height) => {
  const canvas = globalThis.document.createElement("canvas");
  canvas.width = width;
  canvas.height = height;
  return canvas;
};

// DelayedPictures holds copies of the borrowed picture until they are due and
// shows the newest one due on each animation frame, joined with what every
// earlier one it skipped changed.
export class DelayedPictures {
  constructor({
    show,
    now = () => performance.now(),
    schedule = callback => requestAnimationFrame(callback),
    cancel = ticket => cancelAnimationFrame(ticket),
    createCanvas = defaultCanvas,
    maxBytes = MAX_HELD_BYTES,
  } = {}) {
    Object.assign(this, { show, now, schedule, cancel, createCanvas, maxBytes });
    this.delay = 0;
    this.held = [];
    this.spare = [];
    this.bytes = 0;
    this.ticket = null;
  }

  get holding() {
    return this.held.length;
  }

  // setDelay changes how long a picture waits. Whatever is waiting is shown
  // at once, so a shorter delay never leaves an older picture on screen
  // after a newer one.
  setDelay(milliseconds) {
    const delay = Math.max(0, Number(milliseconds) || 0);
    if (delay === this.delay) return;
    this.delay = delay;
    this.flush();
  }

  // push copies the borrowed picture and holds it for the delay.
  push(picture, { scale = 1, dirty = null } = {}) {
    const width = picture?.width ?? 0, height = picture?.height ?? 0;
    if (!width || !height) return;
    const canvas = this.copy(picture, width, height);
    this.held.push({ canvas, scale, dirty, due: this.now() + this.delay });
    this.bytes += width * height * 4;
    while (this.held.length > 1 && this.bytes > this.maxBytes) {
      const dropped = this.held.shift();
      this.held[0].dirty = joinDirty(dropped.dirty, this.held[0].dirty);
      this.release(dropped);
    }
    this.wake();
  }

  copy(picture, width, height) {
    const index = this.spare.findIndex(canvas => canvas.width === width && canvas.height === height);
    const canvas = index >= 0 ? this.spare.splice(index, 1)[0] : this.createCanvas(width, height);
    const context = canvas.getContext("2d");
    // A copy replaces every pixel, transparent ones included.
    context.globalCompositeOperation = "copy";
    context.drawImage(picture, 0, 0);
    return canvas;
  }

  release(entry) {
    this.bytes -= entry.canvas.width * entry.canvas.height * 4;
    if (this.spare.length < MAX_SPARE) this.spare.push(entry.canvas);
  }

  wake() {
    if (this.ticket === null && this.held.length) this.ticket = this.schedule(() => this.tick());
  }

  tick() {
    this.ticket = null;
    const now = this.now();
    let due = 0;
    while (due < this.held.length && this.held[due].due <= now) due++;
    if (due) this.present(this.held.splice(0, due));
    this.wake();
  }

  // flush shows the newest waiting picture now.
  flush() {
    if (this.ticket !== null) this.cancel(this.ticket);
    this.ticket = null;
    if (this.held.length) this.present(this.held.splice(0));
  }

  present(entries) {
    let dirty = entries[0].dirty;
    for (const entry of entries.slice(1)) dirty = joinDirty(dirty, entry.dirty);
    const shown = entries.at(-1);
    this.show(shown.canvas, { scale: shown.scale, dirty });
    for (const entry of entries) this.release(entry);
  }

  // reset forgets every waiting picture: the session they belong to is over.
  reset() {
    if (this.ticket !== null) this.cancel(this.ticket);
    this.ticket = null;
    for (const entry of this.held.splice(0)) this.release(entry);
  }
}
