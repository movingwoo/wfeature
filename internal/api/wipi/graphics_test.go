package wipi

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/api/midp"
	"github.com/movingwoo/wfeature/internal/jvm"
)

// The members in graphics.go are composed from the drawing calls a platform
// registers, so their tests run them over a small surface of the same shape:
// the calls below answer the way internal/platform/skt answers them — a read
// through the translation and not the clip, a write through both, a picture
// blended by its alpha — and the platform's own surface is tested in
// internal/platform/skt/wipi_graphics_members_test.go.

type testSurface struct {
	width, height int
	pixels        []uint32
	alpha         []uint8
	mutable       bool
}

type testGraphics struct {
	surface                *testSurface
	translateX, translateY int32
	clip                   area // device coordinates
	color                  uint32
	font                   *jvm.Object
}

const testGround = 0x2040c0

func newTestSurface(width, height int, mutable bool) *testSurface {
	surface := &testSurface{width: width, height: height, pixels: make([]uint32, width*height), alpha: make([]uint8, width*height), mutable: mutable}
	for index := range surface.pixels {
		surface.pixels[index], surface.alpha[index] = 0xffffff, 0xff
	}
	return surface
}

func (surface *testSurface) at(x, y int) uint32 { return surface.pixels[y*surface.width+x] }

func (graphics *testGraphics) device(x, y int64) (int64, int64) {
	return x + int64(graphics.translateX), y + int64(graphics.translateY)
}

func (graphics *testGraphics) put(x, y int64, rgb uint32) {
	x, y = graphics.device(x, y)
	if x < graphics.clip.left || x >= graphics.clip.right || y < graphics.clip.top || y >= graphics.clip.bottom {
		return
	}
	graphics.surface.pixels[int(y)*graphics.surface.width+int(x)] = rgb & 0xffffff
}

func (graphics *testGraphics) fill(x, y, width, height int64) {
	for row := max(y, graphics.clip.top-int64(graphics.translateY)); row < min(y+height, graphics.clip.bottom-int64(graphics.translateY)); row++ {
		for column := max(x, graphics.clip.left-int64(graphics.translateX)); column < min(x+width, graphics.clip.right-int64(graphics.translateX)); column++ {
			graphics.put(column, row, graphics.color)
		}
	}
}

func (graphics *testGraphics) line(x1, y1, x2, y2 int64) {
	dx, dy := abs64(x2-x1), -abs64(y2-y1)
	sx, sy := int64(1), int64(1)
	if x2 < x1 {
		sx = -1
	}
	if y2 < y1 {
		sy = -1
	}
	err := dx + dy
	for steps := 0; steps < 1<<16; steps++ {
		graphics.put(x1, y1, graphics.color)
		if x1 == x2 && y1 == y2 {
			return
		}
		if 2*err >= dy {
			err += dy
			x1 += sx
		}
		if 2*err <= dx {
			err += dx
			y1 += sy
		}
	}
}

func abs64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

// testText draws each character as a two-pixel-wide bar three pixels tall on
// a three-pixel advance, and refuses a vertical-centre anchor as text does.
func (graphics *testGraphics) text(count int, x, y, anchor int32) error {
	if anchor&2 != 0 || anchor&^(1|4|8|16|32|64) != 0 {
		return jvm.Throw("java/lang/IllegalArgumentException", "invalid Graphics anchor")
	}
	left, top := int64(x), int64(y)
	width := int64(3 * count)
	switch {
	case anchor&1 != 0:
		left -= width / 2
	case anchor&8 != 0:
		left -= width
	}
	switch {
	case anchor&32 != 0:
		top -= 4
	case anchor&64 != 0:
		top -= 3
	}
	for character := 0; character < count; character++ {
		for row := int64(0); row < 3; row++ {
			for column := int64(0); column < 2; column++ {
				graphics.put(left+int64(3*character)+column, top+row, graphics.color)
			}
		}
	}
	return nil
}

func (graphics *testGraphics) picture(source *testSurface, x, y int32) {
	for row := 0; row < source.height; row++ {
		for column := 0; column < source.width; column++ {
			alpha := uint32(source.alpha[row*source.width+column])
			if alpha == 0 {
				continue
			}
			dx, dy := graphics.device(int64(x)+int64(column), int64(y)+int64(row))
			if dx < graphics.clip.left || dx >= graphics.clip.right || dy < graphics.clip.top || dy >= graphics.clip.bottom {
				continue
			}
			index := int(dy)*graphics.surface.width + int(dx)
			graphics.surface.pixels[index] = testBlend(source.pixels[row*source.width+column], graphics.surface.pixels[index], alpha)
		}
	}
}

// testBlend is the platform's straight-alpha blend of one pixel over another.
func testBlend(over, under, alpha uint32) uint32 {
	result := uint32(0)
	for shift := uint(0); shift <= 16; shift += 8 {
		top, bottom := (over>>shift)&0xff, (under>>shift)&0xff
		result |= ((top*alpha + bottom*(0xff-alpha) + 127) / 0xff) << shift
	}
	return result
}

type testPlatform struct {
	t       *testing.T
	machine *jvm.VM
	font    *jvm.Object
}

func newTestPlatform(t *testing.T) *testPlatform {
	t.Helper()
	platform := &testPlatform{t: t, machine: newMachine(t)}
	platform.font = &jvm.Object{ClassName: FontClass, Fields: map[string]jvm.Value{}}
	platform.register()
	return platform
}

func (platform *testPlatform) newImage(surface *testSurface) *jvm.Object {
	return &jvm.Object{ClassName: ImageClass, Fields: map[string]jvm.Value{}, Native: surface}
}

func (platform *testPlatform) newGraphics(surface *testSurface) *jvm.Object {
	state := &testGraphics{surface: surface, clip: area{0, 0, int64(surface.width), int64(surface.height)}, font: platform.font}
	return &jvm.Object{ClassName: GraphicsClass, Fields: map[string]jvm.Value{}, Native: state}
}

// screen answers a Graphics over a new ground-filled surface.
func (platform *testPlatform) screen(width, height int) (*jvm.Object, *testSurface) {
	surface := newTestSurface(width, height, true)
	for index := range surface.pixels {
		surface.pixels[index] = testGround
	}
	return platform.newGraphics(surface), surface
}

func (platform *testPlatform) register() {
	t := platform.t
	graphicsOf := func(arguments []jvm.Value) *testGraphics {
		object, err := arguments[0].Reference()
		if err != nil || object == nil {
			t.Fatalf("Graphics receiver %v: %v", object, err)
		}
		return object.Native.(*testGraphics)
	}
	ints := func(arguments []jvm.Value, first, count int) []int64 {
		values, err := intArguments(arguments, first, count)
		if err != nil {
			t.Fatal(err)
		}
		result := make([]int64, count)
		for index, value := range values {
			result[index] = int64(value)
		}
		return result
	}
	answer := func(value int64) (jvm.Value, error) { return jvm.IntValue(int32(value)), nil }
	none := jvm.VoidValue()
	type native = func(*jvm.VM, []jvm.Value) (jvm.Value, error)
	graphics := map[string]native{
		"getColor()I": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) { return answer(int64(graphicsOf(a).color)) },
		"setColor(I)V": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
			graphicsOf(a).color = uint32(ints(a, 1, 1)[0]) & 0xffffff
			return none, nil
		},
		"getTranslateX()I": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) { return answer(int64(graphicsOf(a).translateX)) },
		"getTranslateY()I": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) { return answer(int64(graphicsOf(a).translateY)) },
		"translate(II)V": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
			g, v := graphicsOf(a), ints(a, 1, 2)
			g.translateX += int32(v[0])
			g.translateY += int32(v[1])
			return none, nil
		},
		// The clip getters answer as the platform's do, wrapping an extreme
		// translation the way an int does.
		"getClipX()I": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
			g := graphicsOf(a)
			return answer(int64(int32(g.clip.left - int64(g.translateX))))
		},
		"getClipY()I": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
			g := graphicsOf(a)
			return answer(int64(int32(g.clip.top - int64(g.translateY))))
		},
		"getClipWidth()I": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
			return answer(max(0, graphicsOf(a).clip.right-graphicsOf(a).clip.left))
		},
		"getClipHeight()I": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
			return answer(max(0, graphicsOf(a).clip.bottom-graphicsOf(a).clip.top))
		},
		"setClip(IIII)V": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
			g, v := graphicsOf(a), ints(a, 1, 4)
			x, y := g.device(v[0], v[1])
			g.clip = areaOf(x, y, v[2], v[3]).intersect(area{0, 0, int64(g.surface.width), int64(g.surface.height)})
			return none, nil
		},
		"fillRect(IIII)V": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
			v := ints(a, 1, 4)
			graphicsOf(a).fill(v[0], v[1], v[2], v[3])
			return none, nil
		},
		"drawLine(IIII)V": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
			v := ints(a, 1, 4)
			graphicsOf(a).line(v[0], v[1], v[2], v[3])
			return none, nil
		},
		// An outline of four lines, which draws each corner twice.
		"drawRect(IIII)V": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
			g, v := graphicsOf(a), ints(a, 1, 4)
			if v[2] < 0 || v[3] < 0 {
				return none, nil
			}
			right, bottom := v[0]+v[2], v[1]+v[3]
			g.line(v[0], v[1], right, v[1])
			g.line(v[0], bottom, right, bottom)
			g.line(v[0], v[1], v[0], bottom)
			g.line(right, v[1], right, bottom)
			return none, nil
		},
		"fillArc(IIIIII)V": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
			v := ints(a, 1, 6)
			if v[2] > 0 && v[3] > 0 {
				graphicsOf(a).fill(v[0]+1, v[1], v[2]-1, v[3])
			}
			return none, nil
		},
		"drawArc(IIIIII)V": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
			g, v := graphicsOf(a), ints(a, 1, 6)
			if v[2] > 0 && v[3] > 0 {
				// A curve rounds to a pixel at its box's right edge.
				g.line(v[0], v[1]+v[3]/2, v[0]+v[2], v[1]+v[3]/2)
			}
			return none, nil
		},
		"fillRoundRect(IIIIII)V": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
			v := ints(a, 1, 6)
			if v[2] > 0 && v[3] > 0 {
				graphicsOf(a).fill(v[0], v[1]+1, v[2], v[3]-2)
			}
			return none, nil
		},
		"drawRoundRect(IIIIII)V": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
			g, v := graphicsOf(a), ints(a, 1, 6)
			if v[2] > 0 && v[3] > 0 {
				g.line(v[0]+1, v[1], v[0]+v[2]-2, v[1])
				g.line(v[0], v[1]+1, v[0], v[1]+v[3]-2)
			}
			return none, nil
		},
		"drawString(Ljava/lang/String;III)V": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
			text := stringArgument(t, a[1])
			if text == nil {
				return none, jvm.Throw("java/lang/NullPointerException", "String is null")
			}
			v := ints(a, 2, 3)
			return none, graphicsOf(a).text(len(*text), int32(v[0]), int32(v[1]), int32(v[2]))
		},
		"drawSubstring(Ljava/lang/String;IIIII)V": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
			text := stringArgument(t, a[1])
			v := ints(a, 2, 5)
			if text == nil || v[0] < 0 || v[1] < 0 || v[0]+v[1] > int64(len(*text)) {
				return none, jvm.Throw("java/lang/StringIndexOutOfBoundsException", "substring range")
			}
			return none, graphicsOf(a).text(int(v[1]), int32(v[2]), int32(v[3]), int32(v[4]))
		},
		"drawChars([CIIIII)V": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
			v := ints(a, 2, 5)
			return none, graphicsOf(a).text(int(v[1]), int32(v[2]), int32(v[3]), int32(v[4]))
		},
		"drawChar(CIII)V": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
			v := ints(a, 2, 3)
			return none, graphicsOf(a).text(1, int32(v[0]), int32(v[1]), int32(v[2]))
		},
		// A rectangle of a picture, refused as the platform refuses it: a
		// picture drawn onto itself, a rectangle outside it, a transform this
		// surface does not draw.
		"drawRegion(Ljavax/microedition/lcdui/Image;IIIIIIII)V": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
			g := graphicsOf(a)
			image, _ := a[1].Reference()
			if image == nil {
				return none, jvm.Throw("java/lang/NullPointerException", "Image is null")
			}
			source := image.Native.(*testSurface)
			v := ints(a, 2, 8)
			if source == g.surface {
				return none, jvm.Throw("java/lang/IllegalArgumentException", "drawRegion source is the Graphics destination")
			}
			if v[2] <= 0 || v[3] <= 0 || v[0] < 0 || v[1] < 0 || v[0]+v[2] > int64(source.width) || v[1]+v[3] > int64(source.height) || v[4] != 0 {
				return none, jvm.Throw("java/lang/IllegalArgumentException", "image region exceeds source bounds")
			}
			region := newTestSurface(int(v[2]), int(v[3]), false)
			for row := 0; row < region.height; row++ {
				for column := 0; column < region.width; column++ {
					index := (int(v[1])+row)*source.width + int(v[0]) + column
					region.pixels[row*region.width+column], region.alpha[row*region.width+column] = source.pixels[index], source.alpha[index]
				}
			}
			g.picture(region, int32(v[5]), int32(v[6]))
			return none, nil
		},
		// The platform's reset: the whole surface clipped in, no translation,
		// the default font.
		"reset()V": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
			g := graphicsOf(a)
			g.translateX, g.translateY = 0, 0
			g.clip = area{0, 0, int64(g.surface.width), int64(g.surface.height)}
			g.font = platform.font
			return none, nil
		},
		"getFont()Ljavax/microedition/lcdui/Font;": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
			return jvm.ReferenceValue(graphicsOf(a).font), nil
		},
		"setFont(Ljavax/microedition/lcdui/Font;)V": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
			font, _ := a[1].Reference()
			graphicsOf(a).font = font
			return none, nil
		},
		"drawImage(Ljavax/microedition/lcdui/Image;III)V": func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
			image, _ := a[1].Reference()
			if image == nil {
				return none, jvm.Throw("java/lang/NullPointerException", "Image is null")
			}
			v := ints(a, 2, 3)
			graphicsOf(a).picture(image.Native.(*testSurface), int32(v[0]), int32(v[1]))
			return none, nil
		},
	}
	for member, method := range graphics {
		platform.registerNative(midp.GraphicsClass, member, method)
	}
	platform.registerNative(GraphicsClass, "getRGBPixels(IIII[III)V", func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
		g, v := graphicsOf(a), ints(a, 1, 4)
		array, _ := a[5].Reference()
		if array == nil {
			return none, jvm.Throw("java/lang/NullPointerException", "array is null")
		}
		tail := ints(a, 6, 2)
		values := make([]jvm.Value, v[2])
		for row := int64(0); row < v[3]; row++ {
			for column := int64(0); column < v[2]; column++ {
				x, y := g.device(v[0]+column, v[1]+row)
				values[column] = jvm.IntValue(0)
				if x >= 0 && y >= 0 && x < int64(g.surface.width) && y < int64(g.surface.height) {
					values[column] = jvm.IntValue(int32(g.surface.at(int(x), int(y))))
				}
			}
			if err := jvm.SetArrayRange(array, int(tail[0]+row*tail[1]), values); err != nil {
				return none, err
			}
		}
		return none, nil
	})
	platform.registerNative(GraphicsClass, "setRGBPixels(IIII[III)V", func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
		g, v := graphicsOf(a), ints(a, 1, 4)
		array, _ := a[5].Reference()
		_, values, err := jvm.ArraySnapshot(array)
		if err != nil {
			return none, err
		}
		tail := ints(a, 6, 2)
		for row := int64(0); row < v[3]; row++ {
			for column := int64(0); column < v[2]; column++ {
				pixel, _ := values[tail[0]+row*tail[1]+column].Int32()
				g.put(v[0]+column, v[1]+row, uint32(pixel))
			}
		}
		return none, nil
	})
	created := func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
		v := ints(a, 0, 2)
		if v[0] <= 0 || v[1] <= 0 {
			return none, jvm.Throw("java/lang/IllegalArgumentException", "image size")
		}
		return jvm.ReferenceValue(platform.newImage(newTestSurface(int(v[0]), int(v[1]), true))), nil
	}
	platform.registerNative(midp.ImageClass, "createImage(II)Ljavax/microedition/lcdui/Image;", created)
	platform.registerNative(ImageClass, "createImage(II)Lorg/kwis/msp/lcdui/Image;", created)
	imageOf := func(arguments []jvm.Value) *testSurface {
		object, _ := arguments[0].Reference()
		return object.Native.(*testSurface)
	}
	graphicsFor := func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
		surface := imageOf(a)
		if !surface.mutable {
			return none, jvm.Throw("java/lang/IllegalStateException", "immutable Image has no Graphics")
		}
		return jvm.ReferenceValue(platform.newGraphics(surface)), nil
	}
	platform.registerNative(midp.ImageClass, "getGraphics()Ljavax/microedition/lcdui/Graphics;", graphicsFor)
	platform.registerNative(ImageClass, "getGraphics()Lorg/kwis/msp/lcdui/Graphics;", graphicsFor)
	platform.registerNative(midp.ImageClass, "getWidth()I", func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) { return answer(int64(imageOf(a).width)) })
	platform.registerNative(midp.ImageClass, "getHeight()I", func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) { return answer(int64(imageOf(a).height)) })
	platform.registerNative(midp.ImageClass, "isMutable()Z", func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) { return answer(int64(boolInt(imageOf(a).mutable))) })
	platform.registerNative(midp.ImageClass, "getRGB([IIIIIII)V", func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
		surface := imageOf(a)
		array, _ := a[1].Reference()
		v := ints(a, 2, 6)
		for row := int64(0); row < v[5]; row++ {
			values := make([]jvm.Value, v[4])
			for column := range values {
				index := int(v[3]+row)*surface.width + int(v[2]) + column
				values[column] = jvm.IntValue(int32(uint32(surface.alpha[index])<<24 | surface.pixels[index]))
			}
			if err := jvm.SetArrayRange(array, int(v[0]+row*v[1]), values); err != nil {
				return none, err
			}
		}
		return none, nil
	})
	platform.registerNative(midp.FontClass, "getHeight()I", func(*jvm.VM, []jvm.Value) (jvm.Value, error) { return answer(4) })
	platform.registerNative(midp.FontClass, "stringWidth(Ljava/lang/String;)I", func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
		text := stringArgument(t, a[1])
		if text == nil {
			return none, jvm.Throw("java/lang/NullPointerException", "String is null")
		}
		return answer(int64(3 * len(*text)))
	})
	platform.registerNative(midp.FontClass, "substringWidth(Ljava/lang/String;II)I", func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
		text, v := stringArgument(t, a[1]), ints(a, 2, 2)
		if text == nil || v[0] < 0 || v[1] < 0 || v[0]+v[1] > int64(len(*text)) {
			return none, jvm.Throw("java/lang/StringIndexOutOfBoundsException", "substring range")
		}
		return answer(3 * v[1])
	})
	platform.registerNative(midp.FontClass, "charsWidth([CII)I", func(_ *jvm.VM, a []jvm.Value) (jvm.Value, error) {
		return answer(3 * ints(a, 2, 2)[1])
	})
	platform.registerNative(midp.FontClass, "charWidth(C)I", func(*jvm.VM, []jvm.Value) (jvm.Value, error) { return answer(3) })
}

func (platform *testPlatform) registerNative(class, member string, method func(*jvm.VM, []jvm.Value) (jvm.Value, error)) {
	split := 0
	for split < len(member) && member[split] != '(' {
		split++
	}
	if err := platform.machine.RegisterNative(class, member[:split], member[split:], method); err != nil {
		platform.t.Fatalf("register %s.%s: %v", class, member, err)
	}
}

func stringArgument(t *testing.T, value jvm.Value) *string {
	t.Helper()
	object, err := value.Reference()
	if err != nil || object == nil {
		return nil
	}
	text, ok := jvm.StringText(object)
	if !ok {
		t.Fatalf("argument is not a String: %v", object)
	}
	return &text
}

// call invokes a member on a WIPI object the way a title's bytecode does,
// through the receiver's own class.
func (platform *testPlatform) call(receiver *jvm.Object, name, descriptor string, arguments ...jvm.Value) (jvm.Value, error) {
	return platform.machine.InvokeVirtual(receiver, name, descriptor, arguments...)
}

func (platform *testPlatform) must(receiver *jvm.Object, name, descriptor string, arguments ...jvm.Value) jvm.Value {
	platform.t.Helper()
	value, err := platform.call(receiver, name, descriptor, arguments...)
	if err != nil {
		platform.t.Fatalf("%s%s: %v", name, descriptor, err)
	}
	return value
}

func (platform *testPlatform) mustInt(receiver *jvm.Object, name, descriptor string, arguments ...jvm.Value) int32 {
	platform.t.Helper()
	value, err := platform.must(receiver, name, descriptor, arguments...).Int32()
	if err != nil {
		platform.t.Fatal(err)
	}
	return value
}

func values(numbers ...int32) []jvm.Value {
	result := make([]jvm.Value, len(numbers))
	for index, number := range numbers {
		result[index] = jvm.IntValue(number)
	}
	return result
}

func (platform *testPlatform) intArray(numbers ...int32) *jvm.Object {
	platform.t.Helper()
	array, err := platform.machine.NewArray(jvm.Type{Kind: jvm.TypeInt}, int32(len(numbers)))
	if err != nil {
		platform.t.Fatal(err)
	}
	if err := jvm.SetArrayRange(array, 0, values(numbers...)); err != nil {
		platform.t.Fatal(err)
	}
	return array
}

func expectThrow(t *testing.T, machine *jvm.VM, err error, class string) {
	t.Helper()
	if !machine.IsGuestException(err, class) {
		t.Fatalf("error = %v, want %s", err, class)
	}
}

// Every member KTF serves on WIPI Graphics is declared on this one with a
// body: a member a title links against and this layer did not declare ends
// the title at its first call.
func TestGraphicsDeclaresTheMembersMIDPDoesNotHave(t *testing.T) {
	declared := make(map[string]jvm.MethodDefinition)
	var graphics, image jvm.ClassDefinition
	for _, definition := range definitions(nil) {
		switch definition.Name {
		case GraphicsClass:
			graphics = definition
		case ImageClass:
			image = definition
		}
		for _, method := range definition.Methods {
			declared[definition.Name+"."+method.Name+method.Descriptor] = method
		}
	}
	for _, member := range []string{
		"copyArea(IIIIII)V", "drawPolygon([I[I)V", "getRedComponent()I", "getGreenComponent()I",
		"getBlueComponent()I", "getPixel(II)I", "setPixel(II)V",
		"getStrokeStyle()I", "setStrokeStyle(I)V", "isXORMode()Z", "setXORMode(Z)V",
		// Answered here rather than by the platform, so that XOR mode and the
		// stroke style reach them.
		"reset()V", "drawChar(CIII)V", "drawImage(Lorg/kwis/msp/lcdui/Image;IIIIIII)V",
	} {
		method, ok := declared[GraphicsClass+"."+member]
		if !ok {
			t.Errorf("Graphics.%s is not declared", member)
			continue
		}
		if method.Body == nil || method.Access&jvm.AccessStatic != 0 || method.Access&jvm.AccessNative != 0 {
			t.Errorf("Graphics.%s is not an instance member with a body", member)
		}
	}
	// KTF answers getGrayScale with the packed colour and the specification
	// with a grey level, and the byte form of getPixels and setPixels is the
	// handset's own; until a title settles them, they are not served.
	for _, member := range []string{"getGrayScale()I", "getPixels(IIII[BII)V", "setPixels(IIII[BII)V"} {
		if _, ok := declared[GraphicsClass+"."+member]; ok {
			t.Errorf("Graphics.%s is declared", member)
		}
	}
	copied, ok := declared[ImageClass+".createImage(Lorg/kwis/msp/lcdui/Image;)Lorg/kwis/msp/lcdui/Image;"]
	if !ok || copied.Body == nil || copied.Access&jvm.AccessStatic == 0 {
		t.Error("Image.createImage(Image) is not a static member with a body")
	}
	if image.Name == "" {
		t.Fatal("no Image definition")
	}
	constants := map[string]int32{}
	for _, field := range graphics.Fields {
		value, _ := field.Constant.Int32()
		constants[field.Name] = value
	}
	if constants["SOLID"] != StrokeSolid || constants["DOTTED"] != StrokeDotted || len(constants) != 2 {
		t.Errorf("Graphics stroke constants = %v", constants)
	}
}

// A Graphics no member has touched, and one a quick save restored without the
// two states, answers XOR off and the solid stroke. The stroke style is kept
// as given, as KTF keeps it.
func TestXORModeAndStrokeStyleAreKeptInTheObject(t *testing.T) {
	platform := newTestPlatform(t)
	bare := &jvm.Object{ClassName: GraphicsClass}
	if got := platform.mustInt(bare, "isXORMode", "()Z"); got != 0 {
		t.Errorf("isXORMode() of a bare Graphics = %d", got)
	}
	if got := platform.mustInt(bare, "getStrokeStyle", "()I"); got != StrokeSolid {
		t.Errorf("getStrokeStyle() of a bare Graphics = %d", got)
	}
	graphics, _ := platform.screen(4, 4)
	platform.must(graphics, "setXORMode", "(Z)V", jvm.IntValue(1))
	platform.must(graphics, "setStrokeStyle", "(I)V", jvm.IntValue(StrokeDotted))
	if platform.mustInt(graphics, "isXORMode", "()Z") != 1 || platform.mustInt(graphics, "getStrokeStyle", "()I") != StrokeDotted {
		t.Fatal("the states were not kept")
	}
	platform.must(graphics, "setStrokeStyle", "(I)V", jvm.IntValue(42))
	if got := platform.mustInt(graphics, "getStrokeStyle", "()I"); got != 42 {
		t.Errorf("getStrokeStyle() after setStrokeStyle(42) = %d, want it kept", got)
	}
	platform.must(graphics, "setXORMode", "(Z)V", jvm.IntValue(0))
	if got := platform.mustInt(graphics, "isXORMode", "()Z"); got != 0 {
		t.Error("setXORMode(false) left the mode on")
	}
}

// The states are fields of the object, so the JVM's own heap capture keeps
// them, and a heap written before they existed restores without them.
func TestXORModeAndStrokeStyleSurviveTheHeapCodec(t *testing.T) {
	platform := newTestPlatform(t)
	graphics := &jvm.Object{ClassName: GraphicsClass, Fields: map[string]jvm.Value{}}
	platform.must(graphics, "setXORMode", "(Z)V", jvm.IntValue(1))
	platform.must(graphics, "setStrokeStyle", "(I)V", jvm.IntValue(StrokeDotted))
	saved, err := platform.machine.CaptureHeapState([]*jvm.Object{graphics}, jvm.HeapCodec{})
	if err != nil {
		t.Fatal(err)
	}
	restore := func(saved jvm.HeapState) *jvm.Object {
		t.Helper()
		fresh := newTestPlatform(t)
		roots, err := fresh.machine.RestoreHeapState(saved, jvm.HeapCodec{})
		if err != nil {
			t.Fatal(err)
		}
		if len(roots) != 1 || roots[0].ClassName != GraphicsClass {
			t.Fatalf("restored roots = %v", roots)
		}
		platform = fresh
		return roots[0]
	}
	restored := restore(saved)
	if platform.mustInt(restored, "isXORMode", "()Z") != 1 || platform.mustInt(restored, "getStrokeStyle", "()I") != StrokeDotted {
		t.Fatal("a heap round trip lost the states")
	}
	for index := range saved.Objects {
		saved.Objects[index].Fields = nil
	}
	older := restore(saved)
	if platform.mustInt(older, "isXORMode", "()Z") != 0 || platform.mustInt(older, "getStrokeStyle", "()I") != StrokeSolid {
		t.Fatal("a Graphics restored without the states does not answer the defaults")
	}
}

// The parts of the colour are the colour as set, eight bits each.
func TestColorComponents(t *testing.T) {
	platform := newTestPlatform(t)
	graphics, _ := platform.screen(1, 1)
	for _, want := range []struct {
		color            int32
		red, green, blue int32
	}{
		{0x123456, 0x12, 0x34, 0x56},
		{0xff0000, 0xff, 0, 0},
		{0x00ff00, 0, 0xff, 0},
		{0x0000ff, 0, 0, 0xff},
		{0xffffff, 0xff, 0xff, 0xff},
		{0x000000, 0, 0, 0},
	} {
		platform.must(graphics, "setColor", "(I)V", jvm.IntValue(want.color))
		got := []int32{
			platform.mustInt(graphics, "getRedComponent", "()I"),
			platform.mustInt(graphics, "getGreenComponent", "()I"),
			platform.mustInt(graphics, "getBlueComponent", "()I"),
		}
		if got[0] != want.red || got[1] != want.green || got[2] != want.blue {
			t.Errorf("colour %#06x answers %v, want %v", want.color, got, []int32{want.red, want.green, want.blue})
		}
	}
}

// getPixel reads through the translation and not the clip, and a point off
// the surface is 0. setPixel is a one-pixel fill: clipped, translated, and in
// XOR mode the difference.
func TestPixelReadAndWrite(t *testing.T) {
	platform := newTestPlatform(t)
	graphics, surface := platform.screen(6, 4)
	surface.pixels[2*6+5] = 0x123456
	platform.must(graphics, "translate", "(II)V", values(2, 1)...)
	platform.must(graphics, "setClip", "(IIII)V", values(0, 0, 1, 1)...)
	if got := platform.mustInt(graphics, "getPixel", "(II)I", values(3, 1)...); got != 0x123456 {
		t.Errorf("getPixel outside the clip = %#06x, want it read", got)
	}
	for _, point := range [][2]int32{{-3, 0}, {0, -2}, {4, 0}, {0, 3}, {-1 << 31, 0}, {1<<31 - 1, 1<<31 - 1}} {
		if got := platform.mustInt(graphics, "getPixel", "(II)I", values(point[0], point[1])...); got != 0 {
			t.Errorf("getPixel%v off the surface = %#06x, want 0", point, got)
		}
	}
	platform.must(graphics, "setColor", "(I)V", jvm.IntValue(0xff0000))
	platform.must(graphics, "setPixel", "(II)V", values(0, 0)...)
	platform.must(graphics, "setPixel", "(II)V", values(1, 0)...)
	if surface.at(2, 1) != 0xff0000 || surface.at(3, 1) != testGround {
		t.Errorf("setPixel inside and outside the clip = %#06x, %#06x", surface.at(2, 1), surface.at(3, 1))
	}
	platform.must(graphics, "setXORMode", "(Z)V", jvm.IntValue(1))
	platform.must(graphics, "setColor", "(I)V", jvm.IntValue(0xffffff))
	platform.must(graphics, "setPixel", "(II)V", values(0, 0)...)
	if surface.at(2, 1) != 0x00ffff {
		t.Errorf("an XOR setPixel = %#06x, want %#06x", surface.at(2, 1), 0x00ffff)
	}
}

// copyArea names the destination first, reads the whole source before it
// writes, moves the source with a clipped destination, leaves a destination
// whose source is left of or above the surface as it was, and in XOR mode
// draws the difference.
func TestCopyArea(t *testing.T) {
	platform := newTestPlatform(t)
	row := func(surface *testSurface, y int) []uint32 {
		return append([]uint32(nil), surface.pixels[y*surface.width:(y+1)*surface.width]...)
	}
	setup := func() (*jvm.Object, *testSurface) {
		graphics, surface := platform.screen(8, 3)
		for index := range surface.pixels {
			surface.pixels[index] = uint32(0x010101 * (index + 1))
		}
		return graphics, surface
	}
	expect := func(t *testing.T, got []uint32, want ...uint32) {
		t.Helper()
		for index := range want {
			if got[index] != want[index] {
				t.Fatalf("row = %#06x, want %#06x", got, want)
			}
		}
	}
	t.Run("destination first, overlapping to the right", func(t *testing.T) {
		graphics, surface := setup()
		before := row(surface, 0)
		platform.must(graphics, "copyArea", "(IIIIII)V", values(2, 0, 0, 0, 5, 1)...)
		expect(t, row(surface, 0), before[0], before[1], before[0], before[1], before[2], before[3], before[4], before[7])
	})
	t.Run("overlapping to the left and up", func(t *testing.T) {
		graphics, surface := setup()
		first, second, third := row(surface, 0), row(surface, 1), row(surface, 2)
		platform.must(graphics, "copyArea", "(IIIIII)V", values(0, 0, 1, 1, 7, 2)...)
		expect(t, row(surface, 0), second[1], second[2], second[3], second[4], second[5], second[6], second[7], first[7])
		expect(t, row(surface, 1), third[1], third[2], third[3], third[4], third[5], third[6], third[7], second[7])
		expect(t, row(surface, 2), third...)
	})
	t.Run("overlapping downwards", func(t *testing.T) {
		graphics, surface := setup()
		first, second := row(surface, 0), row(surface, 1)
		platform.must(graphics, "copyArea", "(IIIIII)V", values(0, 1, 0, 0, 8, 2)...)
		expect(t, row(surface, 0), first...)
		expect(t, row(surface, 1), first...)
		expect(t, row(surface, 2), second...)
	})
	t.Run("a clipped destination moves its source", func(t *testing.T) {
		graphics, surface := setup()
		before := row(surface, 0)
		platform.must(graphics, "setClip", "(IIII)V", values(3, 0, 5, 3)...)
		platform.must(graphics, "copyArea", "(IIIIII)V", values(-2, 0, 0, 0, 8, 1)...)
		expect(t, row(surface, 0), before[0], before[1], before[2], before[5], before[6], before[7], before[6], before[7])
	})
	t.Run("a source left of the surface is not copied", func(t *testing.T) {
		graphics, surface := setup()
		before := row(surface, 0)
		platform.must(graphics, "translate", "(II)V", values(1, 0)...)
		platform.must(graphics, "copyArea", "(IIIIII)V", values(1, 0, -3, 0, 4, 1)...)
		// Destination device columns 2..5 from source device columns -2..1.
		expect(t, row(surface, 0), before[0], before[1], before[2], before[3], before[0], before[1], before[6], before[7])
	})
	t.Run("XOR mode", func(t *testing.T) {
		graphics, surface := setup()
		before := row(surface, 0)
		platform.must(graphics, "setXORMode", "(Z)V", jvm.IntValue(1))
		platform.must(graphics, "copyArea", "(IIIIII)V", values(4, 0, 0, 0, 2, 1)...)
		expect(t, row(surface, 0), before[0], before[1], before[2], before[3], before[4]^before[0], before[5]^before[1], before[6], before[7])
	})
	t.Run("an empty size", func(t *testing.T) {
		graphics, surface := setup()
		before := row(surface, 0)
		for _, size := range [][2]int32{{0, 1}, {1, 0}, {-5, 3}} {
			platform.must(graphics, "copyArea", "(IIIIII)V", values(1, 0, 0, 0, size[0], size[1])...)
		}
		platform.must(graphics, "copyArea", "(IIIIII)V", values(1<<31-1, 1<<31-1, -1<<31, -1<<31, 1<<31-1, 1<<31-1)...)
		expect(t, row(surface, 0), before...)
	})
}

// drawPolygon draws a line from each vertex to the next and back to the
// first, over the vertices both arrays hold.
func TestDrawPolygon(t *testing.T) {
	platform := newTestPlatform(t)
	graphics, surface := platform.screen(8, 8)
	platform.must(graphics, "setColor", "(I)V", jvm.IntValue(0xffff00))
	platform.must(graphics, "drawPolygon", "([I[I)V", jvm.ReferenceValue(platform.intArray(1, 6, 1, 99)), jvm.ReferenceValue(platform.intArray(1, 1, 6)))
	for _, point := range [][2]int{{1, 1}, {4, 1}, {6, 1}, {1, 4}, {1, 6}, {3, 4}} {
		if surface.at(point[0], point[1]) != 0xffff00 {
			t.Errorf("the outline misses %v", point)
		}
	}
	if surface.at(2, 3) != testGround || surface.at(7, 7) != testGround {
		t.Error("the outline drew inside the polygon or past it")
	}
	_, err := platform.call(graphics, "drawPolygon", "([I[I)V", jvm.ReferenceValue(nil), jvm.ReferenceValue(platform.intArray(1, 2)))
	expectThrow(t, platform.machine, err, "java/lang/NullPointerException")
	_, err = platform.call(graphics, "drawPolygon", "([I[I)V", jvm.ReferenceValue(platform.intArray(1, 2)), jvm.ReferenceValue(nil))
	expectThrow(t, platform.machine, err, "java/lang/NullPointerException")
	fresh, untouched := platform.screen(4, 4)
	platform.must(fresh, "drawPolygon", "([I[I)V", jvm.ReferenceValue(platform.intArray(1)), jvm.ReferenceValue(platform.intArray(1)))
	platform.must(fresh, "drawPolygon", "([I[I)V", jvm.ReferenceValue(platform.intArray()), jvm.ReferenceValue(platform.intArray(1, 2)))
	for _, pixel := range untouched.pixels {
		if pixel != testGround {
			t.Fatal("a polygon of fewer than two vertices drew")
		}
	}
}

// testDraw is one drawing call a WIPI title can make, with arguments that
// reach past the clip the tests set.
type testDraw struct {
	name, descriptor string
	// midp is the MIDP call the WIPI one forwards to, or empty for a member
	// MIDP does not have.
	midp      string
	arguments func() []jvm.Value
}

const testInk = 0x35a0f0

func testDraws(platform *testPlatform) []testDraw {
	text := func() jvm.Value { return jvm.ReferenceValue(platform.machine.NewString("abcd")) }
	chars := func() jvm.Value {
		array, _ := platform.machine.NewArray(jvm.Type{Kind: jvm.TypeChar}, 4)
		return jvm.ReferenceValue(array)
	}
	picture := func() jvm.Value {
		surface := newTestSurface(3, 2, false)
		for index := range surface.pixels {
			surface.pixels[index] = testInk
		}
		surface.alpha[0] = 0
		return jvm.ReferenceValue(platform.newImage(surface))
	}
	return []testDraw{
		{"fillRect", "(IIII)V", "(IIII)V", func() []jvm.Value { return values(-2, 1, 5, 3) }},
		{"drawLine", "(IIII)V", "(IIII)V", func() []jvm.Value { return values(9, 0, 0, 7) }},
		{"drawRect", "(IIII)V", "(IIII)V", func() []jvm.Value { return values(1, 1, 5, 4) }},
		{"fillArc", "(IIIIII)V", "(IIIIII)V", func() []jvm.Value { return values(2, 2, 4, 3, 0, 360) }},
		{"drawArc", "(IIIIII)V", "(IIIIII)V", func() []jvm.Value { return values(0, 1, 6, 4, 0, 360) }},
		{"fillRoundRect", "(IIIIII)V", "(IIIIII)V", func() []jvm.Value { return values(3, 0, 4, 5, 2, 2) }},
		{"drawRoundRect", "(IIIIII)V", "(IIIIII)V", func() []jvm.Value { return values(1, 2, 6, 5, 2, 2) }},
		{"drawString", "(Ljava/lang/String;III)V", "(Ljava/lang/String;III)V", func() []jvm.Value {
			return []jvm.Value{text(), jvm.IntValue(8), jvm.IntValue(5), jvm.IntValue(32 | 8)}
		}},
		{"drawSubstring", "(Ljava/lang/String;IIIII)V", "(Ljava/lang/String;IIIII)V", func() []jvm.Value {
			return append([]jvm.Value{text()}, values(1, 2, 4, 4, 64|1)...)
		}},
		{"drawChars", "([CIIIII)V", "([CIIIII)V", func() []jvm.Value { return append([]jvm.Value{chars()}, values(0, 3, -1, 2, 16|4)...) }},
		{"drawPolygon", "([I[I)V", "", func() []jvm.Value {
			return []jvm.Value{jvm.ReferenceValue(platform.intArray(0, 7, 3)), jvm.ReferenceValue(platform.intArray(0, 2, 6))}
		}},
		{"drawImage", "(Lorg/kwis/msp/lcdui/Image;III)V", "(Ljavax/microedition/lcdui/Image;III)V", func() []jvm.Value {
			return append([]jvm.Value{picture()}, values(4, 3, 0)...)
		}},
		{"drawChar", "(CIII)V", "(CIII)V", func() []jvm.Value { return values('W', 6, 6, 32|1) }},
		// The blit with a source rectangle forwards to drawRegion, which
		// TestDrawImageWithASourceRectangleIsDrawRegion compares it with.
		{"drawImage", "(Lorg/kwis/msp/lcdui/Image;IIIIIII)V", "", func() []jvm.Value {
			return append([]jvm.Value{picture()}, values(-1, 4, 0, 0, 3, 2, 0)...)
		}},
	}
}

// testDrawLabel names a draw's subtest: the method, and for the second form
// of a name, the arguments it takes.
func testDrawLabel(draw testDraw) string {
	if draw.descriptor == "(Lorg/kwis/msp/lcdui/Image;IIIIIII)V" {
		return "drawImage with a source rectangle"
	}
	return draw.name
}

// drawTestScene makes one call on a new ground-filled Graphics translated
// and clipped the same way every time, and answers the surface.
func drawTestScene(t *testing.T, platform *testPlatform, xor bool, draw func(*jvm.Object) error) *testSurface {
	t.Helper()
	graphics, surface := platform.screen(9, 7)
	platform.must(graphics, "translate", "(II)V", values(1, -1)...)
	platform.must(graphics, "setClip", "(IIII)V", values(-1, 1, 8, 6)...)
	platform.must(graphics, "setColor", "(I)V", jvm.IntValue(testInk))
	platform.must(graphics, "setXORMode", "(Z)V", jvm.IntValue(boolInt(xor)))
	if err := draw(graphics); err != nil {
		t.Fatal(err)
	}
	return surface
}

// Outside XOR mode a WIPI drawing call MIDP also has is MIDP's call: a title
// that never sets the mode draws exactly what it drew before.
func TestDrawingOutsideXORModeIsMIDPs(t *testing.T) {
	platform := newTestPlatform(t)
	for _, draw := range testDraws(platform) {
		if draw.midp == "" {
			continue
		}
		t.Run(testDrawLabel(draw), func(t *testing.T) {
			direct := drawTestScene(t, platform, false, func(graphics *jvm.Object) error {
				_, err := platform.machine.InvokeSpecial(graphics, midp.GraphicsClass, draw.name, draw.midp, draw.arguments()...)
				return err
			})
			layered := drawTestScene(t, platform, false, func(graphics *jvm.Object) error {
				_, err := platform.call(graphics, draw.name, draw.descriptor, draw.arguments()...)
				return err
			})
			changed := false
			for index := range direct.pixels {
				if layered.pixels[index] != direct.pixels[index] {
					t.Fatalf("pixel (%d, %d) = %#06x, MIDP's call draws %#06x", index%9, index/9, layered.pixels[index], direct.pixels[index])
				}
				changed = changed || direct.pixels[index] != testGround
			}
			if !changed {
				t.Fatal("the call drew nothing to compare")
			}
		})
	}
}

// Every drawing call in XOR mode inverts exactly the pixels the same call
// draws outside it, once each — the corners an outline draws twice and the
// vertices two edges share included — and a second pass puts the ground back.
func TestXORModeInvertsWhatEachCallDraws(t *testing.T) {
	platform := newTestPlatform(t)
	for _, draw := range testDraws(platform) {
		t.Run(testDrawLabel(draw), func(t *testing.T) {
			call := func(graphics *jvm.Object) error {
				_, err := platform.call(graphics, draw.name, draw.descriptor, draw.arguments()...)
				return err
			}
			reference := drawTestScene(t, platform, false, call)
			var graphics *jvm.Object
			surface := drawTestScene(t, platform, true, func(target *jvm.Object) error {
				graphics = target
				return call(target)
			})
			drawn := 0
			for index, plain := range reference.pixels {
				want := uint32(testGround)
				if plain != testGround {
					want = testGround ^ testInk
					drawn++
				}
				if surface.pixels[index] != want {
					t.Fatalf("pixel (%d, %d) = %#06x, want %#06x", index%9, index/9, surface.pixels[index], want)
				}
			}
			if drawn == 0 {
				t.Fatal("the call drew nothing to compare")
			}
			if err := call(graphics); err != nil {
				t.Fatal(err)
			}
			for index, pixel := range surface.pixels {
				if pixel != testGround {
					t.Fatalf("a second XOR pass left (%d, %d) at %#06x", index%9, index/9, pixel)
				}
			}
		})
	}
}

// XOR with black changes nothing, which is what a title's paired XOR fills
// rely on; outside XOR mode the same fill paints black.
func TestXORWithBlackIsTheIdentity(t *testing.T) {
	platform := newTestPlatform(t)
	graphics, surface := platform.screen(3, 3)
	platform.must(graphics, "setColor", "(I)V", jvm.IntValue(0))
	platform.must(graphics, "setXORMode", "(Z)V", jvm.IntValue(1))
	platform.must(graphics, "fillRect", "(IIII)V", values(0, 0, 3, 3)...)
	platform.must(graphics, "drawLine", "(IIII)V", values(0, 0, 2, 2)...)
	for _, pixel := range surface.pixels {
		if pixel != testGround {
			t.Fatal("an XOR draw in black changed a pixel")
		}
	}
	platform.must(graphics, "setXORMode", "(Z)V", jvm.IntValue(0))
	platform.must(graphics, "fillRect", "(IIII)V", values(0, 0, 3, 3)...)
	if surface.at(1, 1) != 0 {
		t.Fatal("leaving XOR mode did not make the fill opaque")
	}
}

// An XOR call that cannot reach the clip still has its arguments checked by
// the platform, so a refused anchor or a missing picture is refused in XOR
// mode as it is outside it.
func TestXOROutsideTheClipStillChecksTheCall(t *testing.T) {
	platform := newTestPlatform(t)
	graphics, surface := platform.screen(4, 4)
	platform.must(graphics, "setXORMode", "(Z)V", jvm.IntValue(1))
	_, err := platform.call(graphics, "drawString", "(Ljava/lang/String;III)V", jvm.ReferenceValue(platform.machine.NewString("x")), jvm.IntValue(500), jvm.IntValue(500), jvm.IntValue(2))
	expectThrow(t, platform.machine, err, "java/lang/IllegalArgumentException")
	_, err = platform.call(graphics, "drawString", "(Ljava/lang/String;III)V", jvm.ReferenceValue(nil), jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(0))
	expectThrow(t, platform.machine, err, "java/lang/NullPointerException")
	_, err = platform.call(graphics, "drawSubstring", "(Ljava/lang/String;IIIII)V", append([]jvm.Value{jvm.ReferenceValue(platform.machine.NewString("ab"))}, values(1, 5, 0, 0, 0)...)...)
	expectThrow(t, platform.machine, err, "java/lang/StringIndexOutOfBoundsException")
	_, err = platform.call(graphics, "drawImage", "(Lorg/kwis/msp/lcdui/Image;III)V", append([]jvm.Value{jvm.ReferenceValue(nil)}, values(0, 0, 0)...)...)
	expectThrow(t, platform.machine, err, "java/lang/NullPointerException")
	for _, pixel := range surface.pixels {
		if pixel != testGround {
			t.Fatal("a refused XOR call drew")
		}
	}
}

// A picture in XOR mode inverts what it draws by how much of each pixel it
// draws: all of an opaque pixel, none of a transparent one, and half of a
// half-drawn one, read back over two grounds.
func TestXORPictureInvertsByCoverage(t *testing.T) {
	platform := newTestPlatform(t)
	graphics, surface := platform.screen(3, 1)
	source := newTestSurface(3, 1, false)
	source.pixels = []uint32{0x112233, 0x4080ff, 0xff2000}
	source.alpha = []uint8{0, 0x80, 0xff}
	platform.must(graphics, "setXORMode", "(Z)V", jvm.IntValue(1))
	platform.must(graphics, "drawImage", "(Lorg/kwis/msp/lcdui/Image;III)V", append([]jvm.Value{jvm.ReferenceValue(platform.newImage(source))}, values(0, 0, 16|4)...)...)
	if surface.at(0, 0) != testGround {
		t.Errorf("a transparent pixel in XOR mode = %#06x, want the ground", surface.at(0, 0))
	}
	if surface.at(2, 0) != testGround^0xff2000 {
		t.Errorf("an opaque pixel in XOR mode = %#06x, want %#06x", surface.at(2, 0), testGround^0xff2000)
	}
	want := xorBlend(testGround, 0x4080ff, 0x80)
	if got := surface.at(1, 0); !closeRGB(got, want, 2) {
		t.Errorf("a half-drawn pixel in XOR mode = %#06x, want about %#06x", got, want)
	}
}

func closeRGB(a, b uint32, slack int) bool {
	for shift := uint(0); shift <= 16; shift += 8 {
		difference := int(a>>shift&0xff) - int(b>>shift&0xff)
		if difference > slack || -difference > slack {
			return false
		}
	}
	return true
}

// coverage recovers a blended pixel's colour and strength from the two grounds
// it was drawn over, for every strength the blend can draw.
func TestCoverageRecoversTheBlend(t *testing.T) {
	for _, color := range []uint32{0x000000, 0xffffff, 0x4080ff, 0x123456} {
		for alpha := uint32(1); alpha <= 0xff; alpha++ {
			source, strength, covered := coverage(testBlend(color, 0, alpha), testBlend(color, 0xffffff, alpha))
			if !covered || strength != alpha {
				t.Fatalf("colour %#06x at %d reads as %v at %d", color, alpha, covered, strength)
			}
			if !closeRGB(source, color, int(0xff/alpha)+1) {
				t.Fatalf("colour %#06x at %d reads back as %#06x", color, alpha, source)
			}
		}
	}
	if _, _, covered := coverage(0x000000, 0xffffff); covered {
		t.Error("a pixel nothing drew reads as covered")
	}
	if _, _, covered := coverage(0x101010, 0x0f0f0f); covered {
		t.Error("grounds read the wrong way round read as covered")
	}
	if got := xorBlend(0x2040c0, 0xffffff, 0xff); got != 0xdfbf3f {
		t.Errorf("a full XOR blend = %#06x", got)
	}
}

// The WIPI copy of an image is a new mutable image of the same size with the
// source drawn in: what the source leaves undrawn is white and the copy keeps
// no transparency.
func TestImageCopyIsMutableAndWhiteUnderTheMask(t *testing.T) {
	platform := newTestPlatform(t)
	source := newTestSurface(3, 1, false)
	source.pixels = []uint32{0x112233, 0x445566, 0x778899}
	source.alpha = []uint8{0, 0xff, 0xff}
	value, err := platform.machine.InvokeStatic(ImageClass, "createImage", "(Lorg/kwis/msp/lcdui/Image;)Lorg/kwis/msp/lcdui/Image;", jvm.ReferenceValue(platform.newImage(source)))
	if err != nil {
		t.Fatal(err)
	}
	copied, _ := value.Reference()
	surface := copied.Native.(*testSurface)
	if !surface.mutable || surface.width != 3 || surface.height != 1 || copied.ClassName != ImageClass {
		t.Fatalf("the copy is %+v of %s", surface, copied.ClassName)
	}
	for x, want := range []uint32{0xffffff, 0x445566, 0x778899} {
		if surface.at(x, 0) != want || surface.alpha[x] != 0xff {
			t.Errorf("copy pixel %d = %#06x alpha %d, want %#06x opaque", x, surface.at(x, 0), surface.alpha[x], want)
		}
	}
	if source.at(0, 0) != 0x112233 {
		t.Error("the copy changed its source")
	}
	_, err = platform.machine.InvokeStatic(ImageClass, "createImage", "(Lorg/kwis/msp/lcdui/Image;)Lorg/kwis/msp/lcdui/Image;", jvm.ReferenceValue(nil))
	expectThrow(t, platform.machine, err, "java/lang/NullPointerException")
}

// reset is the platform's — the clip, the translation and the font put back —
// and puts back XOR mode and the stroke style as well, as KTF's reset does. A
// Graphics that never had either state is not given them.
func TestResetPutsBackXORModeAndStrokeStyle(t *testing.T) {
	platform := newTestPlatform(t)
	graphics, surface := platform.screen(4, 4)
	platform.must(graphics, "translate", "(II)V", values(2, 1)...)
	platform.must(graphics, "setClip", "(IIII)V", values(0, 0, 1, 1)...)
	platform.must(graphics, "setXORMode", "(Z)V", jvm.IntValue(1))
	platform.must(graphics, "setStrokeStyle", "(I)V", jvm.IntValue(StrokeDotted))
	platform.must(graphics, "reset", "()V")
	if got := platform.mustInt(graphics, "isXORMode", "()Z"); got != 0 {
		t.Error("reset left XOR mode on")
	}
	if got := platform.mustInt(graphics, "getStrokeStyle", "()I"); got != StrokeSolid {
		t.Errorf("reset left the stroke style at %d", got)
	}
	if got := platform.mustInt(graphics, "getTranslateX", "()I"); got != 0 {
		t.Errorf("the platform's reset did not run: the translation is %d", got)
	}
	platform.must(graphics, "setColor", "(I)V", jvm.IntValue(0xffffff))
	platform.must(graphics, "fillRect", "(IIII)V", values(0, 0, 4, 4)...)
	for index, pixel := range surface.pixels {
		if pixel != 0xffffff {
			t.Fatalf("a fill after reset left (%d, %d) at %#06x, want the whole surface white", index%4, index/4, pixel)
		}
	}

	untouched, _ := platform.screen(2, 2)
	platform.must(untouched, "reset", "()V")
	for _, field := range []string{xorModeField, strokeStyleField} {
		if _, ok := untouched.FieldValue(field); ok {
			t.Errorf("reset wrote %s into a Graphics that never had it", field)
		}
	}
}

// The WIPI blit with a source rectangle is MIDP's drawRegion with the two
// halves swapped and no transform, and its anchor is not read. In XOR mode it
// inverts what that draws, and a picture drawn onto itself or a rectangle
// outside the picture is refused in XOR mode as it is outside it.
func TestDrawImageWithASourceRectangleIsDrawRegion(t *testing.T) {
	platform := newTestPlatform(t)
	source := newTestSurface(4, 3, false)
	for index := range source.pixels {
		source.pixels[index] = uint32(0x102030 * (index + 1))
	}
	source.alpha[5] = 0
	picture := jvm.ReferenceValue(platform.newImage(source))
	direct := drawTestScene(t, platform, false, func(graphics *jvm.Object) error {
		_, err := platform.machine.InvokeSpecial(graphics, midp.GraphicsClass, "drawRegion", "(Ljavax/microedition/lcdui/Image;IIIIIIII)V",
			append([]jvm.Value{picture}, values(1, 1, 3, 2, 0, 2, 3, 0)...)...)
		return err
	})
	layered := drawTestScene(t, platform, false, func(graphics *jvm.Object) error {
		// HCENTER | VCENTER, which a drawRegion would read and this does not.
		_, err := platform.call(graphics, "drawImage", "(Lorg/kwis/msp/lcdui/Image;IIIIIII)V",
			append([]jvm.Value{picture}, values(2, 3, 1, 1, 3, 2, 1|2)...)...)
		return err
	})
	changed := false
	for index := range direct.pixels {
		if layered.pixels[index] != direct.pixels[index] {
			t.Fatalf("pixel (%d, %d) = %#06x, drawRegion draws %#06x", index%9, index/9, layered.pixels[index], direct.pixels[index])
		}
		changed = changed || direct.pixels[index] != testGround
	}
	if !changed {
		t.Fatal("the region drew nothing to compare")
	}

	for _, xor := range []bool{false, true} {
		own := newTestSurface(4, 3, true)
		graphics := platform.newGraphics(own)
		platform.must(graphics, "setXORMode", "(Z)V", jvm.IntValue(boolInt(xor)))
		_, err := platform.call(graphics, "drawImage", "(Lorg/kwis/msp/lcdui/Image;IIIIIII)V",
			append([]jvm.Value{jvm.ReferenceValue(platform.newImage(own))}, values(0, 0, 0, 0, 2, 2, 0)...)...)
		expectThrow(t, platform.machine, err, "java/lang/IllegalArgumentException")
		_, err = platform.call(graphics, "drawImage", "(Lorg/kwis/msp/lcdui/Image;IIIIIII)V",
			append([]jvm.Value{picture}, values(0, 0, 2, 2, 3, 3, 0)...)...)
		expectThrow(t, platform.machine, err, "java/lang/IllegalArgumentException")
		for _, pixel := range own.pixels {
			if pixel != 0xffffff {
				t.Fatalf("a refused blit drew (XOR mode %v)", xor)
			}
		}
	}
}
