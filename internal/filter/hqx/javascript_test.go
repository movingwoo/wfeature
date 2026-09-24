package hqx

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The page magnifies pictures itself, with these same decision tables, so
// web/hqx-patterns.js is generated from pattern2x.go, pattern3x.go and
// pattern4x.go. This test regenerates it in memory and fails when the file
// differs; WFEATURE_WRITE_HQX_JS=1 writes it instead.
const javaScriptPatterns = "../../../web/hqx-patterns.js"

const javaScriptHeader = `// Copyright (c) 2017 Christopher Serr. Licensed under MIT OR Apache-2.0.
// This file is a mechanical translation of that work's decision tables, made
// from internal/filter/hqx by TestJavaScriptPatternsMatchTheGoTables; see
// THIRD-PARTY-NOTICES.md. Regenerate it rather than editing it.
//
// Each case of a Go table is a function here, and the table is an array of
// them indexed by pattern. One function holding all 256 cases was optimised
// before most cases had run and thrown away each time a new one did.
//
// w holds the pixel and its eight neighbours and n their converted colours,
// so a difference test reads n where the Go tables convert w again.

import {
  yuvDiff, interp1, interp2, interp3, interp4, interp5, interp6, interp7, interp8, interp9, interp10,
} from "./hqx-blend.js";
`

// difference matches the one test the tables make between two neighbours.
var difference = regexp.MustCompile(`^if diff\(w\[(\d)\], w\[(\d)\]\) \{$`)

// translatePatterns turns one Go decision table into JavaScript. The tables
// use a handful of statement shapes and nothing else, and anything outside
// them fails the translation rather than being guessed at.
func translatePatterns(source string) (string, error) {
	var out strings.Builder
	name, cases := "", 0
	var assignments []string
	inFunction := false
	for number, line := range strings.Split(source, "\n") {
		trimmed := strings.TrimSpace(line)
		if !inFunction {
			if declared, ok := strings.CutPrefix(trimmed, "func "); ok {
				name, _, _ = strings.Cut(declared, "(")
				name = strings.TrimSuffix(name, "Pattern")
				inFunction = true
			}
			continue
		}
		depth := len(line) - len(strings.TrimLeft(line, "\t"))
		indent := strings.Repeat("  ", max(depth-1, 0))
		switch {
		case trimmed == "switch pattern {":
		case strings.HasPrefix(trimmed, "case ") && strings.HasSuffix(trimmed, ":"):
			if cases > 0 {
				out.WriteString("};\n")
			}
			function := fmt.Sprintf("%sCase%d", name, cases)
			cases++
			fmt.Fprintf(&out, "\nconst %s = (dst, dstIndex, dstRowElements, w, n) => {\n", function)
			for _, value := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(trimmed, "case "), ":"), ", ") {
				assignments = append(assignments, fmt.Sprintf("%sPatterns[%s] = %s;", name, value, function))
			}
		case difference.MatchString(trimmed):
			pair := difference.FindStringSubmatch(trimmed)
			out.WriteString(indent + "if (yuvDiff(n[" + pair[1] + "], n[" + pair[2] + "])) {\n")
		case trimmed == "} else {":
			out.WriteString(indent + trimmed + "\n")
		case trimmed == "}" && depth == 1:
			// The end of the switch ends its last case.
			out.WriteString("};\n")
		case trimmed == "}" && depth == 0:
			if len(assignments) != 256 {
				return "", fmt.Errorf("the table assigns %d patterns, not 256", len(assignments))
			}
			fmt.Fprintf(&out, "\nexport const %sPatterns = new Array(256);\n", name)
			out.WriteString(strings.Join(assignments, "\n") + "\n")
			return out.String(), nil
		case trimmed == "}":
			out.WriteString(indent + "}\n")
		case strings.HasPrefix(trimmed, "dst[") && !strings.HasSuffix(trimmed, ";"):
			out.WriteString(indent + trimmed + ";\n")
		default:
			return "", fmt.Errorf("line %d: no translation for %q", number+1, trimmed)
		}
	}
	return "", fmt.Errorf("the table never closed")
}

func generatedJavaScript(t *testing.T) string {
	t.Helper()
	generated := javaScriptHeader
	for _, factor := range []int{2, 3, 4} {
		source, err := os.ReadFile(fmt.Sprintf("pattern%dx.go", factor))
		if err != nil {
			t.Fatal(err)
		}
		table, err := translatePatterns(string(source))
		if err != nil {
			t.Fatalf("pattern%dx.go: %v", factor, err)
		}
		generated += table
	}
	return generated
}

func TestJavaScriptPatternsMatchTheGoTables(t *testing.T) {
	generated := generatedJavaScript(t)
	if os.Getenv("WFEATURE_WRITE_HQX_JS") == "1" {
		if err := os.WriteFile(filepath.FromSlash(javaScriptPatterns), []byte(generated), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	committed, err := os.ReadFile(filepath.FromSlash(javaScriptPatterns))
	if err != nil {
		t.Fatal(err)
	}
	if string(committed) != generated {
		t.Fatal("web/hqx-patterns.js differs from the Go tables; regenerate it with WFEATURE_WRITE_HQX_JS=1")
	}
}

// sharedFixture is a picture both implementations magnify and digest; web/
// hqx.test.mjs builds the same one. The top half holds every neighbour
// pattern twice — once against a single other colour, once against two that
// differ from each other and beside colours only similar to the centre — and
// the bottom half is seeded noise with runs in it.
//
// Every colour here has luma and chroma well away from a whole number. The
// conversion is floating point, and a machine that fuses multiply-adds rounds
// a value sitting on a boundary differently from one that does not.
func sharedFixture() ([]byte, int, int) {
	const width, height = 96, 96
	palette := [][3]byte{
		{18, 38, 200}, {200, 60, 30}, {60, 200, 90}, {24, 44, 203},
		{203, 63, 33}, {250, 240, 20}, {120, 120, 121}, {10, 10, 11},
	}
	const centre, other, second, near = 0, 1, 2, 3
	pixels := make([]byte, width*height*4)
	set := func(x, y, colour int) {
		copy(pixels[(y*width+x)*4:], palette[colour][:])
		pixels[(y*width+x)*4+3] = 255
	}
	neighbours := [8][2]int{{-1, -1}, {0, -1}, {1, -1}, {-1, 0}, {1, 0}, {-1, 1}, {0, 1}, {1, 1}}
	for index := 0; index < 512; index++ {
		pattern, variant := index&255, index>>8
		x, y := index%32*3+1, index/32*3+1
		set(x, y, centre)
		for bit, offset := range neighbours {
			colour := centre
			switch {
			case pattern>>bit&1 == 1 && variant == 1 && bit%2 == 1:
				colour = second
			case pattern>>bit&1 == 1:
				colour = other
			case variant == 1 && bit%3 == 0:
				colour = near
			}
			set(x+offset[0], y+offset[1], colour)
		}
	}
	state := uint32(0x9e3779b9)
	for y := 48; y < height; y++ {
		for x := 0; x < width; x++ {
			state ^= state << 13
			state ^= state >> 17
			state ^= state << 5
			if x > 0 && state>>8%3 == 0 {
				copy(pixels[(y*width+x)*4:], pixels[(y*width+x-1)*4:(y*width+x)*4])
				continue
			}
			set(x, y, int(state%uint32(len(palette))))
		}
	}
	return pixels, width, height
}

// sharedDigests are what both implementations must produce for the shared
// fixture; web/hqx.test.mjs holds the same three.
var sharedDigests = map[int]string{
	2: "8ed8d84b0a10ab0aa89b4165293a18ac2af6fafdfd305af89ceeb1276d130ad8",
	3: "ed10b6149b3a51bb858e349dbaa11a982f7531d3068149bb9df3a59769862819",
	4: "ee08e64dc7bd1a01b48474094442a8f00c18cbcd423734b68b8c30474a8b00e1",
}

func TestTheSharedFixtureDigest(t *testing.T) {
	pixels, width, height := sharedFixture()
	for factor, want := range sharedDigests {
		scaled, _, _, err := ScaleRGBA(pixels, width, height, factor)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(scaled)
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("hq%dx digest %s, want %s", factor, got, want)
		}
	}
}
