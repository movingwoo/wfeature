// QR encoding, for the screen that hands a phone the address it should open.
//
// This file is a copy of js/lib/qr/encoder.js from wtools
// (https://wtools.movingwoo.com), the same author's MIT-licensed tools site,
// and is kept verbatim below this comment so a diff against that file says
// whether the two have drifted.
//
// It is a copy rather than a call to that site, and the reason is the whole
// shape of this program: the emulator runs on a machine in the house, the
// screen this draws is the one a user looks at when something is not working,
// and on a `-public` server the text being encoded is the access key itself.
// A QR that needed the internet would be missing exactly when it is needed,
// and a key that travelled to a third party to be drawn would no longer be a
// secret this machine keeps.
//
// MIT License, Copyright (c) 2026 movingwoo.

// QR Code Model 2 encoder: byte mode, versions 1-40, all error-correction levels.

const LEVELS = Object.freeze({
  L: { table: 0, format: 1 },
  M: { table: 1, format: 0 },
  Q: { table: 2, format: 3 },
  H: { table: 3, format: 2 },
});

// ISO/IEC 18004 block groups: count, total codewords, data codewords.
// Entries are ordered by version, then L/M/Q/H error-correction level.
const RS_BLOCKS = [
  [1,26,19], [1,26,16], [1,26,13], [1,26,9],
  [1,44,34], [1,44,28], [1,44,22], [1,44,16],
  [1,70,55], [1,70,44], [2,35,17], [2,35,13],
  [1,100,80], [2,50,32], [2,50,24], [4,25,9],
  [1,134,108], [2,67,43], [2,33,15,2,34,16], [2,33,11,2,34,12],
  [2,86,68], [4,43,27], [4,43,19], [4,43,15],
  [2,98,78], [4,49,31], [2,32,14,4,33,15], [4,39,13,1,40,14],
  [2,121,97], [2,60,38,2,61,39], [4,40,18,2,41,19], [4,40,14,2,41,15],
  [2,146,116], [3,58,36,2,59,37], [4,36,16,4,37,17], [4,36,12,4,37,13],
  [2,86,68,2,87,69], [4,69,43,1,70,44], [6,43,19,2,44,20], [6,43,15,2,44,16],
  [4,101,81], [1,80,50,4,81,51], [4,50,22,4,51,23], [3,36,12,8,37,13],
  [2,116,92,2,117,93], [6,58,36,2,59,37], [4,46,20,6,47,21], [7,42,14,4,43,15],
  [4,133,107], [8,59,37,1,60,38], [8,44,20,4,45,21], [12,33,11,4,34,12],
  [3,145,115,1,146,116], [4,64,40,5,65,41], [11,36,16,5,37,17], [11,36,12,5,37,13],
  [5,109,87,1,110,88], [5,65,41,5,66,42], [5,54,24,7,55,25], [11,36,12,7,37,13],
  [5,122,98,1,123,99], [7,73,45,3,74,46], [15,43,19,2,44,20], [3,45,15,13,46,16],
  [1,135,107,5,136,108], [10,74,46,1,75,47], [1,50,22,15,51,23], [2,42,14,17,43,15],
  [5,150,120,1,151,121], [9,69,43,4,70,44], [17,50,22,1,51,23], [2,42,14,19,43,15],
  [3,141,113,4,142,114], [3,70,44,11,71,45], [17,47,21,4,48,22], [9,39,13,16,40,14],
  [3,135,107,5,136,108], [3,67,41,13,68,42], [15,54,24,5,55,25], [15,43,15,10,44,16],
  [4,144,116,4,145,117], [17,68,42], [17,50,22,6,51,23], [19,46,16,6,47,17],
  [2,139,111,7,140,112], [17,74,46], [7,54,24,16,55,25], [34,37,13],
  [4,151,121,5,152,122], [4,75,47,14,76,48], [11,54,24,14,55,25], [16,45,15,14,46,16],
  [6,147,117,4,148,118], [6,73,45,14,74,46], [11,54,24,16,55,25], [30,46,16,2,47,17],
  [8,132,106,4,133,107], [8,75,47,13,76,48], [7,54,24,22,55,25], [22,45,15,13,46,16],
  [10,142,114,2,143,115], [19,74,46,4,75,47], [28,50,22,6,51,23], [33,46,16,4,47,17],
  [8,152,122,4,153,123], [22,73,45,3,74,46], [8,53,23,26,54,24], [12,45,15,28,46,16],
  [3,147,117,10,148,118], [3,73,45,23,74,46], [4,54,24,31,55,25], [11,45,15,31,46,16],
  [7,146,116,7,147,117], [21,73,45,7,74,46], [1,53,23,37,54,24], [19,45,15,26,46,16],
  [5,145,115,10,146,116], [19,75,47,10,76,48], [15,54,24,25,55,25], [23,45,15,25,46,16],
  [13,145,115,3,146,116], [2,74,46,29,75,47], [42,54,24,1,55,25], [23,45,15,28,46,16],
  [17,145,115], [10,74,46,23,75,47], [10,54,24,35,55,25], [19,45,15,35,46,16],
  [17,145,115,1,146,116], [14,74,46,21,75,47], [29,54,24,19,55,25], [11,45,15,46,46,16],
  [13,145,115,6,146,116], [14,74,46,23,75,47], [44,54,24,7,55,25], [59,46,16,1,47,17],
  [12,151,121,7,152,122], [12,75,47,26,76,48], [39,54,24,14,55,25], [22,45,15,41,46,16],
  [6,151,121,14,152,122], [6,75,47,34,76,48], [46,54,24,10,55,25], [2,45,15,64,46,16],
  [17,152,122,4,153,123], [29,74,46,14,75,47], [49,54,24,10,55,25], [24,45,15,46,46,16],
  [4,152,122,18,153,123], [13,74,46,32,75,47], [48,54,24,14,55,25], [42,45,15,32,46,16],
  [20,147,117,4,148,118], [40,75,47,7,76,48], [43,54,24,22,55,25], [10,45,15,67,46,16],
  [19,148,118,6,149,119], [18,75,47,31,76,48], [34,54,24,34,55,25], [20,45,15,61,46,16],
];

class BitBuffer {
  constructor() {
    this.bytes = [];
    this.length = 0;
  }

  append(value, count) {
    if (!Number.isInteger(value) || value < 0 || count < 0 || count > 31 || value >>> count !== 0)
      throw new RangeError('QR 비트 데이터가 올바르지 않습니다.');
    for (let i = count - 1; i >= 0; i--) {
      const index = this.length >>> 3;
      if (index === this.bytes.length) this.bytes.push(0);
      this.bytes[index] |= ((value >>> i) & 1) << (7 - (this.length & 7));
      this.length++;
    }
  }
}

function blockInfo(version, level) {
  const spec = RS_BLOCKS[(version - 1) * 4 + level.table];
  const blocks = [];
  for (let i = 0; i < spec.length; i += 3) {
    for (let count = 0; count < spec[i]; count++) blocks.push({ total: spec[i + 1], data: spec[i + 2] });
  }
  return blocks;
}

function dataCapacity(version, level) {
  return blockInfo(version, level).reduce((sum, block) => sum + block.data, 0);
}

function chooseVersion(byteLength, level, requested) {
  const start = requested || 1;
  const end = requested || 40;
  for (let version = start; version <= end; version++) {
    const countBits = version < 10 ? 8 : 16;
    if (byteLength < 2 ** countBits && 4 + countBits + byteLength * 8 <= dataCapacity(version, level) * 8)
      return version;
  }
  throw new RangeError(`선택한 오류 복원 레벨의 QR 코드 용량을 초과했습니다 (UTF-8 ${byteLength.toLocaleString()}바이트).`);
}

function gfMultiply(left, right) {
  let result = 0;
  for (let i = 0; i < 8; i++) {
    if (right & 1) result ^= left;
    right >>>= 1;
    left <<= 1;
    if (left & 0x100) left ^= 0x11d;
  }
  return result;
}

const divisors = new Map();
function reedSolomonDivisor(degree) {
  if (divisors.has(degree)) return divisors.get(degree);
  let result = Uint8Array.of(1);
  let root = 1;
  for (let i = 0; i < degree; i++) {
    const next = new Uint8Array(result.length + 1);
    for (let j = 0; j < result.length; j++) {
      next[j] ^= result[j];
      next[j + 1] ^= gfMultiply(result[j], root);
    }
    result = next;
    root = gfMultiply(root, 2);
  }
  divisors.set(degree, result);
  return result;
}

function reedSolomonRemainder(data, degree) {
  const divisor = reedSolomonDivisor(degree);
  const work = new Uint8Array(data.length + degree);
  work.set(data);
  for (let i = 0; i < data.length; i++) {
    const factor = work[i];
    if (!factor) continue;
    for (let j = 0; j < divisor.length; j++) work[i + j] ^= gfMultiply(divisor[j], factor);
  }
  return work.slice(data.length);
}

function makeCodewords(bytes, version, level) {
  const capacity = dataCapacity(version, level) * 8;
  const buffer = new BitBuffer();
  buffer.append(0b0100, 4);
  buffer.append(bytes.length, version < 10 ? 8 : 16);
  for (const byte of bytes) buffer.append(byte, 8);
  buffer.append(0, Math.min(4, capacity - buffer.length));
  buffer.append(0, (8 - buffer.length % 8) % 8);
  for (let pad = 0; buffer.length < capacity; pad++) buffer.append(pad % 2 ? 0x11 : 0xec, 8);

  const blocks = blockInfo(version, level);
  const dataBlocks = [];
  const errorBlocks = [];
  let offset = 0;
  for (const block of blocks) {
    const data = Uint8Array.from(buffer.bytes.slice(offset, offset + block.data));
    offset += block.data;
    dataBlocks.push(data);
    errorBlocks.push(reedSolomonRemainder(data, block.total - block.data));
  }

  const result = [];
  const maxData = Math.max(...dataBlocks.map((block) => block.length));
  for (let i = 0; i < maxData; i++) {
    for (const block of dataBlocks) if (i < block.length) result.push(block[i]);
  }
  const errorLength = errorBlocks[0].length;
  for (let i = 0; i < errorLength; i++) for (const block of errorBlocks) result.push(block[i]);
  return Uint8Array.from(result);
}

function setFunction(matrix, functions, row, column, dark) {
  matrix[row][column] = dark ? 1 : 0;
  functions[row][column] = 1;
}

function drawFinder(matrix, functions, centerRow, centerColumn) {
  const size = matrix.length;
  for (let rowOffset = -4; rowOffset <= 4; rowOffset++) {
    for (let columnOffset = -4; columnOffset <= 4; columnOffset++) {
      const row = centerRow + rowOffset;
      const column = centerColumn + columnOffset;
      if (row < 0 || row >= size || column < 0 || column >= size) continue;
      const distance = Math.max(Math.abs(rowOffset), Math.abs(columnOffset));
      setFunction(matrix, functions, row, column, distance !== 2 && distance !== 4);
    }
  }
}

export function qrAlignmentPositions(version) {
  if (version === 1) return [];
  const size = version * 4 + 17;
  const count = Math.floor(version / 7) + 2;
  const step = version === 32 ? 26 : Math.floor((version * 4 + count * 2 + 1) / (count * 2 - 2)) * 2;
  const result = [6];
  for (let position = size - 7; result.length < count; position -= step) result.splice(1, 0, position);
  return result;
}

function drawAlignment(matrix, functions, centerRow, centerColumn) {
  for (let rowOffset = -2; rowOffset <= 2; rowOffset++) {
    for (let columnOffset = -2; columnOffset <= 2; columnOffset++) {
      const distance = Math.max(Math.abs(rowOffset), Math.abs(columnOffset));
      setFunction(matrix, functions, centerRow + rowOffset, centerColumn + columnOffset, distance !== 1);
    }
  }
}

function bchRemainder(value, polynomial, steps) {
  let remainder = value;
  for (let i = 0; i < steps; i++) remainder = (remainder << 1) ^ ((remainder >>> (steps - 1)) * polynomial);
  return remainder;
}

function drawFormat(matrix, functions, level, mask) {
  const size = matrix.length;
  const data = (level.format << 3) | mask;
  const bits = ((data << 10) | bchRemainder(data, 0x537, 10)) ^ 0x5412;
  const bit = (index) => ((bits >>> index) & 1) !== 0;
  for (let i = 0; i <= 5; i++) setFunction(matrix, functions, i, 8, bit(i));
  setFunction(matrix, functions, 7, 8, bit(6));
  setFunction(matrix, functions, 8, 8, bit(7));
  setFunction(matrix, functions, 8, 7, bit(8));
  for (let i = 9; i < 15; i++) setFunction(matrix, functions, 8, 14 - i, bit(i));
  for (let i = 0; i < 8; i++) setFunction(matrix, functions, 8, size - 1 - i, bit(i));
  for (let i = 8; i < 15; i++) setFunction(matrix, functions, size - 15 + i, 8, bit(i));
  setFunction(matrix, functions, size - 8, 8, true);
}

function drawVersion(matrix, functions, version) {
  if (version < 7) return;
  const size = matrix.length;
  const bits = (version << 12) | bchRemainder(version, 0x1f25, 12);
  for (let i = 0; i < 18; i++) {
    const dark = ((bits >>> i) & 1) !== 0;
    const a = size - 11 + i % 3;
    const b = Math.floor(i / 3);
    setFunction(matrix, functions, b, a, dark);
    setFunction(matrix, functions, a, b, dark);
  }
}

function baseMatrix(version, level) {
  const size = version * 4 + 17;
  const matrix = Array.from({ length: size }, () => new Uint8Array(size));
  const functions = Array.from({ length: size }, () => new Uint8Array(size));
  drawFinder(matrix, functions, 3, 3);
  drawFinder(matrix, functions, 3, size - 4);
  drawFinder(matrix, functions, size - 4, 3);
  const positions = qrAlignmentPositions(version);
  for (const row of positions) for (const column of positions) {
    if (!functions[row][column]) drawAlignment(matrix, functions, row, column);
  }
  for (let i = 8; i < size - 8; i++) {
    if (!functions[6][i]) setFunction(matrix, functions, 6, i, i % 2 === 0);
    if (!functions[i][6]) setFunction(matrix, functions, i, 6, i % 2 === 0);
  }
  drawFormat(matrix, functions, level, 0);
  drawVersion(matrix, functions, version);
  return { matrix, functions };
}

export function qrMaskBit(mask, row, column) {
  const product = row * column;
  if (mask === 0) return (row + column) % 2 === 0;
  if (mask === 1) return row % 2 === 0;
  if (mask === 2) return column % 3 === 0;
  if (mask === 3) return (row + column) % 3 === 0;
  if (mask === 4) return (Math.floor(row / 2) + Math.floor(column / 3)) % 2 === 0;
  if (mask === 5) return product % 2 + product % 3 === 0;
  if (mask === 6) return (product % 2 + product % 3) % 2 === 0;
  return ((row + column) % 2 + product % 3) % 2 === 0;
}

function fillData(base, functions, codewords, level, mask) {
  const matrix = base.map((row) => row.slice());
  const size = matrix.length;
  let bitIndex = 0;
  let upward = true;
  for (let right = size - 1; right >= 1; right -= 2) {
    if (right === 6) right--;
    for (let vertical = 0; vertical < size; vertical++) {
      const row = upward ? size - 1 - vertical : vertical;
      for (let offset = 0; offset < 2; offset++) {
        const column = right - offset;
        if (functions[row][column]) continue;
        const dark = bitIndex < codewords.length * 8
          && ((codewords[bitIndex >>> 3] >>> (7 - (bitIndex & 7))) & 1) !== 0;
        matrix[row][column] = dark !== qrMaskBit(mask, row, column) ? 1 : 0;
        bitIndex++;
      }
    }
    upward = !upward;
  }
  drawFormat(matrix, functions, level, mask);
  return matrix;
}

function sameRunPenalty(line) {
  let penalty = 0;
  let runColor = line[0];
  let runLength = 1;
  for (let i = 1; i < line.length; i++) {
    if (line[i] === runColor) runLength++;
    else {
      if (runLength >= 5) penalty += runLength - 2;
      runColor = line[i];
      runLength = 1;
    }
  }
  if (runLength >= 5) penalty += runLength - 2;
  return penalty;
}

function finderPenalty(line) {
  let penalty = 0;
  for (let i = 0; i <= line.length - 7; i++) {
    if (!(line[i] && !line[i + 1] && line[i + 2] && line[i + 3]
      && line[i + 4] && !line[i + 5] && line[i + 6])) continue;
    if (i >= 4 && !line[i - 1] && !line[i - 2] && !line[i - 3] && !line[i - 4]) penalty += 40;
    if (i + 11 <= line.length && !line[i + 7] && !line[i + 8] && !line[i + 9] && !line[i + 10]) penalty += 40;
  }
  return penalty;
}

function penaltyScore(matrix) {
  const size = matrix.length;
  let penalty = 0;
  let dark = 0;
  for (let row = 0; row < size; row++) {
    penalty += sameRunPenalty(matrix[row]) + finderPenalty(matrix[row]);
    const column = new Uint8Array(size);
    for (let i = 0; i < size; i++) {
      column[i] = matrix[i][row];
      dark += matrix[row][i];
    }
    penalty += sameRunPenalty(column) + finderPenalty(column);
  }
  for (let row = 0; row < size - 1; row++) {
    for (let column = 0; column < size - 1; column++) {
      const value = matrix[row][column];
      if (matrix[row][column + 1] === value && matrix[row + 1][column] === value
        && matrix[row + 1][column + 1] === value) penalty += 3;
    }
  }
  penalty += Math.floor(Math.abs(dark * 20 - size * size * 10) / (size * size)) * 10;
  return penalty;
}

function normalizeBytes(input) {
  if (input instanceof Uint8Array) return input;
  if (input instanceof ArrayBuffer) return new Uint8Array(input);
  if (ArrayBuffer.isView(input)) return new Uint8Array(input.buffer, input.byteOffset, input.byteLength);
  throw new TypeError('QR 입력은 바이트 배열이어야 합니다.');
}

export function encodeQrBytes(input, options = {}) {
  const bytes = normalizeBytes(input);
  const levelName = String(options.level || 'M').toUpperCase();
  const level = LEVELS[levelName];
  if (!level) throw new RangeError('QR 오류 복원 레벨은 L, M, Q, H 중 하나여야 합니다.');
  const requestedVersion = options.version == null ? 0 : Number(options.version);
  if (!Number.isInteger(requestedVersion) || requestedVersion < 0 || requestedVersion > 40)
    throw new RangeError('QR 버전은 자동(0) 또는 1~40이어야 합니다.');
  const requestedMask = options.mask == null ? -1 : Number(options.mask);
  if (!Number.isInteger(requestedMask) || requestedMask < -1 || requestedMask > 7)
    throw new RangeError('QR 마스크는 자동(-1) 또는 0~7이어야 합니다.');

  const version = chooseVersion(bytes.length, level, requestedVersion);
  const codewords = makeCodewords(bytes, version, level);
  const { matrix: base, functions } = baseMatrix(version, level);
  let selectedMask = requestedMask;
  let modules;
  if (selectedMask >= 0) modules = fillData(base, functions, codewords, level, selectedMask);
  else {
    let bestPenalty = Infinity;
    for (let mask = 0; mask < 8; mask++) {
      const candidate = fillData(base, functions, codewords, level, mask);
      const penalty = penaltyScore(candidate);
      if (penalty < bestPenalty) {
        modules = candidate;
        selectedMask = mask;
        bestPenalty = penalty;
      }
    }
  }
  return { modules, size: modules.length, version, mask: selectedMask, level: levelName };
}

export function encodeQr(text, options = {}) {
  if (typeof text !== 'string') throw new TypeError('QR 입력은 문자열이어야 합니다.');
  return encodeQrBytes(new TextEncoder().encode(text), options);
}

// QR specification data shared with the decoder. Callers may safely mutate the returned values.
export function qrBlockInfo(version, levelName) {
  const level = LEVELS[String(levelName).toUpperCase()];
  if (!Number.isInteger(version) || version < 1 || version > 40 || !level)
    throw new RangeError('QR 버전 또는 오류 복원 레벨이 올바르지 않습니다.');
  return blockInfo(version, level).map((block) => ({ ...block }));
}

export function qrFunctionModules(version) {
  if (!Number.isInteger(version) || version < 1 || version > 40)
    throw new RangeError('QR 버전은 1~40이어야 합니다.');
  return baseMatrix(version, LEVELS.M).functions.map((row) => row.slice());
}
