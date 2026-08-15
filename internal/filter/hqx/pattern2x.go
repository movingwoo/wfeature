// Copyright (c) 2017 Christopher Serr. Licensed under MIT OR Apache-2.0.
// This file is a mechanical translation of that work's decision table; see the
// package comment and THIRD-PARTY-NOTICES.md.

package hqx

// Generated from the reference implementation's decision table, one case per
// pattern of differing neighbours. See the package comment: all 256 patterns
// appear exactly once, which is the check that the translation is complete.

func hq2xPattern(dst []uint32, dstIndex, dstRowElements int, w *[10]uint32, pattern int) {
	switch pattern {
	case 0, 1, 4, 32, 128, 5, 132, 160, 33, 129, 36, 133, 164, 161, 37, 165:
		dst[dstIndex] = interp2(w[5], w[4], w[2])
		dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
	case 2, 34, 130, 162:
		dst[dstIndex] = interp2(w[5], w[1], w[4])
		dst[dstIndex+1] = interp2(w[5], w[3], w[6])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
	case 16, 17, 48, 49:
		dst[dstIndex] = interp2(w[5], w[4], w[2])
		dst[dstIndex+1] = interp2(w[5], w[3], w[2])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8])
	case 64, 65, 68, 69:
		dst[dstIndex] = interp2(w[5], w[4], w[2])
		dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6])
	case 8, 12, 136, 140:
		dst[dstIndex] = interp2(w[5], w[1], w[2])
		dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
	case 3, 35, 131, 163:
		dst[dstIndex] = interp1(w[5], w[4])
		dst[dstIndex+1] = interp2(w[5], w[3], w[6])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
	case 6, 38, 134, 166:
		dst[dstIndex] = interp2(w[5], w[1], w[4])
		dst[dstIndex+1] = interp1(w[5], w[6])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
	case 20, 21, 52, 53:
		dst[dstIndex] = interp2(w[5], w[4], w[2])
		dst[dstIndex+1] = interp1(w[5], w[2])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8])
	case 144, 145, 176, 177:
		dst[dstIndex] = interp2(w[5], w[4], w[2])
		dst[dstIndex+1] = interp2(w[5], w[3], w[2])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8])
	case 192, 193, 196, 197:
		dst[dstIndex] = interp2(w[5], w[4], w[2])
		dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6])
	case 96, 97, 100, 101:
		dst[dstIndex] = interp2(w[5], w[4], w[2])
		dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6])
	case 40, 44, 168, 172:
		dst[dstIndex] = interp2(w[5], w[1], w[2])
		dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
	case 9, 13, 137, 141:
		dst[dstIndex] = interp1(w[5], w[2])
		dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
	case 18, 50:
		dst[dstIndex] = interp2(w[5], w[1], w[4])
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = interp1(w[5], w[3])
		} else {
			dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8])
	case 80, 81:
		dst[dstIndex] = interp2(w[5], w[4], w[2])
		dst[dstIndex+1] = interp2(w[5], w[3], w[2])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4])
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
		}
	case 72, 76:
		dst[dstIndex] = interp2(w[5], w[1], w[2])
		dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
		} else {
			dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		}
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6])
	case 10, 138:
		if diff(w[4], w[2]) {
			dst[dstIndex] = interp1(w[5], w[1])
		} else {
			dst[dstIndex] = interp2(w[5], w[4], w[2])
		}
		dst[dstIndex+1] = interp2(w[5], w[3], w[6])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
	case 66:
		dst[dstIndex] = interp2(w[5], w[1], w[4])
		dst[dstIndex+1] = interp2(w[5], w[3], w[6])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6])
	case 24:
		dst[dstIndex] = interp2(w[5], w[1], w[2])
		dst[dstIndex+1] = interp2(w[5], w[3], w[2])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8])
	case 7, 39, 135:
		dst[dstIndex] = interp1(w[5], w[4])
		dst[dstIndex+1] = interp1(w[5], w[6])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
	case 148, 149, 180:
		dst[dstIndex] = interp2(w[5], w[4], w[2])
		dst[dstIndex+1] = interp1(w[5], w[2])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8])
	case 224, 228, 225:
		dst[dstIndex] = interp2(w[5], w[4], w[2])
		dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6])
	case 41, 169, 45:
		dst[dstIndex] = interp1(w[5], w[2])
		dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
	case 22, 54:
		dst[dstIndex] = interp2(w[5], w[1], w[4])
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8])
	case 208, 209:
		dst[dstIndex] = interp2(w[5], w[4], w[2])
		dst[dstIndex+1] = interp2(w[5], w[3], w[2])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4])
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
		}
	case 104, 108:
		dst[dstIndex] = interp2(w[5], w[1], w[2])
		dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		}
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6])
	case 11, 139:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
		} else {
			dst[dstIndex] = interp2(w[5], w[4], w[2])
		}
		dst[dstIndex+1] = interp2(w[5], w[3], w[6])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
	case 19, 51:
		if diff(w[2], w[6]) {
			dst[dstIndex] = interp1(w[5], w[4])
			dst[dstIndex+1] = interp1(w[5], w[3])
		} else {
			dst[dstIndex] = interp6(w[5], w[2], w[4])
			dst[dstIndex+1] = interp9(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8])
	case 146, 178:
		dst[dstIndex] = interp2(w[5], w[1], w[4])
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = interp1(w[5], w[3])
			dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8])
		} else {
			dst[dstIndex+1] = interp9(w[5], w[2], w[6])
			dst[(dstIndex + dstRowElements + 1)] = interp6(w[5], w[6], w[8])
		}
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
	case 84, 85:
		dst[dstIndex] = interp2(w[5], w[4], w[2])
		if diff(w[6], w[8]) {
			dst[dstIndex+1] = interp1(w[5], w[2])
			dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
		} else {
			dst[dstIndex+1] = interp6(w[5], w[6], w[2])
			dst[(dstIndex + dstRowElements + 1)] = interp9(w[5], w[6], w[8])
		}
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4])
	case 112, 113:
		dst[dstIndex] = interp2(w[5], w[4], w[2])
		dst[dstIndex+1] = interp2(w[5], w[3], w[2])
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4])
			dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
		} else {
			dst[(dstIndex + dstRowElements)] = interp6(w[5], w[8], w[4])
			dst[(dstIndex + dstRowElements + 1)] = interp9(w[5], w[6], w[8])
		}
	case 200, 204:
		dst[dstIndex] = interp2(w[5], w[1], w[2])
		dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
			dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6])
		} else {
			dst[(dstIndex + dstRowElements)] = interp9(w[5], w[8], w[4])
			dst[(dstIndex + dstRowElements + 1)] = interp6(w[5], w[8], w[6])
		}
	case 73, 77:
		if diff(w[8], w[4]) {
			dst[dstIndex] = interp1(w[5], w[2])
			dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
		} else {
			dst[dstIndex] = interp6(w[5], w[4], w[2])
			dst[(dstIndex + dstRowElements)] = interp9(w[5], w[8], w[4])
		}
		dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6])
	case 42, 170:
		if diff(w[4], w[2]) {
			dst[dstIndex] = interp1(w[5], w[1])
			dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8])
		} else {
			dst[dstIndex] = interp9(w[5], w[4], w[2])
			dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[8])
		}
		dst[dstIndex+1] = interp2(w[5], w[3], w[6])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
	case 14, 142:
		if diff(w[4], w[2]) {
			dst[dstIndex] = interp1(w[5], w[1])
			dst[dstIndex+1] = interp1(w[5], w[6])
		} else {
			dst[dstIndex] = interp9(w[5], w[4], w[2])
			dst[dstIndex+1] = interp6(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
	case 67:
		dst[dstIndex] = interp1(w[5], w[4])
		dst[dstIndex+1] = interp2(w[5], w[3], w[6])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6])
	case 70:
		dst[dstIndex] = interp2(w[5], w[1], w[4])
		dst[dstIndex+1] = interp1(w[5], w[6])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6])
	case 28:
		dst[dstIndex] = interp2(w[5], w[1], w[2])
		dst[dstIndex+1] = interp1(w[5], w[2])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8])
	case 152:
		dst[dstIndex] = interp2(w[5], w[1], w[2])
		dst[dstIndex+1] = interp2(w[5], w[3], w[2])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8])
	case 194:
		dst[dstIndex] = interp2(w[5], w[1], w[4])
		dst[dstIndex+1] = interp2(w[5], w[3], w[6])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6])
	case 98:
		dst[dstIndex] = interp2(w[5], w[1], w[4])
		dst[dstIndex+1] = interp2(w[5], w[3], w[6])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6])
	case 56:
		dst[dstIndex] = interp2(w[5], w[1], w[2])
		dst[dstIndex+1] = interp2(w[5], w[3], w[2])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8])
	case 25:
		dst[dstIndex] = interp1(w[5], w[2])
		dst[dstIndex+1] = interp2(w[5], w[3], w[2])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8])
	case 26, 31:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
		} else {
			dst[dstIndex] = interp2(w[5], w[4], w[2])
		}
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8])
	case 82, 214:
		dst[dstIndex] = interp2(w[5], w[1], w[4])
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4])
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
		}
	case 88, 248:
		dst[dstIndex] = interp2(w[5], w[1], w[2])
		dst[dstIndex+1] = interp2(w[5], w[3], w[2])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		}
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
		}
	case 74, 107:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
		} else {
			dst[dstIndex] = interp2(w[5], w[4], w[2])
		}
		dst[dstIndex+1] = interp2(w[5], w[3], w[6])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		}
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6])
	case 27:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
		} else {
			dst[dstIndex] = interp2(w[5], w[4], w[2])
		}
		dst[dstIndex+1] = interp1(w[5], w[3])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8])
	case 86:
		dst[dstIndex] = interp2(w[5], w[1], w[4])
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
	case 216:
		dst[dstIndex] = interp2(w[5], w[1], w[2])
		dst[dstIndex+1] = interp2(w[5], w[3], w[2])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
		}
	case 106:
		dst[dstIndex] = interp1(w[5], w[1])
		dst[dstIndex+1] = interp2(w[5], w[3], w[6])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		}
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6])
	case 30:
		dst[dstIndex] = interp1(w[5], w[1])
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8])
	case 210:
		dst[dstIndex] = interp2(w[5], w[1], w[4])
		dst[dstIndex+1] = interp1(w[5], w[3])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4])
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
		}
	case 120:
		dst[dstIndex] = interp2(w[5], w[1], w[2])
		dst[dstIndex+1] = interp2(w[5], w[3], w[2])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		}
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
	case 75:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
		} else {
			dst[dstIndex] = interp2(w[5], w[4], w[2])
		}
		dst[dstIndex+1] = interp2(w[5], w[3], w[6])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6])
	case 29:
		dst[dstIndex] = interp1(w[5], w[2])
		dst[dstIndex+1] = interp1(w[5], w[2])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8])
	case 198:
		dst[dstIndex] = interp2(w[5], w[1], w[4])
		dst[dstIndex+1] = interp1(w[5], w[6])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6])
	case 184:
		dst[dstIndex] = interp2(w[5], w[1], w[2])
		dst[dstIndex+1] = interp2(w[5], w[3], w[2])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8])
	case 99:
		dst[dstIndex] = interp1(w[5], w[4])
		dst[dstIndex+1] = interp2(w[5], w[3], w[6])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6])
	case 57:
		dst[dstIndex] = interp1(w[5], w[2])
		dst[dstIndex+1] = interp2(w[5], w[3], w[2])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8])
	case 71:
		dst[dstIndex] = interp1(w[5], w[4])
		dst[dstIndex+1] = interp1(w[5], w[6])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6])
	case 156:
		dst[dstIndex] = interp2(w[5], w[1], w[2])
		dst[dstIndex+1] = interp1(w[5], w[2])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8])
	case 226:
		dst[dstIndex] = interp2(w[5], w[1], w[4])
		dst[dstIndex+1] = interp2(w[5], w[3], w[6])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6])
	case 60:
		dst[dstIndex] = interp2(w[5], w[1], w[2])
		dst[dstIndex+1] = interp1(w[5], w[2])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8])
	case 195:
		dst[dstIndex] = interp1(w[5], w[4])
		dst[dstIndex+1] = interp2(w[5], w[3], w[6])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6])
	case 102:
		dst[dstIndex] = interp2(w[5], w[1], w[4])
		dst[dstIndex+1] = interp1(w[5], w[6])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6])
	case 153:
		dst[dstIndex] = interp1(w[5], w[2])
		dst[dstIndex+1] = interp2(w[5], w[3], w[2])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8])
	case 58:
		if diff(w[4], w[2]) {
			dst[dstIndex] = interp1(w[5], w[1])
		} else {
			dst[dstIndex] = interp7(w[5], w[4], w[2])
		}
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = interp1(w[5], w[3])
		} else {
			dst[dstIndex+1] = interp7(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8])
	case 83:
		dst[dstIndex] = interp1(w[5], w[4])
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = interp1(w[5], w[3])
		} else {
			dst[dstIndex+1] = interp7(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4])
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8])
		}
	case 92:
		dst[dstIndex] = interp2(w[5], w[1], w[2])
		dst[dstIndex+1] = interp1(w[5], w[2])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
		} else {
			dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4])
		}
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8])
		}
	case 202:
		if diff(w[4], w[2]) {
			dst[dstIndex] = interp1(w[5], w[1])
		} else {
			dst[dstIndex] = interp7(w[5], w[4], w[2])
		}
		dst[dstIndex+1] = interp2(w[5], w[3], w[6])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
		} else {
			dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4])
		}
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6])
	case 78:
		if diff(w[4], w[2]) {
			dst[dstIndex] = interp1(w[5], w[1])
		} else {
			dst[dstIndex] = interp7(w[5], w[4], w[2])
		}
		dst[dstIndex+1] = interp1(w[5], w[6])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
		} else {
			dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4])
		}
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6])
	case 154:
		if diff(w[4], w[2]) {
			dst[dstIndex] = interp1(w[5], w[1])
		} else {
			dst[dstIndex] = interp7(w[5], w[4], w[2])
		}
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = interp1(w[5], w[3])
		} else {
			dst[dstIndex+1] = interp7(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8])
	case 114:
		dst[dstIndex] = interp2(w[5], w[1], w[4])
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = interp1(w[5], w[3])
		} else {
			dst[dstIndex+1] = interp7(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4])
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8])
		}
	case 89:
		dst[dstIndex] = interp1(w[5], w[2])
		dst[dstIndex+1] = interp2(w[5], w[3], w[2])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
		} else {
			dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4])
		}
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8])
		}
	case 90:
		if diff(w[4], w[2]) {
			dst[dstIndex] = interp1(w[5], w[1])
		} else {
			dst[dstIndex] = interp7(w[5], w[4], w[2])
		}
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = interp1(w[5], w[3])
		} else {
			dst[dstIndex+1] = interp7(w[5], w[2], w[6])
		}
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
		} else {
			dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4])
		}
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8])
		}
	case 55, 23:
		if diff(w[2], w[6]) {
			dst[dstIndex] = interp1(w[5], w[4])
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex] = interp6(w[5], w[2], w[4])
			dst[dstIndex+1] = interp9(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8])
	case 182, 150:
		dst[dstIndex] = interp2(w[5], w[1], w[4])
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
			dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8])
		} else {
			dst[dstIndex+1] = interp9(w[5], w[2], w[6])
			dst[(dstIndex + dstRowElements + 1)] = interp6(w[5], w[6], w[8])
		}
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
	case 213, 212:
		dst[dstIndex] = interp2(w[5], w[4], w[2])
		if diff(w[6], w[8]) {
			dst[dstIndex+1] = interp1(w[5], w[2])
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[dstIndex+1] = interp6(w[5], w[6], w[2])
			dst[(dstIndex + dstRowElements + 1)] = interp9(w[5], w[6], w[8])
		}
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4])
	case 241, 240:
		dst[dstIndex] = interp2(w[5], w[4], w[2])
		dst[dstIndex+1] = interp2(w[5], w[3], w[2])
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4])
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp6(w[5], w[8], w[4])
			dst[(dstIndex + dstRowElements + 1)] = interp9(w[5], w[6], w[8])
		}
	case 236, 232:
		dst[dstIndex] = interp2(w[5], w[1], w[2])
		dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
			dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6])
		} else {
			dst[(dstIndex + dstRowElements)] = interp9(w[5], w[8], w[4])
			dst[(dstIndex + dstRowElements + 1)] = interp6(w[5], w[8], w[6])
		}
	case 109, 105:
		if diff(w[8], w[4]) {
			dst[dstIndex] = interp1(w[5], w[2])
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[dstIndex] = interp6(w[5], w[4], w[2])
			dst[(dstIndex + dstRowElements)] = interp9(w[5], w[8], w[4])
		}
		dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6])
	case 171, 43:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
			dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8])
		} else {
			dst[dstIndex] = interp9(w[5], w[4], w[2])
			dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[8])
		}
		dst[dstIndex+1] = interp2(w[5], w[3], w[6])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
	case 143, 15:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
			dst[dstIndex+1] = interp1(w[5], w[6])
		} else {
			dst[dstIndex] = interp9(w[5], w[4], w[2])
			dst[dstIndex+1] = interp6(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
	case 124:
		dst[dstIndex] = interp2(w[5], w[1], w[2])
		dst[dstIndex+1] = interp1(w[5], w[2])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		}
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
	case 203:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
		} else {
			dst[dstIndex] = interp2(w[5], w[4], w[2])
		}
		dst[dstIndex+1] = interp2(w[5], w[3], w[6])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6])
	case 62:
		dst[dstIndex] = interp1(w[5], w[1])
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8])
	case 211:
		dst[dstIndex] = interp1(w[5], w[4])
		dst[dstIndex+1] = interp1(w[5], w[3])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4])
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
		}
	case 118:
		dst[dstIndex] = interp2(w[5], w[1], w[4])
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
	case 217:
		dst[dstIndex] = interp1(w[5], w[2])
		dst[dstIndex+1] = interp2(w[5], w[3], w[2])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
		}
	case 110:
		dst[dstIndex] = interp1(w[5], w[1])
		dst[dstIndex+1] = interp1(w[5], w[6])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		}
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6])
	case 155:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
		} else {
			dst[dstIndex] = interp2(w[5], w[4], w[2])
		}
		dst[dstIndex+1] = interp1(w[5], w[3])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8])
	case 188:
		dst[dstIndex] = interp2(w[5], w[1], w[2])
		dst[dstIndex+1] = interp1(w[5], w[2])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8])
	case 185:
		dst[dstIndex] = interp1(w[5], w[2])
		dst[dstIndex+1] = interp2(w[5], w[3], w[2])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8])
	case 61:
		dst[dstIndex] = interp1(w[5], w[2])
		dst[dstIndex+1] = interp1(w[5], w[2])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8])
	case 157:
		dst[dstIndex] = interp1(w[5], w[2])
		dst[dstIndex+1] = interp1(w[5], w[2])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8])
	case 103:
		dst[dstIndex] = interp1(w[5], w[4])
		dst[dstIndex+1] = interp1(w[5], w[6])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6])
	case 227:
		dst[dstIndex] = interp1(w[5], w[4])
		dst[dstIndex+1] = interp2(w[5], w[3], w[6])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6])
	case 230:
		dst[dstIndex] = interp2(w[5], w[1], w[4])
		dst[dstIndex+1] = interp1(w[5], w[6])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6])
	case 199:
		dst[dstIndex] = interp1(w[5], w[4])
		dst[dstIndex+1] = interp1(w[5], w[6])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6])
	case 220:
		dst[dstIndex] = interp2(w[5], w[1], w[2])
		dst[dstIndex+1] = interp1(w[5], w[2])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
		} else {
			dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4])
		}
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
		}
	case 158:
		if diff(w[4], w[2]) {
			dst[dstIndex] = interp1(w[5], w[1])
		} else {
			dst[dstIndex] = interp7(w[5], w[4], w[2])
		}
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8])
	case 234:
		if diff(w[4], w[2]) {
			dst[dstIndex] = interp1(w[5], w[1])
		} else {
			dst[dstIndex] = interp7(w[5], w[4], w[2])
		}
		dst[dstIndex+1] = interp2(w[5], w[3], w[6])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		}
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6])
	case 242:
		dst[dstIndex] = interp2(w[5], w[1], w[4])
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = interp1(w[5], w[3])
		} else {
			dst[dstIndex+1] = interp7(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4])
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
		}
	case 59:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
		} else {
			dst[dstIndex] = interp2(w[5], w[4], w[2])
		}
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = interp1(w[5], w[3])
		} else {
			dst[dstIndex+1] = interp7(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8])
	case 121:
		dst[dstIndex] = interp1(w[5], w[2])
		dst[dstIndex+1] = interp2(w[5], w[3], w[2])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		}
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8])
		}
	case 87:
		dst[dstIndex] = interp1(w[5], w[4])
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4])
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8])
		}
	case 79:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
		} else {
			dst[dstIndex] = interp2(w[5], w[4], w[2])
		}
		dst[dstIndex+1] = interp1(w[5], w[6])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
		} else {
			dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4])
		}
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6])
	case 122:
		if diff(w[4], w[2]) {
			dst[dstIndex] = interp1(w[5], w[1])
		} else {
			dst[dstIndex] = interp7(w[5], w[4], w[2])
		}
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = interp1(w[5], w[3])
		} else {
			dst[dstIndex+1] = interp7(w[5], w[2], w[6])
		}
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		}
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8])
		}
	case 94:
		if diff(w[4], w[2]) {
			dst[dstIndex] = interp1(w[5], w[1])
		} else {
			dst[dstIndex] = interp7(w[5], w[4], w[2])
		}
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		}
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
		} else {
			dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4])
		}
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8])
		}
	case 218:
		if diff(w[4], w[2]) {
			dst[dstIndex] = interp1(w[5], w[1])
		} else {
			dst[dstIndex] = interp7(w[5], w[4], w[2])
		}
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = interp1(w[5], w[3])
		} else {
			dst[dstIndex+1] = interp7(w[5], w[2], w[6])
		}
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
		} else {
			dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4])
		}
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
		}
	case 91:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
		} else {
			dst[dstIndex] = interp2(w[5], w[4], w[2])
		}
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = interp1(w[5], w[3])
		} else {
			dst[dstIndex+1] = interp7(w[5], w[2], w[6])
		}
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
		} else {
			dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4])
		}
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8])
		}
	case 229:
		dst[dstIndex] = interp2(w[5], w[4], w[2])
		dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6])
	case 167:
		dst[dstIndex] = interp1(w[5], w[4])
		dst[dstIndex+1] = interp1(w[5], w[6])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
	case 173:
		dst[dstIndex] = interp1(w[5], w[2])
		dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
	case 181:
		dst[dstIndex] = interp2(w[5], w[4], w[2])
		dst[dstIndex+1] = interp1(w[5], w[2])
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8])
	case 186:
		if diff(w[4], w[2]) {
			dst[dstIndex] = interp1(w[5], w[1])
		} else {
			dst[dstIndex] = interp7(w[5], w[4], w[2])
		}
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = interp1(w[5], w[3])
		} else {
			dst[dstIndex+1] = interp7(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8])
	case 115:
		dst[dstIndex] = interp1(w[5], w[4])
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = interp1(w[5], w[3])
		} else {
			dst[dstIndex+1] = interp7(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4])
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8])
		}
	case 93:
		dst[dstIndex] = interp1(w[5], w[2])
		dst[dstIndex+1] = interp1(w[5], w[2])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
		} else {
			dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4])
		}
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8])
		}
	case 206:
		if diff(w[4], w[2]) {
			dst[dstIndex] = interp1(w[5], w[1])
		} else {
			dst[dstIndex] = interp7(w[5], w[4], w[2])
		}
		dst[dstIndex+1] = interp1(w[5], w[6])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
		} else {
			dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4])
		}
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6])
	case 205, 201:
		dst[dstIndex] = interp1(w[5], w[2])
		dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
		} else {
			dst[(dstIndex + dstRowElements)] = interp7(w[5], w[8], w[4])
		}
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6])
	case 174, 46:
		if diff(w[4], w[2]) {
			dst[dstIndex] = interp1(w[5], w[1])
		} else {
			dst[dstIndex] = interp7(w[5], w[4], w[2])
		}
		dst[dstIndex+1] = interp1(w[5], w[6])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
	case 179, 147:
		dst[dstIndex] = interp1(w[5], w[4])
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = interp1(w[5], w[3])
		} else {
			dst[dstIndex+1] = interp7(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8])
	case 117, 116:
		dst[dstIndex] = interp2(w[5], w[4], w[2])
		dst[dstIndex+1] = interp1(w[5], w[2])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4])
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp7(w[5], w[6], w[8])
		}
	case 189:
		dst[dstIndex] = interp1(w[5], w[2])
		dst[dstIndex+1] = interp1(w[5], w[2])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8])
	case 231:
		dst[dstIndex] = interp1(w[5], w[4])
		dst[dstIndex+1] = interp1(w[5], w[6])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6])
	case 126:
		dst[dstIndex] = interp1(w[5], w[1])
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		}
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		}
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
	case 219:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
		} else {
			dst[dstIndex] = interp2(w[5], w[4], w[2])
		}
		dst[dstIndex+1] = interp1(w[5], w[3])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
		}
	case 125:
		if diff(w[8], w[4]) {
			dst[dstIndex] = interp1(w[5], w[2])
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[dstIndex] = interp6(w[5], w[4], w[2])
			dst[(dstIndex + dstRowElements)] = interp9(w[5], w[8], w[4])
		}
		dst[dstIndex+1] = interp1(w[5], w[2])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
	case 221:
		dst[dstIndex] = interp1(w[5], w[2])
		if diff(w[6], w[8]) {
			dst[dstIndex+1] = interp1(w[5], w[2])
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[dstIndex+1] = interp6(w[5], w[6], w[2])
			dst[(dstIndex + dstRowElements + 1)] = interp9(w[5], w[6], w[8])
		}
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
	case 207:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
			dst[dstIndex+1] = interp1(w[5], w[6])
		} else {
			dst[dstIndex] = interp9(w[5], w[4], w[2])
			dst[dstIndex+1] = interp6(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6])
	case 238:
		dst[dstIndex] = interp1(w[5], w[1])
		dst[dstIndex+1] = interp1(w[5], w[6])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
			dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6])
		} else {
			dst[(dstIndex + dstRowElements)] = interp9(w[5], w[8], w[4])
			dst[(dstIndex + dstRowElements + 1)] = interp6(w[5], w[8], w[6])
		}
	case 190:
		dst[dstIndex] = interp1(w[5], w[1])
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
			dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8])
		} else {
			dst[dstIndex+1] = interp9(w[5], w[2], w[6])
			dst[(dstIndex + dstRowElements + 1)] = interp6(w[5], w[6], w[8])
		}
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8])
	case 187:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
			dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8])
		} else {
			dst[dstIndex] = interp9(w[5], w[4], w[2])
			dst[(dstIndex + dstRowElements)] = interp6(w[5], w[4], w[8])
		}
		dst[dstIndex+1] = interp1(w[5], w[3])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8])
	case 243:
		dst[dstIndex] = interp1(w[5], w[4])
		dst[dstIndex+1] = interp1(w[5], w[3])
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4])
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp6(w[5], w[8], w[4])
			dst[(dstIndex + dstRowElements + 1)] = interp9(w[5], w[6], w[8])
		}
	case 119:
		if diff(w[2], w[6]) {
			dst[dstIndex] = interp1(w[5], w[4])
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex] = interp6(w[5], w[2], w[4])
			dst[dstIndex+1] = interp9(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
	case 237, 233:
		dst[dstIndex] = interp1(w[5], w[2])
		dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp10(w[5], w[8], w[4])
		}
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6])
	case 175, 47:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
		} else {
			dst[dstIndex] = interp10(w[5], w[4], w[2])
		}
		dst[dstIndex+1] = interp1(w[5], w[6])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
	case 183, 151:
		dst[dstIndex] = interp1(w[5], w[4])
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex+1] = interp10(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8])
	case 245, 244:
		dst[dstIndex] = interp2(w[5], w[4], w[2])
		dst[dstIndex+1] = interp1(w[5], w[2])
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4])
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp10(w[5], w[6], w[8])
		}
	case 250:
		dst[dstIndex] = interp1(w[5], w[1])
		dst[dstIndex+1] = interp1(w[5], w[3])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		}
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
		}
	case 123:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
		} else {
			dst[dstIndex] = interp2(w[5], w[4], w[2])
		}
		dst[dstIndex+1] = interp1(w[5], w[3])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		}
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
	case 95:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
		} else {
			dst[dstIndex] = interp2(w[5], w[4], w[2])
		}
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
	case 222:
		dst[dstIndex] = interp1(w[5], w[1])
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
		}
	case 252:
		dst[dstIndex] = interp2(w[5], w[1], w[2])
		dst[dstIndex+1] = interp1(w[5], w[2])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		}
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp10(w[5], w[6], w[8])
		}
	case 249:
		dst[dstIndex] = interp1(w[5], w[2])
		dst[dstIndex+1] = interp2(w[5], w[3], w[2])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp10(w[5], w[8], w[4])
		}
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
		}
	case 235:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
		} else {
			dst[dstIndex] = interp2(w[5], w[4], w[2])
		}
		dst[dstIndex+1] = interp2(w[5], w[3], w[6])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp10(w[5], w[8], w[4])
		}
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6])
	case 111:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
		} else {
			dst[dstIndex] = interp10(w[5], w[4], w[2])
		}
		dst[dstIndex+1] = interp1(w[5], w[6])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		}
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[6])
	case 63:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
		} else {
			dst[dstIndex] = interp10(w[5], w[4], w[2])
		}
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[9], w[8])
	case 159:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
		} else {
			dst[dstIndex] = interp2(w[5], w[4], w[2])
		}
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex+1] = interp10(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8])
	case 215:
		dst[dstIndex] = interp1(w[5], w[4])
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex+1] = interp10(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp2(w[5], w[7], w[4])
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
		}
	case 246:
		dst[dstIndex] = interp2(w[5], w[1], w[4])
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4])
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp10(w[5], w[6], w[8])
		}
	case 254:
		dst[dstIndex] = interp1(w[5], w[1])
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		}
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		}
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp10(w[5], w[6], w[8])
		}
	case 253:
		dst[dstIndex] = interp1(w[5], w[2])
		dst[dstIndex+1] = interp1(w[5], w[2])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp10(w[5], w[8], w[4])
		}
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp10(w[5], w[6], w[8])
		}
	case 251:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
		} else {
			dst[dstIndex] = interp2(w[5], w[4], w[2])
		}
		dst[dstIndex+1] = interp1(w[5], w[3])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp10(w[5], w[8], w[4])
		}
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
		}
	case 239:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
		} else {
			dst[dstIndex] = interp10(w[5], w[4], w[2])
		}
		dst[dstIndex+1] = interp1(w[5], w[6])
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp10(w[5], w[8], w[4])
		}
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[6])
	case 127:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
		} else {
			dst[dstIndex] = interp10(w[5], w[4], w[2])
		}
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex+1] = interp2(w[5], w[2], w[6])
		}
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp2(w[5], w[8], w[4])
		}
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[9])
	case 191:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
		} else {
			dst[dstIndex] = interp10(w[5], w[4], w[2])
		}
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex+1] = interp10(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[8])
		dst[(dstIndex + dstRowElements + 1)] = interp1(w[5], w[8])
	case 223:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
		} else {
			dst[dstIndex] = interp2(w[5], w[4], w[2])
		}
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex+1] = interp10(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[7])
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp2(w[5], w[6], w[8])
		}
	case 247:
		dst[dstIndex] = interp1(w[5], w[4])
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex+1] = interp10(w[5], w[2], w[6])
		}
		dst[(dstIndex + dstRowElements)] = interp1(w[5], w[4])
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp10(w[5], w[6], w[8])
		}
	case 255:
		if diff(w[4], w[2]) {
			dst[dstIndex] = w[5]
		} else {
			dst[dstIndex] = interp10(w[5], w[4], w[2])
		}
		if diff(w[2], w[6]) {
			dst[dstIndex+1] = w[5]
		} else {
			dst[dstIndex+1] = interp10(w[5], w[2], w[6])
		}
		if diff(w[8], w[4]) {
			dst[(dstIndex + dstRowElements)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements)] = interp10(w[5], w[8], w[4])
		}
		if diff(w[6], w[8]) {
			dst[(dstIndex + dstRowElements + 1)] = w[5]
		} else {
			dst[(dstIndex + dstRowElements + 1)] = interp10(w[5], w[6], w[8])
		}
	}
}
