// Copyright (c) 2017 Christopher Serr. Licensed under MIT OR Apache-2.0.
// This file is a mechanical translation of that work's decision tables, made
// from internal/filter/hqx by TestJavaScriptPatternsMatchTheGoTables; see
// THIRD-PARTY-NOTICES.md. Regenerate it rather than editing it.
//
// Each case of a Go table is a function here, and the table is an array of
// them indexed by pattern. One function holding all 256 cases was optimised
// before most cases had run and thrown away each time a new one did.
//
// w holds the pixel and its eight neighbours and n their converted colours,
// so a difference test reads n where the Go tables convert w again.

import {
  yuvDiff, interp1, interp2, interp3, interp4, interp5, interp6, interp7, interp8, interp9, interp10,
} from "./hqx-blend.js";

const hq2xCase0 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
};

const hq2xCase1 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[4]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[6]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
};

const hq2xCase2 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[2]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8]);
};

const hq2xCase3 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6]);
};

const hq2xCase4 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
};

const hq2xCase5 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[6]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
};

const hq2xCase6 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[4]);
  dst[dstIndex+1] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
};

const hq2xCase7 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8]);
};

const hq2xCase8 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[2]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8]);
};

const hq2xCase9 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6]);
};

const hq2xCase10 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6]);
};

const hq2xCase11 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
};

const hq2xCase12 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
};

const hq2xCase13 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[4]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8]);
};

const hq2xCase14 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[2]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
  }
};

const hq2xCase15 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6]);
};

const hq2xCase16 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = interp2(w[5], w[3], w[6]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
};

const hq2xCase17 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[4]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[6]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6]);
};

const hq2xCase18 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[2]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8]);
};

const hq2xCase19 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
};

const hq2xCase20 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8]);
};

const hq2xCase21 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6]);
};

const hq2xCase22 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
};

const hq2xCase23 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[4]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8]);
};

const hq2xCase24 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[2]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
  }
};

const hq2xCase25 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6]);
};

const hq2xCase26 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = interp2(w[5], w[3], w[6]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
};

const hq2xCase27 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex] = interp1(w[5], w[4]);
    dst[dstIndex+1] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex] = interp6(w[5], w[2], w[4]);
    dst[dstIndex+1] = interp9(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8]);
};

const hq2xCase28 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[4]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = interp1(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8]);
  } else {
    dst[dstIndex+1] = interp9(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 1)] = interp6(w[5], w[6], w[8]);
  }
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
};

const hq2xCase29 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  if (yuvDiff(n[6], n[8])) {
    dst[dstIndex+1] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
  } else {
    dst[dstIndex+1] = interp6(w[5], w[6], w[2]);
    dst[(dstIndex + dstRowElements + 1)] = interp9(w[5], w[6], w[8]);
  }
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4]);
};

const hq2xCase30 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[2]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements)] = interp6(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements + 1)] = interp9(w[5], w[6], w[8]);
  }
};

const hq2xCase31 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
    dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6]);
  } else {
    dst[(dstIndex + dstRowElements)] = interp9(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements + 1)] = interp6(w[5], w[8], w[6]);
  }
};

const hq2xCase32 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[8], n[4])) {
    dst[dstIndex] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
  } else {
    dst[dstIndex] = interp6(w[5], w[4], w[2]);
    dst[(dstIndex + dstRowElements)] = interp9(w[5], w[8], w[4]);
  }
  dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6]);
};

const hq2xCase33 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8]);
  } else {
    dst[dstIndex] = interp9(w[5], w[4], w[2]);
    dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[8]);
  }
  dst[dstIndex+1] = interp2(w[5], w[3], w[6]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
};

const hq2xCase34 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
    dst[dstIndex+1] = interp1(w[5], w[6]);
  } else {
    dst[dstIndex] = interp9(w[5], w[4], w[2]);
    dst[dstIndex+1] = interp6(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
};

const hq2xCase35 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[6]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6]);
};

const hq2xCase36 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[4]);
  dst[dstIndex+1] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6]);
};

const hq2xCase37 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8]);
};

const hq2xCase38 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[2]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8]);
};

const hq2xCase39 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[4]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[6]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6]);
};

const hq2xCase40 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[4]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6]);
};

const hq2xCase41 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8]);
};

const hq2xCase42 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[2]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8]);
};

const hq2xCase43 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8]);
};

const hq2xCase44 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[4]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
  }
};

const hq2xCase45 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[2]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
  }
};

const hq2xCase46 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = interp2(w[5], w[3], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6]);
};

const hq2xCase47 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8]);
};

const hq2xCase48 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[4]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
};

const hq2xCase49 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
  }
};

const hq2xCase50 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6]);
};

const hq2xCase51 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8]);
};

const hq2xCase52 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[4]);
  dst[dstIndex+1] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
  }
};

const hq2xCase53 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[2]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
};

const hq2xCase54 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = interp2(w[5], w[3], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6]);
};

const hq2xCase55 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8]);
};

const hq2xCase56 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[4]);
  dst[dstIndex+1] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6]);
};

const hq2xCase57 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8]);
};

const hq2xCase58 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6]);
};

const hq2xCase59 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8]);
};

const hq2xCase60 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6]);
};

const hq2xCase61 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8]);
};

const hq2xCase62 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[4]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6]);
};

const hq2xCase63 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8]);
};

const hq2xCase64 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[6]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6]);
};

const hq2xCase65 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[4]);
  dst[dstIndex+1] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6]);
};

const hq2xCase66 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[2]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8]);
};

const hq2xCase67 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp7(w[5], w[4], w[2]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+1] = interp7(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8]);
};

const hq2xCase68 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+1] = interp7(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8]);
  }
};

const hq2xCase69 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8]);
  }
};

const hq2xCase70 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp7(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = interp2(w[5], w[3], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6]);
};

const hq2xCase71 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp7(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = interp1(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6]);
};

const hq2xCase72 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp7(w[5], w[4], w[2]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+1] = interp7(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8]);
};

const hq2xCase73 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[4]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+1] = interp7(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8]);
  }
};

const hq2xCase74 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[2]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8]);
  }
};

const hq2xCase75 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp7(w[5], w[4], w[2]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+1] = interp7(w[5], w[2], w[6]);
  }
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8]);
  }
};

const hq2xCase76 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex] = interp1(w[5], w[4]);
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex] = interp6(w[5], w[2], w[4]);
    dst[dstIndex+1] = interp9(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8]);
};

const hq2xCase77 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[4]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8]);
  } else {
    dst[dstIndex+1] = interp9(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 1)] = interp6(w[5], w[6], w[8]);
  }
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
};

const hq2xCase78 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  if (yuvDiff(n[6], n[8])) {
    dst[dstIndex+1] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[dstIndex+1] = interp6(w[5], w[6], w[2]);
    dst[(dstIndex + dstRowElements + 1)] = interp9(w[5], w[6], w[8]);
  }
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4]);
};

const hq2xCase79 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[2]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp6(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements + 1)] = interp9(w[5], w[6], w[8]);
  }
};

const hq2xCase80 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6]);
  } else {
    dst[(dstIndex + dstRowElements)] = interp9(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements + 1)] = interp6(w[5], w[8], w[6]);
  }
};

const hq2xCase81 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[8], n[4])) {
    dst[dstIndex] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp6(w[5], w[4], w[2]);
    dst[(dstIndex + dstRowElements)] = interp9(w[5], w[8], w[4]);
  }
  dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6]);
};

const hq2xCase82 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8]);
  } else {
    dst[dstIndex] = interp9(w[5], w[4], w[2]);
    dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[8]);
  }
  dst[dstIndex+1] = interp2(w[5], w[3], w[6]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
};

const hq2xCase83 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = interp1(w[5], w[6]);
  } else {
    dst[dstIndex] = interp9(w[5], w[4], w[2]);
    dst[dstIndex+1] = interp6(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
};

const hq2xCase84 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
};

const hq2xCase85 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = interp2(w[5], w[3], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6]);
};

const hq2xCase86 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8]);
};

const hq2xCase87 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
  }
};

const hq2xCase88 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[4]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
};

const hq2xCase89 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
  }
};

const hq2xCase90 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6]);
};

const hq2xCase91 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8]);
};

const hq2xCase92 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8]);
};

const hq2xCase93 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8]);
};

const hq2xCase94 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8]);
};

const hq2xCase95 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8]);
};

const hq2xCase96 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6]);
};

const hq2xCase97 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6]);
};

const hq2xCase98 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[4]);
  dst[dstIndex+1] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6]);
};

const hq2xCase99 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6]);
};

const hq2xCase100 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
  }
};

const hq2xCase101 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp7(w[5], w[4], w[2]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8]);
};

const hq2xCase102 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp7(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = interp2(w[5], w[3], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6]);
};

const hq2xCase103 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[4]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+1] = interp7(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
  }
};

const hq2xCase104 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+1] = interp7(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8]);
};

const hq2xCase105 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[2]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8]);
  }
};

const hq2xCase106 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8]);
  }
};

const hq2xCase107 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = interp1(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6]);
};

const hq2xCase108 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp7(w[5], w[4], w[2]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+1] = interp7(w[5], w[2], w[6]);
  }
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8]);
  }
};

const hq2xCase109 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp7(w[5], w[4], w[2]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  }
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8]);
  }
};

const hq2xCase110 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp7(w[5], w[4], w[2]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+1] = interp7(w[5], w[2], w[6]);
  }
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
  }
};

const hq2xCase111 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+1] = interp7(w[5], w[2], w[6]);
  }
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8]);
  }
};

const hq2xCase112 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6]);
};

const hq2xCase113 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
};

const hq2xCase114 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
};

const hq2xCase115 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8]);
};

const hq2xCase116 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp7(w[5], w[4], w[2]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+1] = interp7(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8]);
};

const hq2xCase117 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+1] = interp7(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8]);
  }
};

const hq2xCase118 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8]);
  }
};

const hq2xCase119 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp7(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = interp1(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6]);
};

const hq2xCase120 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6]);
};

const hq2xCase121 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp7(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
};

const hq2xCase122 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+1] = interp7(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8]);
};

const hq2xCase123 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8]);
  }
};

const hq2xCase124 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8]);
};

const hq2xCase125 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6]);
};

const hq2xCase126 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  }
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
};

const hq2xCase127 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
  }
};

const hq2xCase128 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[8], n[4])) {
    dst[dstIndex] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp6(w[5], w[4], w[2]);
    dst[(dstIndex + dstRowElements)] = interp9(w[5], w[8], w[4]);
  }
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
};

const hq2xCase129 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  if (yuvDiff(n[6], n[8])) {
    dst[dstIndex+1] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[dstIndex+1] = interp6(w[5], w[6], w[2]);
    dst[(dstIndex + dstRowElements + 1)] = interp9(w[5], w[6], w[8]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
};

const hq2xCase130 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = interp1(w[5], w[6]);
  } else {
    dst[dstIndex] = interp9(w[5], w[4], w[2]);
    dst[dstIndex+1] = interp6(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6]);
};

const hq2xCase131 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6]);
  } else {
    dst[(dstIndex + dstRowElements)] = interp9(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements + 1)] = interp6(w[5], w[8], w[6]);
  }
};

const hq2xCase132 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8]);
  } else {
    dst[dstIndex+1] = interp9(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 1)] = interp6(w[5], w[6], w[8]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8]);
};

const hq2xCase133 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8]);
  } else {
    dst[dstIndex] = interp9(w[5], w[4], w[2]);
    dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[8]);
  }
  dst[dstIndex+1] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8]);
};

const hq2xCase134 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = interp1(w[5], w[3]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp6(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements + 1)] = interp9(w[5], w[6], w[8]);
  }
};

const hq2xCase135 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex] = interp1(w[5], w[4]);
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex] = interp6(w[5], w[2], w[4]);
    dst[dstIndex+1] = interp9(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
};

const hq2xCase136 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp10(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6]);
};

const hq2xCase137 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp10(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
};

const hq2xCase138 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex+1] = interp10(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8]);
};

const hq2xCase139 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp10(w[5], w[6], w[8]);
  }
};

const hq2xCase140 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[3]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
  }
};

const hq2xCase141 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = interp1(w[5], w[3]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
};

const hq2xCase142 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
};

const hq2xCase143 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
  }
};

const hq2xCase144 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp10(w[5], w[6], w[8]);
  }
};

const hq2xCase145 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp2(w[5], w[3], w[2]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp10(w[5], w[8], w[4]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
  }
};

const hq2xCase146 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = interp2(w[5], w[3], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp10(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6]);
};

const hq2xCase147 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp10(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = interp1(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6]);
};

const hq2xCase148 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp10(w[5], w[4], w[2]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8]);
};

const hq2xCase149 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex+1] = interp10(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8]);
};

const hq2xCase150 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex+1] = interp10(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
  }
};

const hq2xCase151 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[1], w[4]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp10(w[5], w[6], w[8]);
  }
};

const hq2xCase152 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  }
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp10(w[5], w[6], w[8]);
  }
};

const hq2xCase153 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp10(w[5], w[8], w[4]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp10(w[5], w[6], w[8]);
  }
};

const hq2xCase154 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = interp1(w[5], w[3]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp10(w[5], w[8], w[4]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
  }
};

const hq2xCase155 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp10(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = interp1(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp10(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6]);
};

const hq2xCase156 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp10(w[5], w[4], w[2]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex+1] = interp2(w[5], w[2], w[6]);
  }
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9]);
};

const hq2xCase157 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp10(w[5], w[4], w[2]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex+1] = interp10(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8]);
};

const hq2xCase158 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex+1] = interp10(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8]);
  }
};

const hq2xCase159 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex+1] = interp10(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp10(w[5], w[6], w[8]);
  }
};

const hq2xCase160 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp10(w[5], w[4], w[2]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex+1] = interp10(w[5], w[2], w[6]);
  }
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp10(w[5], w[8], w[4]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 1)] = interp10(w[5], w[6], w[8]);
  }
};

export const hq2xPatterns = new Array(256);
hq2xPatterns[0] = hq2xCase0;
hq2xPatterns[1] = hq2xCase0;
hq2xPatterns[4] = hq2xCase0;
hq2xPatterns[32] = hq2xCase0;
hq2xPatterns[128] = hq2xCase0;
hq2xPatterns[5] = hq2xCase0;
hq2xPatterns[132] = hq2xCase0;
hq2xPatterns[160] = hq2xCase0;
hq2xPatterns[33] = hq2xCase0;
hq2xPatterns[129] = hq2xCase0;
hq2xPatterns[36] = hq2xCase0;
hq2xPatterns[133] = hq2xCase0;
hq2xPatterns[164] = hq2xCase0;
hq2xPatterns[161] = hq2xCase0;
hq2xPatterns[37] = hq2xCase0;
hq2xPatterns[165] = hq2xCase0;
hq2xPatterns[2] = hq2xCase1;
hq2xPatterns[34] = hq2xCase1;
hq2xPatterns[130] = hq2xCase1;
hq2xPatterns[162] = hq2xCase1;
hq2xPatterns[16] = hq2xCase2;
hq2xPatterns[17] = hq2xCase2;
hq2xPatterns[48] = hq2xCase2;
hq2xPatterns[49] = hq2xCase2;
hq2xPatterns[64] = hq2xCase3;
hq2xPatterns[65] = hq2xCase3;
hq2xPatterns[68] = hq2xCase3;
hq2xPatterns[69] = hq2xCase3;
hq2xPatterns[8] = hq2xCase4;
hq2xPatterns[12] = hq2xCase4;
hq2xPatterns[136] = hq2xCase4;
hq2xPatterns[140] = hq2xCase4;
hq2xPatterns[3] = hq2xCase5;
hq2xPatterns[35] = hq2xCase5;
hq2xPatterns[131] = hq2xCase5;
hq2xPatterns[163] = hq2xCase5;
hq2xPatterns[6] = hq2xCase6;
hq2xPatterns[38] = hq2xCase6;
hq2xPatterns[134] = hq2xCase6;
hq2xPatterns[166] = hq2xCase6;
hq2xPatterns[20] = hq2xCase7;
hq2xPatterns[21] = hq2xCase7;
hq2xPatterns[52] = hq2xCase7;
hq2xPatterns[53] = hq2xCase7;
hq2xPatterns[144] = hq2xCase8;
hq2xPatterns[145] = hq2xCase8;
hq2xPatterns[176] = hq2xCase8;
hq2xPatterns[177] = hq2xCase8;
hq2xPatterns[192] = hq2xCase9;
hq2xPatterns[193] = hq2xCase9;
hq2xPatterns[196] = hq2xCase9;
hq2xPatterns[197] = hq2xCase9;
hq2xPatterns[96] = hq2xCase10;
hq2xPatterns[97] = hq2xCase10;
hq2xPatterns[100] = hq2xCase10;
hq2xPatterns[101] = hq2xCase10;
hq2xPatterns[40] = hq2xCase11;
hq2xPatterns[44] = hq2xCase11;
hq2xPatterns[168] = hq2xCase11;
hq2xPatterns[172] = hq2xCase11;
hq2xPatterns[9] = hq2xCase12;
hq2xPatterns[13] = hq2xCase12;
hq2xPatterns[137] = hq2xCase12;
hq2xPatterns[141] = hq2xCase12;
hq2xPatterns[18] = hq2xCase13;
hq2xPatterns[50] = hq2xCase13;
hq2xPatterns[80] = hq2xCase14;
hq2xPatterns[81] = hq2xCase14;
hq2xPatterns[72] = hq2xCase15;
hq2xPatterns[76] = hq2xCase15;
hq2xPatterns[10] = hq2xCase16;
hq2xPatterns[138] = hq2xCase16;
hq2xPatterns[66] = hq2xCase17;
hq2xPatterns[24] = hq2xCase18;
hq2xPatterns[7] = hq2xCase19;
hq2xPatterns[39] = hq2xCase19;
hq2xPatterns[135] = hq2xCase19;
hq2xPatterns[148] = hq2xCase20;
hq2xPatterns[149] = hq2xCase20;
hq2xPatterns[180] = hq2xCase20;
hq2xPatterns[224] = hq2xCase21;
hq2xPatterns[228] = hq2xCase21;
hq2xPatterns[225] = hq2xCase21;
hq2xPatterns[41] = hq2xCase22;
hq2xPatterns[169] = hq2xCase22;
hq2xPatterns[45] = hq2xCase22;
hq2xPatterns[22] = hq2xCase23;
hq2xPatterns[54] = hq2xCase23;
hq2xPatterns[208] = hq2xCase24;
hq2xPatterns[209] = hq2xCase24;
hq2xPatterns[104] = hq2xCase25;
hq2xPatterns[108] = hq2xCase25;
hq2xPatterns[11] = hq2xCase26;
hq2xPatterns[139] = hq2xCase26;
hq2xPatterns[19] = hq2xCase27;
hq2xPatterns[51] = hq2xCase27;
hq2xPatterns[146] = hq2xCase28;
hq2xPatterns[178] = hq2xCase28;
hq2xPatterns[84] = hq2xCase29;
hq2xPatterns[85] = hq2xCase29;
hq2xPatterns[112] = hq2xCase30;
hq2xPatterns[113] = hq2xCase30;
hq2xPatterns[200] = hq2xCase31;
hq2xPatterns[204] = hq2xCase31;
hq2xPatterns[73] = hq2xCase32;
hq2xPatterns[77] = hq2xCase32;
hq2xPatterns[42] = hq2xCase33;
hq2xPatterns[170] = hq2xCase33;
hq2xPatterns[14] = hq2xCase34;
hq2xPatterns[142] = hq2xCase34;
hq2xPatterns[67] = hq2xCase35;
hq2xPatterns[70] = hq2xCase36;
hq2xPatterns[28] = hq2xCase37;
hq2xPatterns[152] = hq2xCase38;
hq2xPatterns[194] = hq2xCase39;
hq2xPatterns[98] = hq2xCase40;
hq2xPatterns[56] = hq2xCase41;
hq2xPatterns[25] = hq2xCase42;
hq2xPatterns[26] = hq2xCase43;
hq2xPatterns[31] = hq2xCase43;
hq2xPatterns[82] = hq2xCase44;
hq2xPatterns[214] = hq2xCase44;
hq2xPatterns[88] = hq2xCase45;
hq2xPatterns[248] = hq2xCase45;
hq2xPatterns[74] = hq2xCase46;
hq2xPatterns[107] = hq2xCase46;
hq2xPatterns[27] = hq2xCase47;
hq2xPatterns[86] = hq2xCase48;
hq2xPatterns[216] = hq2xCase49;
hq2xPatterns[106] = hq2xCase50;
hq2xPatterns[30] = hq2xCase51;
hq2xPatterns[210] = hq2xCase52;
hq2xPatterns[120] = hq2xCase53;
hq2xPatterns[75] = hq2xCase54;
hq2xPatterns[29] = hq2xCase55;
hq2xPatterns[198] = hq2xCase56;
hq2xPatterns[184] = hq2xCase57;
hq2xPatterns[99] = hq2xCase58;
hq2xPatterns[57] = hq2xCase59;
hq2xPatterns[71] = hq2xCase60;
hq2xPatterns[156] = hq2xCase61;
hq2xPatterns[226] = hq2xCase62;
hq2xPatterns[60] = hq2xCase63;
hq2xPatterns[195] = hq2xCase64;
hq2xPatterns[102] = hq2xCase65;
hq2xPatterns[153] = hq2xCase66;
hq2xPatterns[58] = hq2xCase67;
hq2xPatterns[83] = hq2xCase68;
hq2xPatterns[92] = hq2xCase69;
hq2xPatterns[202] = hq2xCase70;
hq2xPatterns[78] = hq2xCase71;
hq2xPatterns[154] = hq2xCase72;
hq2xPatterns[114] = hq2xCase73;
hq2xPatterns[89] = hq2xCase74;
hq2xPatterns[90] = hq2xCase75;
hq2xPatterns[55] = hq2xCase76;
hq2xPatterns[23] = hq2xCase76;
hq2xPatterns[182] = hq2xCase77;
hq2xPatterns[150] = hq2xCase77;
hq2xPatterns[213] = hq2xCase78;
hq2xPatterns[212] = hq2xCase78;
hq2xPatterns[241] = hq2xCase79;
hq2xPatterns[240] = hq2xCase79;
hq2xPatterns[236] = hq2xCase80;
hq2xPatterns[232] = hq2xCase80;
hq2xPatterns[109] = hq2xCase81;
hq2xPatterns[105] = hq2xCase81;
hq2xPatterns[171] = hq2xCase82;
hq2xPatterns[43] = hq2xCase82;
hq2xPatterns[143] = hq2xCase83;
hq2xPatterns[15] = hq2xCase83;
hq2xPatterns[124] = hq2xCase84;
hq2xPatterns[203] = hq2xCase85;
hq2xPatterns[62] = hq2xCase86;
hq2xPatterns[211] = hq2xCase87;
hq2xPatterns[118] = hq2xCase88;
hq2xPatterns[217] = hq2xCase89;
hq2xPatterns[110] = hq2xCase90;
hq2xPatterns[155] = hq2xCase91;
hq2xPatterns[188] = hq2xCase92;
hq2xPatterns[185] = hq2xCase93;
hq2xPatterns[61] = hq2xCase94;
hq2xPatterns[157] = hq2xCase95;
hq2xPatterns[103] = hq2xCase96;
hq2xPatterns[227] = hq2xCase97;
hq2xPatterns[230] = hq2xCase98;
hq2xPatterns[199] = hq2xCase99;
hq2xPatterns[220] = hq2xCase100;
hq2xPatterns[158] = hq2xCase101;
hq2xPatterns[234] = hq2xCase102;
hq2xPatterns[242] = hq2xCase103;
hq2xPatterns[59] = hq2xCase104;
hq2xPatterns[121] = hq2xCase105;
hq2xPatterns[87] = hq2xCase106;
hq2xPatterns[79] = hq2xCase107;
hq2xPatterns[122] = hq2xCase108;
hq2xPatterns[94] = hq2xCase109;
hq2xPatterns[218] = hq2xCase110;
hq2xPatterns[91] = hq2xCase111;
hq2xPatterns[229] = hq2xCase112;
hq2xPatterns[167] = hq2xCase113;
hq2xPatterns[173] = hq2xCase114;
hq2xPatterns[181] = hq2xCase115;
hq2xPatterns[186] = hq2xCase116;
hq2xPatterns[115] = hq2xCase117;
hq2xPatterns[93] = hq2xCase118;
hq2xPatterns[206] = hq2xCase119;
hq2xPatterns[205] = hq2xCase120;
hq2xPatterns[201] = hq2xCase120;
hq2xPatterns[174] = hq2xCase121;
hq2xPatterns[46] = hq2xCase121;
hq2xPatterns[179] = hq2xCase122;
hq2xPatterns[147] = hq2xCase122;
hq2xPatterns[117] = hq2xCase123;
hq2xPatterns[116] = hq2xCase123;
hq2xPatterns[189] = hq2xCase124;
hq2xPatterns[231] = hq2xCase125;
hq2xPatterns[126] = hq2xCase126;
hq2xPatterns[219] = hq2xCase127;
hq2xPatterns[125] = hq2xCase128;
hq2xPatterns[221] = hq2xCase129;
hq2xPatterns[207] = hq2xCase130;
hq2xPatterns[238] = hq2xCase131;
hq2xPatterns[190] = hq2xCase132;
hq2xPatterns[187] = hq2xCase133;
hq2xPatterns[243] = hq2xCase134;
hq2xPatterns[119] = hq2xCase135;
hq2xPatterns[237] = hq2xCase136;
hq2xPatterns[233] = hq2xCase136;
hq2xPatterns[175] = hq2xCase137;
hq2xPatterns[47] = hq2xCase137;
hq2xPatterns[183] = hq2xCase138;
hq2xPatterns[151] = hq2xCase138;
hq2xPatterns[245] = hq2xCase139;
hq2xPatterns[244] = hq2xCase139;
hq2xPatterns[250] = hq2xCase140;
hq2xPatterns[123] = hq2xCase141;
hq2xPatterns[95] = hq2xCase142;
hq2xPatterns[222] = hq2xCase143;
hq2xPatterns[252] = hq2xCase144;
hq2xPatterns[249] = hq2xCase145;
hq2xPatterns[235] = hq2xCase146;
hq2xPatterns[111] = hq2xCase147;
hq2xPatterns[63] = hq2xCase148;
hq2xPatterns[159] = hq2xCase149;
hq2xPatterns[215] = hq2xCase150;
hq2xPatterns[246] = hq2xCase151;
hq2xPatterns[254] = hq2xCase152;
hq2xPatterns[253] = hq2xCase153;
hq2xPatterns[251] = hq2xCase154;
hq2xPatterns[239] = hq2xCase155;
hq2xPatterns[127] = hq2xCase156;
hq2xPatterns[191] = hq2xCase157;
hq2xPatterns[223] = hq2xCase158;
hq2xPatterns[247] = hq2xCase159;
hq2xPatterns[255] = hq2xCase160;

const hq3xCase0 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
};

const hq3xCase1 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
};

const hq3xCase2 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase3 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase4 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
};

const hq3xCase5 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
};

const hq3xCase6 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
};

const hq3xCase7 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase8 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[8]);
};

const hq3xCase9 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[6]);
};

const hq3xCase10 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase11 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
};

const hq3xCase12 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
};

const hq3xCase13 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = interp1(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 2)] = w[5];
  } else {
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[dstIndex+2] = interp4(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase14 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp4(w[5], w[6], w[8]);
  }
};

const hq3xCase15 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2)] = interp4(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  }
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase16 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp4(w[5], w[4], w[2]);
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
};

const hq3xCase17 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase18 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase19 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
};

const hq3xCase20 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[8]);
};

const hq3xCase21 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[6]);
};

const hq3xCase22 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
};

const hq3xCase23 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = w[5];
    dst[(dstIndex + dstRowElements + 2)] = w[5];
  } else {
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[dstIndex+2] = interp4(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase24 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp4(w[5], w[6], w[8]);
  }
};

const hq3xCase25 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2)] = interp4(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  }
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase26 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp4(w[5], w[4], w[2]);
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
};

const hq3xCase27 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex] = interp1(w[5], w[4]);
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = interp1(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 2)] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
    dst[dstIndex+1] = interp1(w[2], w[5]);
    dst[dstIndex+2] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase28 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = interp1(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[8]);
  } else {
    dst[dstIndex+1] = interp1(w[5], w[2]);
    dst[dstIndex+2] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp1(w[6], w[5]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
};

const hq3xCase29 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[6], n[8])) {
    dst[dstIndex+2] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
  } else {
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp1(w[6], w[5]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp5(w[6], w[8]);
  }
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
};

const hq3xCase30 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[8], w[5]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp5(w[6], w[8]);
  }
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
};

const hq3xCase31 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[6]);
  } else {
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[8], w[5]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
};

const hq3xCase32 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[8], n[4])) {
    dst[dstIndex] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
    dst[(dstIndex + dstRowElements)] = interp1(w[4], w[5]);
    dst[(dstIndex + dstRowElements*2)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  }
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase33 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[8]);
  } else {
    dst[dstIndex] = interp5(w[4], w[2]);
    dst[dstIndex+1] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp1(w[4], w[5]);
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
};

const hq3xCase34 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = interp1(w[5], w[6]);
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp5(w[4], w[2]);
    dst[dstIndex+1] = interp1(w[2], w[5]);
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  }
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
};

const hq3xCase35 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase36 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase37 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase38 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[8]);
};

const hq3xCase39 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[6]);
};

const hq3xCase40 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase41 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase42 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase43 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp4(w[5], w[4], w[2]);
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
  }
  dst[dstIndex+1] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
    dst[(dstIndex + dstRowElements + 2)] = w[5];
  } else {
    dst[dstIndex+2] = interp4(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase44 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = w[5];
  } else {
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[dstIndex+2] = interp4(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp4(w[5], w[6], w[8]);
  }
};

const hq3xCase45 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2)] = interp4(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp4(w[5], w[6], w[8]);
  }
};

const hq3xCase46 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex] = interp4(w[5], w[4], w[2]);
    dst[dstIndex+1] = interp3(w[5], w[2]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp4(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  }
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase47 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp4(w[5], w[4], w[2]);
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase48 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = w[5];
    dst[(dstIndex + dstRowElements + 2)] = w[5];
  } else {
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[dstIndex+2] = interp4(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase49 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp4(w[5], w[6], w[8]);
  }
};

const hq3xCase50 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2)] = interp4(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  }
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase51 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = w[5];
    dst[(dstIndex + dstRowElements + 2)] = w[5];
  } else {
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[dstIndex+2] = interp4(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase52 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp4(w[5], w[6], w[8]);
  }
};

const hq3xCase53 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2)] = interp4(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  }
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase54 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp4(w[5], w[4], w[2]);
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase55 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase56 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[6]);
};

const hq3xCase57 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[8]);
};

const hq3xCase58 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase59 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase60 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase61 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[8]);
};

const hq3xCase62 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[6]);
};

const hq3xCase63 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase64 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[6]);
};

const hq3xCase65 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase66 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[8]);
};

const hq3xCase67 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase68 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
};

const hq3xCase69 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
};

const hq3xCase70 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[6]);
};

const hq3xCase71 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase72 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[8]);
};

const hq3xCase73 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
};

const hq3xCase74 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
};

const hq3xCase75 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
};

const hq3xCase76 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex] = interp1(w[5], w[4]);
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = w[5];
    dst[(dstIndex + dstRowElements + 2)] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
    dst[dstIndex+1] = interp1(w[2], w[5]);
    dst[dstIndex+2] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase77 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = w[5];
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[8]);
  } else {
    dst[dstIndex+1] = interp1(w[5], w[2]);
    dst[dstIndex+2] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp1(w[6], w[5]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
};

const hq3xCase78 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[6], n[8])) {
    dst[dstIndex+2] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp1(w[6], w[5]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp5(w[6], w[8]);
  }
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
};

const hq3xCase79 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[8], w[5]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp5(w[6], w[8]);
  }
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
};

const hq3xCase80 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[6]);
  } else {
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[8], w[5]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
};

const hq3xCase81 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[8], n[4])) {
    dst[dstIndex] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
    dst[(dstIndex + dstRowElements)] = interp1(w[4], w[5]);
    dst[(dstIndex + dstRowElements*2)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  }
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase82 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[8]);
  } else {
    dst[dstIndex] = interp5(w[4], w[2]);
    dst[dstIndex+1] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp1(w[4], w[5]);
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
};

const hq3xCase83 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = interp1(w[5], w[6]);
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp5(w[4], w[2]);
    dst[dstIndex+1] = interp1(w[2], w[5]);
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  }
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
};

const hq3xCase84 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2)] = interp4(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  }
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase85 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp4(w[5], w[4], w[2]);
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[6]);
};

const hq3xCase86 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = w[5];
    dst[(dstIndex + dstRowElements + 2)] = w[5];
  } else {
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[dstIndex+2] = interp4(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase87 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp4(w[5], w[6], w[8]);
  }
};

const hq3xCase88 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = w[5];
    dst[(dstIndex + dstRowElements + 2)] = w[5];
  } else {
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[dstIndex+2] = interp4(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase89 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp4(w[5], w[6], w[8]);
  }
};

const hq3xCase90 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2)] = interp4(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  }
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase91 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp4(w[5], w[4], w[2]);
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[8]);
};

const hq3xCase92 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[8]);
};

const hq3xCase93 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[8]);
};

const hq3xCase94 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase95 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[8]);
};

const hq3xCase96 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase97 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[6]);
};

const hq3xCase98 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[6]);
};

const hq3xCase99 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[6]);
};

const hq3xCase100 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp4(w[5], w[6], w[8]);
  }
};

const hq3xCase101 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = w[5];
    dst[(dstIndex + dstRowElements + 2)] = w[5];
  } else {
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[dstIndex+2] = interp4(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[8]);
};

const hq3xCase102 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2)] = interp4(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  }
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[6]);
};

const hq3xCase103 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp4(w[5], w[6], w[8]);
  }
};

const hq3xCase104 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp4(w[5], w[4], w[2]);
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase105 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2)] = interp4(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
};

const hq3xCase106 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = w[5];
    dst[(dstIndex + dstRowElements + 2)] = w[5];
  } else {
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[dstIndex+2] = interp4(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
};

const hq3xCase107 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp4(w[5], w[4], w[2]);
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
  }
  dst[dstIndex+2] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase108 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2)] = interp4(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
};

const hq3xCase109 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = w[5];
    dst[(dstIndex + dstRowElements + 2)] = w[5];
  } else {
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[dstIndex+2] = interp4(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
};

const hq3xCase110 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp4(w[5], w[6], w[8]);
  }
};

const hq3xCase111 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp4(w[5], w[4], w[2]);
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
};

const hq3xCase112 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[6]);
};

const hq3xCase113 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
};

const hq3xCase114 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
};

const hq3xCase115 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[8]);
};

const hq3xCase116 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[8]);
};

const hq3xCase117 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
};

const hq3xCase118 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
};

const hq3xCase119 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[6]);
};

const hq3xCase120 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[6]);
};

const hq3xCase121 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp1(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
};

const hq3xCase122 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[8]);
};

const hq3xCase123 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
};

const hq3xCase124 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[8]);
};

const hq3xCase125 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[6]);
};

const hq3xCase126 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = w[5];
    dst[(dstIndex + dstRowElements + 2)] = w[5];
  } else {
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[dstIndex+2] = interp4(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2)] = interp4(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  }
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase127 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp4(w[5], w[4], w[2]);
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp4(w[5], w[6], w[8]);
  }
};

const hq3xCase128 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[8], n[4])) {
    dst[dstIndex] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
    dst[(dstIndex + dstRowElements)] = interp1(w[4], w[5]);
    dst[(dstIndex + dstRowElements*2)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  }
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase129 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[6], n[8])) {
    dst[dstIndex+2] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp1(w[6], w[5]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp5(w[6], w[8]);
  }
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
};

const hq3xCase130 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = interp1(w[5], w[6]);
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp5(w[4], w[2]);
    dst[dstIndex+1] = interp1(w[2], w[5]);
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  }
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[6]);
};

const hq3xCase131 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[6]);
  } else {
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[8], w[5]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
};

const hq3xCase132 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = w[5];
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[8]);
  } else {
    dst[dstIndex+1] = interp1(w[5], w[2]);
    dst[dstIndex+2] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp1(w[6], w[5]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
};

const hq3xCase133 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[8]);
  } else {
    dst[dstIndex] = interp5(w[4], w[2]);
    dst[dstIndex+1] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp1(w[4], w[5]);
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[8]);
};

const hq3xCase134 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[8], w[5]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp5(w[6], w[8]);
  }
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
};

const hq3xCase135 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex] = interp1(w[5], w[4]);
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = w[5];
    dst[(dstIndex + dstRowElements + 2)] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
    dst[dstIndex+1] = interp1(w[2], w[5]);
    dst[dstIndex+2] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase136 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[6]);
};

const hq3xCase137 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
};

const hq3xCase138 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
  } else {
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[8]);
};

const hq3xCase139 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[4], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
};

const hq3xCase140 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2)] = interp4(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp4(w[5], w[6], w[8]);
  }
};

const hq3xCase141 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex] = interp4(w[5], w[4], w[2]);
    dst[dstIndex+1] = interp3(w[5], w[2]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp4(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  }
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase142 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp4(w[5], w[4], w[2]);
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
  }
  dst[dstIndex+1] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
    dst[(dstIndex + dstRowElements + 2)] = w[5];
  } else {
    dst[dstIndex+2] = interp4(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase143 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = w[5];
  } else {
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[dstIndex+2] = interp4(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp4(w[5], w[6], w[8]);
  }
};

const hq3xCase144 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2)] = interp4(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
};

const hq3xCase145 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp4(w[5], w[6], w[8]);
  }
};

const hq3xCase146 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex] = interp4(w[5], w[4], w[2]);
    dst[dstIndex+1] = interp3(w[5], w[2]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[6]);
};

const hq3xCase147 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp4(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  }
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase148 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
    dst[(dstIndex + dstRowElements + 2)] = w[5];
  } else {
    dst[dstIndex+2] = interp4(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase149 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp4(w[5], w[4], w[2]);
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
  }
  dst[dstIndex+1] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
  } else {
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[8]);
};

const hq3xCase150 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
  } else {
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp4(w[5], w[6], w[8]);
  }
};

const hq3xCase151 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = w[5];
  } else {
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[dstIndex+2] = interp4(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
};

const hq3xCase152 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = w[5];
  } else {
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[dstIndex+2] = interp4(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2)] = interp4(w[5], w[8], w[4]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
};

const hq3xCase153 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[2]);
  dst[dstIndex+1] = interp1(w[5], w[2]);
  dst[dstIndex+2] = interp1(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
};

const hq3xCase154 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
  } else {
    dst[dstIndex] = interp4(w[5], w[4], w[2]);
    dst[dstIndex+1] = interp3(w[5], w[2]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp4(w[5], w[6], w[8]);
  }
};

const hq3xCase155 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp1(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp1(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[6]);
};

const hq3xCase156 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
    dst[(dstIndex + dstRowElements + 2)] = w[5];
  } else {
    dst[dstIndex+2] = interp4(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp4(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  }
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[9]);
};

const hq3xCase157 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
  } else {
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp1(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp1(w[5], w[8]);
};

const hq3xCase158 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp4(w[5], w[4], w[2]);
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[4]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = w[5];
    dst[(dstIndex + dstRowElements + 2)] = w[5];
  } else {
    dst[dstIndex+1] = interp3(w[5], w[2]);
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp4(w[5], w[6], w[8]);
  }
};

const hq3xCase159 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp1(w[5], w[4]);
  dst[dstIndex+1] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
  } else {
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
};

const hq3xCase160 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[4], w[2]);
  }
  dst[dstIndex+1] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
  } else {
    dst[dstIndex+2] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp2(w[5], w[6], w[8]);
  }
};

export const hq3xPatterns = new Array(256);
hq3xPatterns[0] = hq3xCase0;
hq3xPatterns[1] = hq3xCase0;
hq3xPatterns[4] = hq3xCase0;
hq3xPatterns[32] = hq3xCase0;
hq3xPatterns[128] = hq3xCase0;
hq3xPatterns[5] = hq3xCase0;
hq3xPatterns[132] = hq3xCase0;
hq3xPatterns[160] = hq3xCase0;
hq3xPatterns[33] = hq3xCase0;
hq3xPatterns[129] = hq3xCase0;
hq3xPatterns[36] = hq3xCase0;
hq3xPatterns[133] = hq3xCase0;
hq3xPatterns[164] = hq3xCase0;
hq3xPatterns[161] = hq3xCase0;
hq3xPatterns[37] = hq3xCase0;
hq3xPatterns[165] = hq3xCase0;
hq3xPatterns[2] = hq3xCase1;
hq3xPatterns[34] = hq3xCase1;
hq3xPatterns[130] = hq3xCase1;
hq3xPatterns[162] = hq3xCase1;
hq3xPatterns[16] = hq3xCase2;
hq3xPatterns[17] = hq3xCase2;
hq3xPatterns[48] = hq3xCase2;
hq3xPatterns[49] = hq3xCase2;
hq3xPatterns[64] = hq3xCase3;
hq3xPatterns[65] = hq3xCase3;
hq3xPatterns[68] = hq3xCase3;
hq3xPatterns[69] = hq3xCase3;
hq3xPatterns[8] = hq3xCase4;
hq3xPatterns[12] = hq3xCase4;
hq3xPatterns[136] = hq3xCase4;
hq3xPatterns[140] = hq3xCase4;
hq3xPatterns[3] = hq3xCase5;
hq3xPatterns[35] = hq3xCase5;
hq3xPatterns[131] = hq3xCase5;
hq3xPatterns[163] = hq3xCase5;
hq3xPatterns[6] = hq3xCase6;
hq3xPatterns[38] = hq3xCase6;
hq3xPatterns[134] = hq3xCase6;
hq3xPatterns[166] = hq3xCase6;
hq3xPatterns[20] = hq3xCase7;
hq3xPatterns[21] = hq3xCase7;
hq3xPatterns[52] = hq3xCase7;
hq3xPatterns[53] = hq3xCase7;
hq3xPatterns[144] = hq3xCase8;
hq3xPatterns[145] = hq3xCase8;
hq3xPatterns[176] = hq3xCase8;
hq3xPatterns[177] = hq3xCase8;
hq3xPatterns[192] = hq3xCase9;
hq3xPatterns[193] = hq3xCase9;
hq3xPatterns[196] = hq3xCase9;
hq3xPatterns[197] = hq3xCase9;
hq3xPatterns[96] = hq3xCase10;
hq3xPatterns[97] = hq3xCase10;
hq3xPatterns[100] = hq3xCase10;
hq3xPatterns[101] = hq3xCase10;
hq3xPatterns[40] = hq3xCase11;
hq3xPatterns[44] = hq3xCase11;
hq3xPatterns[168] = hq3xCase11;
hq3xPatterns[172] = hq3xCase11;
hq3xPatterns[9] = hq3xCase12;
hq3xPatterns[13] = hq3xCase12;
hq3xPatterns[137] = hq3xCase12;
hq3xPatterns[141] = hq3xCase12;
hq3xPatterns[18] = hq3xCase13;
hq3xPatterns[50] = hq3xCase13;
hq3xPatterns[80] = hq3xCase14;
hq3xPatterns[81] = hq3xCase14;
hq3xPatterns[72] = hq3xCase15;
hq3xPatterns[76] = hq3xCase15;
hq3xPatterns[10] = hq3xCase16;
hq3xPatterns[138] = hq3xCase16;
hq3xPatterns[66] = hq3xCase17;
hq3xPatterns[24] = hq3xCase18;
hq3xPatterns[7] = hq3xCase19;
hq3xPatterns[39] = hq3xCase19;
hq3xPatterns[135] = hq3xCase19;
hq3xPatterns[148] = hq3xCase20;
hq3xPatterns[149] = hq3xCase20;
hq3xPatterns[180] = hq3xCase20;
hq3xPatterns[224] = hq3xCase21;
hq3xPatterns[228] = hq3xCase21;
hq3xPatterns[225] = hq3xCase21;
hq3xPatterns[41] = hq3xCase22;
hq3xPatterns[169] = hq3xCase22;
hq3xPatterns[45] = hq3xCase22;
hq3xPatterns[22] = hq3xCase23;
hq3xPatterns[54] = hq3xCase23;
hq3xPatterns[208] = hq3xCase24;
hq3xPatterns[209] = hq3xCase24;
hq3xPatterns[104] = hq3xCase25;
hq3xPatterns[108] = hq3xCase25;
hq3xPatterns[11] = hq3xCase26;
hq3xPatterns[139] = hq3xCase26;
hq3xPatterns[19] = hq3xCase27;
hq3xPatterns[51] = hq3xCase27;
hq3xPatterns[146] = hq3xCase28;
hq3xPatterns[178] = hq3xCase28;
hq3xPatterns[84] = hq3xCase29;
hq3xPatterns[85] = hq3xCase29;
hq3xPatterns[112] = hq3xCase30;
hq3xPatterns[113] = hq3xCase30;
hq3xPatterns[200] = hq3xCase31;
hq3xPatterns[204] = hq3xCase31;
hq3xPatterns[73] = hq3xCase32;
hq3xPatterns[77] = hq3xCase32;
hq3xPatterns[42] = hq3xCase33;
hq3xPatterns[170] = hq3xCase33;
hq3xPatterns[14] = hq3xCase34;
hq3xPatterns[142] = hq3xCase34;
hq3xPatterns[67] = hq3xCase35;
hq3xPatterns[70] = hq3xCase36;
hq3xPatterns[28] = hq3xCase37;
hq3xPatterns[152] = hq3xCase38;
hq3xPatterns[194] = hq3xCase39;
hq3xPatterns[98] = hq3xCase40;
hq3xPatterns[56] = hq3xCase41;
hq3xPatterns[25] = hq3xCase42;
hq3xPatterns[26] = hq3xCase43;
hq3xPatterns[31] = hq3xCase43;
hq3xPatterns[82] = hq3xCase44;
hq3xPatterns[214] = hq3xCase44;
hq3xPatterns[88] = hq3xCase45;
hq3xPatterns[248] = hq3xCase45;
hq3xPatterns[74] = hq3xCase46;
hq3xPatterns[107] = hq3xCase46;
hq3xPatterns[27] = hq3xCase47;
hq3xPatterns[86] = hq3xCase48;
hq3xPatterns[216] = hq3xCase49;
hq3xPatterns[106] = hq3xCase50;
hq3xPatterns[30] = hq3xCase51;
hq3xPatterns[210] = hq3xCase52;
hq3xPatterns[120] = hq3xCase53;
hq3xPatterns[75] = hq3xCase54;
hq3xPatterns[29] = hq3xCase55;
hq3xPatterns[198] = hq3xCase56;
hq3xPatterns[184] = hq3xCase57;
hq3xPatterns[99] = hq3xCase58;
hq3xPatterns[57] = hq3xCase59;
hq3xPatterns[71] = hq3xCase60;
hq3xPatterns[156] = hq3xCase61;
hq3xPatterns[226] = hq3xCase62;
hq3xPatterns[60] = hq3xCase63;
hq3xPatterns[195] = hq3xCase64;
hq3xPatterns[102] = hq3xCase65;
hq3xPatterns[153] = hq3xCase66;
hq3xPatterns[58] = hq3xCase67;
hq3xPatterns[83] = hq3xCase68;
hq3xPatterns[92] = hq3xCase69;
hq3xPatterns[202] = hq3xCase70;
hq3xPatterns[78] = hq3xCase71;
hq3xPatterns[154] = hq3xCase72;
hq3xPatterns[114] = hq3xCase73;
hq3xPatterns[89] = hq3xCase74;
hq3xPatterns[90] = hq3xCase75;
hq3xPatterns[55] = hq3xCase76;
hq3xPatterns[23] = hq3xCase76;
hq3xPatterns[182] = hq3xCase77;
hq3xPatterns[150] = hq3xCase77;
hq3xPatterns[213] = hq3xCase78;
hq3xPatterns[212] = hq3xCase78;
hq3xPatterns[241] = hq3xCase79;
hq3xPatterns[240] = hq3xCase79;
hq3xPatterns[236] = hq3xCase80;
hq3xPatterns[232] = hq3xCase80;
hq3xPatterns[109] = hq3xCase81;
hq3xPatterns[105] = hq3xCase81;
hq3xPatterns[171] = hq3xCase82;
hq3xPatterns[43] = hq3xCase82;
hq3xPatterns[143] = hq3xCase83;
hq3xPatterns[15] = hq3xCase83;
hq3xPatterns[124] = hq3xCase84;
hq3xPatterns[203] = hq3xCase85;
hq3xPatterns[62] = hq3xCase86;
hq3xPatterns[211] = hq3xCase87;
hq3xPatterns[118] = hq3xCase88;
hq3xPatterns[217] = hq3xCase89;
hq3xPatterns[110] = hq3xCase90;
hq3xPatterns[155] = hq3xCase91;
hq3xPatterns[188] = hq3xCase92;
hq3xPatterns[185] = hq3xCase93;
hq3xPatterns[61] = hq3xCase94;
hq3xPatterns[157] = hq3xCase95;
hq3xPatterns[103] = hq3xCase96;
hq3xPatterns[227] = hq3xCase97;
hq3xPatterns[230] = hq3xCase98;
hq3xPatterns[199] = hq3xCase99;
hq3xPatterns[220] = hq3xCase100;
hq3xPatterns[158] = hq3xCase101;
hq3xPatterns[234] = hq3xCase102;
hq3xPatterns[242] = hq3xCase103;
hq3xPatterns[59] = hq3xCase104;
hq3xPatterns[121] = hq3xCase105;
hq3xPatterns[87] = hq3xCase106;
hq3xPatterns[79] = hq3xCase107;
hq3xPatterns[122] = hq3xCase108;
hq3xPatterns[94] = hq3xCase109;
hq3xPatterns[218] = hq3xCase110;
hq3xPatterns[91] = hq3xCase111;
hq3xPatterns[229] = hq3xCase112;
hq3xPatterns[167] = hq3xCase113;
hq3xPatterns[173] = hq3xCase114;
hq3xPatterns[181] = hq3xCase115;
hq3xPatterns[186] = hq3xCase116;
hq3xPatterns[115] = hq3xCase117;
hq3xPatterns[93] = hq3xCase118;
hq3xPatterns[206] = hq3xCase119;
hq3xPatterns[205] = hq3xCase120;
hq3xPatterns[201] = hq3xCase120;
hq3xPatterns[174] = hq3xCase121;
hq3xPatterns[46] = hq3xCase121;
hq3xPatterns[179] = hq3xCase122;
hq3xPatterns[147] = hq3xCase122;
hq3xPatterns[117] = hq3xCase123;
hq3xPatterns[116] = hq3xCase123;
hq3xPatterns[189] = hq3xCase124;
hq3xPatterns[231] = hq3xCase125;
hq3xPatterns[126] = hq3xCase126;
hq3xPatterns[219] = hq3xCase127;
hq3xPatterns[125] = hq3xCase128;
hq3xPatterns[221] = hq3xCase129;
hq3xPatterns[207] = hq3xCase130;
hq3xPatterns[238] = hq3xCase131;
hq3xPatterns[190] = hq3xCase132;
hq3xPatterns[187] = hq3xCase133;
hq3xPatterns[243] = hq3xCase134;
hq3xPatterns[119] = hq3xCase135;
hq3xPatterns[237] = hq3xCase136;
hq3xPatterns[233] = hq3xCase136;
hq3xPatterns[175] = hq3xCase137;
hq3xPatterns[47] = hq3xCase137;
hq3xPatterns[183] = hq3xCase138;
hq3xPatterns[151] = hq3xCase138;
hq3xPatterns[245] = hq3xCase139;
hq3xPatterns[244] = hq3xCase139;
hq3xPatterns[250] = hq3xCase140;
hq3xPatterns[123] = hq3xCase141;
hq3xPatterns[95] = hq3xCase142;
hq3xPatterns[222] = hq3xCase143;
hq3xPatterns[252] = hq3xCase144;
hq3xPatterns[249] = hq3xCase145;
hq3xPatterns[235] = hq3xCase146;
hq3xPatterns[111] = hq3xCase147;
hq3xPatterns[63] = hq3xCase148;
hq3xPatterns[159] = hq3xCase149;
hq3xPatterns[215] = hq3xCase150;
hq3xPatterns[246] = hq3xCase151;
hq3xPatterns[254] = hq3xCase152;
hq3xPatterns[253] = hq3xCase153;
hq3xPatterns[251] = hq3xCase154;
hq3xPatterns[239] = hq3xCase155;
hq3xPatterns[127] = hq3xCase156;
hq3xPatterns[191] = hq3xCase157;
hq3xPatterns[223] = hq3xCase158;
hq3xPatterns[247] = hq3xCase159;
hq3xPatterns[255] = hq3xCase160;

const hq4xCase0 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[2], w[4]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[4]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[6]);
  dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp7(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp7(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp7(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
};

const hq4xCase1 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp7(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp7(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
};

const hq4xCase2 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[2], w[4]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[4]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp7(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase3 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[2], w[4]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[4]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[6]);
  dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp7(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase4 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[1]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[6]);
  dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp7(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp7(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
};

const hq4xCase5 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[4]);
  dst[dstIndex+1] = interp3(w[5], w[4]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp7(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp7(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
};

const hq4xCase6 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  dst[dstIndex+2] = interp3(w[5], w[6]);
  dst[dstIndex+3] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp7(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp7(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
};

const hq4xCase7 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[2], w[4]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[4]);
  dst[dstIndex+2] = interp8(w[5], w[2]);
  dst[dstIndex+3] = interp8(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp7(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase8 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[2], w[4]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[4]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp7(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[8]);
};

const hq4xCase9 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[2], w[4]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[4]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[6]);
  dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp7(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[6]);
};

const hq4xCase10 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[2], w[4]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[4]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[6]);
  dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp7(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase11 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[1]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[6]);
  dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp7(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp7(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
};

const hq4xCase12 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[2]);
  dst[dstIndex+1] = interp8(w[5], w[2]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[6]);
  dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp7(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp7(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
};

const hq4xCase13 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
    dst[dstIndex+3] = interp8(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp5(w[2], w[5]);
    dst[dstIndex+3] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = interp5(w[6], w[5]);
  }
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp7(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase14 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[2], w[4]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[4]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 3)] = interp5(w[6], w[5]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp5(w[8], w[5]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp5(w[8], w[6]);
  }
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
};

const hq4xCase15 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[1]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[6]);
  dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp7(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[2]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp5(w[4], w[5]);
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp5(w[8], w[5]);
  }
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[9]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase16 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp8(w[5], w[1]);
    dst[dstIndex+1] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  } else {
    dst[dstIndex] = interp5(w[2], w[4]);
    dst[dstIndex+1] = interp5(w[2], w[5]);
    dst[(dstIndex + dstRowElements)] = interp5(w[4], w[5]);
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp7(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
};

const hq4xCase17 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase18 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[1]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase19 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[4]);
  dst[dstIndex+1] = interp3(w[5], w[4]);
  dst[dstIndex+2] = interp3(w[5], w[6]);
  dst[dstIndex+3] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp7(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp7(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
};

const hq4xCase20 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[2], w[4]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[4]);
  dst[dstIndex+2] = interp8(w[5], w[2]);
  dst[dstIndex+3] = interp8(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp7(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[8]);
};

const hq4xCase21 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[2], w[4]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[4]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[6]);
  dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp7(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[6]);
};

const hq4xCase22 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[2]);
  dst[dstIndex+1] = interp8(w[5], w[2]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[6]);
  dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp7(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp7(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
};

const hq4xCase23 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
    dst[dstIndex+3] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = w[5];
  } else {
    dst[dstIndex+2] = interp5(w[2], w[5]);
    dst[dstIndex+3] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 3)] = interp5(w[6], w[5]);
  }
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp7(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase24 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[2], w[4]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[4]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 3)] = interp5(w[6], w[5]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp5(w[8], w[5]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp5(w[8], w[6]);
  }
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
};

const hq4xCase25 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[1]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[6]);
  dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp7(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[2]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp5(w[4], w[5]);
    dst[(dstIndex + dstRowElements*3)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp5(w[8], w[5]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[9]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase26 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp5(w[2], w[4]);
    dst[dstIndex+1] = interp5(w[2], w[5]);
    dst[(dstIndex + dstRowElements)] = interp5(w[4], w[5]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp7(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
};

const hq4xCase27 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex] = interp8(w[5], w[4]);
    dst[dstIndex+1] = interp3(w[5], w[4]);
    dst[dstIndex+2] = interp1(w[5], w[3]);
    dst[dstIndex+3] = interp8(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex] = interp1(w[5], w[2]);
    dst[dstIndex+1] = interp1(w[2], w[5]);
    dst[dstIndex+2] = interp8(w[2], w[6]);
    dst[dstIndex+3] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp7(w[5], w[6], w[2]);
    dst[(dstIndex + dstRowElements + 3)] = interp2(w[6], w[5], w[2]);
  }
  dst[(dstIndex + dstRowElements)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp7(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase28 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
    dst[dstIndex+3] = interp8(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp3(w[5], w[8]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[8]);
  } else {
    dst[dstIndex+2] = interp2(w[2], w[5], w[6]);
    dst[dstIndex+3] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp7(w[5], w[6], w[2]);
    dst[(dstIndex + dstRowElements + 3)] = interp8(w[6], w[2]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[6], w[5]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp1(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp7(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[5], w[8]);
};

const hq4xCase29 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[2], w[4]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[4]);
  dst[dstIndex+2] = interp8(w[5], w[2]);
  if (yuvDiff(n[6], n[8])) {
    dst[dstIndex+3] = interp8(w[5], w[2]);
    dst[(dstIndex + dstRowElements + 3)] = interp3(w[5], w[2]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
  } else {
    dst[dstIndex+3] = interp1(w[5], w[6]);
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[6], w[5]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp7(w[5], w[6], w[8]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[6], w[8]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp2(w[8], w[5], w[6]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp5(w[8], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
};

const hq4xCase30 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[2], w[4]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[4]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[4]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp3(w[5], w[4]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp7(w[5], w[6], w[8]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp2(w[6], w[5], w[8]);
    dst[(dstIndex + dstRowElements*3)] = interp1(w[5], w[8]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[8], w[5]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[8], w[6]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp5(w[8], w[6]);
  }
};

const hq4xCase31 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[1]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[6]);
  dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp7(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[2]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp3(w[5], w[6]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[6]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp2(w[4], w[5], w[8]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp7(w[5], w[4], w[8]);
    dst[(dstIndex + dstRowElements*3)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[8], w[5]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp1(w[5], w[8]);
  }
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[5], w[6]);
};

const hq4xCase32 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[8], n[4])) {
    dst[dstIndex] = interp8(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[2]);
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  } else {
    dst[dstIndex] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements)] = interp1(w[4], w[5]);
    dst[(dstIndex + dstRowElements*2)] = interp8(w[4], w[8]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp7(w[5], w[4], w[8]);
    dst[(dstIndex + dstRowElements*3)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp2(w[8], w[5], w[4]);
  }
  dst[dstIndex+1] = interp8(w[5], w[2]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[6]);
  dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp7(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[9]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase33 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp8(w[5], w[1]);
    dst[dstIndex+1] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
    dst[(dstIndex + dstRowElements*2)] = interp3(w[5], w[8]);
    dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[8]);
  } else {
    dst[dstIndex] = interp5(w[2], w[4]);
    dst[dstIndex+1] = interp2(w[2], w[5], w[4]);
    dst[(dstIndex + dstRowElements)] = interp8(w[4], w[2]);
    dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[4], w[2]);
    dst[(dstIndex + dstRowElements*2)] = interp1(w[4], w[5]);
    dst[(dstIndex + dstRowElements*3)] = interp1(w[5], w[4]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[3]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp7(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
};

const hq4xCase34 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp8(w[5], w[1]);
    dst[dstIndex+1] = interp1(w[5], w[1]);
    dst[dstIndex+2] = interp3(w[5], w[6]);
    dst[dstIndex+3] = interp8(w[5], w[6]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  } else {
    dst[dstIndex] = interp5(w[2], w[4]);
    dst[dstIndex+1] = interp8(w[2], w[4]);
    dst[dstIndex+2] = interp1(w[2], w[5]);
    dst[dstIndex+3] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp2(w[4], w[5], w[2]);
    dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[4], w[2]);
  }
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp7(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
};

const hq4xCase35 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[4]);
  dst[dstIndex+1] = interp3(w[5], w[4]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase36 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  dst[dstIndex+2] = interp3(w[5], w[6]);
  dst[dstIndex+3] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase37 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[1]);
  dst[dstIndex+2] = interp8(w[5], w[2]);
  dst[dstIndex+3] = interp8(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase38 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[1]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[8]);
};

const hq4xCase39 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[6]);
};

const hq4xCase40 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase41 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[1]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase42 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[2]);
  dst[dstIndex+1] = interp8(w[5], w[2]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase43 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp5(w[2], w[4]);
    dst[dstIndex+1] = interp5(w[2], w[5]);
    dst[(dstIndex + dstRowElements)] = interp5(w[4], w[5]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
    dst[dstIndex+3] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = w[5];
  } else {
    dst[dstIndex+2] = interp5(w[2], w[5]);
    dst[dstIndex+3] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 3)] = interp5(w[6], w[5]);
  }
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase44 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
    dst[dstIndex+3] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = w[5];
  } else {
    dst[dstIndex+2] = interp5(w[2], w[5]);
    dst[dstIndex+3] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 3)] = interp5(w[6], w[5]);
  }
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 3)] = interp5(w[6], w[5]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp5(w[8], w[5]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp5(w[8], w[6]);
  }
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
};

const hq4xCase45 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[1]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp5(w[4], w[5]);
    dst[(dstIndex + dstRowElements*3)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp5(w[8], w[5]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 3)] = interp5(w[6], w[5]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp5(w[8], w[5]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp5(w[8], w[6]);
  }
};

const hq4xCase46 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp5(w[2], w[4]);
    dst[dstIndex+1] = interp5(w[2], w[5]);
    dst[(dstIndex + dstRowElements)] = interp5(w[4], w[5]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[3]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp5(w[4], w[5]);
    dst[(dstIndex + dstRowElements*3)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp5(w[8], w[5]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[9]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase47 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp5(w[2], w[4]);
    dst[dstIndex+1] = interp5(w[2], w[5]);
    dst[(dstIndex + dstRowElements)] = interp5(w[4], w[5]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase48 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
    dst[dstIndex+3] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = w[5];
  } else {
    dst[dstIndex+2] = interp5(w[2], w[5]);
    dst[dstIndex+3] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 3)] = interp5(w[6], w[5]);
  }
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase49 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[1]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 3)] = interp5(w[6], w[5]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp5(w[8], w[5]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp5(w[8], w[6]);
  }
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
};

const hq4xCase50 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[3]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp5(w[4], w[5]);
    dst[(dstIndex + dstRowElements*3)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp5(w[8], w[5]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[9]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase51 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
    dst[dstIndex+3] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = w[5];
  } else {
    dst[dstIndex+2] = interp5(w[2], w[5]);
    dst[dstIndex+3] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 3)] = interp5(w[6], w[5]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase52 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 3)] = interp5(w[6], w[5]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp5(w[8], w[5]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp5(w[8], w[6]);
  }
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
};

const hq4xCase53 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[1]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp5(w[4], w[5]);
    dst[(dstIndex + dstRowElements*3)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp5(w[8], w[5]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase54 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp5(w[2], w[4]);
    dst[dstIndex+1] = interp5(w[2], w[5]);
    dst[(dstIndex + dstRowElements)] = interp5(w[4], w[5]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase55 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[2]);
  dst[dstIndex+1] = interp8(w[5], w[2]);
  dst[dstIndex+2] = interp8(w[5], w[2]);
  dst[dstIndex+3] = interp8(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase56 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  dst[dstIndex+2] = interp3(w[5], w[6]);
  dst[dstIndex+3] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[6]);
};

const hq4xCase57 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[1]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[8]);
};

const hq4xCase58 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[4]);
  dst[dstIndex+1] = interp3(w[5], w[4]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase59 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[2]);
  dst[dstIndex+1] = interp8(w[5], w[2]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase60 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[4]);
  dst[dstIndex+1] = interp3(w[5], w[4]);
  dst[dstIndex+2] = interp3(w[5], w[6]);
  dst[dstIndex+3] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase61 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[1]);
  dst[dstIndex+2] = interp8(w[5], w[2]);
  dst[dstIndex+3] = interp8(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[8]);
};

const hq4xCase62 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[6]);
};

const hq4xCase63 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[1]);
  dst[dstIndex+2] = interp8(w[5], w[2]);
  dst[dstIndex+3] = interp8(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase64 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[4]);
  dst[dstIndex+1] = interp3(w[5], w[4]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[6]);
};

const hq4xCase65 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  dst[dstIndex+2] = interp3(w[5], w[6]);
  dst[dstIndex+3] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase66 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[2]);
  dst[dstIndex+1] = interp8(w[5], w[2]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[8]);
};

const hq4xCase67 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp8(w[5], w[1]);
    dst[dstIndex+1] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[2], w[4]);
    dst[dstIndex+1] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
    dst[dstIndex+3] = interp8(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp1(w[5], w[2]);
    dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements*2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase68 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[4]);
  dst[dstIndex+1] = interp3(w[5], w[4]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
    dst[dstIndex+3] = interp8(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp1(w[5], w[2]);
    dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[6]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[8]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
  }
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
};

const hq4xCase69 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[1]);
  dst[dstIndex+2] = interp8(w[5], w[2]);
  dst[dstIndex+3] = interp8(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp3(w[5], w[2]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[8]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[6]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[8]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
  }
};

const hq4xCase70 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp8(w[5], w[1]);
    dst[dstIndex+1] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[2], w[4]);
    dst[dstIndex+1] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[3]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[8]);
  }
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[6]);
};

const hq4xCase71 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp8(w[5], w[1]);
    dst[dstIndex+1] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[2], w[4]);
    dst[dstIndex+1] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  }
  dst[dstIndex+2] = interp3(w[5], w[6]);
  dst[dstIndex+3] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 3)] = interp8(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[8]);
  }
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[9]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase72 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp8(w[5], w[1]);
    dst[dstIndex+1] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[2], w[4]);
    dst[dstIndex+1] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
    dst[dstIndex+3] = interp8(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp1(w[5], w[2]);
    dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[8]);
};

const hq4xCase73 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
    dst[dstIndex+3] = interp8(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp1(w[5], w[2]);
    dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements*2)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[4]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[6]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[8]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
  }
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp3(w[5], w[4]);
};

const hq4xCase74 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[2]);
  dst[dstIndex+1] = interp8(w[5], w[2]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[8]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[6]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[8]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
  }
};

const hq4xCase75 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp8(w[5], w[1]);
    dst[dstIndex+1] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[2], w[4]);
    dst[dstIndex+1] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
    dst[dstIndex+3] = interp8(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp1(w[5], w[2]);
    dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[6]);
  }
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[8]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[6]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[8]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
  }
};

const hq4xCase76 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex] = interp8(w[5], w[4]);
    dst[dstIndex+1] = interp3(w[5], w[4]);
    dst[dstIndex+2] = w[5];
    dst[dstIndex+3] = w[5];
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = w[5];
  } else {
    dst[dstIndex] = interp1(w[5], w[2]);
    dst[dstIndex+1] = interp1(w[2], w[5]);
    dst[dstIndex+2] = interp8(w[2], w[6]);
    dst[dstIndex+3] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp7(w[5], w[6], w[2]);
    dst[(dstIndex + dstRowElements + 3)] = interp2(w[6], w[5], w[2]);
  }
  dst[(dstIndex + dstRowElements)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp7(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase77 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
    dst[dstIndex+3] = w[5];
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 3)] = interp3(w[5], w[8]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[8]);
  } else {
    dst[dstIndex+2] = interp2(w[2], w[5], w[6]);
    dst[dstIndex+3] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp7(w[5], w[6], w[2]);
    dst[(dstIndex + dstRowElements + 3)] = interp8(w[6], w[2]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[6], w[5]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp1(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp7(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[5], w[8]);
};

const hq4xCase78 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[2], w[4]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[4]);
  dst[dstIndex+2] = interp8(w[5], w[2]);
  if (yuvDiff(n[6], n[8])) {
    dst[dstIndex+3] = interp8(w[5], w[2]);
    dst[(dstIndex + dstRowElements + 3)] = interp3(w[5], w[2]);
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[dstIndex+3] = interp1(w[5], w[6]);
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[6], w[5]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp7(w[5], w[6], w[8]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[6], w[8]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp2(w[8], w[5], w[6]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp5(w[8], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
};

const hq4xCase79 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[2], w[4]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[4]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[4]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp3(w[5], w[4]);
    dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp7(w[5], w[6], w[8]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp2(w[6], w[5], w[8]);
    dst[(dstIndex + dstRowElements*3)] = interp1(w[5], w[8]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[8], w[5]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[8], w[6]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp5(w[8], w[6]);
  }
};

const hq4xCase80 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[1]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[6]);
  dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp7(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[2]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 2)] = interp3(w[5], w[6]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[6]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp2(w[4], w[5], w[8]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp7(w[5], w[4], w[8]);
    dst[(dstIndex + dstRowElements*3)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[8], w[5]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp1(w[5], w[8]);
  }
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[5], w[6]);
};

const hq4xCase81 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[8], n[4])) {
    dst[dstIndex] = interp8(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[2]);
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
  } else {
    dst[dstIndex] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements)] = interp1(w[4], w[5]);
    dst[(dstIndex + dstRowElements*2)] = interp8(w[4], w[8]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp7(w[5], w[4], w[8]);
    dst[(dstIndex + dstRowElements*3)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp2(w[8], w[5], w[4]);
  }
  dst[dstIndex+1] = interp8(w[5], w[2]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[6]);
  dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp7(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[9]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase82 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = interp3(w[5], w[8]);
    dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[8]);
  } else {
    dst[dstIndex] = interp5(w[2], w[4]);
    dst[dstIndex+1] = interp2(w[2], w[5], w[4]);
    dst[(dstIndex + dstRowElements)] = interp8(w[4], w[2]);
    dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[4], w[2]);
    dst[(dstIndex + dstRowElements*2)] = interp1(w[4], w[5]);
    dst[(dstIndex + dstRowElements*3)] = interp1(w[5], w[4]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[3]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp7(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
};

const hq4xCase83 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = interp3(w[5], w[6]);
    dst[dstIndex+3] = interp8(w[5], w[6]);
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[dstIndex] = interp5(w[2], w[4]);
    dst[dstIndex+1] = interp8(w[2], w[4]);
    dst[dstIndex+2] = interp1(w[2], w[5]);
    dst[dstIndex+3] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp2(w[4], w[5], w[2]);
    dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[4], w[2]);
  }
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp7(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
};

const hq4xCase84 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[1]);
  dst[dstIndex+2] = interp8(w[5], w[2]);
  dst[dstIndex+3] = interp8(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp3(w[5], w[2]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp5(w[4], w[5]);
    dst[(dstIndex + dstRowElements*3)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp5(w[8], w[5]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase85 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp5(w[2], w[4]);
    dst[dstIndex+1] = interp5(w[2], w[5]);
    dst[(dstIndex + dstRowElements)] = interp5(w[4], w[5]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[6]);
};

const hq4xCase86 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
    dst[dstIndex+3] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = w[5];
  } else {
    dst[dstIndex+2] = interp5(w[2], w[5]);
    dst[dstIndex+3] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 3)] = interp5(w[6], w[5]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase87 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[4]);
  dst[dstIndex+1] = interp3(w[5], w[4]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 3)] = interp5(w[6], w[5]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp5(w[8], w[5]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp5(w[8], w[6]);
  }
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
};

const hq4xCase88 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
    dst[dstIndex+3] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = w[5];
  } else {
    dst[dstIndex+2] = interp5(w[2], w[5]);
    dst[dstIndex+3] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 3)] = interp5(w[6], w[5]);
  }
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase89 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[2]);
  dst[dstIndex+1] = interp8(w[5], w[2]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 3)] = interp5(w[6], w[5]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp5(w[8], w[5]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp5(w[8], w[6]);
  }
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
};

const hq4xCase90 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  dst[dstIndex+2] = interp3(w[5], w[6]);
  dst[dstIndex+3] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 3)] = interp8(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp5(w[4], w[5]);
    dst[(dstIndex + dstRowElements*3)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp5(w[8], w[5]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[9]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase91 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp5(w[2], w[4]);
    dst[dstIndex+1] = interp5(w[2], w[5]);
    dst[(dstIndex + dstRowElements)] = interp5(w[4], w[5]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[8]);
};

const hq4xCase92 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[1]);
  dst[dstIndex+2] = interp8(w[5], w[2]);
  dst[dstIndex+3] = interp8(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[8]);
};

const hq4xCase93 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[2]);
  dst[dstIndex+1] = interp8(w[5], w[2]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[8]);
};

const hq4xCase94 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[2]);
  dst[dstIndex+1] = interp8(w[5], w[2]);
  dst[dstIndex+2] = interp8(w[5], w[2]);
  dst[dstIndex+3] = interp8(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase95 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[2]);
  dst[dstIndex+1] = interp8(w[5], w[2]);
  dst[dstIndex+2] = interp8(w[5], w[2]);
  dst[dstIndex+3] = interp8(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[8]);
};

const hq4xCase96 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[4]);
  dst[dstIndex+1] = interp3(w[5], w[4]);
  dst[dstIndex+2] = interp3(w[5], w[6]);
  dst[dstIndex+3] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase97 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[4]);
  dst[dstIndex+1] = interp3(w[5], w[4]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[6]);
};

const hq4xCase98 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  dst[dstIndex+2] = interp3(w[5], w[6]);
  dst[dstIndex+3] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[6]);
};

const hq4xCase99 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[4]);
  dst[dstIndex+1] = interp3(w[5], w[4]);
  dst[dstIndex+2] = interp3(w[5], w[6]);
  dst[dstIndex+3] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[6]);
};

const hq4xCase100 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[1]);
  dst[dstIndex+2] = interp8(w[5], w[2]);
  dst[dstIndex+3] = interp8(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp3(w[5], w[2]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[8]);
  }
  dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 3)] = interp5(w[6], w[5]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp5(w[8], w[5]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp5(w[8], w[6]);
  }
};

const hq4xCase101 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp8(w[5], w[1]);
    dst[dstIndex+1] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[2], w[4]);
    dst[dstIndex+1] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
    dst[dstIndex+3] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = w[5];
  } else {
    dst[dstIndex+2] = interp5(w[2], w[5]);
    dst[dstIndex+3] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 3)] = interp5(w[6], w[5]);
  }
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[8]);
};

const hq4xCase102 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp8(w[5], w[1]);
    dst[dstIndex+1] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[2], w[4]);
    dst[dstIndex+1] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[3]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp5(w[4], w[5]);
    dst[(dstIndex + dstRowElements*3)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp5(w[8], w[5]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[6]);
};

const hq4xCase103 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
    dst[dstIndex+3] = interp8(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp1(w[5], w[2]);
    dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements*2)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 3)] = interp5(w[6], w[5]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp5(w[8], w[5]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp5(w[8], w[6]);
  }
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp3(w[5], w[4]);
};

const hq4xCase104 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp5(w[2], w[4]);
    dst[dstIndex+1] = interp5(w[2], w[5]);
    dst[(dstIndex + dstRowElements)] = interp5(w[4], w[5]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
    dst[dstIndex+3] = interp8(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp1(w[5], w[2]);
    dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase105 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[2]);
  dst[dstIndex+1] = interp8(w[5], w[2]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp5(w[4], w[5]);
    dst[(dstIndex + dstRowElements*3)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp5(w[8], w[5]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[6]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[8]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
  }
};

const hq4xCase106 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[4]);
  dst[dstIndex+1] = interp3(w[5], w[4]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
    dst[dstIndex+3] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = w[5];
  } else {
    dst[dstIndex+2] = interp5(w[2], w[5]);
    dst[dstIndex+3] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 3)] = interp5(w[6], w[5]);
  }
  dst[(dstIndex + dstRowElements)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[6]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[8]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
  }
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
};

const hq4xCase107 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp5(w[2], w[4]);
    dst[dstIndex+1] = interp5(w[2], w[5]);
    dst[(dstIndex + dstRowElements)] = interp5(w[4], w[5]);
  }
  dst[dstIndex+2] = interp3(w[5], w[6]);
  dst[dstIndex+3] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 3)] = interp8(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[8]);
  }
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[9]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase108 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp8(w[5], w[1]);
    dst[dstIndex+1] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[2], w[4]);
    dst[dstIndex+1] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
    dst[dstIndex+3] = interp8(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp1(w[5], w[2]);
    dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[6]);
  }
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp5(w[4], w[5]);
    dst[(dstIndex + dstRowElements*3)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp5(w[8], w[5]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[6]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[8]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
  }
};

const hq4xCase109 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp8(w[5], w[1]);
    dst[dstIndex+1] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[2], w[4]);
    dst[dstIndex+1] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
    dst[dstIndex+3] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = w[5];
  } else {
    dst[dstIndex+2] = interp5(w[2], w[5]);
    dst[dstIndex+3] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 3)] = interp5(w[6], w[5]);
  }
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[8]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[6]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[8]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
  }
};

const hq4xCase110 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp8(w[5], w[1]);
    dst[dstIndex+1] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[2], w[4]);
    dst[dstIndex+1] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
    dst[dstIndex+3] = interp8(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp1(w[5], w[2]);
    dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[6]);
  }
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[8]);
  }
  dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 3)] = interp5(w[6], w[5]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp5(w[8], w[5]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp5(w[8], w[6]);
  }
};

const hq4xCase111 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp5(w[2], w[4]);
    dst[dstIndex+1] = interp5(w[2], w[5]);
    dst[(dstIndex + dstRowElements)] = interp5(w[4], w[5]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
    dst[dstIndex+3] = interp8(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp1(w[5], w[2]);
    dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[8]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[6]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[8]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
  }
};

const hq4xCase112 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[2], w[4]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[4]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[6]);
  dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp7(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[6]);
};

const hq4xCase113 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[4]);
  dst[dstIndex+1] = interp3(w[5], w[4]);
  dst[dstIndex+2] = interp3(w[5], w[6]);
  dst[dstIndex+3] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp7(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp7(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
};

const hq4xCase114 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[2]);
  dst[dstIndex+1] = interp8(w[5], w[2]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[6]);
  dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp7(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp7(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
};

const hq4xCase115 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[2], w[4]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[4]);
  dst[dstIndex+2] = interp8(w[5], w[2]);
  dst[dstIndex+3] = interp8(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp7(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[8]);
};

const hq4xCase116 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp8(w[5], w[1]);
    dst[dstIndex+1] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[2], w[4]);
    dst[dstIndex+1] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
    dst[dstIndex+3] = interp8(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp1(w[5], w[2]);
    dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements*2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[8]);
};

const hq4xCase117 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[4]);
  dst[dstIndex+1] = interp3(w[5], w[4]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
    dst[dstIndex+3] = interp8(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp1(w[5], w[2]);
    dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[4]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[6]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[8]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
  }
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp3(w[5], w[4]);
};

const hq4xCase118 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[2]);
  dst[dstIndex+1] = interp8(w[5], w[2]);
  dst[dstIndex+2] = interp8(w[5], w[2]);
  dst[dstIndex+3] = interp8(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp3(w[5], w[2]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[8]);
  }
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[6]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[8]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
  }
};

const hq4xCase119 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp8(w[5], w[1]);
    dst[dstIndex+1] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[2], w[4]);
    dst[dstIndex+1] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  }
  dst[dstIndex+2] = interp3(w[5], w[6]);
  dst[dstIndex+3] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 3)] = interp8(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[8]);
  }
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[6]);
};

const hq4xCase120 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[2]);
  dst[dstIndex+1] = interp8(w[5], w[2]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[6]);
  dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp7(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[2]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[8]);
  }
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[6]);
};

const hq4xCase121 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = interp8(w[5], w[1]);
    dst[dstIndex+1] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
    dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  } else {
    dst[dstIndex] = interp2(w[5], w[2], w[4]);
    dst[dstIndex+1] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  }
  dst[dstIndex+2] = interp3(w[5], w[6]);
  dst[dstIndex+3] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp7(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
};

const hq4xCase122 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[4]);
  dst[dstIndex+1] = interp3(w[5], w[4]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = interp1(w[5], w[3]);
    dst[dstIndex+3] = interp8(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  } else {
    dst[dstIndex+2] = interp1(w[5], w[2]);
    dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp7(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[8]);
};

const hq4xCase123 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[2], w[4]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[4]);
  dst[dstIndex+2] = interp8(w[5], w[2]);
  dst[dstIndex+3] = interp8(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[4]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[6]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[8]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
  }
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp3(w[5], w[4]);
};

const hq4xCase124 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[2]);
  dst[dstIndex+1] = interp8(w[5], w[2]);
  dst[dstIndex+2] = interp8(w[5], w[2]);
  dst[dstIndex+3] = interp8(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[8]);
};

const hq4xCase125 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[4]);
  dst[dstIndex+1] = interp3(w[5], w[4]);
  dst[dstIndex+2] = interp3(w[5], w[6]);
  dst[dstIndex+3] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[6]);
};

const hq4xCase126 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
    dst[dstIndex+3] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = w[5];
  } else {
    dst[dstIndex+2] = interp5(w[2], w[5]);
    dst[dstIndex+3] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 3)] = interp5(w[6], w[5]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp5(w[4], w[5]);
    dst[(dstIndex + dstRowElements*3)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp5(w[8], w[5]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase127 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp5(w[2], w[4]);
    dst[dstIndex+1] = interp5(w[2], w[5]);
    dst[(dstIndex + dstRowElements)] = interp5(w[4], w[5]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 3)] = interp5(w[6], w[5]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp5(w[8], w[5]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp5(w[8], w[6]);
  }
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
};

const hq4xCase128 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[8], n[4])) {
    dst[dstIndex] = interp8(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp3(w[5], w[2]);
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
  } else {
    dst[dstIndex] = interp1(w[5], w[4]);
    dst[(dstIndex + dstRowElements)] = interp1(w[4], w[5]);
    dst[(dstIndex + dstRowElements*2)] = interp8(w[4], w[8]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp7(w[5], w[4], w[8]);
    dst[(dstIndex + dstRowElements*3)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp2(w[8], w[5], w[4]);
  }
  dst[dstIndex+1] = interp8(w[5], w[2]);
  dst[dstIndex+2] = interp8(w[5], w[2]);
  dst[dstIndex+3] = interp8(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase129 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[2]);
  dst[dstIndex+1] = interp8(w[5], w[2]);
  dst[dstIndex+2] = interp8(w[5], w[2]);
  if (yuvDiff(n[6], n[8])) {
    dst[dstIndex+3] = interp8(w[5], w[2]);
    dst[(dstIndex + dstRowElements + 3)] = interp3(w[5], w[2]);
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[dstIndex+3] = interp1(w[5], w[6]);
    dst[(dstIndex + dstRowElements + 3)] = interp1(w[6], w[5]);
    dst[(dstIndex + dstRowElements*2 + 2)] = interp7(w[5], w[6], w[8]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[6], w[8]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp2(w[8], w[5], w[6]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp5(w[8], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
};

const hq4xCase130 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[dstIndex+2] = interp3(w[5], w[6]);
    dst[dstIndex+3] = interp8(w[5], w[6]);
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements + 1)] = w[5];
  } else {
    dst[dstIndex] = interp5(w[2], w[4]);
    dst[dstIndex+1] = interp8(w[2], w[4]);
    dst[dstIndex+2] = interp1(w[2], w[5]);
    dst[dstIndex+3] = interp1(w[5], w[2]);
    dst[(dstIndex + dstRowElements)] = interp2(w[4], w[5], w[2]);
    dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[4], w[2]);
  }
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[6]);
};

const hq4xCase131 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  dst[dstIndex+2] = interp3(w[5], w[6]);
  dst[dstIndex+3] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 3)] = interp8(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 2)] = interp3(w[5], w[6]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[6]);
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp2(w[4], w[5], w[8]);
    dst[(dstIndex + dstRowElements*2 + 1)] = interp7(w[5], w[4], w[8]);
    dst[(dstIndex + dstRowElements*3)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[8], w[5]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp1(w[5], w[8]);
  }
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[5], w[6]);
};

const hq4xCase132 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
    dst[dstIndex+3] = w[5];
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 3)] = interp3(w[5], w[8]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[8]);
  } else {
    dst[dstIndex+2] = interp2(w[2], w[5], w[6]);
    dst[dstIndex+3] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp7(w[5], w[6], w[2]);
    dst[(dstIndex + dstRowElements + 3)] = interp8(w[6], w[2]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[6], w[5]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp1(w[5], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements*2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[5], w[8]);
};

const hq4xCase133 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
    dst[(dstIndex + dstRowElements + 1)] = w[5];
    dst[(dstIndex + dstRowElements*2)] = interp3(w[5], w[8]);
    dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[8]);
  } else {
    dst[dstIndex] = interp5(w[2], w[4]);
    dst[dstIndex+1] = interp2(w[2], w[5], w[4]);
    dst[(dstIndex + dstRowElements)] = interp8(w[4], w[2]);
    dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[4], w[2]);
    dst[(dstIndex + dstRowElements*2)] = interp1(w[4], w[5]);
    dst[(dstIndex + dstRowElements*3)] = interp1(w[5], w[4]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[8]);
};

const hq4xCase134 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[4]);
  dst[dstIndex+1] = interp3(w[5], w[4]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements*2)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[4]);
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp3(w[5], w[4]);
    dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 2)] = interp7(w[5], w[6], w[8]);
    dst[(dstIndex + dstRowElements*2 + 3)] = interp2(w[6], w[5], w[8]);
    dst[(dstIndex + dstRowElements*3)] = interp1(w[5], w[8]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[8], w[5]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[8], w[6]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp5(w[8], w[6]);
  }
};

const hq4xCase135 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex] = interp8(w[5], w[4]);
    dst[dstIndex+1] = interp3(w[5], w[4]);
    dst[dstIndex+2] = w[5];
    dst[dstIndex+3] = w[5];
    dst[(dstIndex + dstRowElements + 2)] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = w[5];
  } else {
    dst[dstIndex] = interp1(w[5], w[2]);
    dst[dstIndex+1] = interp1(w[2], w[5]);
    dst[dstIndex+2] = interp8(w[2], w[6]);
    dst[dstIndex+3] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 2)] = interp7(w[5], w[6], w[2]);
    dst[(dstIndex + dstRowElements + 3)] = interp2(w[6], w[5], w[2]);
  }
  dst[(dstIndex + dstRowElements)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase136 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[2]);
  dst[dstIndex+1] = interp8(w[5], w[2]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[6]);
  dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  dst[(dstIndex + dstRowElements)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp7(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[2]);
  dst[(dstIndex + dstRowElements*2)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*3 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[6]);
};

const hq4xCase137 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[2], w[4]);
  }
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp3(w[5], w[6]);
  dst[dstIndex+3] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp7(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
};

const hq4xCase138 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[4]);
  dst[dstIndex+1] = interp3(w[5], w[4]);
  dst[dstIndex+2] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+3] = w[5];
  } else {
    dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements + 3)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp7(w[5], w[4], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[8]);
};

const hq4xCase139 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp2(w[5], w[2], w[4]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[4]);
  dst[dstIndex+2] = interp8(w[5], w[2]);
  dst[dstIndex+3] = interp8(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[4], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements*2)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
  }
};

const hq4xCase140 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp5(w[4], w[5]);
    dst[(dstIndex + dstRowElements*3)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp5(w[8], w[5]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 3)] = interp5(w[6], w[5]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp5(w[8], w[5]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp5(w[8], w[6]);
  }
};

const hq4xCase141 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp5(w[2], w[4]);
    dst[dstIndex+1] = interp5(w[2], w[5]);
    dst[(dstIndex + dstRowElements)] = interp5(w[4], w[5]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp5(w[4], w[5]);
    dst[(dstIndex + dstRowElements*3)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp5(w[8], w[5]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase142 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp5(w[2], w[4]);
    dst[dstIndex+1] = interp5(w[2], w[5]);
    dst[(dstIndex + dstRowElements)] = interp5(w[4], w[5]);
  }
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
    dst[dstIndex+3] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = w[5];
  } else {
    dst[dstIndex+2] = interp5(w[2], w[5]);
    dst[dstIndex+3] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 3)] = interp5(w[6], w[5]);
  }
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase143 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
    dst[dstIndex+3] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = w[5];
  } else {
    dst[dstIndex+2] = interp5(w[2], w[5]);
    dst[dstIndex+3] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 3)] = interp5(w[6], w[5]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 3)] = interp5(w[6], w[5]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp5(w[8], w[5]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp5(w[8], w[6]);
  }
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
};

const hq4xCase144 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp6(w[5], w[2], w[1]);
  dst[dstIndex+2] = interp8(w[5], w[2]);
  dst[dstIndex+3] = interp8(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp3(w[5], w[2]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp5(w[4], w[5]);
    dst[(dstIndex + dstRowElements*3)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp5(w[8], w[5]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
  dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
  }
};

const hq4xCase145 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[2]);
  dst[dstIndex+1] = interp8(w[5], w[2]);
  dst[dstIndex+2] = interp6(w[5], w[2], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements*2)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 3)] = interp5(w[6], w[5]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp5(w[8], w[5]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp5(w[8], w[6]);
  }
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
};

const hq4xCase146 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp5(w[2], w[4]);
    dst[dstIndex+1] = interp5(w[2], w[5]);
    dst[(dstIndex + dstRowElements)] = interp5(w[4], w[5]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp6(w[5], w[6], w[3]);
  dst[(dstIndex + dstRowElements*2)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*3 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[6]);
};

const hq4xCase147 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[2], w[4]);
  }
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp3(w[5], w[6]);
  dst[dstIndex+3] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 3)] = interp8(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp5(w[4], w[5]);
    dst[(dstIndex + dstRowElements*3)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp5(w[8], w[5]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp6(w[5], w[6], w[9]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase148 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[2], w[4]);
  }
  dst[dstIndex+1] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
    dst[dstIndex+3] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = w[5];
  } else {
    dst[dstIndex+2] = interp5(w[2], w[5]);
    dst[dstIndex+3] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 3)] = interp5(w[6], w[5]);
  }
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp6(w[5], w[8], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase149 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp5(w[2], w[4]);
    dst[dstIndex+1] = interp5(w[2], w[5]);
    dst[(dstIndex + dstRowElements)] = interp5(w[4], w[5]);
  }
  dst[dstIndex+2] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+3] = w[5];
  } else {
    dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements + 3)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp6(w[5], w[8], w[7]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[8]);
};

const hq4xCase150 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[4]);
  dst[dstIndex+1] = interp3(w[5], w[4]);
  dst[dstIndex+2] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+3] = w[5];
  } else {
    dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements + 3)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp6(w[5], w[4], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 3)] = interp5(w[6], w[5]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp5(w[8], w[5]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp5(w[8], w[6]);
  }
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
};

const hq4xCase151 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
    dst[dstIndex+3] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = w[5];
  } else {
    dst[dstIndex+2] = interp5(w[2], w[5]);
    dst[dstIndex+3] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 3)] = interp5(w[6], w[5]);
  }
  dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
  }
};

const hq4xCase152 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[1]);
  dst[dstIndex+1] = interp1(w[5], w[1]);
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
    dst[dstIndex+3] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = w[5];
  } else {
    dst[dstIndex+2] = interp5(w[2], w[5]);
    dst[dstIndex+3] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 3)] = interp5(w[6], w[5]);
  }
  dst[(dstIndex + dstRowElements)] = interp1(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[1]);
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp5(w[4], w[5]);
    dst[(dstIndex + dstRowElements*3)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp5(w[8], w[5]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
  dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
  }
};

const hq4xCase153 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[2]);
  dst[dstIndex+1] = interp8(w[5], w[2]);
  dst[dstIndex+2] = interp8(w[5], w[2]);
  dst[dstIndex+3] = interp8(w[5], w[2]);
  dst[(dstIndex + dstRowElements)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements + 3)] = interp3(w[5], w[2]);
  dst[(dstIndex + dstRowElements*2)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
  }
};

const hq4xCase154 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp5(w[2], w[4]);
    dst[dstIndex+1] = interp5(w[2], w[5]);
    dst[(dstIndex + dstRowElements)] = interp5(w[4], w[5]);
  }
  dst[dstIndex+2] = interp1(w[5], w[3]);
  dst[dstIndex+3] = interp8(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[3]);
  dst[(dstIndex + dstRowElements + 3)] = interp1(w[5], w[3]);
  dst[(dstIndex + dstRowElements*2)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 3)] = interp5(w[6], w[5]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp5(w[8], w[5]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp5(w[8], w[6]);
  }
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
};

const hq4xCase155 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[2], w[4]);
  }
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = interp3(w[5], w[6]);
  dst[dstIndex+3] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements + 3)] = interp8(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp8(w[5], w[6]);
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*3 + 2)] = interp3(w[5], w[6]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[6]);
};

const hq4xCase156 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[2], w[4]);
  }
  dst[dstIndex+1] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+2] = w[5];
    dst[dstIndex+3] = w[5];
    dst[(dstIndex + dstRowElements + 3)] = w[5];
  } else {
    dst[dstIndex+2] = interp5(w[2], w[5]);
    dst[dstIndex+3] = interp5(w[2], w[6]);
    dst[(dstIndex + dstRowElements + 3)] = interp5(w[6], w[5]);
  }
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*2)] = w[5];
    dst[(dstIndex + dstRowElements*3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2)] = interp5(w[4], w[5]);
    dst[(dstIndex + dstRowElements*3)] = interp5(w[8], w[4]);
    dst[(dstIndex + dstRowElements*3 + 1)] = interp5(w[8], w[5]);
  }
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[9]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp1(w[5], w[9]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[9]);
};

const hq4xCase157 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[2], w[4]);
  }
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+3] = w[5];
  } else {
    dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements + 3)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 2)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*2 + 3)] = interp3(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 2)] = interp8(w[5], w[8]);
  dst[(dstIndex + dstRowElements*3 + 3)] = interp8(w[5], w[8]);
};

const hq4xCase158 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
    dst[dstIndex+1] = w[5];
    dst[(dstIndex + dstRowElements)] = w[5];
  } else {
    dst[dstIndex] = interp5(w[2], w[4]);
    dst[dstIndex+1] = interp5(w[2], w[5]);
    dst[(dstIndex + dstRowElements)] = interp5(w[4], w[5]);
  }
  dst[dstIndex+2] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+3] = w[5];
  } else {
    dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements + 3)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp1(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[7]);
  dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*2 + 3)] = interp5(w[6], w[5]);
    dst[(dstIndex + dstRowElements*3 + 2)] = interp5(w[8], w[5]);
    dst[(dstIndex + dstRowElements*3 + 3)] = interp5(w[8], w[6]);
  }
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[7]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp1(w[5], w[7]);
};

const hq4xCase159 = (dst, dstIndex, dstRowElements, w, n) => {
  dst[dstIndex] = interp8(w[5], w[4]);
  dst[dstIndex+1] = interp3(w[5], w[4]);
  dst[dstIndex+2] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+3] = w[5];
  } else {
    dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements + 3)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
  dst[(dstIndex + dstRowElements*3)] = interp8(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 1)] = interp3(w[5], w[4]);
  dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
  }
};

const hq4xCase160 = (dst, dstIndex, dstRowElements, w, n) => {
  if (yuvDiff(n[4], n[2])) {
    dst[dstIndex] = w[5];
  } else {
    dst[dstIndex] = interp2(w[5], w[2], w[4]);
  }
  dst[dstIndex+1] = w[5];
  dst[dstIndex+2] = w[5];
  if (yuvDiff(n[2], n[6])) {
    dst[dstIndex+3] = w[5];
  } else {
    dst[dstIndex+3] = interp2(w[5], w[2], w[6]);
  }
  dst[(dstIndex + dstRowElements)] = w[5];
  dst[(dstIndex + dstRowElements + 1)] = w[5];
  dst[(dstIndex + dstRowElements + 2)] = w[5];
  dst[(dstIndex + dstRowElements + 3)] = w[5];
  dst[(dstIndex + dstRowElements*2)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 2)] = w[5];
  dst[(dstIndex + dstRowElements*2 + 3)] = w[5];
  if (yuvDiff(n[8], n[4])) {
    dst[(dstIndex + dstRowElements*3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*3)] = interp2(w[5], w[8], w[4]);
  }
  dst[(dstIndex + dstRowElements*3 + 1)] = w[5];
  dst[(dstIndex + dstRowElements*3 + 2)] = w[5];
  if (yuvDiff(n[6], n[8])) {
    dst[(dstIndex + dstRowElements*3 + 3)] = w[5];
  } else {
    dst[(dstIndex + dstRowElements*3 + 3)] = interp2(w[5], w[8], w[6]);
  }
};

export const hq4xPatterns = new Array(256);
hq4xPatterns[0] = hq4xCase0;
hq4xPatterns[1] = hq4xCase0;
hq4xPatterns[4] = hq4xCase0;
hq4xPatterns[32] = hq4xCase0;
hq4xPatterns[128] = hq4xCase0;
hq4xPatterns[5] = hq4xCase0;
hq4xPatterns[132] = hq4xCase0;
hq4xPatterns[160] = hq4xCase0;
hq4xPatterns[33] = hq4xCase0;
hq4xPatterns[129] = hq4xCase0;
hq4xPatterns[36] = hq4xCase0;
hq4xPatterns[133] = hq4xCase0;
hq4xPatterns[164] = hq4xCase0;
hq4xPatterns[161] = hq4xCase0;
hq4xPatterns[37] = hq4xCase0;
hq4xPatterns[165] = hq4xCase0;
hq4xPatterns[2] = hq4xCase1;
hq4xPatterns[34] = hq4xCase1;
hq4xPatterns[130] = hq4xCase1;
hq4xPatterns[162] = hq4xCase1;
hq4xPatterns[16] = hq4xCase2;
hq4xPatterns[17] = hq4xCase2;
hq4xPatterns[48] = hq4xCase2;
hq4xPatterns[49] = hq4xCase2;
hq4xPatterns[64] = hq4xCase3;
hq4xPatterns[65] = hq4xCase3;
hq4xPatterns[68] = hq4xCase3;
hq4xPatterns[69] = hq4xCase3;
hq4xPatterns[8] = hq4xCase4;
hq4xPatterns[12] = hq4xCase4;
hq4xPatterns[136] = hq4xCase4;
hq4xPatterns[140] = hq4xCase4;
hq4xPatterns[3] = hq4xCase5;
hq4xPatterns[35] = hq4xCase5;
hq4xPatterns[131] = hq4xCase5;
hq4xPatterns[163] = hq4xCase5;
hq4xPatterns[6] = hq4xCase6;
hq4xPatterns[38] = hq4xCase6;
hq4xPatterns[134] = hq4xCase6;
hq4xPatterns[166] = hq4xCase6;
hq4xPatterns[20] = hq4xCase7;
hq4xPatterns[21] = hq4xCase7;
hq4xPatterns[52] = hq4xCase7;
hq4xPatterns[53] = hq4xCase7;
hq4xPatterns[144] = hq4xCase8;
hq4xPatterns[145] = hq4xCase8;
hq4xPatterns[176] = hq4xCase8;
hq4xPatterns[177] = hq4xCase8;
hq4xPatterns[192] = hq4xCase9;
hq4xPatterns[193] = hq4xCase9;
hq4xPatterns[196] = hq4xCase9;
hq4xPatterns[197] = hq4xCase9;
hq4xPatterns[96] = hq4xCase10;
hq4xPatterns[97] = hq4xCase10;
hq4xPatterns[100] = hq4xCase10;
hq4xPatterns[101] = hq4xCase10;
hq4xPatterns[40] = hq4xCase11;
hq4xPatterns[44] = hq4xCase11;
hq4xPatterns[168] = hq4xCase11;
hq4xPatterns[172] = hq4xCase11;
hq4xPatterns[9] = hq4xCase12;
hq4xPatterns[13] = hq4xCase12;
hq4xPatterns[137] = hq4xCase12;
hq4xPatterns[141] = hq4xCase12;
hq4xPatterns[18] = hq4xCase13;
hq4xPatterns[50] = hq4xCase13;
hq4xPatterns[80] = hq4xCase14;
hq4xPatterns[81] = hq4xCase14;
hq4xPatterns[72] = hq4xCase15;
hq4xPatterns[76] = hq4xCase15;
hq4xPatterns[10] = hq4xCase16;
hq4xPatterns[138] = hq4xCase16;
hq4xPatterns[66] = hq4xCase17;
hq4xPatterns[24] = hq4xCase18;
hq4xPatterns[7] = hq4xCase19;
hq4xPatterns[39] = hq4xCase19;
hq4xPatterns[135] = hq4xCase19;
hq4xPatterns[148] = hq4xCase20;
hq4xPatterns[149] = hq4xCase20;
hq4xPatterns[180] = hq4xCase20;
hq4xPatterns[224] = hq4xCase21;
hq4xPatterns[228] = hq4xCase21;
hq4xPatterns[225] = hq4xCase21;
hq4xPatterns[41] = hq4xCase22;
hq4xPatterns[169] = hq4xCase22;
hq4xPatterns[45] = hq4xCase22;
hq4xPatterns[22] = hq4xCase23;
hq4xPatterns[54] = hq4xCase23;
hq4xPatterns[208] = hq4xCase24;
hq4xPatterns[209] = hq4xCase24;
hq4xPatterns[104] = hq4xCase25;
hq4xPatterns[108] = hq4xCase25;
hq4xPatterns[11] = hq4xCase26;
hq4xPatterns[139] = hq4xCase26;
hq4xPatterns[19] = hq4xCase27;
hq4xPatterns[51] = hq4xCase27;
hq4xPatterns[146] = hq4xCase28;
hq4xPatterns[178] = hq4xCase28;
hq4xPatterns[84] = hq4xCase29;
hq4xPatterns[85] = hq4xCase29;
hq4xPatterns[112] = hq4xCase30;
hq4xPatterns[113] = hq4xCase30;
hq4xPatterns[200] = hq4xCase31;
hq4xPatterns[204] = hq4xCase31;
hq4xPatterns[73] = hq4xCase32;
hq4xPatterns[77] = hq4xCase32;
hq4xPatterns[42] = hq4xCase33;
hq4xPatterns[170] = hq4xCase33;
hq4xPatterns[14] = hq4xCase34;
hq4xPatterns[142] = hq4xCase34;
hq4xPatterns[67] = hq4xCase35;
hq4xPatterns[70] = hq4xCase36;
hq4xPatterns[28] = hq4xCase37;
hq4xPatterns[152] = hq4xCase38;
hq4xPatterns[194] = hq4xCase39;
hq4xPatterns[98] = hq4xCase40;
hq4xPatterns[56] = hq4xCase41;
hq4xPatterns[25] = hq4xCase42;
hq4xPatterns[26] = hq4xCase43;
hq4xPatterns[31] = hq4xCase43;
hq4xPatterns[82] = hq4xCase44;
hq4xPatterns[214] = hq4xCase44;
hq4xPatterns[88] = hq4xCase45;
hq4xPatterns[248] = hq4xCase45;
hq4xPatterns[74] = hq4xCase46;
hq4xPatterns[107] = hq4xCase46;
hq4xPatterns[27] = hq4xCase47;
hq4xPatterns[86] = hq4xCase48;
hq4xPatterns[216] = hq4xCase49;
hq4xPatterns[106] = hq4xCase50;
hq4xPatterns[30] = hq4xCase51;
hq4xPatterns[210] = hq4xCase52;
hq4xPatterns[120] = hq4xCase53;
hq4xPatterns[75] = hq4xCase54;
hq4xPatterns[29] = hq4xCase55;
hq4xPatterns[198] = hq4xCase56;
hq4xPatterns[184] = hq4xCase57;
hq4xPatterns[99] = hq4xCase58;
hq4xPatterns[57] = hq4xCase59;
hq4xPatterns[71] = hq4xCase60;
hq4xPatterns[156] = hq4xCase61;
hq4xPatterns[226] = hq4xCase62;
hq4xPatterns[60] = hq4xCase63;
hq4xPatterns[195] = hq4xCase64;
hq4xPatterns[102] = hq4xCase65;
hq4xPatterns[153] = hq4xCase66;
hq4xPatterns[58] = hq4xCase67;
hq4xPatterns[83] = hq4xCase68;
hq4xPatterns[92] = hq4xCase69;
hq4xPatterns[202] = hq4xCase70;
hq4xPatterns[78] = hq4xCase71;
hq4xPatterns[154] = hq4xCase72;
hq4xPatterns[114] = hq4xCase73;
hq4xPatterns[89] = hq4xCase74;
hq4xPatterns[90] = hq4xCase75;
hq4xPatterns[55] = hq4xCase76;
hq4xPatterns[23] = hq4xCase76;
hq4xPatterns[182] = hq4xCase77;
hq4xPatterns[150] = hq4xCase77;
hq4xPatterns[213] = hq4xCase78;
hq4xPatterns[212] = hq4xCase78;
hq4xPatterns[241] = hq4xCase79;
hq4xPatterns[240] = hq4xCase79;
hq4xPatterns[236] = hq4xCase80;
hq4xPatterns[232] = hq4xCase80;
hq4xPatterns[109] = hq4xCase81;
hq4xPatterns[105] = hq4xCase81;
hq4xPatterns[171] = hq4xCase82;
hq4xPatterns[43] = hq4xCase82;
hq4xPatterns[143] = hq4xCase83;
hq4xPatterns[15] = hq4xCase83;
hq4xPatterns[124] = hq4xCase84;
hq4xPatterns[203] = hq4xCase85;
hq4xPatterns[62] = hq4xCase86;
hq4xPatterns[211] = hq4xCase87;
hq4xPatterns[118] = hq4xCase88;
hq4xPatterns[217] = hq4xCase89;
hq4xPatterns[110] = hq4xCase90;
hq4xPatterns[155] = hq4xCase91;
hq4xPatterns[188] = hq4xCase92;
hq4xPatterns[185] = hq4xCase93;
hq4xPatterns[61] = hq4xCase94;
hq4xPatterns[157] = hq4xCase95;
hq4xPatterns[103] = hq4xCase96;
hq4xPatterns[227] = hq4xCase97;
hq4xPatterns[230] = hq4xCase98;
hq4xPatterns[199] = hq4xCase99;
hq4xPatterns[220] = hq4xCase100;
hq4xPatterns[158] = hq4xCase101;
hq4xPatterns[234] = hq4xCase102;
hq4xPatterns[242] = hq4xCase103;
hq4xPatterns[59] = hq4xCase104;
hq4xPatterns[121] = hq4xCase105;
hq4xPatterns[87] = hq4xCase106;
hq4xPatterns[79] = hq4xCase107;
hq4xPatterns[122] = hq4xCase108;
hq4xPatterns[94] = hq4xCase109;
hq4xPatterns[218] = hq4xCase110;
hq4xPatterns[91] = hq4xCase111;
hq4xPatterns[229] = hq4xCase112;
hq4xPatterns[167] = hq4xCase113;
hq4xPatterns[173] = hq4xCase114;
hq4xPatterns[181] = hq4xCase115;
hq4xPatterns[186] = hq4xCase116;
hq4xPatterns[115] = hq4xCase117;
hq4xPatterns[93] = hq4xCase118;
hq4xPatterns[206] = hq4xCase119;
hq4xPatterns[205] = hq4xCase120;
hq4xPatterns[201] = hq4xCase120;
hq4xPatterns[174] = hq4xCase121;
hq4xPatterns[46] = hq4xCase121;
hq4xPatterns[179] = hq4xCase122;
hq4xPatterns[147] = hq4xCase122;
hq4xPatterns[117] = hq4xCase123;
hq4xPatterns[116] = hq4xCase123;
hq4xPatterns[189] = hq4xCase124;
hq4xPatterns[231] = hq4xCase125;
hq4xPatterns[126] = hq4xCase126;
hq4xPatterns[219] = hq4xCase127;
hq4xPatterns[125] = hq4xCase128;
hq4xPatterns[221] = hq4xCase129;
hq4xPatterns[207] = hq4xCase130;
hq4xPatterns[238] = hq4xCase131;
hq4xPatterns[190] = hq4xCase132;
hq4xPatterns[187] = hq4xCase133;
hq4xPatterns[243] = hq4xCase134;
hq4xPatterns[119] = hq4xCase135;
hq4xPatterns[237] = hq4xCase136;
hq4xPatterns[233] = hq4xCase136;
hq4xPatterns[175] = hq4xCase137;
hq4xPatterns[47] = hq4xCase137;
hq4xPatterns[183] = hq4xCase138;
hq4xPatterns[151] = hq4xCase138;
hq4xPatterns[245] = hq4xCase139;
hq4xPatterns[244] = hq4xCase139;
hq4xPatterns[250] = hq4xCase140;
hq4xPatterns[123] = hq4xCase141;
hq4xPatterns[95] = hq4xCase142;
hq4xPatterns[222] = hq4xCase143;
hq4xPatterns[252] = hq4xCase144;
hq4xPatterns[249] = hq4xCase145;
hq4xPatterns[235] = hq4xCase146;
hq4xPatterns[111] = hq4xCase147;
hq4xPatterns[63] = hq4xCase148;
hq4xPatterns[159] = hq4xCase149;
hq4xPatterns[215] = hq4xCase150;
hq4xPatterns[246] = hq4xCase151;
hq4xPatterns[254] = hq4xCase152;
hq4xPatterns[253] = hq4xCase153;
hq4xPatterns[251] = hq4xCase154;
hq4xPatterns[239] = hq4xCase155;
hq4xPatterns[127] = hq4xCase156;
hq4xPatterns[191] = hq4xCase157;
hq4xPatterns[223] = hq4xCase158;
hq4xPatterns[247] = hq4xCase159;
hq4xPatterns[255] = hq4xCase160;
