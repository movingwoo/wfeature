package skt

func transformScriptSISObject(source scriptSISLiteralObject, placement scriptSISComposition) scriptSISLiteralObject {
	width, height := source.width, source.height
	if placement.rotateLeft {
		width, height = height, width
	}
	result := scriptSISLiteralObject{
		width:  width,
		height: height,
		pixels: make([]byte, width/8*height),
	}
	for sourceY := 0; sourceY < source.height; sourceY++ {
		for sourceX := 0; sourceX < source.width; sourceX++ {
			if !scriptSISObjectPixel(source, sourceX, sourceY) {
				continue
			}
			x, y := sourceX, sourceY
			if placement.rotateLeft {
				x, y = sourceY, source.width-1-sourceX
			}
			if placement.mirrorVertical {
				y = height - 1 - y
			}
			if placement.mirrorHorizontal {
				x = width - 1 - x
			}
			result.pixels[y*(width/8)+x/8] |= 0x80 >> (x & 7)
		}
	}
	return result
}

func compositeScriptSISObject(frame *scriptSISLiteralFrame, object scriptSISLiteralObject, destinationX, destinationY int, masked bool) {
	var mask []byte
	if masked {
		mask = scriptSISObjectRowSpanMask(object)
	}
	for sourceY := 0; sourceY < object.height; sourceY++ {
		y := destinationY + sourceY
		if y < 0 || y >= frame.height {
			continue
		}
		for sourceX := 0; sourceX < object.width; sourceX++ {
			x := destinationX + sourceX
			if x < 0 || x >= frame.width {
				continue
			}
			pixel := scriptSISObjectPixel(object, sourceX, sourceY)
			bit := byte(0x80 >> (x & 7))
			index := y*(frame.width/8) + x/8
			if masked && scriptSISPackedPixel(mask, object.width, sourceX, sourceY) {
				if pixel {
					frame.pixels[index] |= bit
				} else {
					frame.pixels[index] &^= bit
				}
			} else if pixel {
				frame.pixels[index] |= bit
			}
		}
	}
}

// Later composition passes replace the horizontal span between the first and
// last set pixels in each nonempty row. The original helper then trims that
// span above and below set pixels in nonempty columns. In an empty column, its
// forced endpoint behavior preserves a row span only on row zero.
func scriptSISObjectRowSpanMask(object scriptSISLiteralObject) []byte {
	mask := make([]byte, len(object.pixels))
	for y := 0; y < object.height; y++ {
		first, last := object.width, -1
		for x := 0; x < object.width; x++ {
			if scriptSISObjectPixel(object, x, y) {
				first = min(first, x)
				last = x
			}
		}
		for x := first; x <= last; x++ {
			mask[y*(object.width/8)+x/8] |= 0x80 >> (x & 7)
		}
	}
	for x := 0; x < object.width; x++ {
		first, last := object.height, -1
		for y := 0; y < object.height; y++ {
			if scriptSISObjectPixel(object, x, y) {
				first = min(first, y)
				last = y
			}
		}
		if last < 0 {
			// The native reverse scan stores its forced zero endpoint in the
			// forward endpoint slot for an empty column. Preserve that observed
			// behavior: a row span in an empty column survives only on row zero.
			first, last = 0, 0
		}
		bit := byte(0x80 >> (x & 7))
		for y := 0; y < first; y++ {
			mask[y*(object.width/8)+x/8] &^= bit
		}
		for y := last + 1; y < object.height; y++ {
			mask[y*(object.width/8)+x/8] &^= bit
		}
	}
	return mask
}

func scriptSISObjectPixel(object scriptSISLiteralObject, x, y int) bool {
	return scriptSISPackedPixel(object.pixels, object.width, x, y)
}

func scriptSISPackedPixel(pixels []byte, width, x, y int) bool {
	return pixels[y*(width/8)+x/8]&(0x80>>(x&7)) != 0
}
