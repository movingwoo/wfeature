package skt

import (
	"fmt"
	"image"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

// roundCall draws inclusive rounded bounds using the integer midpoint rule.
func (g *scriptGraphics) roundCall(op byte, vm *sgsvm.VM) error {
	if op != 0xcb && op != 0xcc {
		return fmt.Errorf("unsupported SGS rounded rectangle service 0x%02x", op)
	}
	if err := vm.Require(5); err != nil {
		return err
	}
	a := vm.Args(5)
	left, top, right, bottom := int(a[0]), int(a[1]), int(a[2]), int(a[3])
	if left > right {
		left, right = right, left
	}
	if top > bottom {
		top, bottom = bottom, top
	}
	radius := min(absScript(int(a[4])), (right-left)/2, (bottom-top)/2)
	if g.color == 4 {
		return nil
	}
	color, err := g.mappedColor(g.color)
	if err != nil {
		return err
	}
	middleTop, middleBottom := top+radius+1, bottom-radius-1
	// The original fill helper sorts even an inverted middle interval. Keep
	// this visible behavior for zero-height and tightly rounded rectangles.
	if middleTop > middleBottom {
		middleTop, middleBottom = middleBottom, middleTop
	}
	middle := image.Rect(left, middleTop, right+1, middleBottom+1).Intersect(g.clip)
	pixels := int64(2*(right-left+1) + 2*(bottom-top+1) + 8*(radius+1))
	if op == 0xcc {
		pixels = int64(4*(radius+1))*int64(min(right-left+1, g.clip.Dx())) + int64(middle.Dx())*int64(middle.Dy())
	}
	if err := vm.ChargeWork(1 + radius + int(pixels/64)); err != nil {
		return err
	}
	horizontal := func(x1, x2, y int) {
		if y < g.clip.Min.Y || y >= g.clip.Max.Y {
			return
		}
		if x1 > x2 {
			x1, x2 = x2, x1
		}
		for x := max(x1, g.clip.Min.X); x <= min(x2, g.clip.Max.X-1); x++ {
			g.point(x, y, color)
		}
	}
	if op == 0xcb {
		horizontal(left+radius, right-radius, top)
		horizontal(left+radius, right-radius, bottom)
		for y := max(top+radius, g.clip.Min.Y); y <= min(bottom-radius, g.clip.Max.Y-1); y++ {
			g.point(left, y, color)
			g.point(right, y, color)
		}
	}
	cx1, cx2, cy1, cy2 := left+radius, right-radius, top+radius, bottom-radius
	x, y, decision := radius, 0, radius
	for x >= y {
		if op == 0xcb {
			g.point(cx1-x, cy1-y, color)
			g.point(cx1-y, cy1-x, color)
			g.point(cx2+x, cy1-y, color)
			g.point(cx2+y, cy1-x, color)
			g.point(cx1-x, cy2+y, color)
			g.point(cx1-y, cy2+x, color)
			g.point(cx2+x, cy2+y, color)
			g.point(cx2+y, cy2+x, color)
		} else {
			horizontal(cx1-y, cx2+y, cy1-x)
			horizontal(cx1-x, cx2+x, cy1-y)
			horizontal(cx1-y, cx2+y, cy2+x)
			horizontal(cx1-x, cx2+x, cy2+y)
		}
		decision -= 2*y + 1
		if decision < 0 {
			decision += 2*x - 2
			x--
		}
		y++
	}
	if op == 0xcc {
		for y := middle.Min.Y; y < middle.Max.Y; y++ {
			horizontal(middle.Min.X, middle.Max.X-1, y)
		}
	}
	return nil
}
