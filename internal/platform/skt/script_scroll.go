package skt

import "github.com/movingwoo/wfeature/internal/sgsvm"

func (g *scriptGraphics) scroll(vm *sgsvm.VM) error {
	a := vm.Args(4)
	buffer, dx, dy, mode := int(a[0]), int(a[1]), int(a[2]), int(a[3])
	if buffer < 0 || buffer > 1 || mode < 0 || mode > 1 {
		return nil
	}
	if err := vm.ChargeWork(len(g.pixels) + 1); err != nil {
		return err
	}
	// The original oversized blank path always clears the drawing buffer,
	// including when the caller selected backup. Exact dimensions do not.
	if mode == 0 && (dx < -g.width || dx > g.width || dy < -g.height || dy > g.height) {
		for i := range g.pixels {
			g.pixels[i] = 255
		}
		return nil
	}
	dst := g.pixels
	if buffer == 1 {
		dst = g.backup
	}
	src := append([]byte(nil), dst...)
	for y := 0; y < g.height; y++ {
		for x := 0; x < g.width; x++ {
			sx, sy := x-dx, y-dy
			if mode == 1 {
				sx = (sx%g.width + g.width) % g.width
				sy = (sy%g.height + g.height) % g.height
			}
			value := byte(255)
			if sx >= 0 && sx < g.width && sy >= 0 && sy < g.height {
				value = src[sy*g.width+sx]
			}
			dst[y*g.width+x] = value
		}
	}
	return nil
}
