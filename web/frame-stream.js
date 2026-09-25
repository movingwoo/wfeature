// Pictures from the server, composed in wire order on a retained canvas.
//
// A protocol 2 message is a sixteen-byte header and usually a PNG; see
// docs/session.md. It is a complete picture, a rectangle that replaces what is
// under it, or a rectangle drawn over the held picture, where a transparent
// pixel keeps the one that was there — optionally after the held picture has
// been drawn shifted over itself, which is how a scrolling field arrives as
// the strip it uncovered. A bare PNG is a complete picture from a server that
// speaks only the first protocol and has already magnified it.
//
// Every message builds on all the ones before it, so they are decoded one at
// a time, in order, before the page coalesces its display draws. The canvas
// is borrowed by onFrame and stays owned by this receiver.
const maxDimension = 4096; // Maximum handset dimension (1024) at hq4x.
const maxQueuedBytes = 96 * 1024 * 1024;
const maxQueuedFrames = 32;

const pictureMagic = 0x57465032; // "WFP2"
const COMPLETE = 0, REPLACE = 1, MASKED = 2;

export class FrameReceiver {
  constructor(onFrame, onError) {
    this.onFrame = onFrame;
    this.onError = onError;
    this.queue = [];
    this.queuedBytes = 0;
    this.reading = false;
    this.closed = false;
    this.canvas = null;
    this.context = null;
    this.scratch = null;
  }

  // receive takes one binary message as an ArrayBuffer.
  receive(buffer) {
    if (this.closed) return;
    if (this.queue.length >= maxQueuedFrames || this.queuedBytes + buffer.byteLength > maxQueuedBytes) {
      this.fail(new Error("frame decode backlog exceeded"));
      return;
    }
    this.queue.push(buffer);
    this.queuedBytes += buffer.byteLength;
    if (!this.reading) void this.drain();
  }

  async drain() {
    this.reading = true;
    try {
      while (!this.closed && this.queue.length) {
        const buffer = this.queue.shift();
        await this.decode(buffer);
        if (!this.closed) this.queuedBytes -= buffer.byteLength;
      }
    } catch (error) {
      if (!this.closed) this.fail(error);
    } finally {
      this.reading = false;
    }
  }

  async decode(buffer) {
    const bytes = new Uint8Array(buffer);
    const view = new DataView(buffer);
    let operation = COMPLETE, scale = 1, dx = 0, dy = 0, x = 0, y = 0, offset = 0;
    if (bytes.length >= 4 && view.getUint32(0) === pictureMagic) {
      if (bytes.length < 16 || view.getUint16(14) !== 0) throw new Error("invalid frame header");
      operation = bytes[4];
      scale = bytes[5];
      dx = view.getInt16(6);
      dy = view.getInt16(8);
      x = view.getUint16(10);
      y = view.getUint16(12);
      offset = 16;
      if (operation > MASKED || scale < 1 || scale > 4 || ((dx || dy) && operation !== MASKED) ||
          (operation === COMPLETE && (x || y))) {
        throw new Error("invalid frame header");
      }
    }
    // A masked update whose shift already produced every pixel has no PNG.
    const drawn = bytes.length > offset;
    let width = 0, height = 0;
    if (drawn) {
      if (bytes.length < offset + 24 || view.getUint32(offset) !== 0x89504e47 ||
          view.getUint32(offset + 4) !== 0x0d0a1a0a) throw new Error("invalid frame header");
      width = view.getUint32(offset + 16);
      height = view.getUint32(offset + 20);
      if (!width || !height || width > maxDimension || height > maxDimension) {
        throw new Error("invalid frame dimensions");
      }
    } else if (operation !== MASKED || !(dx || dy)) {
      throw new Error("invalid frame header");
    }
    if (operation !== COMPLETE) {
      const base = this.canvas;
      if (!base || x + width > base.width || y + height > base.height ||
          Math.abs(dx) >= base.width || Math.abs(dy) >= base.height) {
        throw new Error("invalid frame dimensions");
      }
    }
    const bitmap = drawn ? await createImageBitmap(new Blob([bytes.subarray(offset)], { type: "image/png" })) : null;
    try {
      if (this.closed) return;
      if (bitmap && (bitmap.width !== width || bitmap.height !== height)) {
        throw new Error("decoded frame dimensions differ");
      }
      if (!this.canvas) this.canvas = document.createElement("canvas");
      // The magnifier reads pixels back from this canvas, so it is kept where
      // reading is cheap.
      this.context ??= this.canvas.getContext("2d", { willReadFrequently: true });
      const context = this.context;
      // What changed, for a magnifier that redoes only that; null is all.
      let dirty = null;
      if (operation === COMPLETE) {
        if (this.canvas.width !== width || this.canvas.height !== height) {
          this.canvas.width = width;
          this.canvas.height = height;
        }
        context.clearRect(0, 0, width, height);
        context.drawImage(bitmap, 0, 0);
      } else {
        if (dx || dy) this.shift(dx, dy);
        else dirty = { left: x, top: y, right: x + width, bottom: y + height };
        if (bitmap) {
          // A replacing rectangle takes alpha as well as colour: drawing a
          // transparent pixel over the old one would keep what the game erased.
          if (operation === REPLACE) context.clearRect(x, y, width, height);
          context.drawImage(bitmap, x, y);
        }
      }
      this.onFrame?.(this.canvas, { scale, dirty });
    } finally {
      bitmap?.close();
    }
  }

  // shift draws the held picture over itself moved by (dx, dy). The strip it
  // uncovers keeps what it had, which is what the server predicted.
  shift(dx, dy) {
    const { width, height } = this.canvas;
    this.scratch ??= document.createElement("canvas");
    if (this.scratch.width !== width || this.scratch.height !== height) {
      this.scratch.width = width;
      this.scratch.height = height;
    }
    const scratch = this.scratch.getContext("2d");
    scratch.clearRect(0, 0, width, height);
    scratch.drawImage(this.canvas, 0, 0);
    this.context.drawImage(this.scratch, dx, dy);
  }

  fail(error) {
    this.close();
    this.onError?.(error);
  }

  close() {
    this.closed = true;
    this.queue = [];
    this.queuedBytes = 0;
    this.canvas = null;
    this.context = null;
    this.scratch = null;
  }
}
