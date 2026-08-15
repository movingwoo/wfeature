package hqx

import (
	"bytes"
	"encoding/binary"
	"math/rand"
	"os"
	"os/exec"
	"testing"
)

// The decision table in pattern*.go is 8,000 lines mechanically translated
// from the reference implementation. "It compiles and all 256 patterns are
// present" is a structural check, not a correctness one — a case routed to the
// wrong blend would pass it. So when the reference is available, this compares
// the two implementations pixel for pixel on images chosen to hit every case.
//
//	WFEATURE_HQX_REFERENCE=/path/to/hqxref go test ./internal/filter/hqx
//
// The harness reads width, height, and scale as arguments and streams
// little-endian 0xAARRGGBB pixels through stdin and stdout.
func TestMatchesReferenceImplementation(t *testing.T) {
	reference := os.Getenv("WFEATURE_HQX_REFERENCE")
	if reference == "" {
		t.Skip("set WFEATURE_HQX_REFERENCE to a reference hqx binary to compare against")
	}

	for _, image := range referenceImages(t) {
		for _, factor := range []int{2, 3, 4} {
			mine, err := Scale(image.pixels, image.width, image.height, factor)
			if err != nil {
				t.Fatalf("%s hq%dx: %v", image.name, factor, err)
			}
			theirs, err := runReference(reference, image, factor)
			if err != nil {
				t.Fatalf("%s hq%dx reference: %v", image.name, factor, err)
			}
			if len(mine) != len(theirs) {
				t.Fatalf("%s hq%dx: %d pixels, reference produced %d", image.name, factor, len(mine), len(theirs))
			}
			for index := range mine {
				if mine[index] != theirs[index] {
					row, column := index/(image.width*factor), index%(image.width*factor)
					t.Fatalf("%s hq%dx: pixel (%d,%d) is %#08x, reference says %#08x",
						image.name, factor, column, row, mine[index], theirs[index])
				}
			}
		}
	}
}

type referenceImage struct {
	name          string
	width, height int
	pixels        []uint32
}

// referenceImages are chosen to exercise the table rather than to look like
// anything: noise over a small palette makes every neighbour combination
// likely, and the structured ones cover the cases noise reaches rarely.
func referenceImages(t *testing.T) []referenceImage {
	t.Helper()
	var images []referenceImage

	// Two-color noise: every pattern is reachable and each is hit often.
	random := rand.New(rand.NewSource(1))
	twoColor := make([]uint32, 64*64)
	for index := range twoColor {
		twoColor[index] = 0xff000000
		if random.Intn(2) == 0 {
			twoColor[index] = 0xffffffff
		}
	}
	images = append(images, referenceImage{"two-color noise", 64, 64, twoColor})

	// Full-range noise: exercises the interpolation arithmetic, including the
	// alpha channel the masked blends handle separately.
	full := make([]uint32, 48*48)
	for index := range full {
		full[index] = random.Uint32()
	}
	images = append(images, referenceImage{"full-range noise", 48, 48, full})

	// Diagonals and corners, which is what the filter exists for.
	diagonal := make([]uint32, 32*32)
	for row := 0; row < 32; row++ {
		for column := 0; column < 32; column++ {
			color := uint32(0xff102030)
			if (row+column)%7 < 3 {
				color = 0xffe0d0c0
			}
			diagonal[row*32+column] = color
		}
	}
	images = append(images, referenceImage{"diagonals", 32, 32, diagonal})

	// A single pixel, and a single row, to pin the edge clamping.
	images = append(images, referenceImage{"one pixel", 1, 1, []uint32{0xff123456}})
	images = append(images, referenceImage{"one row", 5, 1, []uint32{0xff000000, 0xffffffff, 0xff000000, 0xffff0000, 0xff00ff00}})

	return images
}

func runReference(referencePath string, image referenceImage, factor int) ([]uint32, error) {
	input := new(bytes.Buffer)
	for _, pixel := range image.pixels {
		_ = binary.Write(input, binary.LittleEndian, pixel)
	}
	command := exec.Command(referencePath, itoa(image.width), itoa(image.height), itoa(factor))
	command.Stdin = input
	output, err := command.Output()
	if err != nil {
		return nil, err
	}
	pixels := make([]uint32, len(output)/4)
	for index := range pixels {
		pixels[index] = binary.LittleEndian.Uint32(output[index*4:])
	}
	return pixels, nil
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}
