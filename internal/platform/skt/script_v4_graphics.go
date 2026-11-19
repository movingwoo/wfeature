package skt

import (
	"bytes"
	"fmt"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/curve"
	"github.com/movingwoo/wfeature/internal/glyph"
	"github.com/movingwoo/wfeature/internal/sgsvm"
	"golang.org/x/text/encoding/korean"
)

// Drawing, backup and presentation each have their own lifetime. In
// particular RestoreLCD after Flush must not overwrite the visible frame.
type wideScreen struct {
	g                                  graphicsContext
	fb                                 backend.Framebuffer
	back, visible                      []byte
	presents                           uint64
	gamma, alpha, zoom, mirror, rotate int
	font, fg, bg, align, style         int
	palette                            [182]uint32
}

func newWideScreen(fb backend.Framebuffer) (*wideScreen, error) {
	w, h, err := backend.ValidateFramebuffer(fb)
	if err != nil {
		return nil, err
	}
	r := &wideScreen{fb: fb, font: 2, fg: 3, gamma: 3}
	r.g = graphicsContext{width: w, height: h, pixels: make([]byte, 4*w*h), clip: paintRect{0, 0, w, h}}
	for i := 3; i < len(r.g.pixels); i += 4 {
		r.g.pixels[i] = 255
	}
	r.back = bytes.Clone(r.g.pixels)
	r.visible = bytes.Clone(r.g.pixels)
	for i := range r.palette {
		c, err := scriptColor(i)
		if err != nil {
			return nil, err
		}
		r.palette[i] = widePaletteRGB(c)
	}
	return r, nil
}

func (s *wideScreen) full() paintRect { return paintRect{0, 0, s.g.width, s.g.height} }
func (s *wideScreen) present(r paintRect) error {
	r = r.intersect(s.full())
	for y := r.minY; y < r.maxY; y++ {
		start, end := (y*s.g.width+r.minX)*4, (y*s.g.width+r.maxX)*4
		for i := start; i < end; i += 4 {
			s.visible[i] = byte(int(s.g.pixels[i]>>3) * 255 / 31)
			s.visible[i+1] = byte(int(s.g.pixels[i+1]>>2) * 255 / 63)
			s.visible[i+2] = byte(int(s.g.pixels[i+2]>>3) * 255 / 31)
		}
	}
	if err := s.fb.Present(backend.Frame{Width: s.g.width, Height: s.g.height, RGBA: s.visible}); err != nil {
		return err
	}
	s.presents++
	return nil
}

// The public master palette is indexed by a four-by-eight-by-four cube.
// The stored RGB332 selectors use nonlinear channel levels on this handset.
func widePaletteRGB(packed byte) uint32 {
	levels := [8]uint32{0, 64, 128, 160, 192, 224, 240, 255}
	blue := [4]uint32{0, 128, 192, 255}
	return levels[packed>>5]<<16 | levels[(packed>>2)&7]<<8 | blue[packed&3]
}

const wideTransparent = 0x01000000

type wideBitmap struct {
	w, h, ax, ay, bits int
	pixels             []byte
	colors             [256]uint32
}

func readWideBitmap(b []byte, palette *[182]uint32) (wideBitmap, error) {
	var img wideBitmap
	if len(b) < 5 {
		return img, fmt.Errorf("SGS v4 bitmap header truncated")
	}
	kind := int(b[0])
	if kind < 2 || kind > 12 {
		return img, fmt.Errorf("SGS v4 bitmap type %d", kind)
	}
	img.w, img.h, img.ax, img.ay = int(b[1]), int(b[2]), int(int8(b[3])), int(int8(b[4]))
	img.bits = [...]int{1, 2, 4, 1, 2, 4, 8, 1, 2, 4, 8}[kind-2]
	n := 1 << img.bits
	offset := 5
	if kind >= 9 {
		if len(b) < 7 {
			return img, fmt.Errorf("SGS v4 RGB palette header truncated")
		}
		colors := int(b[6])
		if colors == 0 {
			colors = 256
		}
		if b[5] > 1 || colors > n || len(b) < 7+3*colors {
			return img, fmt.Errorf("SGS v4 RGB palette invalid")
		}
		offset = 7 + 3*colors
		for i := 0; i < colors; i++ {
			v := b[7+3*i:]
			img.colors[i] = uint32(v[0])<<16 | uint32(v[1])<<8 | uint32(v[2])
		}
		if b[5] != 0 {
			img.colors[0] = wideTransparent
		}
	} else {
		if kind <= 3 {
			offset += n / 2
		}
		if kind >= 5 && kind <= 7 {
			offset += n
		}
		if offset > len(b) {
			return img, fmt.Errorf("SGS v4 bitmap palette truncated")
		}
		for i := 0; i < n; i++ {
			c := i
			switch kind {
			case 2, 3:
				c = int(b[5+i/2] >> uint(4-4*(i&1)) & 15)
			case 5, 6, 7:
				c = int(b[5+i])
			}
			if c == 4 || c >= 182 {
				img.colors[i] = wideTransparent
				continue
			}
			if palette != nil {
				img.colors[i] = palette[c]
			} else {
				v, _ := scriptColor(c)
				img.colors[i] = widePaletteRGB(v)
			}
		}
	}
	length := (img.w*img.h*img.bits + 7) / 8
	if length > len(b)-offset {
		return img, fmt.Errorf("SGS v4 bitmap raster truncated: type %d, need %d bytes, have %d", kind, length, len(b)-offset)
	}
	img.pixels = b[offset : offset+length]
	return img, nil
}
func (b *wideBitmap) pixel(i int) (uint32, bool) {
	bit := i * b.bits
	c := b.colors[int(b.pixels[bit/8]>>uint(8-b.bits-bit%8))&((1<<b.bits)-1)]
	return c, c != wideTransparent
}

func (s *wideScreen) blend(x, y int, c uint32, alpha int) {
	o := (y*s.g.width + x) * 4
	for i := range 3 {
		p := &s.g.pixels[o+i]
		v := byte(c >> uint(16-8*i))
		switch alpha {
		case 0:
			*p = v
		case 1, 2, 3:
			shift := 3
			if i == 1 {
				shift = 2
			}
			source, dest := v>>shift, *p>>shift
			var mix byte
			switch alpha {
			case 1:
				mix = (source >> 1) + (source >> 2) + (dest >> 2)
			case 2:
				mix = (source >> 1) + (dest >> 1)
			case 3:
				mix = (source >> 2) + (dest >> 1) + (dest >> 2)
			}
			*p = mix << shift
		case 4:
			*p = ^v
		case 5:
			*p ^= v
		}
	}
}

// Coordinates relative to the hotspot avoid a second translation when
// mirroring or rotating an image with an off-center (even external) anchor.
func (s *wideScreen) blit(b wideBitmap, x, y, w, h, alpha, mirror, rotate int) error {
	if w <= 0 || h <= 0 || b.w == 0 || b.h == 0 {
		return nil
	}
	if w > 65536 || h > 65536 || alpha < 0 || alpha > 5 || mirror < 0 || mirror > 1 || rotate < 0 || rotate > 3 {
		return fmt.Errorf("SGS v4 invalid image transform")
	}
	ax, ay := b.ax*w/b.w, b.ay*h/b.h
	if mirror == 1 {
		ax = w - 1 - ax
	}
	outW, outH := w, h
	switch rotate {
	case 1:
		ax, ay = h-1-ay, ax
		outW, outH = h, w
	case 2:
		ax, ay = w-1-ax, h-1-ay
	case 3:
		ax, ay = ay, w-1-ax
		outW, outH = h, w
	}
	x, y = x-ax, y-ay
	target := paintRect{x, y, x + outW, y + outH}.intersect(s.g.clip)
	for py := target.minY; py < target.maxY; py++ {
		for px := target.minX; px < target.maxX; px++ {
			dx, dy := px-x, py-y
			switch rotate {
			case 1:
				dx, dy = dy, h-1-dx
			case 2:
				dx, dy = w-1-dx, h-1-dy
			case 3:
				dx, dy = w-1-dy, dx
			}
			if mirror == 1 {
				dx = w - 1 - dx
			}
			if c, ok := b.pixel((dy*b.h/h)*b.w + dx*b.w/w); ok {
				s.blend(px, py, s.gammaRGB(c), alpha)
			}
		}
	}
	return nil
}

// Palette and image colors move toward white or black by successive halves.
// An explicit RGB device color bypasses the palette gamma setting.
func (s *wideScreen) gammaRGB(c uint32) uint32 {
	if s.gamma == 3 {
		return c
	}
	shift := uint(abs(s.gamma - 3))
	mask := uint32(255>>shift) * 0x010101
	v := (c >> shift) & mask
	if s.gamma < 3 {
		v |= 0xffffff ^ mask
	}
	return v
}

func (s *wideScreen) master(v int) uint32 {
	if v == 4 {
		return wideTransparent
	}
	if v < 0 || v >= len(s.palette) {
		return 0
	}
	return s.gammaRGB(s.palette[v])
}

var wideFontW = [6]int{4, 6, 6, 12, 8, 16}
var wideFontH = [6]int{6, 8, 12, 24, 16, 32}

func (s *wideScreen) text(x, y int, data []byte, font, fg, bg, align, style int, solid bool) error {
	if font < 0 || font >= len(wideFontW) {
		return fmt.Errorf("SGS v4 invalid font %d", font)
	}
	if end := bytes.IndexByte(data, 0); end >= 0 {
		data = data[:end]
	}
	if len(data) > 65536 {
		return fmt.Errorf("SGS v4 text exceeds 64 KiB")
	}
	decoded, err := korean.EUCKR.NewDecoder().Bytes(data)
	if err != nil {
		return err
	}
	cell, height := wideFontW[font], wideFontH[font]
	width := len(data) * cell
	switch align {
	case 1:
		x -= width / 2
	case 2:
		x -= width
	}
	previous := s.g.color
	defer func() { s.g.color = previous }()
	if solid && bg != 4 {
		s.g.color = s.master(bg)
		s.g.fillClipped(paintRect{x, y, x + width, y + height})
	}
	s.g.color = s.master(fg)
	if s.g.color == wideTransparent {
		return nil
	}
	face := glyph.Handset()
	for _, r := range string(decoded) {
		w := cell
		if r >= 128 {
			w *= 2
		}
		if x >= s.g.clip.maxX {
			break
		}
		if x+w > s.g.clip.minX && y+height > s.g.clip.minY && y < s.g.clip.maxY && (r < 128 || font >= 2) {
			b := face.Render(r)
			for v := 0; v < height; v++ {
				gy := v*12/height - face.Ascent + b.Ascent
				for u := 0; u < w; u++ {
					gx := u * 6 / cell
					if b.Coverage(gy, gx) < 128 {
						continue
					}
					px := x + u
					if style&2 != 0 {
						px += (height - 1 - v) / 4
					}
					s.g.putColorPixel(int64(px), int64(y+v))
					if style&1 != 0 {
						s.g.putColorPixel(int64(px+1), int64(y+v))
					}
				}
			}
		}
		if style&4 != 0 {
			s.g.drawLine(int64(x), int64(y+height-1), int64(x+w-1), int64(y+height-1))
		}
		x += w
	}
	return nil
}

func wideRect(a []int32) paintRect {
	return paintRect{int(a[0]), int(a[1]), int(a[2]) + 1, int(a[3]) + 1}
}
func wideRGB(a []int32) uint32 {
	return uint32(byte(a[0]))<<16 | uint32(byte(a[1]))<<8 | uint32(byte(a[2]))
}

var wideDrawArgs = map[uint16]int{
	0x0c: 1, 0x0e: 1, 0xf: 0, 0x14: 2, 0x16: 0, 0x17: 0, 0x18: 0, 0x19: 0,
	0x1b: 0, 0x1c: 0, 0x1d: 0, 0x1e: 0, 0x1f: 0, 0x20: 1, 0x22: 4, 0x23: 0, 0x25: 1, 0x26: 1, 0x27: 3, 0x28: 4, 0x29: 1, 0x2a: 2, 0x2b: 1, 0x2c: 1, 0x2d: 2,
	0x30: 1, 0x31: 1, 0x32: 1, 0x33: 1, 0x36: 1, 0x37: 5, 0x38: 3, 0x39: 7, 0x3a: 0, 0x3b: 0, 0x3c: 3, 0x3e: 5,
	0x42: 4, 0x43: 3, 0x44: 3, 0x45: 4, 0x46: 4, 0x47: 5, 0x48: 5, 0x49: 5, 0x4a: 4, 0x4b: 4, 0x4c: 4,
	0x51: 0, 0x52: 0, 0x55: 3, 0x56: 3, 0x57: 3, 0x58: 3, 0x59: 4, 0x5a: 4, 0x5b: 9, 0x5f: 0, 0x60: 4, 0x187: 5,
	0xf094: 3, 0xf095: 4, 0xf096: 4, 0xf097: 5, 0xf098: 7, 0xf099: 8,
}

func (s *wideScreen) call(id uint16, vm *sgsvm.WideVM) (bool, error) {
	n, ok := wideDrawArgs[id]
	if !ok {
		return false, nil
	}
	a, err := vm.Args(n)
	if err != nil {
		return true, err
	}
	return true, s.draw(id, a, vm)
}

// Charge native scans in pixels before touching the target. A flush always
// hands the host a whole frame, even when the requested update is partial.
func (s *wideScreen) drawingWork(id uint16, a []int32) int {
	full := s.g.width * s.g.height
	var rect paintRect
	switch id {
	case 0x36, 0x38, 0x3a, 0x3b, 0x51, 0x52:
		return full
	case 0x5f, 0x60:
		return 2 * full
	case 0x37, 0x39, 0x46, 0x47, 0x48, 0x49, 0x4a, 0x4b, 0x4c:
		rect = wideRect(a).intersect(s.g.clip)
	case 0x42, 0x43, 0x44, 0x45:
		return 2 * (s.g.width + s.g.height)
	default:
		return 0
	}
	return (rect.maxX - rect.minX) * (rect.maxY - rect.minY)
}

func (s *wideScreen) draw(id uint16, a []int32, vm *sgsvm.WideVM) error {
	if pixels := s.drawingWork(id, a); pixels > 0 {
		if err := vm.Charge((pixels + 31) / 32); err != nil {
			return err
		}
	}
	resource := func(i int32) ([]byte, error) {
		r, err := wideResource(vm, i, false)
		if err != nil {
			return nil, err
		}
		return r.Data, nil
	}
	switch id {
	case 0x0c, 0x0e:
		q, err := vm.Address(a[0], 4)
		if err != nil {
			return err
		}
		if id == 0x0c {
			copy(q, []int32{int32(s.g.width * s.g.height * 2), int32(s.g.width), int32(s.g.height), 6})
		} else {
			c := s.g.clip
			copy(q, []int32{int32(c.minX), int32(c.minY), int32(c.maxX - 1), int32(c.maxY - 1)})
		}
	case 0xf:
		return vm.Push(int32(s.gamma))
	case 0x14:
		d, err := resource(a[0])
		if err != nil {
			return err
		}
		k := int(a[1])
		if len(d) < 5 || k < 0 {
			return vm.Push(-1)
		}
		switch d[0] {
		case 2, 3:
			if k >= 1<<(1<<(d[0]-2)) || 5+k/2 >= len(d) {
				return vm.Push(-1)
			}
			return vm.Push(int32(d[5+k/2] >> uint(4-4*(k&1)) & 15))
		case 5, 6, 7:
			if k >= 1<<(1<<(d[0]-5)) || 5+k >= len(d) {
				return vm.Push(-1)
			}
			return vm.Push(int32(d[5+k]))
		case 4:
			if k < 16 {
				return vm.Push(a[1])
			}
		case 8:
			if k < 182 {
				return vm.Push(a[1])
			}
		}
		return vm.Push(-1)
	case 0x16, 0x17, 0x18, 0x19:
		return vm.Push(int32([]int{s.alpha, s.zoom, s.mirror, s.rotate}[id-0x16]))
	case 0x1b, 0x1c, 0x1d, 0x1e, 0x1f:
		return vm.Push(int32([]int{s.font, s.align, s.style, wideFontW[s.font], wideFontH[s.font]}[id-0x1b]))
	case 0x20:
		d, err := resource(a[0])
		if err != nil {
			return err
		}
		if err := vm.Charge(1 + len(d)/32); err != nil {
			return err
		}
		if i := bytes.IndexByte(d, 0); i >= 0 {
			d = d[:i]
		}
		return vm.Push(int32(len(d) * wideFontW[s.font]))
	case 0x22:
		s.g.clip = wideRect(a).intersect(s.full())
		if a[0] == 0 && a[1] == 0 && a[2] == -1 && a[3] == -1 {
			s.g.clip = s.full()
		}
	case 0x23:
		s.g.clip = s.full()
	case 0x25:
		if a[0] >= 0 && a[0] <= 6 {
			s.gamma = int(a[0])
		}
	case 0x26:
		s.g.color = s.master(int(a[0]))
	case 0x27:
		s.g.color = wideRGB(a)
	case 0x28, 0x29:
		if a[0] < 0 || a[0] >= 6 {
			return fmt.Errorf("SGS v4 invalid font %d", a[0])
		}
		s.font = int(a[0])
		if id == 0x28 {
			s.fg, s.bg, s.align = int(a[1]), int(a[2]), int(a[3])
		}
	case 0x2a:
		s.fg, s.bg = int(a[0]), int(a[1])
	case 0x2b:
		s.align = int(a[0])
	case 0x2c:
		s.style = int(a[0])
	case 0x2d:
		kind, err := vm.Address(a[1], 1)
		if err != nil {
			return err
		}
		if kind[0] < 2 || kind[0] > 7 {
			return fmt.Errorf("SGS v4 palette type %d", kind[0])
		}
		count := [3]int{2, 4, 16}[(kind[0]-2)%3]
		colors, err := vm.Address(a[1]+1, count)
		if err != nil {
			return err
		}
		size := count
		if kind[0] <= 4 {
			size /= 2
		}
		out, err := vm.Address(a[0], (size+3)/4)
		if err != nil {
			return err
		}
		packed := make([]byte, size)
		for i := range packed {
			if kind[0] <= 4 {
				packed[i] = byte(colors[2*i])<<4 | byte(colors[2*i+1]&15)
			} else {
				packed[i] = byte(colors[i])
			}
		}
		for i, v := range packed {
			shift := uint(i % 4 * 8)
			out[i/4] = int32(uint32(out[i/4])&^(uint32(255)<<shift) | uint32(v)<<shift)
		}
	case 0x30, 0x31, 0x32, 0x33:
		slot := []*int{&s.alpha, &s.zoom, &s.mirror, &s.rotate}[id-0x30]
		*slot = int(a[0])
	case 0x36, 0x37, 0x38, 0x39, 0x3a, 0x3b:
		prev := s.g.color
		r := s.full()
		switch id {
		case 0x36:
			s.g.color = s.master(int(a[0]))
		case 0x37:
			r = wideRect(a).intersect(s.g.clip)
			s.g.color = s.master(int(a[4]))
		case 0x38:
			s.g.color = wideRGB(a)
		case 0x39:
			r = wideRect(a).intersect(s.g.clip)
			s.g.color = wideRGB(a[4:])
		case 0x3a:
			s.g.color = s.master(0)
		case 0x3b:
			s.g.color = s.master(3)
		}
		s.g.fillRect(r)
		s.g.color = prev
	case 0x3c, 0x3e:
		prev := s.g.color
		if id == 0x3c {
			s.g.color = s.master(int(a[2]))
		} else {
			s.g.color = wideRGB(a[2:])
		}
		s.g.putColorPixel(int64(a[0]), int64(a[1]))
		s.g.color = prev
	case 0x42:
		s.g.drawLine(int64(a[0]), int64(a[1]), int64(a[2]), int64(a[3]))
	case 0x43:
		s.g.drawLine(int64(a[0]), int64(a[2]), int64(a[1]), int64(a[2]))
	case 0x44:
		s.g.drawLine(int64(a[0]), int64(a[1]), int64(a[0]), int64(a[2]))
	case 0x45:
		x, y, X, Y := int64(a[0]), int64(a[1]), int64(a[2]), int64(a[3])
		s.g.drawLine(x, y, X, y)
		s.g.drawLine(x, Y, X, Y)
		s.g.drawLine(x, y, x, Y)
		s.g.drawLine(X, y, X, Y)
	case 0x46:
		s.g.fillClipped(wideRect(a))
	case 0x47, 0x4a:
		rect := wideRect(a).intersect(s.g.clip)
		for y := rect.minY; y < rect.maxY; y++ {
			for x := rect.minX; x < rect.maxX; x++ {
				if id == 0x4a {
					s.blend(x, y, 0xffffff, 5)
				} else {
					s.blend(x, y, s.g.color, int(a[4]))
				}
			}
		}
	case 0x48, 0x49, 0x4b, 0x4c:
		w, h := int64(a[2])-int64(a[0])+1, int64(a[3])-int64(a[1])+1
		if w < 1 || h < 1 || w > 1<<30 || h > 1<<30 {
			return nil
		}
		emit := func(span curve.Span) error {
			s.g.fillClipped(paintRect{int(span.X), int(span.Y), int(span.X) + int(span.Width), int(span.Y) + 1})
			return nil
		}
		if id == 0x48 || id == 0x49 {
			fn := curve.FillRoundRectWithin
			if id == 0x48 {
				fn = curve.DrawRoundRectWithin
			}
			diam := min(max(a[4], 0), 1<<29) * 2
			return fn(s.g.curveBounds(), a[0], a[1], int32(w), int32(h), diam, diam, emit)
		}
		fn := curve.FillArcWithin
		if id == 0x4b {
			fn = curve.DrawArcWithin
		}
		return fn(s.g.curveBounds(), a[0], a[1], int32(w), int32(h), 0, 360, emit)
	case 0x51:
		copy(s.back, s.g.pixels)
	case 0x52:
		copy(s.g.pixels, s.back)
	case 0x55, 0x56, 0x57, 0x58, 0x59, 0x5a, 0x5b:
		d, err := resource(a[2])
		if err != nil {
			return err
		}
		font, fg, bg, align, style := s.font, s.fg, s.bg, s.align, s.style
		if id == 0x57 || id == 0x58 {
			font, fg, style = 2, 3, 0
		}
		if id == 0x59 || id == 0x5a {
			style = int(a[3])
		}
		if id == 0x5b {
			font, fg, bg, align, style = int(a[4]), int(a[5]), int(a[6]), int(a[7]), int(a[8])
		}
		if err := vm.Charge(1 + len(d)/32 + min(len(d), 65536)*32); err != nil {
			return err
		}
		return s.text(int(a[0]), int(a[1]), d, font, fg, bg, align, style, id == 0x56 || id == 0x58 || id == 0x5a || id == 0x5b && a[3] != 0)
	case 0x5f:
		return s.present(s.full())
	case 0x60:
		return s.present(wideRect(a))
	case 0x187, 0xf094, 0xf095, 0xf096, 0xf097, 0xf098, 0xf099:
		slot := a[2]
		if id == 0x187 {
			slot = a[4]
		}
		d, err := resource(slot)
		if err != nil {
			return err
		}
		img, err := readWideBitmap(d, &s.palette)
		if err != nil {
			return err
		}
		if id == 0xf096 || id == 0xf097 || id == 0xf099 {
			addr := a[3]
			if id == 0xf097 {
				addr = a[4]
			}
			if err := s.override(&img, d[0], vm, addr); err != nil {
				return err
			}
		}
		alpha, zoom, mirror, rot := s.alpha, s.zoom, s.mirror, s.rotate
		switch id {
		case 0xf095, 0xf097:
			mirror = int(a[3])
		case 0xf098:
			alpha, zoom, mirror, rot = int(a[3]), int(a[4]), int(a[5]), int(a[6])
		case 0xf099:
			alpha, zoom, mirror, rot = int(a[4]), int(a[5]), int(a[6]), int(a[7])
		}
		w, h, x, y := img.w*(1+zoom), img.h*(1+zoom), int(a[0]), int(a[1])
		if id == 0x187 {
			w, h = int(a[2])-x+1, int(a[3])-y+1
			if w > 0 && h > 0 && img.w != 0 && img.h != 0 {
				x += img.ax * w / img.w
				y += img.ay * h / img.h
			}
		}
		outW, outH := w, h
		if rot&1 != 0 {
			outW, outH = h, w
		}
		pixels := min(max(0, outW), s.g.width) * min(max(0, outH), s.g.height)
		if err := vm.Charge(16 + pixels/32); err != nil {
			return err
		}
		return s.blit(img, x, y, w, h, alpha, mirror, rot)
	default:
		return fmt.Errorf("SGS v4 graphics service %#x unsupported", id)
	}
	return nil
}

func (s *wideScreen) override(img *wideBitmap, kind byte, vm *sgsvm.WideVM, addr int32) error {
	count := 1 << img.bits
	size := count
	switch kind {
	case 2, 3:
		size = count / 2
	case 5, 6, 7:
	default:
		return fmt.Errorf("SGS v4 palette override for bitmap type %d unsupported", kind)
	}
	data, err := vm.Address(addr, (size+3)/4)
	if err != nil {
		return err
	}
	for i := 0; i < count; i++ {
		at := i
		if kind < 5 {
			at /= 2
		}
		c := byte(uint32(data[at/4]) >> uint(8*(at%4)))
		if kind < 5 {
			c = c >> uint(4-4*(i%2)) & 15
		}
		if c == 4 {
			img.colors[i] = wideTransparent
			continue
		}
		if int(c) >= len(s.palette) {
			return fmt.Errorf("SGS v4 palette selector %d", c)
		}
		img.colors[i] = s.palette[c]
	}
	return nil
}
