package skt

import (
	"bytes"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

type scriptSISLiteralObject struct {
	width  int
	height int
	pixels []byte
}

type scriptSISComposition struct {
	included         bool
	pass             int
	x                int
	y                int
	mirrorVertical   bool
	mirrorHorizontal bool
	rotateLeft       bool
}

type scriptSISLiteralFrame struct {
	width  int
	height int
	pixels []byte
}

type scriptSISBitReader struct {
	data     []byte
	position int
}

func (r *scriptSISBitReader) read(count int) (uint, bool) {
	if count < 0 || count > 32 || r.position > len(r.data)*8-count {
		return 0, false
	}
	var value uint
	for range count {
		value = value<<1 | uint(r.data[r.position/8]>>(7-r.position%8)&1)
		r.position++
	}
	return value, true
}

func (r *scriptSISBitReader) peek(count int) (uint, bool) {
	position := r.position
	value, ok := r.read(count)
	r.position = position
	return value, ok
}

// decodeScriptSISLiteralFrame implements the independently verified type-1
// subset: independent objects containing literal or coded 8-by-8 tiles,
// reference objects with optional tile replacement streams, and ordered frame
// composition with the measured transforms and inversion fields. Unresolved
// header fields fail closed until their contracts have executable evidence.
func decodeScriptSISLiteralFrame(data []byte, frameIndex int) (scriptSISLiteralFrame, bool) {
	var result scriptSISLiteralFrame
	if len(data) < 3 || !bytes.Equal(data[:3], []byte("SIS")) {
		return result, false
	}
	r := scriptSISBitReader{data: data[3:]}
	frameCount, ok := r.read(5)
	if !ok || frameCount < 1 || frameCount > 20 || frameIndex < 0 || frameIndex >= int(frameCount) {
		return result, false
	}
	if _, ok = r.read(5); !ok { // Playback timing does not change a frame's pixels.
		return result, false
	}
	widthUnits, ok := r.read(5)
	if !ok || widthUnits == 0 {
		return result, false
	}
	heightUnits, ok := r.read(4)
	if !ok || heightUnits == 0 {
		return result, false
	}
	headerInvert, ok := r.read(1)
	if !ok {
		return result, false
	}
	objectField, ok := r.read(5)
	objectCount := int(objectField) + 1
	if !ok || objectCount > 20 {
		return result, false
	}
	referenceWidth, ok := r.read(3)
	if !ok {
		return result, false
	}
	unresolved, ok := r.read(1)
	if !ok || unresolved != 0 {
		return result, false
	}
	if _, ok = r.read(4); !ok { // Frame delay does not change a frame's pixels.
		return result, false
	}
	variant, ok := r.read(3)
	if !ok {
		return result, false
	}
	unresolved, ok = r.read(4)
	if !ok || unresolved != 0 {
		return result, false
	}

	objects := make([]scriptSISLiteralObject, objectCount)
	for objectIndex := range objects {
		lookahead, ok := r.peek(8)
		if !ok {
			return result, false
		}
		if lookahead == 0 {
			prefix, ok := r.read(9)
			if !ok || prefix != 0 || objectIndex == 0 {
				return result, false
			}
			mode, ok := r.read(1)
			if !ok {
				return result, false
			}
			objects[objectIndex] = objects[objectIndex-1]
			if mode != 0 {
				objects[objectIndex].pixels = bytes.Clone(objects[objectIndex].pixels)
				if !decodeScriptSISReferenceTransform(&r, int(referenceWidth), &objects[objectIndex]) {
					return result, false
				}
			}
			continue
		}
		columns, ok := r.read(5)
		if !ok || columns < 1 {
			return result, false
		}
		rows, ok := r.read(4)
		if !ok || rows < 1 || rows > 12 {
			return result, false
		}
		tileCount := int(columns * rows)
		coding := make([]bool, tileCount)
		for tile := range coding {
			coded, ok := r.read(1)
			if !ok {
				return result, false
			}
			coding[tile] = coded != 0
		}
		object := &objects[objectIndex]
		object.width = int(columns) * 8
		object.height = int(rows) * 8
		object.pixels = make([]byte, int(columns)*object.height)
		for tile, coded := range coding {
			encodedPixels, ok := decodeScriptSISTile(&r, coded)
			if !ok {
				return result, false
			}
			for encodedPosition, pixel := range encodedPixels {
				if pixel == 0 {
					continue
				}
				x, y := scriptSISLiteralPosition(encodedPosition)
				x += tile % int(columns) * 8
				y += tile / int(columns) * 8
				object.pixels[y*int(columns)+x/8] |= 0x80 >> (x & 7)
			}
		}
	}

	var selected []scriptSISComposition
	selectedInvert := false
	for currentFrame := 0; currentFrame < int(frameCount); currentFrame++ {
		frameInvert, ok := r.read(1)
		if !ok {
			return result, false
		}
		composition := make([]scriptSISComposition, objectCount)
		for objectIndex := range composition {
			included, ok := r.read(1)
			if !ok {
				return result, false
			}
			composition[objectIndex].included = included != 0
		}
		for objectIndex := range composition {
			if !composition[objectIndex].included {
				continue
			}
			if variant != 1 {
				pass, ok := r.read(3)
				if !ok {
					return result, false
				}
				composition[objectIndex].pass = int(pass)
			}
			x, ok := r.read(8)
			if !ok {
				return result, false
			}
			y, ok := r.read(7)
			if !ok {
				return result, false
			}
			mirrorVertical, ok := r.read(1)
			if !ok {
				return result, false
			}
			mirrorHorizontal, ok := r.read(1)
			if !ok {
				return result, false
			}
			rotateLeft, ok := r.read(1)
			if !ok {
				return result, false
			}
			trailingFlag, ok := r.read(1)
			if !ok {
				return result, false
			}
			if trailingFlag != 0 {
				if _, ok = r.read(2); !ok {
					return result, false
				}
			}
			composition[objectIndex].x = scriptSISSignedMagnitude(x, 7)
			composition[objectIndex].y = scriptSISSignedMagnitude(y, 6)
			composition[objectIndex].mirrorVertical = mirrorVertical != 0
			composition[objectIndex].mirrorHorizontal = mirrorHorizontal != 0
			composition[objectIndex].rotateLeft = rotateLeft != 0
		}
		if currentFrame == frameIndex {
			selected = composition
			selectedInvert = frameInvert != 0
		}
	}

	result.width = int(widthUnits) * 8
	result.height = int(heightUnits) * 8
	result.pixels = make([]byte, int(widthUnits)*result.height)
	for pass := 0; pass < int(variant); pass++ {
		for objectIndex, placement := range selected {
			if !placement.included || placement.pass != pass {
				continue
			}
			object := transformScriptSISObject(objects[objectIndex], placement)
			compositeScriptSISObject(&result, object, placement.x, placement.y, pass != 0)
		}
	}
	headerInverted := headerInvert != 0
	if headerInverted != selectedInvert {
		for index := range result.pixels {
			result.pixels[index] = ^result.pixels[index]
		}
	}
	return result, true
}

func decodeScriptSISTile(r *scriptSISBitReader, coded bool) ([64]byte, bool) {
	if coded {
		return decodeScriptSISCodedTile(r)
	}
	var pixels [64]byte
	for position := range pixels {
		pixel, ok := r.read(1)
		if !ok {
			return [64]byte{}, false
		}
		pixels[position] = byte(pixel)
	}
	return pixels, true
}

// Coded tiles can expand a short run stream to a maximum-size object, and
// references can render that object once for every declared object. Transform
// references can also replace the same tile repeatedly. Source length alone is
// therefore not a sufficient pixel-work bound. The fixed header supplies a
// bounded object count and reference-index width before parsing begins. Ten
// maximum-object terms cover run iteration, coded expansion, tile placement,
// reference snapshots, geometric transformation, frame rendering, horizontal
// mask scanning, row-span filling, vertical mask scanning and mask trimming.
// Each possible replacement adds
// three tile terms for run iteration, decoded expansion and placement. Source
// terms cover bit parsing and the input snapshot. Charging the total before
// decoding also accounts for malformed streams and other late format failures.
func scriptSISLiteralDecodeWork(data []byte) int {
	objectCount := 20
	replacementCount := 0
	if len(data) >= 7 && bytes.Equal(data[:3], []byte("SIS")) {
		objectCount = int(data[5]&0x0f)<<1 | int(data[6]>>7)
		objectCount++
		objectCount = min(objectCount, 20)
		referenceWidth := int(data[6]>>4) & 7
		if referenceWidth != 0 {
			// A complete replacement needs an index, a coding selector, an
			// initial color and at least the five-bit all-white run code.
			replacementCount = len(data) * 8 / (referenceWidth + 7)
		}
	}
	const maximumObjectPixelWork = (31 * 8 * 12 * 8) / 64
	return 1 + 2*((len(data)+7)/8) + (len(data)+63)/64 + objectCount*10*maximumObjectPixelWork + replacementCount*3
}

func scriptSISSignedMagnitude(value uint, magnitudeBits uint) int {
	magnitude := int(value & (1<<magnitudeBits - 1))
	if value&(1<<magnitudeBits) != 0 {
		return -magnitude
	}
	return magnitude
}

// scriptSISLiteralPosition generates the conventional diagonal 8-by-8 scan.
// Generating it keeps the implementation independent of runtime lookup data.
func scriptSISLiteralPosition(position int) (int, int) {
	remaining := position
	for diagonal := 0; diagonal <= 14; diagonal++ {
		low := max(0, diagonal-7)
		high := min(7, diagonal)
		count := high - low + 1
		if remaining >= count {
			remaining -= count
			continue
		}
		x := low + remaining
		if diagonal&1 != 0 {
			x = high - remaining
		}
		return x, diagonal - x
	}
	panic("unreachable SIS literal position")
}

func scriptImageFrameCall(vm *sgsvm.VM) error {
	if err := vm.Require(7); err != nil {
		return err
	}
	a := vm.Args(7)
	source := vm.Resource(int(a[2]))
	if err := vm.Error(); err != nil {
		return err
	}
	if a[0] != 1 || a[1] != 1 {
		vm.Push(-1)
		return vm.Error()
	}
	destination := vm.Resource(int(a[3]))
	if err := vm.Error(); err != nil {
		return err
	}
	if len(source.Data) > 65535 {
		vm.Push(-1)
		return vm.Error()
	}
	if err := vm.ChargeWork(scriptSISLiteralDecodeWork(source.Data)); err != nil {
		return err
	}
	// The native service retains a source pointer across destination growth.
	// Snapshotting makes the supported path safe when both IDs alias.
	data := bytes.Clone(source.Data)
	frame, ok := decodeScriptSISLiteralFrame(data, int(a[4]))
	if !ok {
		vm.Push(-1)
		return vm.Error()
	}
	destinationSize := frame.width * frame.height
	if err := vm.ChargeWork(1 + destinationSize/64); err != nil {
		return err
	}
	ok, err := resizeScriptResource(vm, destination, destinationSize)
	if err != nil {
		return err
	}
	if !ok {
		vm.Push(-1)
		return vm.Error()
	}
	clear(destination.Data[:destinationSize])
	copy(destination.Data, frame.pixels)
	vm.Push(0)
	return vm.Error()
}
