// Drawing the game's picture on the page's canvas at the scale the server
// names. hqx.js says why the page, not the server, magnifies.

import { HqxScaler } from "./hqx.js";

// Every phone and desktop this page runs on is little-endian, which reading an
// ImageData buffer one word a pixel assumes. A machine that is not gets a
// plain enlargement rather than wrong colours.
const littleEndian = new Uint8Array(new Uint32Array([1]).buffer)[0] === 1;

// unionRect joins two changed areas; null is all of the picture.
export const unionRect = (first, second) => {
  if (!first || !second) return null;
  return {
    left: Math.min(first.left, second.left),
    top: Math.min(first.top, second.top),
    right: Math.max(first.right, second.right),
    bottom: Math.max(first.bottom, second.bottom),
  };
};

export class Magnifier {
  constructor() {
    this.scaler = new HqxScaler();
    this.reset();
  }

  // reset forgets the held picture, so the next present redraws all of it.
  reset() {
    this.width = 0;
    this.height = 0;
    this.scale = 0;
    this.source = null;
    this.output = null;
    this.outputPixels = null;
  }

  // present draws picture — a canvas at the game's own size — on the canvas
  // behind context, magnified by scale. dirty is what changed in the picture
  // since the last present, as { left, top, right, bottom }, or null for all
  // of it; only the magnified blocks a change can reach are redone.
  present(context, picture, scale, dirty) {
    const { width, height } = picture;
    const canvas = context.canvas;
    if (scale <= 1 || !littleEndian) {
      this.reset();
      if (canvas.width !== width * scale || canvas.height !== height * scale) {
        // The space the canvas occupies is the page's to decide and does not
        // move with the picture; the stylesheet fits one of another shape.
        canvas.width = width * scale;
        canvas.height = height * scale;
      }
      context.imageSmoothingEnabled = false;
      context.clearRect(0, 0, canvas.width, canvas.height);
      context.drawImage(picture, 0, 0, width * scale, height * scale);
      return;
    }
    if (width !== this.width || height !== this.height || scale !== this.scale) {
      this.width = width;
      this.height = height;
      this.scale = scale;
      this.source = new Int32Array(width * height);
      this.output = new ImageData(width * scale, height * scale);
      this.outputPixels = new Int32Array(this.output.data.buffer);
      canvas.width = width * scale;
      canvas.height = height * scale;
      dirty = null;
    }
    const area = dirty ?? { left: 0, top: 0, right: width, bottom: height };
    const areaWidth = area.right - area.left, areaHeight = area.bottom - area.top;
    if (areaWidth <= 0 || areaHeight <= 0) return;
    // Only the changed pixels are read back; the rest are already held.
    const changed = picture.getContext("2d", { willReadFrequently: true })
      .getImageData(area.left, area.top, areaWidth, areaHeight);
    const rows = new Int32Array(changed.data.buffer, changed.data.byteOffset, areaWidth * areaHeight);
    for (let row = 0; row < areaHeight; row++) {
      this.source.set(rows.subarray(row * areaWidth, (row + 1) * areaWidth), (area.top + row) * width + area.left);
    }
    // A block depends on its pixel's neighbours, so the blocks one pixel
    // around the change are redone as well.
    const left = Math.max(0, area.left - 1), top = Math.max(0, area.top - 1);
    const right = Math.min(width, area.right + 1), bottom = Math.min(height, area.bottom + 1);
    this.scaler.scale(this.source, width, height, scale, left, top, right, bottom, this.outputPixels);
    context.putImageData(this.output, 0, 0, left * scale, top * scale, (right - left) * scale, (bottom - top) * scale);
  }
}
