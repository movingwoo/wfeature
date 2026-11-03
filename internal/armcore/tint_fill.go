package armcore

import "encoding/binary"

// Recognising a fill that blends every pixel toward one colour, and standing
// in for it.
//
// Two local titles darken or tint a rectangle of the screen with this loop —
// a translucent panel behind a menu, a fade — and in one of them it was 32%
// of the guest instructions executed in a run a person reported as slow. Each
// 16-bit pixel is split into three fields by three masks, each field is moved
// toward the same field of a fixed colour by a fraction out of 256, and the
// fields are put back together:
//
//	field = (field + (target - field) * alpha >> 8) & mask
//
// Forty-one instructions a pixel, most of them reloading the masks and the
// target colour from frame slots, because the compiler ran out of registers.
//
// **This is a template, not an analysis.** Both titles emit the loop byte for
// byte — the same registers, the same frame slots, the same spills — so the
// strictest proof available is also the simplest: the body has to be exactly
// these forty instructions and a `bne` back to the first. A loop that differs
// in any way is not this one and is refused. That trades generality, which no
// third title has asked for, for a recogniser that cannot be wrong about what
// it read.
//
// The last iteration is left to the interpreter, as in `byte_blend.go`. The
// body's scratch registers and flags are all written before they are read, and
// its two spill slots, `[sp, #8]` and `[sp, #0xc]`, are written by every
// iteration before anything reads them, so the interpreted iteration leaves
// all of them where the code after the loop finds them.

// tintFillBody is the loop, branch excluded. r4 walks the pixels, r5 counts
// them down, r7 is the fraction, r8 and r12 are the green and blue masks, and
// the red mask and the three target fields are in frame slots.
var tintFillBody = [...]uint16{
	0x2300, // movs  r3, #0
	0x5ee2, // ldrsh r2, [r4, r3]      the pixel
	0x990b, // ldr   r1, [sp, #0x2c]   the red mask
	0x4640, // mov   r0, r8            the green mask
	0x4663, // mov   r3, ip            the blue mask
	0x4011, // ands  r1, r2
	0x4010, // ands  r0, r2
	0x4013, // ands  r3, r2
	0x9a11, // ldr   r2, [sp, #0x44]   the target's red
	0x1a52, // subs  r2, r2, r1
	0x9302, // str   r3, [sp, #8]      the pixel's blue, spilled
	0x1c13, // adds  r3, r2, #0
	0x437b, // muls  r3, r7, r3
	0x121b, // asrs  r3, r3, #8
	0x18c9, // adds  r1, r1, r3
	0x9b10, // ldr   r3, [sp, #0x40]   the target's green
	0x9a0b, // ldr   r2, [sp, #0x2c]
	0x1a1b, // subs  r3, r3, r0
	0x4011, // ands  r1, r2
	0x1c1a, // adds  r2, r3, #0
	0x437a, // muls  r2, r7, r2
	0x9303, // str   r3, [sp, #0xc]    the green difference, spilled
	0x1213, // asrs  r3, r2, #8
	0x18c0, // adds  r0, r0, r3
	0x4643, // mov   r3, r8
	0x4018, // ands  r0, r3
	0x4301, // orrs  r1, r0
	0x9a02, // ldr   r2, [sp, #8]
	0x980f, // ldr   r0, [sp, #0x3c]   the target's blue
	0x1a83, // subs  r3, r0, r2
	0x437b, // muls  r3, r7, r3
	0x121b, // asrs  r3, r3, #8
	0x18d2, // adds  r2, r2, r3
	0x4663, // mov   r3, ip
	0x401a, // ands  r2, r3
	0x4311, // orrs  r1, r2
	0x3d01, // subs  r5, #1
	0x8021, // strh  r1, [r4]
	0x3402, // adds  r4, #2
	0x2d00, // cmp   r5, #0
}

// The frame slots the body reads and writes, as offsets from the stack
// pointer, and the span they cover together.
const (
	tintFillRedMask   = 0x2c
	tintFillRed       = 0x44
	tintFillGreen     = 0x40
	tintFillBlue      = 0x3c
	tintFillFrameLow  = 0x08
	tintFillFrameHigh = 0x48
	// tintFillSteps is one iteration, the branch included.
	tintFillSteps = uint32(len(tintFillBody)) + 1
	// tintFillBytes is how far the branch sits past the loop's head.
	tintFillBytes = uint32(len(tintFillBody)) * 2
)

// analyseTintFill answers whether the body between head and branchPC is the
// template, branch included.
func (memory *Memory) analyseTintFill(head, branchPC uint32) bool {
	if branchPC <= head || branchPC-head != tintFillBytes {
		return false
	}
	var buffer [len(tintFillBody) + 1]uint16
	code, ok := memory.thumbHalfwords(head, branchPC, buffer[:])
	if !ok || len(code) != len(tintFillBody)+1 {
		return false
	}
	for index, want := range tintFillBody {
		if code[index] != want {
			return false
		}
	}
	// bne head
	branch := uint32(code[len(tintFillBody)])
	return branch&0xff00 == 0xd100 && uint32(int32(branchPC+4)+int32(int8(branch))*2) == head
}

// runTintFill stands in for every remaining iteration but the last, and
// reports how many guest instructions it stood in for.
func (memory *Memory) runTintFill(context *Context) (uint32, error) {
	remaining := context.Registers[5]
	if remaining < 2 || remaining > maxStoreLoopIterations {
		return 0, nil
	}
	iterations := remaining - 1
	// The body spills two words every pixel. The interpreted last iteration
	// leaves them as the guest would, but a watch on either would see one
	// store where the guest made many, so a watched run is not stood in for.
	if memory.watchCount > 0 {
		return 0, nil
	}

	stack := context.Registers[RegisterSP]
	destination := context.Registers[4]
	span := uint64(iterations) * 2
	if stack&3 != 0 || destination&1 != 0 || uint64(destination)+span > 1<<32 ||
		uint64(stack)+tintFillFrameHigh > 1<<32 {
		return 0, nil
	}
	if err := memory.validateLocked(destination, span, PermissionReadWrite, "write"); err != nil {
		return 0, nil
	}
	// The masks and the target are read once and the guest reads them every
	// pixel, so a destination over the frame would be a loop whose later
	// pixels do not do what its first did. Refused rather than modelled.
	if uint64(stack)+tintFillFrameLow < uint64(destination)+span &&
		uint64(destination) < uint64(stack)+tintFillFrameHigh {
		return 0, nil
	}
	var frame [4]uint32
	for index, offset := range [...]uint32{tintFillRedMask, tintFillRed, tintFillGreen, tintFillBlue} {
		value, err := memory.readData32(stack + offset)
		if err != nil {
			// The next iteration's own load faults, where it would have.
			return 0, nil
		}
		frame[index] = value
	}
	redMask, red, green, blue := frame[0], frame[1], frame[2], frame[3]
	alpha, greenMask, blueMask := context.Registers[7], context.Registers[8], context.Registers[12]

	// One pixel exactly as the body computes it: the halfword sign-extended,
	// each difference multiplied and shifted as a signed 32-bit word.
	tint := func(pixel uint16) uint16 {
		value := uint32(int32(int16(pixel)))
		r, g, b := redMask&value, greenMask&value, blueMask&value
		r = (r + uint32(int32((red-r)*alpha)>>8)) & redMask
		g = (g + uint32(int32((green-g)*alpha)>>8)) & greenMask
		b = (b + uint32(int32((blue-b)*alpha)>>8)) & blueMask
		return uint16(r | g | b)
	}

	for pixel := uint32(0); pixel < iterations; {
		address := destination + pixel*2
		if to := memory.rawSpan(address, (iterations-pixel)*2, true); to != nil {
			for at := 0; at+1 < len(to); at += 2 {
				binary.LittleEndian.PutUint16(to[at:], tint(binary.LittleEndian.Uint16(to[at:])))
			}
			pixel += uint32(len(to)) / 2
			continue
		}
		value, err := memory.readData16(address)
		if err != nil {
			return pixel * tintFillSteps, err
		}
		if err := memory.writeData16(address, tint(value)); err != nil {
			return pixel * tintFillSteps, err
		}
		pixel++
	}

	context.Registers[4] = destination + iterations*2
	context.Registers[5] = remaining - iterations
	// The PC stays at the loop's head, where the taken branch left it.
	return iterations * tintFillSteps, nil
}
