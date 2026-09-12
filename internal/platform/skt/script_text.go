package skt

import (
	"bytes"
	"fmt"
	"image"

	"github.com/movingwoo/wfeature/internal/glyph"
	"github.com/movingwoo/wfeature/internal/sgsvm"
	"golang.org/x/text/encoding/korean"
)

func (g *scriptGraphics) textCall(op byte, vm *sgsvm.VM) (bool, error) {
	switch op {
	case 0x66:
		a := vm.Args(4)
		g.textStyle = int(byte(a[0])) % 4
		g.textFG = int(byte(a[1])) % 182
		g.textBG = int(byte(a[2])) % 182
		g.textAlign = int(byte(a[3])) % 3
	case 0x67:
		g.textStyle = int(byte(vm.Pop())) % 4
	case 0x68:
		a := vm.Args(2)
		g.textFG = int(byte(a[0])) % 182
		g.textBG = int(byte(a[1])) % 182
	case 0x69:
		g.textAlign = int(byte(vm.Pop())) % 3
	case 0x6a, 0x6b, 0x6c, 0x6d, 0xcf, 0xd0:
		extended := op == 0xcf || op == 0xd0
		count := 3
		if extended {
			count = 4
		}
		if err := vm.Require(count); err != nil {
			return true, err
		}
		a := vm.Args(count)
		flags := 0
		if extended {
			flags = int(a[3]) & 7
		}
		data := vm.Resource(int(a[2])).Data
		if vm.Error() != nil {
			return true, vm.Error()
		}
		if i := bytes.IndexByte(data, 0); i >= 0 {
			data = data[:i]
		}
		if len(data) == 0 {
			return true, nil
		}
		if len(data) > 4096 {
			return true, fmt.Errorf("SGS text exceeds 4096 bytes")
		}
		decoded, err := korean.EUCKR.NewDecoder().Bytes(data)
		if err != nil {
			return true, err
		}
		style := g.textStyle
		if op == 0x6c || op == 0x6d {
			style = 2
		}
		cellW, cellH := []int{4, 6, 6, 12}[style], []int{6, 8, 12, 24}[style]
		x, y := int(a[0]), int(a[1])
		width := len(data) * cellW
		if g.textAlign == 1 {
			x -= width / 2
		} else if g.textAlign == 2 {
			x -= width
		}
		fg, err := g.mappedColor(g.textFG)
		if err != nil {
			return true, err
		}
		if extended && style == 3 {
			cursor := x + 1
			for _, r := range string(decoded) {
				w := cellW
				if r > 127 {
					w *= 2
				}
				if _, err := scriptLargeTextColumns(cursor, w/2); err != nil {
					return true, err
				}
				cursor += w
			}
		}
		background := op == 0x6b || op == 0x6d || op == 0xd0
		extension := 0
		if extended {
			extension = flags & 1
			if background && flags&2 != 0 {
				if style == 3 {
					extension += 4
				} else {
					extension += cellH/4 - 1
				}
			}
		}
		if extended {
			work := len(data)*cellW*cellH/8 + len(data) + 1
			if background {
				work += g.width*min(g.height, 25)/16 + 1
			}
			if flags&4 != 0 {
				work += g.width/16 + 1
			}
			if err := vm.ChargeWork(work); err != nil {
				return true, err
			}
		}
		if background {
			if g.textBG != 4 {
				bg, err := g.mappedColor(g.textBG)
				if err != nil {
					return true, err
				}
				right, bottom := x+width+extension, y+cellH
				if extended && style == 3 {
					right++
					bottom++
				}
				r := image.Rect(x-1, y-1, right, bottom).Intersect(g.clip)
				for py := r.Min.Y; py < r.Max.Y; py++ {
					for px := r.Min.X; px < r.Max.X; px++ {
						g.point(px, py, bg)
					}
				}
			}
		}
		if extended && flags&4 != 0 && g.textFG != 4 {
			right := x + width + extension - 1
			if style == 3 {
				right++
			}
			// Bound the underline before drawing, including entirely offscreen text.
			left, end := max(x-1, g.clip.Min.X), min(right, g.clip.Max.X-1)
			if end >= left {
				g.line(left, y+cellH, end, y+cellH, fg)
			}
		}
		// The large style doubles the ordinary 12-pixel grid. Retain the
		// substitute face's native strokes, bearings and baseline rather than
		// stretching each character's ink bounds into its entire advance cell.
		scale := 1
		if style == 3 {
			scale = 2
			x++
			y++
		}
		for _, r := range string(decoded) {
			w := cellW
			if r > 127 {
				w *= 2
			}
			if !extended {
				if err := vm.ChargeWork(w*cellH/16 + 1); err != nil {
					return true, err
				}
			}
			if g.textFG != 4 {
				g.textGlyph(r, style, x, y, w/scale, cellH/scale, scale, fg, extended, flags)
			}
			x += w
		}
	default:
		return false, nil
	}
	return true, vm.Error()
}

// textGlyph draws within a fixed device cell. The original small styles have
// Latin-only tables; Korean support begins at style 2. The existing authored
// 5x7 Latin face fits the 6x8 cell, while the handset face fits the 12-pixel grid.
func (g *scriptGraphics) textGlyph(r rune, style, x, y, cellW, cellH, scale int, fg byte, extended bool, flags int) {
	if style < 2 && r > 127 {
		return
	}
	var rows [12]uint16
	if style == 0 {
		if r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		for py, row := range scriptLatin3x5[byte(r)] {
			for px := 0; px < 3; px++ {
				if row&(4>>uint(px)) != 0 {
					rows[py] |= 1 << uint(px)
				}
			}
		}
	} else {
		face := glyph.Handset()
		baseline := face.Ascent
		if style < 2 {
			face = glyph.Default()
			baseline = 7
		}
		bitmap := face.Render(r)
		for row := range bitmap.Rows {
			py := baseline - bitmap.Ascent + row
			if py < 0 || py >= cellH {
				continue
			}
			for px := 0; px < min(bitmap.Width, cellW); px++ {
				if bitmap.Coverage(row, px) >= 128 {
					rows[py] |= 1 << uint(px)
				}
			}
		}
	}
	if extended && style == 3 {
		columns, _ := scriptLargeTextColumns(x, cellW) // Preflighted before any drawing.
		for row := 0; row < cellH-1; row++ {
			shift := 0
			if flags&2 != 0 {
				shift = max(0, 4-row/2)
			}
			for col := 0; col < columns; col++ {
				bit := row*columns + col
				sourceRow, sourceCol := bit/(cellW-1), bit%(cellW-1)
				if sourceRow >= cellH-1 || rows[sourceRow]&(1<<uint(sourceCol)) == 0 {
					continue
				}
				for dy := 0; dy < 2; dy++ {
					for dx := 0; dx < 2+2*(flags&1); dx++ {
						g.point(x+col*2+shift+dx, y+row*2+dy, fg)
					}
				}
			}
		}
		return
	}
	for py := 0; py < cellH; py++ {
		shift, bold := 0, 0
		if extended {
			bold = flags & 1
			if flags&2 != 0 {
				shift = max(0, cellH/4-1-(py+1)/4)
			}
		}
		for px := 0; px < cellW; px++ {
			if rows[py]&(1<<uint(px)) == 0 {
				continue
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale+bold; dx++ {
					g.point(x+px*scale+shift+dx, y+py*scale+dy, fg)
				}
			}
		}
	}
}

// The extended large-style helper treats width-minus-one as an absolute
// endpoint, then consumes one continuous source bitstream. Preserve that
// observed coordinate behavior, but reject reads beyond the substitute glyph.
func scriptLargeTextColumns(x, cellW int) (int, error) {
	columns := max(0, (cellW-1-x+1)/2)
	sourceBits := ((cellW-1)*11 + 7) / 8 * 8
	if columns*11 > sourceBits {
		return 0, fmt.Errorf("SGS extended large text exceeds glyph bounds")
	}
	return columns, nil
}

// These compact Latin shapes are authored on their own 3x5 grid. Sampling
// alternate columns from a larger pixel font loses complete strokes at this
// size; the 4x6 device cell needs three ink columns and one separating column.
// Lowercase uses the uppercase forms, as in the shared authored Latin face.
var scriptLatin3x5 = map[byte][5]byte{
	' ':  {0, 0, 0, 0, 0},
	'!':  {2, 2, 2, 0, 2},
	'"':  {5, 5, 0, 0, 0},
	'#':  {5, 7, 5, 7, 5},
	'$':  {3, 6, 3, 6, 2},
	'%':  {5, 1, 2, 4, 5},
	'&':  {2, 5, 2, 5, 3},
	'\'': {2, 2, 0, 0, 0},
	'(':  {1, 2, 2, 2, 1},
	')':  {4, 2, 2, 2, 4},
	'*':  {0, 5, 2, 5, 0},
	'+':  {0, 2, 7, 2, 0},
	',':  {0, 0, 0, 2, 4},
	'-':  {0, 0, 7, 0, 0},
	'.':  {0, 0, 0, 0, 2},
	'/':  {1, 1, 2, 4, 4},
	'0':  {7, 5, 5, 5, 7},
	'1':  {2, 6, 2, 2, 7},
	'2':  {6, 1, 7, 4, 7},
	'3':  {6, 1, 3, 1, 6},
	'4':  {5, 5, 7, 1, 1},
	'5':  {7, 4, 6, 1, 6},
	'6':  {3, 4, 7, 5, 7},
	'7':  {7, 1, 2, 2, 2},
	'8':  {7, 5, 7, 5, 7},
	'9':  {7, 5, 7, 1, 6},
	':':  {0, 2, 0, 2, 0},
	';':  {0, 2, 0, 2, 4},
	'<':  {1, 2, 4, 2, 1},
	'=':  {0, 7, 0, 7, 0},
	'>':  {4, 2, 1, 2, 4},
	'?':  {6, 1, 2, 0, 2},
	'@':  {7, 5, 7, 4, 3},
	'A':  {2, 5, 7, 5, 5},
	'B':  {6, 5, 6, 5, 6},
	'C':  {3, 4, 4, 4, 3},
	'D':  {6, 5, 5, 5, 6},
	'E':  {7, 4, 6, 4, 7},
	'F':  {7, 4, 6, 4, 4},
	'G':  {3, 4, 5, 5, 3},
	'H':  {5, 5, 7, 5, 5},
	'I':  {7, 2, 2, 2, 7},
	'J':  {1, 1, 1, 5, 2},
	'K':  {5, 5, 6, 5, 5},
	'L':  {4, 4, 4, 4, 7},
	'M':  {5, 7, 7, 5, 5},
	'N':  {5, 7, 7, 7, 5},
	'O':  {2, 5, 5, 5, 2},
	'P':  {6, 5, 6, 4, 4},
	'Q':  {2, 5, 5, 7, 3},
	'R':  {6, 5, 6, 5, 5},
	'S':  {3, 4, 2, 1, 6},
	'T':  {7, 2, 2, 2, 2},
	'U':  {5, 5, 5, 5, 7},
	'V':  {5, 5, 5, 5, 2},
	'W':  {5, 5, 7, 7, 5},
	'X':  {5, 5, 2, 5, 5},
	'Y':  {5, 5, 2, 2, 2},
	'Z':  {7, 1, 2, 4, 7},
	'[':  {3, 2, 2, 2, 3},
	'\\': {4, 4, 2, 1, 1},
	']':  {6, 2, 2, 2, 6},
	'^':  {2, 5, 0, 0, 0},
	'_':  {0, 0, 0, 0, 7},
	'`':  {4, 2, 0, 0, 0},
	'{':  {3, 2, 6, 2, 3},
	'|':  {2, 2, 2, 2, 2},
	'}':  {6, 2, 3, 2, 6},
	'~':  {0, 3, 6, 0, 0},
}
