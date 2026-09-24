package webhost

import (
	"bytes"
	"image"
)

// changedFrameBounds compares the final, scaled pixels. Including every changed
// pixel here also includes the neighboring pixels a magnification filter blends.
// Both images own their storage and have the same dimensions and packed stride.
func changedFrameBounds(current, previous *image.RGBA) image.Rectangle {
	width, height := current.Rect.Dx(), current.Rect.Dy()
	left, top, right, bottom := width, height, 0, 0
	for y := 0; y < height; y++ {
		start := y * current.Stride
		row, before := current.Pix[start:start+width*4], previous.Pix[start:start+width*4]
		if bytes.Equal(row, before) {
			continue
		}
		top, bottom = min(top, y), y+1
		for x := 0; x < width; x++ {
			i := x * 4
			if !bytes.Equal(row[i:i+4], before[i:i+4]) {
				left, right = min(left, x), max(right, x+1)
			}
		}
	}
	if top == height {
		return image.Rectangle{}
	}
	return image.Rect(left, top, right, bottom)
}
