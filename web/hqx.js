// hq2x, hq3x and hq4x in the page.
//
// The server used to magnify each picture and send the result, so every
// picture it sent was four to sixteen times the pixels the game drew: over
// recorded play, a session at hq2x cost 3.4 to 3.8 times the traffic of the
// same session at the original size. The page now receives the game's own
// picture and magnifies it here, with the server's decision tables and
// arithmetic, redoing only the blocks a change can reach.

import { hq2xPatterns, hq3xPatterns, hq4xPatterns } from "./hqx-patterns.js";
import { rgbToYUV, yuvDiff } from "./hqx-blend.js";

const tables = { 2: hq2xPatterns, 3: hq3xPatterns, 4: hq4xPatterns };

// HqxScaler keeps its working planes between pictures. Not safe to share
// between two pictures of different sizes at once, which nothing here does.
export class HqxScaler {
  constructor() {
    this.yuv = new Int32Array(0);
    this.w = new Int32Array(10);
    this.neighbourYUV = new Int32Array(10);
  }

  // scale writes the magnified blocks of the source pixels in
  // [left, right) x [top, bottom) into dst. src is the whole picture and dst
  // the whole magnified one, each an Int32Array over an ImageData-shaped
  // buffer, one word a pixel; see hqx-blend.js for why signed. A block reads
  // the pixel's eight neighbours, and a pixel on the picture's edge repeats
  // itself outward, exactly as on the server.
  scale(src, width, height, factor, left, top, right, bottom, dst) {
    const apply = tables[factor];
    if (!apply) throw new Error(`hqx factor ${factor} is not 2, 3, or 4`);
    if (this.yuv.length < width * height) this.yuv = new Int32Array(width * height);
    const yuv = this.yuv;
    // Every pixel the blocks read is the rectangle and a pixel around it, and
    // each is converted once rather than once per neighbour.
    const readLeft = Math.max(0, left - 1), readRight = Math.min(width, right + 1);
    for (let row = Math.max(0, top - 1), end = Math.min(height, bottom + 1); row < end; row++) {
      for (let index = row * width + readLeft, last = row * width + readRight; index < last; index++) {
        yuv[index] = rgbToYUV(src[index]);
      }
    }
    const w = this.w, n = this.neighbourYUV;
    const dstRowElements = width * factor;
    for (let row = top; row < bottom; row++) {
      const previousRow = row > 0 ? -width : 0;
      const nextRow = row < height - 1 ? width : 0;
      let srcIndex = row * width + left;
      let dstIndex = row * factor * dstRowElements + left * factor;
      for (let column = left; column < right; column++, srcIndex++, dstIndex += factor) {
        w[2] = src[srcIndex + previousRow]; n[2] = yuv[srcIndex + previousRow];
        w[5] = src[srcIndex]; n[5] = yuv[srcIndex];
        w[8] = src[srcIndex + nextRow]; n[8] = yuv[srcIndex + nextRow];
        if (column > 0) {
          w[1] = src[srcIndex + previousRow - 1]; n[1] = yuv[srcIndex + previousRow - 1];
          w[4] = src[srcIndex - 1]; n[4] = yuv[srcIndex - 1];
          w[7] = src[srcIndex + nextRow - 1]; n[7] = yuv[srcIndex + nextRow - 1];
        } else {
          w[1] = w[2]; w[4] = w[5]; w[7] = w[8];
          n[1] = n[2]; n[4] = n[5]; n[7] = n[8];
        }
        if (column < width - 1) {
          w[3] = src[srcIndex + previousRow + 1]; n[3] = yuv[srcIndex + previousRow + 1];
          w[6] = src[srcIndex + 1]; n[6] = yuv[srcIndex + 1];
          w[9] = src[srcIndex + nextRow + 1]; n[9] = yuv[srcIndex + nextRow + 1];
        } else {
          w[3] = w[2]; w[6] = w[5]; w[9] = w[8];
          n[3] = n[2]; n[6] = n[5]; n[9] = n[8];
        }
        const centre = w[5], centreYUV = n[5];
        // A pixel whose eight neighbours are all its own colour magnifies to
        // a block of that colour: every blend the tables make is then of one
        // colour with itself. Flat areas are most of a picture.
        if (w[1] === centre && w[2] === centre && w[3] === centre && w[4] === centre &&
            w[6] === centre && w[7] === centre && w[8] === centre && w[9] === centre) {
          for (let line = 0, start = dstIndex; line < factor; line++, start += dstRowElements) {
            dst.fill(centre, start, start + factor);
          }
          continue;
        }
        // One bit per neighbour, set when it is a different enough colour to
        // be a different shape.
        let pattern = 0;
        if (w[1] !== centre && yuvDiff(centreYUV, n[1])) pattern |= 1;
        if (w[2] !== centre && yuvDiff(centreYUV, n[2])) pattern |= 2;
        if (w[3] !== centre && yuvDiff(centreYUV, n[3])) pattern |= 4;
        if (w[4] !== centre && yuvDiff(centreYUV, n[4])) pattern |= 8;
        if (w[6] !== centre && yuvDiff(centreYUV, n[6])) pattern |= 16;
        if (w[7] !== centre && yuvDiff(centreYUV, n[7])) pattern |= 32;
        if (w[8] !== centre && yuvDiff(centreYUV, n[8])) pattern |= 64;
        if (w[9] !== centre && yuvDiff(centreYUV, n[9])) pattern |= 128;
        apply[pattern](dst, dstIndex, dstRowElements, w, n);
      }
    }
  }
}
