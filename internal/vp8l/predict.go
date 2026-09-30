package vp8l

import "math"

// predictorModes is how many predictions the predictor transform offers.
const predictorModes = 14

// predictor is the block-wise prediction of a picture and its working memory.
type predictor struct {
	residuals []uint32
}

// residualCost prices a residual channel value by how far it is from zero,
// as a stand-in for its share of the entropy: a zero residual is what makes
// a block cheap, and small ones are commoner than large ones.
var residualCost = func() (costs [256]float32) {
	for value := range costs {
		distance := min(value, 256-value)
		if distance > 0 {
			costs[value] = float32(2 + 2*math.Log2(float64(distance)))
		}
	}
	return costs
}()

// predict chooses a mode for every block of pixels, writes the modes as
// pixels in modes, and answers the residuals: each pixel minus its prediction
// from the pixels before it.
//
// A transparent pixel's colour is never seen, so it is replaced, in pixels,
// by the colour of its prediction: its residual is then zero in every channel
// but alpha. That changes what the pixels after it are predicted from, which
// is why the residuals are taken in scan order, as the decoder undoes them,
// once every block has its mode.
func (p *predictor) predict(pixels []uint32, width, height, blockBits int, modes []uint32) []uint32 {
	blocksWide := (width + 1<<blockBits - 1) >> blockBits
	block := 1 << blockBits
	for blockY := 0; blockY*block < height; blockY++ {
		top, bottom := blockY*block, min((blockY+1)*block, height)
		for blockX := 0; blockX*block < width; blockX++ {
			left, right := blockX*block, min((blockX+1)*block, width)
			best, bestCost := 0, float32(math.Inf(1))
			for mode := range predictorModes {
				cost := float32(0)
				for y := top; y < bottom && cost < bestCost; y++ {
					for x := left; x < right; x++ {
						pixel := pixels[y*width+x]
						residual := subtractPixels(pixel, predictPixel(pixels, width, x, y, mode))
						cost += residualCost[residual>>24]
						if pixel>>24 != 0 {
							cost += residualCost[residual>>16&0xff] + residualCost[residual>>8&0xff] + residualCost[residual&0xff]
						}
					}
				}
				if cost < bestCost {
					best, bestCost = mode, cost
				}
			}
			modes[blockY*blocksWide+blockX] = 0xff000000 | uint32(best)<<8
		}
	}
	p.residuals = resize(p.residuals, len(pixels))
	for y := range height {
		for x := range width {
			index := y*width + x
			mode := int(modes[(y>>blockBits)*blocksWide+x>>blockBits] >> 8 & 0xff)
			prediction := predictPixel(pixels, width, x, y, mode)
			if pixels[index]>>24 == 0 {
				pixels[index] = prediction & 0x00ffffff
			}
			p.residuals[index] = subtractPixels(pixels[index], prediction)
		}
	}
	return p.residuals
}

// predictPixel is the prediction of the pixel at (x, y) under mode, from
// pixels already decoded. The first pixel is predicted as opaque black, the
// rest of the first row from the left and the first column from above,
// whatever the mode; a pixel in the last column takes the first pixel of its
// own row as its top-right neighbour, which is where it sits in memory.
func predictPixel(pixels []uint32, width, x, y, mode int) uint32 {
	index := y*width + x
	switch {
	case y == 0 && x == 0:
		return 0xff000000
	case y == 0:
		return pixels[index-1]
	case x == 0:
		return pixels[index-width]
	}
	left, top := pixels[index-1], pixels[index-width]
	topLeft, topRight := pixels[index-width-1], pixels[index-width+1]
	switch mode {
	case 0:
		return 0xff000000
	case 1:
		return left
	case 2:
		return top
	case 3:
		return topRight
	case 4:
		return topLeft
	case 5:
		return average(average(left, topRight), top)
	case 6:
		return average(left, topLeft)
	case 7:
		return average(left, top)
	case 8:
		return average(topLeft, top)
	case 9:
		return average(top, topRight)
	case 10:
		return average(average(left, topLeft), average(top, topRight))
	case 11:
		return selectPixel(left, top, topLeft)
	case 12:
		return clampAddSubtractFull(left, top, topLeft)
	default:
		return clampAddSubtractHalf(average(left, top), topLeft)
	}
}

// average halves the sum of each channel, rounding down.
func average(a, b uint32) uint32 {
	return (a^b)&0xfefefefe>>1 + a&b
}

func channel(pixel uint32, shift uint) int {
	return int(pixel >> shift & 0xff)
}

func absolute(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func clampChannel(value int) uint32 {
	return uint32(min(max(value, 0), 255))
}

// selectPixel answers left or top, whichever is nearer the gradient
// estimate left + top - topLeft.
func selectPixel(left, top, topLeft uint32) uint32 {
	towardsLeft, towardsTop := 0, 0
	for shift := uint(0); shift < 32; shift += 8 {
		estimate := channel(left, shift) + channel(top, shift) - channel(topLeft, shift)
		towardsLeft += absolute(estimate - channel(left, shift))
		towardsTop += absolute(estimate - channel(top, shift))
	}
	if towardsLeft < towardsTop {
		return left
	}
	return top
}

func clampAddSubtractFull(a, b, c uint32) uint32 {
	result := uint32(0)
	for shift := uint(0); shift < 32; shift += 8 {
		result |= clampChannel(channel(a, shift)+channel(b, shift)-channel(c, shift)) << shift
	}
	return result
}

func clampAddSubtractHalf(a, b uint32) uint32 {
	result := uint32(0)
	for shift := uint(0); shift < 32; shift += 8 {
		value := channel(a, shift)
		result |= clampChannel(value+(value-channel(b, shift))/2) << shift
	}
	return result
}
