package vp8l

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"golang.org/x/image/webp"
)

// roundTrip encodes the picture, decodes it with the reference decoder in
// golang.org/x/image and fails unless every pixel comes back exactly.
func roundTrip(t testing.TB, encoder *Encoder, pixels []uint32, width, height int) []byte {
	t.Helper()
	encoded, err := encoder.Encode(nil, pixels, width, height)
	if err != nil {
		t.Fatalf("%dx%d: %v", width, height, err)
	}
	if string(encoded[0:4]) != "RIFF" || string(encoded[8:16]) != "WEBPVP8L" ||
		int(binary.LittleEndian.Uint32(encoded[4:8])) != len(encoded)-8 || len(encoded)%2 != 0 {
		t.Fatalf("%dx%d: malformed container", width, height)
	}
	decoded, err := webp.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("%dx%d: decode: %v", width, height, err)
	}
	picture, ok := decoded.(*image.NRGBA)
	if !ok || picture.Rect.Dx() != width || picture.Rect.Dy() != height {
		t.Fatalf("%dx%d: decoded %T %v", width, height, decoded, decoded.Bounds())
	}
	for y := range height {
		for x := range width {
			offset := y*picture.Stride + x*4
			got := uint32(picture.Pix[offset+3])<<24 | uint32(picture.Pix[offset])<<16 |
				uint32(picture.Pix[offset+1])<<8 | uint32(picture.Pix[offset+2])
			// A transparent pixel's colour is not kept.
			if want := pixels[y*width+x]; got != want && (want>>24 != 0 || got>>24 != 0) {
				t.Fatalf("%dx%d: pixel (%d, %d) is %08x, want %08x", width, height, x, y, got, want)
			}
		}
	}
	return encoded
}

// encoders answers an encoder with the default settings and ones that take
// the paths the defaults avoid on most pictures: every picture through the
// predictor, and no colour cache and a single short chain.
func encoders() map[string]*Encoder {
	predicted, plain := defaultSettings, defaultSettings
	predicted.predictAbove = 0
	plain.colorCache, plain.chains = false, chains{{span: 3, depth: 2}}
	return map[string]*Encoder{"default": {}, "predicted": {options: &predicted}, "plain": {options: &plain}}
}

// randomColors answers count distinct colours, some of them translucent.
func randomColors(random *rand.Rand, count int) []uint32 {
	seen := map[uint32]bool{}
	var colors []uint32
	for len(colors) < count {
		color := random.Uint32()
		if random.IntN(3) > 0 {
			color |= 0xff000000
		}
		if !seen[color] {
			seen[color] = true
			colors = append(colors, color)
		}
	}
	return colors
}

func TestEncodeRoundTripsColorCounts(t *testing.T) {
	random := rand.New(rand.NewPCG(1, 2))
	for _, count := range []int{1, 2, 3, 4, 5, 16, 17, 255, 256, 257, 4000} {
		for _, size := range [][2]int{{1, 1}, {1, 9}, {9, 1}, {7, 3}, {33, 17}, {64, 64}, {240, 100}} {
			width, height := size[0], size[1]
			colors := randomColors(random, count)
			pixels := make([]uint32, width*height)
			for index := range pixels {
				pixels[index] = colors[random.IntN(len(colors))]
			}
			for name, encoder := range encoders() {
				t.Run(fmt.Sprintf("%s %d colors %dx%d", name, count, width, height), func(t *testing.T) {
					roundTrip(t, encoder, pixels, width, height)
				})
			}
		}
	}
}

// A picture like the games': tiles repeated across the field, a sprite over
// them, and a band of transparent pixels where an update keeps the picture
// the page holds.
func pixelArt(random *rand.Rand, width, height, colorCount int) []uint32 {
	colors := randomColors(random, colorCount)
	tile := make([]uint32, 64)
	for index := range tile {
		tile[index] = colors[random.IntN(len(colors))|0]
	}
	pixels := make([]uint32, width*height)
	for y := range height {
		for x := range width {
			pixels[y*width+x] = tile[(y%8)*8+x%8] | 0xff000000
		}
	}
	for y := height / 3; y < height/3+min(20, height/3); y++ {
		for x := width / 4; x < width/4+min(20, width/2); x++ {
			pixels[y*width+x] = colors[random.IntN(len(colors))] | 0xff000000
		}
	}
	for y := 0; y < height/5; y++ {
		for x := range width {
			pixels[y*width+x] = 0
		}
	}
	return pixels
}

func TestEncodeRoundTripsPixelArt(t *testing.T) {
	random := rand.New(rand.NewPCG(3, 4))
	for _, colors := range []int{6, 40, 250, 3000} {
		for _, size := range [][2]int{{176, 208}, {240, 320}, {128, 40}} {
			pixels := pixelArt(random, size[0], size[1], colors)
			for name, encoder := range encoders() {
				t.Run(fmt.Sprintf("%s %d colors %dx%d", name, colors, size[0], size[1]), func(t *testing.T) {
					roundTrip(t, encoder, pixels, size[0], size[1])
				})
			}
		}
	}
}

// Gradients exercise every prediction mode's arithmetic, including the
// clamped ones, on channels that change by more than one step.
func TestEncodeRoundTripsGradients(t *testing.T) {
	width, height := 97, 61
	pixels := make([]uint32, width*height)
	for y := range height {
		for x := range width {
			pixels[y*width+x] = uint32(255-y*4)<<24 | uint32(x*2)<<16 | uint32(x*y)<<8 | uint32(y*3+x)
		}
	}
	for _, encoder := range encoders() {
		roundTrip(t, encoder, pixels, width, height)
	}
	for index := range pixels {
		pixels[index] ^= uint32(index*2654435761) & 0x0f0f0f0f
	}
	for _, encoder := range encoders() {
		roundTrip(t, encoder, pixels, width, height)
	}
}

// An encoder keeps its working memory from picture to picture, and some of
// it is left dirty on purpose: nothing a picture leaves behind may change
// how the next one is written. Pictures of very different sizes and colour
// counts go through one encoder of each kind in turn, and each must come out
// as a fresh encoder writes it.
func TestEncoderReuseMatchesAFreshEncoder(t *testing.T) {
	random := rand.New(rand.NewPCG(11, 12))
	type picture struct {
		width, height int
		pixels        []uint32
	}
	var pictures []picture
	add := func(width, height int, pixels []uint32) {
		pictures = append(pictures, picture{width, height, pixels})
	}
	noise := func(width, height, colorCount int) []uint32 {
		colors := randomColors(random, colorCount)
		pixels := make([]uint32, width*height)
		for index := range pixels {
			pixels[index] = colors[random.IntN(len(colors))]
		}
		return pixels
	}
	add(240, 320, pixelArt(random, 240, 320, 3000))
	add(1, 1, []uint32{0xff123456})
	add(240, 320, pixelArt(random, 240, 320, 40))
	add(3, 2, noise(3, 2, 6))
	add(1, 300, noise(1, 300, 200))
	add(300, 1, noise(300, 1, 2))
	add(64, 64, noise(64, 64, 5000))
	add(97, 61, pixelArt(random, 97, 61, 250))
	add(16, 16, make([]uint32, 256))
	add(176, 208, pixelArt(random, 176, 208, 6))
	add(9, 1, noise(9, 1, 1))
	add(128, 40, noise(128, 40, 257))
	// Mostly transparent, as an update is, with a few changed pixels in
	// short runs: where the searches skip the most.
	changed := make([]uint32, 240*320)
	colors := randomColors(random, 5)
	for at := 0; at < len(changed); at += 1 + random.IntN(90) {
		for run := random.IntN(6); run >= 0 && at < len(changed); run-- {
			changed[at] = colors[random.IntN(len(colors))] | 0xff000000
			at++
		}
	}
	add(240, 320, changed)
	for name, reused := range encoders() {
		for round := range 2 {
			for index, picture := range pictures {
				got := roundTrip(t, reused, picture.pixels, picture.width, picture.height)
				fresh := &Encoder{options: reused.options}
				want, _ := fresh.Encode(nil, picture.pixels, picture.width, picture.height)
				if !bytes.Equal(got, want) {
					t.Fatalf("%s round %d picture %d (%dx%d): %d bytes after the others, %d from a fresh encoder",
						name, round, index, picture.width, picture.height, len(got), len(want))
				}
			}
		}
	}
}

// The backward reference search keeps the positions of earlier pictures in
// its hash chains and tells them apart by an offset that grows with every
// image written; when the offset would overflow, the chains are cleared and
// the offset starts again, which must not change what is written, whether it
// happens before a picture or between the images a picture is written as.
func TestMatcherOffsetStartsAgainBeforeOverflow(t *testing.T) {
	random := rand.New(rand.NewPCG(13, 14))
	for _, colors := range []int{40, 3000} {
		pixels := pixelArt(random, 120, 90, colors)
		want, _ := (&Encoder{}).Encode(nil, pixels, 120, 90)
		for _, headroom := range []uint32{0, 5, 39, 40, 41, 120*90 - 1, 120 * 90, 120*90 + 1, 120*90 + 41} {
			var encoder Encoder
			roundTrip(t, &encoder, pixels, 120, 90)
			encoder.image.matcher.next = math.MaxUint32 - headroom
			got := roundTrip(t, &encoder, pixels, 120, 90)
			if !bytes.Equal(got, want) {
				t.Fatalf("%d colours, offset %d below the limit: %d bytes, want %d", colors, headroom, len(got), len(want))
			}
			if next := encoder.image.matcher.next; next == 0 {
				t.Fatalf("%d colours, offset %d below the limit: left at %d", colors, headroom, next)
			}
		}
	}
}

// A picture of 256 colours fits a colour table even when the last colour
// found is the transparent one, which is zero, as the slot numbers colours
// from one to 256.
func TestPaletteFitsTransparentLastColour(t *testing.T) {
	colors := make([]uint32, 256)
	for index := range colors {
		colors[index] = 0xff000000 | uint32(index)*0x010101
	}
	colors[255] = 0
	width, height := 32, 16
	pixels := make([]uint32, width*height)
	for index := range pixels {
		pixels[index] = colors[index%len(colors)]
	}
	var p palette
	if fits, translucent := p.collect(pixels); !fits || !translucent {
		t.Fatalf("collect answers fits %v, translucent %v", fits, translucent)
	}
	for _, encoder := range encoders() {
		roundTrip(t, encoder, pixels, width, height)
	}
}

func TestEncodeRejectsBadDimensions(t *testing.T) {
	var encoder Encoder
	for _, size := range [][2]int{{0, 1}, {1, 0}, {MaxDimension + 1, 1}, {4, 4}} {
		if _, err := encoder.Encode(nil, make([]uint32, 4), size[0], size[1]); err == nil {
			t.Errorf("%v: no error", size)
		}
	}
}

// Encode appends to what it is given and leaves it alone.
func TestEncodeAppends(t *testing.T) {
	var encoder Encoder
	prefix := []byte("header")
	out, err := encoder.Encode(prefix, []uint32{0xff102030}, 1, 1)
	if err != nil || string(out[:6]) != "header" || string(out[6:10]) != "RIFF" {
		t.Fatalf("got %q, %v", out, err)
	}
}

func TestPrefixEncodeMatchesTheDecoder(t *testing.T) {
	for value := uint32(1); value <= maxDistanceValue; value++ {
		symbol, extraBits, extra := prefixEncode(value)
		decoded := symbol + 1
		if symbol >= 4 {
			bits := (symbol - 2) >> 1
			if bits != extraBits {
				t.Fatalf("%d: %d extra bits, want %d", value, extraBits, bits)
			}
			decoded = (2+symbol&1)<<bits + extra + 1
		}
		if decoded != value || symbol >= distanceSymbols {
			t.Fatalf("%d: symbol %d extra %d decodes as %d", value, symbol, extra, decoded)
		}
	}
}

func TestDistanceCodeMatchesTheDecoder(t *testing.T) {
	for _, width := range []int{1, 2, 3, 7, 8, 9, 16, 240} {
		for distance := 1; distance < 20*width+40; distance++ {
			code := distanceCode(distance, width)
			decoded := int(code) - 120
			if code <= 120 {
				offset := neighbourhood[code-1]
				decoded = max(int(offset[0])+int(offset[1])*width, 1)
			}
			if decoded != distance {
				t.Fatalf("width %d distance %d: code %d decodes as %d", width, distance, code, decoded)
			}
		}
	}
}

// The builder's codes are complete and within the limit even for counts whose
// plain Huffman code is far deeper.
func TestHuffmanLengthsAreCompleteAndLimited(t *testing.T) {
	var builder huffmanBuilder
	fibonacci := make([]uint32, 40)
	fibonacci[0], fibonacci[1] = 1, 1
	for index := 2; index < len(fibonacci); index++ {
		fibonacci[index] = fibonacci[index-1] + fibonacci[index-2]
	}
	random := rand.New(rand.NewPCG(5, 6))
	uniform := make([]uint32, 2328)
	for index := range uniform {
		uniform[index] = uint32(random.IntN(1000))
	}
	for name, counts := range map[string][]uint32{"fibonacci": fibonacci, "uniform": uniform, "pair": {0, 5, 0, 9}} {
		for _, limit := range []int{maxCodeLengthLength, maxCodeLength} {
			if len(counts) > 1<<limit {
				continue
			}
			lengths := make([]uint8, len(counts))
			builder.build(counts, limit, lengths)
			kraft := 0.0
			for symbol, length := range lengths {
				if (length == 0) != (counts[symbol] == 0) {
					t.Fatalf("%s: symbol %d count %d length %d", name, symbol, counts[symbol], length)
				}
				if int(length) > limit {
					t.Fatalf("%s: length %d over %d", name, length, limit)
				}
				if length > 0 {
					kraft += 1 / float64(uint64(1)<<length)
				}
			}
			if kraft != 1 {
				t.Fatalf("%s limit %d: Kraft sum %v", name, limit, kraft)
			}
		}
	}
}

// FuzzEncode round-trips pictures drawn from a few colours or from any, at
// any small size, which is where the special cases of the format live.
func FuzzEncode(f *testing.F) {
	f.Add([]byte{3, 2, 0, 1, 2, 3, 4, 5})
	f.Add([]byte{1, 1, 9})
	f.Add(bytes.Repeat([]byte{7, 250, 1, 2, 3}, 40))
	all := encoders()
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 3 {
			return
		}
		width, height := int(data[0])%40+1, int(data[1])%40+1
		spread := int(data[2])
		random := rand.New(rand.NewPCG(uint64(len(data)), uint64(spread)))
		palette := randomColors(random, spread%300+1)
		pixels := make([]uint32, width*height)
		for index := range pixels {
			choice := 0
			if len(data) > 3 {
				choice = int(data[3+index%(len(data)-3)]) + index/(len(data)-3)
			}
			pixels[index] = palette[choice%len(palette)]
		}
		for _, encoder := range all {
			roundTrip(t, encoder, pixels, width, height)
		}
	})
}

// plainReferences is the parse the matcher makes, written as plainly as it
// can be: every position entered in the chains one at a time, every
// candidate considered in turn and measured a pixel at a time, and nothing
// kept from one image to the next. The matcher is written for speed and must
// choose exactly the tokens this does.
func plainReferences(pixels []uint32, width int, search chains, model *costModel) []token {
	count := len(pixels)
	var heads [len(chains{})][]int
	var previous [len(chains{})][]int
	for index := range search {
		heads[index] = make([]int, 1<<matcherHashBits)
		for slot := range heads[index] {
			heads[index][slot] = -1
		}
		previous[index] = make([]int, count)
	}
	runHash := func(position, span int) uint32 {
		return hashRun(0, pixels[position:position+span]) >> (32 - matcherHashBits)
	}
	distanceCost := func(code uint32) float32 {
		symbol, extraBits, _ := prefixEncode(code)
		return model.distance[symbol] + float32(extraBits)
	}
	inserted := 0
	insertBefore := func(end int) {
		for ; inserted < end; inserted++ {
			position := inserted
			for index, chain := range search {
				if chain.depth == 0 || position+chain.span > count {
					continue
				}
				if position > 0 && pixels[position-1] == pixels[position] && pixels[position-1] == pixels[position+chain.span-1] {
					continue
				}
				hash := runHash(position, chain.span)
				previous[index][position] = heads[index][hash]
				heads[index][hash] = position
			}
		}
	}
	find := func(position int) (int, uint32, float32) {
		insertBefore(position)
		limit := min(maxCopyLength, count-position)
		bestLength, bestCode, bestWorth := 0, uint32(0), float32(0)
		consider := func(distance int) {
			if distance <= 0 || distance > position || distance+120 > maxDistanceValue {
				return
			}
			longer := bestLength == 0 || (bestLength < limit && pixels[position+bestLength] == pixels[position+bestLength-distance])
			code := distanceCode(distance, width)
			if !longer && distanceCost(code) >= distanceCost(bestCode) {
				return
			}
			length := 0
			for length < limit && pixels[position-distance+length] == pixels[position+length] {
				length++
			}
			if length == 0 {
				return
			}
			distanceSymbol, distanceBits, _ := prefixEncode(code)
			worth := model.literals[position+length] - model.literals[position] - model.copyCost(uint32(length), distanceSymbol, distanceBits)
			if worth > bestWorth {
				bestLength, bestCode, bestWorth = length, code, worth
			}
		}
		consider(1)
		consider(width)
		for index, chain := range search {
			if chain.depth == 0 || position+chain.span > count {
				continue
			}
			candidate := heads[index][runHash(position, chain.span)]
			for tries := 0; candidate >= 0 && tries < chain.depth && bestLength < limit; tries++ {
				consider(position - candidate)
				candidate = previous[index][candidate]
			}
		}
		return bestLength, bestCode, bestWorth
	}
	var tokens []token
	for position := 0; position < count; {
		length, code, worth := find(position)
		if worth > 0 && length < maxCopyLength && position+1 < count {
			if next, nextCode, nextWorth := find(position + 1); nextWorth > worth {
				tokens = append(tokens, token{})
				position++
				length, code, worth = next, nextCode, nextWorth
			}
		}
		if worth <= 0 {
			tokens = append(tokens, token{})
			position++
			continue
		}
		tokens = append(tokens, token{length: uint32(length), distance: code})
		position += length
	}
	return tokens
}

// checkReferences parses the picture with the matcher and with the plain
// parse, priced alike, and fails unless they choose the same tokens.
func checkReferences(t *testing.T, m *matcher, pixels []uint32, width int, search chains) {
	t.Helper()
	var iw imageWriter
	iw.priorCounts(pixels)
	iw.priceTokens(pixels)
	got := m.references(nil, pixels, width, search, &iw.model)
	want := plainReferences(pixels, width, search, &iw.model)
	if !slices.Equal(got, want) {
		index := 0
		for index < min(len(got), len(want)) && got[index] == want[index] {
			index++
		}
		t.Fatalf("%d pixels %d wide, chains %v: %d tokens, want %d; token %d differs", len(pixels), width, search, len(got), len(want), index)
	}
}

// matcherPicture is pixels to parse, width to a row.
type matcherPicture struct {
	pixels []uint32
	width  int
}

// matcherPictures answers pictures that take the matcher's every path: runs
// of one colour long and short, tiles repeated near and far, noise, and the
// shapes at the edges of the format, one pixel wide or high.
func matcherPictures() []matcherPicture {
	random := rand.New(rand.NewPCG(7, 8))
	var pictures []matcherPicture
	add := func(pixels []uint32, width int) {
		pictures = append(pictures, matcherPicture{pixels, width})
	}
	for _, colors := range []int{2, 6, 40, 3000} {
		for _, size := range [][2]int{{240, 320}, {176, 208}, {33, 17}, {128, 40}} {
			add(pixelArt(random, size[0], size[1], colors), size[0])
		}
	}
	for _, size := range [][2]int{{1, 1}, {1, 300}, {300, 1}, {2, 2}, {7, 3}, {64, 64}} {
		for _, colors := range []int{1, 2, 5, 300} {
			palette := randomColors(random, colors)
			pixels := make([]uint32, size[0]*size[1])
			for index := range pixels {
				pixels[index] = palette[random.IntN(len(palette))]
			}
			add(pixels, size[0])
		}
	}
	// Runs of one colour of every length up to a little past the longest
	// chain's run, between single pixels of others.
	runs := make([]uint32, 0, 4000)
	for length := 1; len(runs) < 3900; length = length%9 + 1 {
		color := uint32(random.IntN(3))
		for range length {
			runs = append(runs, color)
		}
		runs = append(runs, uint32(3+random.IntN(2)))
	}
	add(runs, 50)
	add(runs, 1)
	// One colour throughout, longer than the longest copy.
	add(make([]uint32, 9000), 90)
	return pictures
}

// The matcher chooses the tokens of the plain parse for every picture and
// chain setting, reusing its tables from one picture to the next, including
// across the point where the positions it enters them with start over.
func TestReferencesMatchThePlainParse(t *testing.T) {
	settings := []chains{
		defaultSettings.chains,
		{{span: 3, depth: 2}},
		{{span: 6, depth: 16}, {span: 2, depth: 8}},
		{{span: 0, depth: 0}, {span: 4, depth: 5}},
		{{span: 2, depth: 3}, {span: 2, depth: 3}},
		{{span: 1, depth: 4}, {span: 9, depth: 1}},
		{{span: 0, depth: 3}},
	}
	var m matcher
	for _, search := range settings {
		for index, picture := range matcherPictures() {
			if index == 5 {
				// The next picture's positions do not fit above the last
				// one's, so the chains are cleared and begin again.
				m.next = 1<<32 - 100
			}
			t.Run(fmt.Sprintf("%v %d", search, index), func(t *testing.T) {
				checkReferences(t, &m, picture.pixels, picture.width, search)
			})
		}
	}
}

// FuzzReferences compares the matcher with the plain parse on pictures drawn
// from a few colours, reusing one matcher for every picture the fuzzer tries.
func FuzzReferences(f *testing.F) {
	f.Add([]byte{3, 2, 0, 1, 2, 3, 4, 5})
	// A picture 240 wide of runs of seven pixels, three colours in turn.
	runs := []byte{239, 7, 3}
	for index := range 240 {
		runs = append(runs, byte(index/7))
	}
	f.Add(runs)
	var m matcher
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) < 3 {
			return
		}
		width, height := int(data[0])%64+1, int(data[1])%64+1
		colors := int(data[2])%6 + 1
		pixels := make([]uint32, width*height)
		for index := range pixels {
			if len(data) > 3 {
				pixels[index] = uint32(data[3+index%(len(data)-3)]) % uint32(colors)
			}
		}
		checkReferences(t, &m, pixels, width, defaultSettings.chains)
	})
}

// countedCacheCounts is the cache trial done the plain way: every cache size
// simulated on its own, and the entropy of each alphabet summed over all its
// symbols. It answers the cheapest size and the counts written with it.
func countedCacheCounts(tokens []token, pixels []uint32) (best int, counts [codeCount][]uint32) {
	bestCost := math.Inf(1)
	for cacheBits := 0; cacheBits <= maxColorCacheBits; cacheBits++ {
		cache := make([]uint32, colorCacheSize(cacheBits))
		trial := [codeCount][]uint32{
			make([]uint32, literalSymbols+lengthSymbols+colorCacheSize(cacheBits)),
			make([]uint32, literalSymbols), make([]uint32, literalSymbols), make([]uint32, literalSymbols),
			make([]uint32, distanceSymbols),
		}
		position := 0
		for _, step := range tokens {
			if step.length == 0 {
				pixel := pixels[position]
				position++
				if cacheBits > 0 {
					key := cacheKey(pixel, cacheBits)
					if cache[key] == pixel {
						trial[codeGreen][literalSymbols+lengthSymbols+int(key)]++
						continue
					}
					cache[key] = pixel
				}
				trial[codeGreen][pixel>>8&0xff]++
				trial[codeRed][pixel>>16&0xff]++
				trial[codeBlue][pixel&0xff]++
				trial[codeAlpha][pixel>>24]++
				continue
			}
			symbol, _, _ := prefixEncode(step.length)
			trial[codeGreen][literalSymbols+int(symbol)]++
			symbol, _, _ = prefixEncode(step.distance)
			trial[codeDistance][symbol]++
			if cacheBits > 0 {
				for _, pixel := range pixels[position : position+int(step.length)] {
					cache[cacheKey(pixel, cacheBits)] = pixel
				}
			}
			position += int(step.length)
		}
		cost := plainEntropyBits(trial[codeGreen])
		for channel := codeRed; channel <= codeAlpha; channel++ {
			cost += plainEntropyBits(trial[channel])
		}
		if cost < bestCost {
			best, bestCost, counts = cacheBits, cost, trial
		}
	}
	return best, counts
}

func plainEntropyBits(counts []uint32) float64 {
	total, used := uint32(0), 0
	sum := 0.0
	for _, count := range counts {
		if count > 0 {
			total += count
			used++
			sum += float64(count) * math.Log2(float64(count))
		}
	}
	if used <= 1 {
		return float64(used) * 8
	}
	return float64(total)*math.Log2(float64(total)) - sum + float64(used)*4
}

// The cache trial counts every size in one pass, relying on the caches
// nesting; it must choose the size, and leave the counts, that simulating
// each size does, for one image writer used on image after image.
func TestCacheTrialMatchesEachSizeCounted(t *testing.T) {
	random := rand.New(rand.NewPCG(9, 10))
	var writer imageWriter
	writer.settings = defaultSettings
	for trial := range 60 {
		width, height := 1+random.IntN(90), 1+random.IntN(70)
		colors := randomColors(random, []int{1, 2, 7, 40, 300, 5000}[trial%6])
		colors = append(colors, 0)
		pixels := make([]uint32, width*height)
		for index := range pixels {
			switch {
			case index > 0 && random.IntN(3) == 0:
				pixels[index] = pixels[index-1]
			case index >= width && random.IntN(3) == 0:
				pixels[index] = pixels[index-width]
			default:
				pixels[index] = colors[random.IntN(len(colors))]
			}
		}
		writer.priorCounts(pixels)
		writer.priceTokens(pixels)
		writer.tokens = writer.matcher.references(writer.tokens[:0], pixels, width, writer.settings.chains, &writer.model)
		got := writer.chooseCacheBits(pixels)
		want, counts := countedCacheCounts(writer.tokens, pixels)
		if got != want {
			t.Fatalf("%dx%d, %d colours: cache bits %d, want %d", width, height, len(colors), got, want)
		}
		for index := range codeCount {
			if !slices.Equal(writer.counts[index], counts[index]) {
				t.Fatalf("%dx%d, %d colours: code %d counts\n%v, want\n%v", width, height, len(colors), index, writer.counts[index], counts[index])
			}
		}
	}
}

// An Encoder copied by value after it has been used, with the copy and the
// original then used in turn, writes what a fresh encoder writes: the copies
// share the matcher's tables, and each must notice the other has used them.
func TestCopiedEncoderMatchesAFreshOne(t *testing.T) {
	random := rand.New(rand.NewPCG(11, 12))
	pictures := make([][3]any, 0, 60)
	for range 60 {
		width, height := random.IntN(60)+1, random.IntN(60)+1
		pixels := pixelArt(random, width, height, []int{3, 40, 300, 3000}[random.IntN(4)])
		pictures = append(pictures, [3]any{pixels, width, height})
	}
	var original Encoder
	copies := []*Encoder{&original}
	for index, picture := range pictures {
		pixels, width, height := picture[0].([]uint32), picture[1].(int), picture[2].(int)
		if index%5 == 4 {
			copied := *copies[random.IntN(len(copies))]
			copies = append(copies, &copied)
		}
		encoder := copies[random.IntN(len(copies))]
		got, err := encoder.Encode(nil, pixels, width, height)
		if err != nil {
			t.Fatal(err)
		}
		var fresh Encoder
		want, _ := fresh.Encode(nil, pixels, width, height)
		if !bytes.Equal(got, want) {
			t.Fatalf("picture %d, %dx%d: a copied encoder wrote %d bytes, a fresh one %d", index, width, height, len(got), len(want))
		}
	}
}
