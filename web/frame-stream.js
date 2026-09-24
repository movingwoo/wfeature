// Full PNGs establish a picture; WFP1 messages replace a rectangle in it.
// Compose every update in wire order before the page coalesces display draws.
// The retained canvas is borrowed by onFrame and remains owned by this receiver.
const maxDimension = 4096; // Maximum handset dimension (1024) at hq4x.
const maxQueuedBytes = 96 * 1024 * 1024;
const maxQueuedFrames = 32;

export class FrameReceiver {
  constructor(onFrame, onError) {
    this.onFrame = onFrame;
    this.onError = onError;
    this.queue = [];
    this.queuedBytes = 0;
    this.reading = false;
    this.closed = false;
    this.canvas = null;
  }

  receive(blob) {
    if (this.closed) return;
    if (this.queue.length >= maxQueuedFrames || this.queuedBytes + blob.size > maxQueuedBytes) {
      this.fail(new Error("frame decode backlog exceeded"));
      return;
    }
    this.queue.push(blob);
    this.queuedBytes += blob.size;
    if (!this.reading) void this.drain();
  }

  async drain() {
    this.reading = true;
    try {
      while (!this.closed && this.queue.length) {
        const blob = this.queue.shift();
        await this.decode(blob);
        if (!this.closed) this.queuedBytes -= blob.size;
      }
    } catch (error) {
      if (!this.closed) this.fail(error);
    } finally {
      this.reading = false;
    }
  }

  async decode(blob) {
    const header = new DataView(await blob.slice(0, 36).arrayBuffer());
    if (this.closed) return;
    const patch = header.byteLength >= 4 && header.getUint32(0) === 0x57465031; // WFP1
    const offset = patch ? 12 : 0;
    if (header.byteLength < offset + 24 || header.getUint32(offset) !== 0x89504e47 ||
        header.getUint32(offset + 4) !== 0x0d0a1a0a) throw new Error("invalid frame header");
    const width = header.getUint32(offset + 16), height = header.getUint32(offset + 20);
    const x = patch ? header.getUint32(4) : 0, y = patch ? header.getUint32(8) : 0;
    if (!width || !height || width > maxDimension || height > maxDimension ||
        (patch && (!this.canvas || x + width > this.canvas.width || y + height > this.canvas.height))) {
      throw new Error("invalid frame dimensions");
    }
    const bitmap = await createImageBitmap(blob.slice(offset, blob.size, "image/png"));
    try {
      if (this.closed) return;
      if (bitmap.width !== width || bitmap.height !== height) throw new Error("decoded frame dimensions differ");
      if (!this.canvas) this.canvas = document.createElement("canvas");
      if (!patch && (this.canvas.width !== width || this.canvas.height !== height)) {
        this.canvas.width = width;
        this.canvas.height = height;
      }
      const context = this.canvas.getContext("2d");
      // Replace alpha as well as RGB. Blending a transparent patch would keep
      // pixels the game erased. The rest of the canvas remains untouched.
      context.clearRect(x, y, width, height);
      context.drawImage(bitmap, x, y);
      this.onFrame?.(this.canvas);
    } finally {
      bitmap.close();
    }
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
  }
}
