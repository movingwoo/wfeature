package skt

import (
	"fmt"
	"image"
	"math"
	"sort"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/sgsvm"
)

// scriptGraphics retains indexed pixels until the guest explicitly presents.
type scriptGraphics struct {
	// Byte palettes alias the original renderer's adjacent queue state. Native
	// pointer bytes become unknown when queued; never substitute host addresses.
	bitmapQueueScratch                   [256 - 182]byte
	bitmapQueueOpaque                    [256 - 182]bool
	bitmapQueue                          []scriptQueuedBitmap
	textStyle, textFG, textBG, textAlign int
	width, height                        int
	pixels, backup                       []byte
	framebuffer                          backend.Framebuffer
	clip                                 image.Rectangle
	color                                int
	bank                                 int
}

func newScriptGraphics(fb backend.Framebuffer) (*scriptGraphics, error) {
	w, h, err := backend.ValidateFramebuffer(fb)
	if err != nil {
		return nil, err
	}
	return &scriptGraphics{width: w, height: h, pixels: make([]byte, w*h), backup: make([]byte, w*h), framebuffer: fb, clip: image.Rect(0, 0, w, h), color: 3, textStyle: 2, textFG: 3, bank: 3}, nil
}

// Palette banks change the quantized RGB channels of future drawing. Their
// piecewise arithmetic reproduces the original brightening and dimming steps.
func scriptPaletteBank(c byte, bank int) byte {
	channel := func(x int) int {
		switch bank {
		case 0:
			return min(7, (x+10)/2)
		case 1:
			return min(7, (3*x+20)/5)
		case 2:
			return min(7, (3*x+10)/4)
		case 4:
			return (x + 1) / 3
		case 5:
			return (x + 2) / 5
		case 6:
			return 0
		default:
			return x
		}
	}
	b := int(c & 3)
	switch bank {
	case 0:
		b = 3
	case 1:
		b = min(3, b+2)
	case 2:
		b = min(3, b+1)
	case 4:
		b /= 2
	case 5, 6:
		b = 0
	}
	return byte(channel(int(c>>5))<<5 | channel(int(c>>2&7))<<2 | b)
}

func (g *scriptGraphics) mappedColor(c int) (byte, error) {
	v, err := scriptColor(c)
	return scriptPaletteBank(v, g.bank), err
}

// scriptColor generates the regular color cube instead of embedding a palette
// from a handset runtime. Accent aliases are generated as RGB ramps.
func scriptColor(c int) (byte, error) {
	if c < 0 || c >= 182 {
		return 0, fmt.Errorf("SGS palette color %d is unsupported", c)
	}
	if c >= 140 {
		if c == 140 {
			return 0, nil
		}
		if c == 141 {
			return 0xe0, nil
		}
		if c == 142 {
			return 0x1c, nil
		}
		// Accent ramps repeat the six primary/secondary RGB hues. Each ramp
		// contains black, full intensity and one-third intensity; the second
		// family adds a bright endpoint (a pale endpoint for yellow).
		group, phase := (c-143)/3+1, (c-143)%3
		if c >= 158 {
			group, phase = (c-158)/4, (c-158)%4
		}
		if phase == 0 {
			return 0, nil
		}
		r, g, b := 0, 0, 0
		if group == 0 || group == 1 || group == 5 {
			r = 7
		}
		if group == 1 || group == 2 || group == 3 {
			g = 7
		}
		if group == 3 || group == 4 || group == 5 {
			b = 3
		}
		if phase == 2 {
			r /= 3
			g /= 3
			b /= 3
		}
		if group == 1 && phase == 3 {
			b = 1
		}
		return byte(r<<5 | g<<2 | b), nil
	}
	if c < 16 {
		level := 0
		switch c {
		case 0, 5, 11, 15:
			level = 3
		case 1, 6, 8, 12:
			level = 2
		case 2, 7, 9, 13:
			level = 1
		}
		v := level * 7 / 3
		return byte(v<<5 | v<<2 | level), nil
	}
	index := 16
	for _, r := range []int{0, 2, 4, 7} {
		for g := 0; g < 8; g++ {
			for b := 0; b < 4; b++ {
				v := byte(r<<5 | g<<2 | b)
				if v == 0 || v == 73 || v == 146 || v == 255 {
					continue
				}
				if index == c {
					return v, nil
				}
				index++
			}
		}
	}
	return 0, fmt.Errorf("SGS palette color %d is unsupported", c)
}

func (g *scriptGraphics) point(x, y int, c byte) {
	if image.Pt(x, y).In(g.clip) {
		g.pixels[y*g.width+x] = c
	}
}
func (g *scriptGraphics) line(x, y, x2, y2 int, c byte) {
	dx, dy := x2-x, y2-y
	sx, sy := 1, 1
	if dx < 0 {
		dx = -dx
		sx = -1
	}
	if dy < 0 {
		dy = -dy
		sy = -1
	}
	err := dx - dy
	for {
		g.point(x, y, c)
		if x == x2 && y == y2 {
			return
		}
		e := err * 2
		if e > -dy {
			err -= dy
			x += sx
		}
		if e < dx {
			err += dx
			y += sy
		}
	}
}
func (g *scriptGraphics) present() error {
	rgba := make([]byte, len(g.pixels)*4)
	for i, c := range g.pixels {
		rgba[i*4] = byte(int(c>>5) * 255 / 7)
		rgba[i*4+1] = byte(int(c>>2&7) * 255 / 7)
		rgba[i*4+2] = byte(int(c&3) * 255 / 3)
		rgba[i*4+3] = 255
	}
	return g.framebuffer.Present(backend.Frame{Width: g.width, Height: g.height, RGBA: rgba})
}

func (g *scriptGraphics) call(op byte, vm *sgsvm.VM) (bool, error) {
	if n, ok := scriptArguments[op]; ok {
		if err := vm.Require(n); err != nil {
			return true, err
		}
	}
	if op >= 0x55 && op <= 0x78 {
		work := 1
		switch op {
		case 0x55, 0x56, 0x57, 0x76, 0x77, 0x78:
			work += len(g.pixels) / 64
		case 0x6b, 0x6d:
			// Text charges each glyph separately; only its background fill
			// needs an additional bound here (largest cell plus one border).
			work += g.width * min(g.height, 25) / 64
		case 0x6e:
			work += 1 + len(vm.Variables)
		case 0x6f, 0x70, 0x71, 0x72:
			// Bitmap dimensions are unsigned bytes, independent of the clip.
			work += 65536 / 64
		}
		if err := vm.ChargeWork(work); err != nil {
			return true, err
		}
	}
	if handled, err := g.textCall(op, vm); handled || err != nil {
		return handled, err
	}
	switch op {
	case 0x55, 0x56:
		c := byte(0)
		if op == 0x55 {
			c = 255
		}
		for i := range g.pixels {
			g.pixels[i] = c
		}
	case 0x57:
		c, err := g.mappedColor(int(vm.Pop()) % 182)
		if err != nil {
			return true, err
		}
		for i := range g.pixels {
			g.pixels[i] = c
		}
	case 0x58:
		a := vm.Args(3)
		v := int(a[2]) % 182
		if v == 4 {
			return true, nil
		}
		c, err := g.mappedColor(v)
		if err != nil {
			return true, err
		}
		g.point(int(a[0]), int(a[1]), c)
	case 0x59:
		g.bank = min(6, max(0, int(vm.Pop())))
	case 0x5a, 0x5b, 0x5c, 0x5d:
		v := int16(0)
		if op != 0x5a {
			v = int16(op - 0x58)
		}
		vm.Push(v)
	case 0x5e:
		g.color = int(byte(vm.Pop())) % 182
	case 0x5f, 0x60, 0x61, 0x62, 0x63:
		n := 4
		if op == 0x60 || op == 0x61 {
			n = 3
		}
		a := vm.Args(n)
		if g.color == 4 {
			return true, nil
		}
		c, err := g.mappedColor(g.color)
		if err != nil {
			return true, err
		}
		x, y, x2, y2 := int(a[0]), int(a[1]), 0, 0
		switch op {
		case 0x60:
			x2, y, y2 = int(a[1]), int(a[2]), int(a[2])
		case 0x61:
			x2, y2 = x, int(a[2])
		default:
			x2, y2 = int(a[2]), int(a[3])
		}
		if op <= 0x61 {
			if err := vm.ChargeWork(1 + max(absScript(x2-x), absScript(y2-y))/64); err != nil {
				return true, err
			}
			g.line(x, y, x2, y2, c)
			break
		}
		if x > x2 {
			x, x2 = x2, x
		}
		if y > y2 {
			y, y2 = y2, y
		}
		if op == 0x62 {
			if err := vm.ChargeWork(1 + (2*(x2-x+1)+2*(y2-y+1))/64); err != nil {
				return true, err
			}
			g.line(x, y, x2, y, c)
			g.line(x, y2, x2, y2, c)
			g.line(x, y, x, y2, c)
			g.line(x2, y, x2, y2, c)
		} else {
			r := image.Rect(x, y, x2+1, y2+1).Intersect(g.clip)
			if err := vm.ChargeWork(1 + r.Dx()*r.Dy()/64); err != nil {
				return true, err
			}
			for py := r.Min.Y; py < r.Max.Y; py++ {
				for px := r.Min.X; px < r.Max.X; px++ {
					g.pixels[py*g.width+px] = c
				}
			}
		}
	case 0x64, 0x65:
		a := vm.Args(4)
		cx, cy, rx, ry := int(a[0]), int(a[1]), absScript(int(a[2])), absScript(int(a[3]))
		if vm.Error() != nil {
			return true, vm.Error()
		}
		if g.color == 4 {
			return true, nil
		}
		c, err := g.mappedColor(g.color)
		if err != nil {
			return true, err
		}
		if rx == 0 || ry == 0 {
			if err := vm.ChargeWork(1 + 2*max(rx, ry)/64); err != nil {
				return true, err
			}
			g.line(cx-rx, cy-ry, cx+rx, cy+ry, c)
			return true, nil
		}
		bounds := image.Rect(cx-rx, cy-ry, cx+rx+1, cy+ry+1).Intersect(g.clip)
		work := 1 + 2*(min(2*rx+1, g.width)+min(2*ry+1, g.height))/64
		if op == 0x65 {
			work += bounds.Dx() * bounds.Dy() / 64
		}
		if err := vm.ChargeWork(work); err != nil {
			return true, err
		}
		for y := max(cy-ry, g.clip.Min.Y); y <= min(cy+ry, g.clip.Max.Y-1); y++ {
			dy := float64(y-cy) / float64(ry)
			half := int(math.Round(float64(rx) * math.Sqrt(max(0, 1-dy*dy))))
			if op == 0x65 {
				for x := max(cx-half, g.clip.Min.X); x <= min(cx+half, g.clip.Max.X-1); x++ {
					g.point(x, y, c)
				}
			} else {
				g.point(cx-half, y, c)
				g.point(cx+half, y, c)
			}
		}
		if op == 0x64 {
			for x := max(cx-rx, g.clip.Min.X); x <= min(cx+rx, g.clip.Max.X-1); x++ {
				dx := float64(x-cx) / float64(rx)
				half := int(math.Round(float64(ry) * math.Sqrt(max(0, 1-dx*dx))))
				g.point(x, cy-half, c)
				g.point(x, cy+half, c)
			}
		}
	case 0x6e:
		a := vm.Args(2)
		return true, scriptPackPalette(vm, a[0], a[1])
	case 0x6f:
		a := vm.Args(3)
		return true, g.bitmap(vm.Resource(int(a[2])).Data, int(a[0]), int(a[1]))
	case 0x70:
		a := vm.Args(4)
		return true, g.bitmapMirrored(vm.Resource(int(a[2])).Data, int(a[0]), int(a[1]), a[3] != 0)
	case 0x71, 0x72:
		n := 4
		if op == 0x72 {
			n = 5
		}
		a := vm.Args(n)
		data := vm.Resource(int(a[2])).Data
		if vm.Error() != nil {
			return true, vm.Error()
		}
		if scriptEmptyBitmap(data) {
			return true, nil
		}
		if len(data) < 5 {
			return true, fmt.Errorf("SGS bitmap header is truncated")
		}
		count := 0
		switch data[0] {
		case 2:
			count = 1
		case 3:
			count = 2
		case 5:
			count = 2
		case 6:
			count = 4
		case 7:
			count = 16
		}
		words, err := scriptPaletteWords(vm, a[n-1], (count+1)/2)
		if err != nil {
			return true, err
		}
		palette := make([]byte, count)
		for i := range palette {
			palette[i] = byte(uint16(words[i/2]) >> uint(8*(i%2)))
		}
		return true, g.bitmapPalette(data, int(a[0]), int(a[1]), op == 0x72 && a[3] != 0, palette)
	case 0x73:
		g.bitmapQueue = g.bitmapQueue[:0]
		g.bitmapQueueScratch[0] = 0
	case 0x74:
		// The native call consumes a reserved result slot plus four arguments.
		// A following stack restore exposes the result left in that slot.
		if err := vm.Require(5); err != nil {
			return true, err
		}
		a := vm.Args(5)
		resource := vm.Resource(int(a[3]))
		if err := vm.Error(); err != nil {
			return true, err
		}
		result := int16(0)
		if len(g.bitmapQueue) < 20 {
			entry := scriptQueuedBitmap{data: resource.Data, x: a[1], y: a[2], mirror: a[4]}
			g.recordBitmapQueue(entry)
			result = 1
		}
		vm.Push(result)
		vm.Pop()
	case 0x75:
		if err := vm.Require(1); err != nil {
			return true, err
		}
		return true, g.drawBitmapQueue(vm, vm.Pop())
	case 0x76:
		copy(g.backup, g.pixels)
	case 0x77:
		copy(g.pixels, g.backup)
	case 0x78:
		return true, g.present()
	case 0xce:
		return true, g.scroll(vm)
	case 0xcb, 0xcc:
		return true, g.roundCall(op, vm)
	case 0xcd:
		a := vm.Args(4)
		x1, y1, x2, y2 := int(a[0]), int(a[1]), int(a[2]), int(a[3])
		area := image.Rect(min(x1, x2), min(y1, y2), max(x1, x2)+1, max(y1, y2)+1).Intersect(g.clip)
		if err := vm.ChargeWork(area.Dx()*area.Dy() + 1); err != nil {
			return true, err
		}
		for y := area.Min.Y; y < area.Max.Y; y++ {
			for x := area.Min.X; x < area.Max.X; x++ {
				g.pixels[y*g.width+x] = ^g.pixels[y*g.width+x]
			}
		}
	case 0xc8:
		a := vm.Args(4)
		x1, y1, x2, y2 := int(a[0]), int(a[1]), int(a[2]), int(a[3])
		g.clip = image.Rect(min(x1, x2), min(y1, y2), max(x1, x2)+1, max(y1, y2)+1).Intersect(image.Rect(0, 0, g.width, g.height))
	case 0xca:
		a := vm.Args(2)
		x, y := int(a[0]), int(a[1])
		value := int16(-1)
		// Pixel reads use screen bounds, independently of the drawing clip.
		if x >= 0 && x < g.width && y >= 0 && y < g.height {
			value = int16(g.pixels[y*g.width+x])
		}
		vm.Push(value)
	case 0xc9:
		g.clip = image.Rect(0, 0, g.width, g.height)
	default:
		return false, nil
	}
	return true, nil
}

// Bitmap rows share a continuous MSB-first bitstream, including odd widths.
func (g *scriptGraphics) bitmap(data []byte, x, y int) error {
	return g.bitmapMirrored(data, x, y, false)
}

func (g *scriptGraphics) bitmapMirrored(data []byte, x, y int, mirror bool) error {
	return g.bitmapPalette(data, x, y, mirror, nil)
}

// A known zero dimension is enough to establish that no pixels can be read.
// Some archives use a two-byte type/zero-width placeholder without a full header.
func scriptEmptyBitmap(data []byte) bool {
	return len(data) >= 2 && data[0] >= 2 && data[0] <= 8 &&
		(data[1] == 0 || len(data) >= 3 && data[2] == 0)
}

func (g *scriptGraphics) bitmapPalette(data []byte, x, y int, mirror bool, override []byte) error {
	if scriptEmptyBitmap(data) {
		return nil
	}
	if len(data) < 5 {
		return fmt.Errorf("SGS bitmap header is truncated")
	}
	kind, w, h := int(data[0]), int(data[1]), int(data[2])
	if kind < 2 || kind > 8 {
		return fmt.Errorf("SGS bitmap type %d is unsupported", kind)
	}
	bits, colors, offset := 8, 182, 5
	switch kind {
	case 2, 5:
		bits, colors = 1, 2
	case 3, 6:
		bits, colors = 2, 4
	case 4, 7:
		bits, colors = 4, 16
	}
	palette := make([]int, colors)
	for i := range palette {
		palette[i] = i
	}
	if kind == 2 || kind == 3 {
		offset += colors / 2
	} else if kind >= 5 && kind <= 7 {
		offset += colors
	}
	needed := offset + (w*h*bits+7)/8
	if needed > len(data) {
		return fmt.Errorf("SGS bitmap payload is truncated: need %d, have %d", needed, len(data))
	}
	paletteBytes := data[5:offset]
	if override != nil && (kind == 2 || kind == 3 || kind >= 5 && kind <= 7) {
		if len(override) < offset-5 {
			return fmt.Errorf("SGS bitmap override palette is truncated")
		}
		paletteBytes = override
	}
	if kind == 2 || kind == 3 {
		for i := range palette {
			palette[i] = int(paletteBytes[i/2] >> uint(4-4*(i%2)) & 15)
		}
	} else if kind >= 5 && kind <= 7 {
		for i := range palette {
			palette[i] = int(paletteBytes[i])
		}
	}
	if mirror {
		x += int(int8(data[3])) - w
	} else {
		x -= int(int8(data[3]))
	}
	y -= int(int8(data[4]))
	// Validate every used color before changing the target image.
	decoded := make([]byte, w*h)
	transparent := make([]bool, w*h)
	for i := range decoded {
		bit := i * bits
		index := int(data[offset+bit/8]>>uint(8-bits-bit%8)) & ((1 << bits) - 1)
		if index >= len(palette) {
			return fmt.Errorf("SGS bitmap palette index %d is invalid", index)
		}
		c := palette[index]
		if c == 4 {
			transparent[i] = true
			continue
		}
		if kind >= 5 && kind <= 7 && c >= 182 && c < 256 {
			// These bytes bypass bank remapping in the original indexed lookup.
			if g.bitmapQueueOpaque[c-182] {
				return fmt.Errorf("SGS bitmap palette index %d aliases an unknown native queue pointer", c)
			}
			decoded[i] = g.bitmapQueueScratch[c-182]
		} else {
			v, err := g.mappedColor(c)
			if err != nil {
				return err
			}
			decoded[i] = v
		}
	}
	for i, c := range decoded {
		if !transparent[i] {
			px := i % w
			if mirror {
				px = w - 1 - px
			}
			g.point(x+px, y+i/w, c)
		}
	}
	return nil
}

func absScript(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// Palette APIs resolve word addresses but store packed little-endian bytes.
// Keep the entire span inside its selected bank before resolving any words.
func scriptPaletteWords(vm *sgsvm.VM, address int16, count int) ([]int16, error) {
	if count == 0 {
		return nil, nil
	}
	if address < 0 || count < 0 || int(uint16(address)&0x3fff)+count > 0x4000 {
		return nil, fmt.Errorf("SGS palette address span is invalid")
	}
	words := make([]int16, count)
	for i := range words {
		words[i] = vm.AddressRead(address + int16(i))
		if vm.Error() != nil {
			return nil, vm.Error()
		}
	}
	return words, nil
}

func scriptPackPalette(vm *sgsvm.VM, destination, source int16) error {
	header, err := scriptPaletteWords(vm, source, 1)
	if err != nil {
		return err
	}
	kind := int(header[0])
	if kind < 2 || kind > 7 {
		return fmt.Errorf("SGS packed palette type %d is unsupported", kind)
	}
	count := []int{2, 4, 16}[((kind - 2) % 3)]
	words, err := scriptPaletteWords(vm, source, count+1)
	if err != nil {
		return err
	}
	packed := make([]byte, count)
	if kind <= 4 {
		packed = packed[:count/2]
		for i := range packed {
			packed[i] = byte(words[1+2*i])<<4 | byte(words[2+2*i])&15
		}
	} else {
		for i := range packed {
			packed[i] = byte(words[i+1])
		}
	}
	// Preflight every destination word and preserve an unused high byte.
	prior, err := scriptPaletteWords(vm, destination, (len(packed)+1)/2)
	if err != nil {
		return err
	}
	for i, b := range packed {
		shift := uint(8 * (i % 2))
		prior[i/2] = int16(uint16(prior[i/2]) & ^(uint16(255)<<shift) | uint16(b)<<shift)
	}
	for i, word := range prior {
		vm.AddressWrite(destination+int16(i), word)
	}
	return vm.Error()
}

// Queue entries retain the resource's byte storage and observe in-place edits.
// A later reallocation leaves the retained storage alive, never a dangling address.
type scriptQueuedBitmap struct {
	data         []byte
	x, y, mirror int16
}

func (g *scriptGraphics) recordBitmapQueue(entry scriptQueuedBitmap) {
	at := 6 + len(g.bitmapQueue)*16
	for i := at; i < min(at+4, len(g.bitmapQueueScratch)); i++ {
		g.bitmapQueueOpaque[i] = true
	}
	for word, value := range []int16{entry.x, entry.y, entry.mirror} {
		bits := uint32(int32(value))
		for b := 0; b < 4; b++ {
			i := at + 4 + word*4 + b
			if i < len(g.bitmapQueueScratch) {
				g.bitmapQueueScratch[i] = byte(bits >> uint(8*b))
			}
		}
	}
	g.bitmapQueue = append(g.bitmapQueue, entry)
	g.bitmapQueueScratch[0] = byte(len(g.bitmapQueue))
}

func (g *scriptGraphics) drawBitmapQueue(vm *sgsvm.VM, mode int16) error {
	if len(g.bitmapQueue) == 0 {
		return nil
	}
	if err := vm.ChargeWork(1 + len(g.pixels)/64 + len(g.bitmapQueue)*(1+65536/64)); err != nil {
		return err
	}
	order := make([]int, len(g.bitmapQueue))
	for i := range order {
		order[i] = i
	}
	if mode >= 0 && mode <= 3 {
		sort.SliceStable(order, func(i, j int) bool {
			a, b := g.bitmapQueue[order[i]], g.bitmapQueue[order[j]]
			x, y := a.x, b.x
			if mode >= 2 {
				x, y = a.y, b.y
			}
			if mode%2 == 0 {
				return x < y
			}
			return x > y
		})
	}
	// Validate and render the bounded batch offscreen before changing pixels.
	staged := *g
	staged.pixels = append([]byte(nil), g.pixels...)
	for _, i := range order {
		if err := vm.ChargeWork(0); err != nil {
			return err
		}
		entry := g.bitmapQueue[i]
		if err := staged.bitmapMirrored(entry.data, int(entry.x), int(entry.y), entry.mirror != 0); err != nil {
			return err
		}
	}
	copy(g.pixels, staged.pixels)
	return nil
}
