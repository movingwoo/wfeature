// The colour arithmetic the hqx decision tables call for, in the page.
//
// It is the arithmetic of internal/filter/hqx, pixel for pixel, so a picture
// magnified here is the picture the server used to magnify. hqx.test.mjs and
// the Go package's test check the two against the same digest.
//
// A pixel is one 32-bit word of an ImageData buffer, which on every
// little-endian machine is 0xAABBGGRR. The interpolations treat red and blue
// alike, so only the luma/chroma conversion needs to know which is which.
//
// Words are handled signed. An opaque pixel read unsigned is above 2^31, and
// JavaScript engines store such a number boxed rather than as a small integer;
// read through an Int32Array the same bits stay small integers, and the
// filter ran measurably slower the other way.

// rgbToYUV converts a colour to the space the difference test uses.
export const rgbToYUV = color => {
  const red = color & 0xff;
  const green = (color >>> 8) & 0xff;
  const blue = (color >>> 16) & 0xff;
  const y = Math.trunc(0.299 * red + 0.587 * green + 0.114 * blue);
  const u = Math.trunc(-0.169 * red - 0.331 * green + 0.5 * blue) + 128;
  const v = Math.trunc(0.5 * red - 0.419 * green - 0.081 * blue) + 128;
  return (y << 16) + (u << 8) + v;
};

// yuvDiff reports whether two converted colours belong to different shapes:
// loose on luma, tight on chroma.
export const yuvDiff = (first, second) =>
  Math.abs((first & 0xff0000) - (second & 0xff0000)) > 0x300000 ||
  Math.abs((first & 0xff00) - (second & 0xff00)) > 0x700 ||
  Math.abs((first & 0xff) - (second & 0xff)) > 6;

// Weighted averages on the masked channel groups. JavaScript's shifts work on
// 32-bit integers exactly as the Go code's int32 arithmetic does, including
// the alpha term overflowing past bit 31. The three groups share no bits, so
// their sum is the word they make together.
const interpolate2 = (first, firstWeight, second, secondWeight, shift) => {
  if (first === second) return first;
  const alpha = (((first >>> 24) * firstWeight + (second >>> 24) * secondWeight) << (24 - shift)) & 0xff000000;
  const green = (((first & 0xff00) * firstWeight + (second & 0xff00) * secondWeight) >> shift) & 0xff00;
  const redBlue = (((first & 0xff00ff) * firstWeight + (second & 0xff00ff) * secondWeight) >> shift) & 0xff00ff;
  return (alpha + green + redBlue) | 0;
};

const interpolate3 = (first, firstWeight, second, secondWeight, third, thirdWeight, shift) => {
  const alpha = (((first >>> 24) * firstWeight + (second >>> 24) * secondWeight + (third >>> 24) * thirdWeight) << (24 - shift)) & 0xff000000;
  const green = (((first & 0xff00) * firstWeight + (second & 0xff00) * secondWeight + (third & 0xff00) * thirdWeight) >> shift) & 0xff00;
  const redBlue = (((first & 0xff00ff) * firstWeight + (second & 0xff00ff) * secondWeight + (third & 0xff00ff) * thirdWeight) >> shift) & 0xff00ff;
  return (alpha + green + redBlue) | 0;
};

// The named blends. The comment on each is the arithmetic it performs.
export const interp1 = (c1, c2) => interpolate2(c1, 3, c2, 1, 2); // (c1*3 + c2) / 4
export const interp2 = (c1, c2, c3) => interpolate3(c1, 2, c2, 1, c3, 1, 2); // (c1*2 + c2 + c3) / 4
export const interp3 = (c1, c2) => interpolate2(c1, 7, c2, 1, 3); // (c1*7 + c2) / 8
export const interp4 = (c1, c2, c3) => interpolate3(c1, 2, c2, 7, c3, 7, 4); // (c1*2 + (c2+c3)*7) / 16
export const interp5 = (c1, c2) => interpolate2(c1, 1, c2, 1, 1); // (c1 + c2) / 2
export const interp6 = (c1, c2, c3) => interpolate3(c1, 5, c2, 2, c3, 1, 3); // (c1*5 + c2*2 + c3) / 8
export const interp7 = (c1, c2, c3) => interpolate3(c1, 6, c2, 1, c3, 1, 3); // (c1*6 + c2 + c3) / 8
export const interp8 = (c1, c2) => interpolate2(c1, 5, c2, 3, 3); // (c1*5 + c2*3) / 8
export const interp9 = (c1, c2, c3) => interpolate3(c1, 2, c2, 3, c3, 3, 3); // (c1*2 + (c2+c3)*3) / 8
export const interp10 = (c1, c2, c3) => interpolate3(c1, 14, c2, 1, c3, 1, 4); // (c1*14 + c2 + c3) / 16
