package vp8l

import "math"

// The five prefix codes of a group, in the order they are written.
const (
	codeGreen = iota
	codeRed
	codeBlue
	codeAlpha
	codeDistance
	codeCount
)

const (
	literalSymbols     = 256
	lengthSymbols      = 24
	distanceSymbols    = 40
	maxColorCacheBits  = 10
	colorCacheMultiply = 0x1e35a7bd
)

// imageWriter entropy codes one image: the main one or one a transform
// carries. Everything in it is reused from image to image.
type imageWriter struct {
	matcher matcher
	tokens  []token
	// runStarts are the positions where the image's pixels change: zero and
	// every position whose pixel differs from the one before it.
	runStarts []int32
	// holders are, for each literal of the tokens in turn, the bits of the
	// smallest colour cache that holds it, or one more than
	// maxColorCacheBits when none does, as chooseCacheBits finds them. A
	// literal is a cache hit exactly when the cache written is at least that
	// size.
	holders  []uint8
	builder  huffmanBuilder
	counts   [codeCount][]uint32
	codes    [codeCount]prefixCode
	trial    cacheTrial
	lengths  lengthCoder
	settings settings
	model    costModel
	// prices are the bits of each literal symbol the model is filled from.
	prices struct {
		green            [literalSymbols + lengthSymbols]float32
		red, blue, alpha [literalSymbols]float32
	}
}

// cacheTrial counts what the image's tokens would be written with under
// each colour cache size at once, to pick one before writing anything.
//
// The caches of every size are kept as one binary tree: a pixel's slot in
// the cache of 2^k entries is node 1<<k | key, and the node's children are
// the two slots it splits into in the cache twice its size. A pixel is
// entered in every cache by walking from its slot in the largest up to the
// root.
type cacheTrial struct {
	caches [cacheNodes]uint32
	// hits are the cache hits of each slot, laid out the same way.
	hits [cacheNodes]uint32
	// literals are each size's counts of the green, red, blue and alpha
	// symbols of literal pixels.
	literals [maxColorCacheBits + 1][codeAlpha + 1][literalSymbols]uint32
}

// cacheNodes is the number of nodes in the tree of caches, a power of two,
// so that a node masked with one less is always inside it.
const cacheNodes = 2 << maxColorCacheBits

// write appends the image: its color cache choice, for the main image the
// single group of prefix codes it uses everywhere, the codes, and the pixels.
//
// The pixels are parsed once, with tokens priced by a model that knows only
// how often each channel value occurs; the colour cache is sized for the
// tokens that parse chose.
func (iw *imageWriter) write(w *bitWriter, pixels []uint32, width int, main bool) {
	iw.priorCounts(pixels)
	iw.priceTokens(pixels)
	iw.tokens = iw.matcher.references(iw.tokens[:0], pixels, width, iw.settings.chains, &iw.model)
	cacheBits := 0
	if iw.settings.colorCache && len(pixels) >= 64 {
		// The trial leaves the counts of the size it chooses.
		cacheBits = iw.chooseCacheBits(pixels)
	} else {
		iw.count(pixels)
	}
	if cacheBits > 0 {
		w.write(1, 1)
		w.write(uint32(cacheBits), 4)
	} else {
		w.write(0, 1)
	}
	if main {
		// One group of codes for the whole image.
		w.write(0, 1)
	}
	alphabets := [codeCount]int{literalSymbols + lengthSymbols + colorCacheSize(cacheBits), literalSymbols, literalSymbols, literalSymbols, distanceSymbols}
	for index := range codeCount {
		iw.writeCode(w, &iw.codes[index], iw.counts[index], alphabets[index])
	}
	iw.emit(w, pixels, cacheBits)
}

// runCursor walks an image's run starts alongside its tokens, which only
// ever move forward, so that no start is looked at twice.
type runCursor struct {
	starts []int32
	next   int
}

// within answers the run starts after position and before end.
func (cursor *runCursor) within(position, end int) []int32 {
	starts, next := cursor.starts, cursor.next
	for next < len(starts) && int(starts[next]) <= position {
		next++
	}
	first := next
	for next < len(starts) && int(starts[next]) < end {
		next++
	}
	cursor.next = next
	return starts[first:next]
}

func colorCacheSize(cacheBits int) int {
	if cacheBits == 0 {
		return 0
	}
	return 1 << cacheBits
}

func cacheKey(pixel uint32, cacheBits int) uint32 {
	return (pixel * colorCacheMultiply) >> (32 - cacheBits)
}

// count fills the symbol counts the tokens produce without a colour cache.
func (iw *imageWriter) count(pixels []uint32) {
	alphabets := [codeCount]int{literalSymbols + lengthSymbols, literalSymbols, literalSymbols, literalSymbols, distanceSymbols}
	for index := range codeCount {
		iw.counts[index] = resize(iw.counts[index], alphabets[index])
		clear(iw.counts[index])
	}
	green, red, blue, alpha := iw.counts[codeGreen], iw.counts[codeRed][:literalSymbols], iw.counts[codeBlue][:literalSymbols], iw.counts[codeAlpha][:literalSymbols]
	position := 0
	for _, step := range iw.tokens {
		if step.length == 0 {
			pixel := pixels[position]
			position++
			green[pixel>>8&0xff]++
			red[pixel>>16&0xff]++
			blue[pixel&0xff]++
			alpha[pixel>>24]++
			continue
		}
		symbol, _, _ := prefixEncode(step.length)
		green[literalSymbols+int(symbol)]++
		symbol, _, _ = prefixEncode(step.distance)
		iw.counts[codeDistance][symbol]++
		position += int(step.length)
	}
}

// emit writes the tokens with the codes built from their counts. With a
// colour cache, which literals it holds is what chooseCacheBits found.
func (iw *imageWriter) emit(w *bitWriter, pixels []uint32, cacheBits int) {
	green, red, blue, alpha, distance := &iw.codes[codeGreen], &iw.codes[codeRed], &iw.codes[codeBlue], &iw.codes[codeAlpha], &iw.codes[codeDistance]
	literal := 0
	position := 0
	for _, step := range iw.tokens {
		if step.length == 0 {
			pixel := pixels[position]
			position++
			if cacheBits > 0 {
				held := int(iw.holders[literal]) <= cacheBits
				literal++
				if held {
					green.put(w, literalSymbols+lengthSymbols+int(cacheKey(pixel, cacheBits)))
					continue
				}
			}
			green.put(w, int(pixel>>8&0xff))
			red.put(w, int(pixel>>16&0xff))
			blue.put(w, int(pixel&0xff))
			alpha.put(w, int(pixel>>24))
			continue
		}
		symbol, extraBits, extra := prefixEncode(step.length)
		green.put(w, literalSymbols+int(symbol))
		w.write(extra, uint(extraBits))
		symbol, extraBits, extra = prefixEncode(step.distance)
		distance.put(w, int(symbol))
		w.write(extra, uint(extraBits))
		position += int(step.length)
	}
}

// chooseCacheBits estimates, for every cache size, the entropy of the
// literals and cache hits the tokens would be written with, and answers the
// cheapest. A larger cache hits more often but widens the green alphabet
// that every literal is coded in and that has to be described. It reads the
// runs priorCounts found for the same pixels. The counts of the size chosen
// are left in iw.counts, as count would leave them.
func (iw *imageWriter) chooseCacheBits(pixels []uint32) int {
	trial := &iw.trial
	clear(trial.caches[:])
	clear(trial.hits[:])
	clear(trial.literals[:])
	distances := resize(iw.counts[codeDistance], distanceSymbols)
	clear(distances)
	iw.counts[codeDistance] = distances
	holders := iw.holders[:0]
	var lengths [lengthSymbols]uint32
	literal := func(cacheBits int, pixel uint32) {
		counts := &trial.literals[cacheBits]
		counts[codeGreen][pixel>>8&0xff]++
		counts[codeRed][pixel>>16&0xff]++
		counts[codeBlue][pixel&0xff]++
		counts[codeAlpha][pixel>>24]++
	}
	runs := runCursor{starts: iw.runStarts}
	position := 0
	for _, step := range iw.tokens {
		if step.length == 0 {
			pixel := pixels[position]
			position++
			literal(0, pixel)
			hash := pixel * colorCacheMultiply
			// A pixel a cache holds is held by every larger one too: its
			// slot there takes only pixels that would have replaced it in
			// the smaller one. So the smallest cache that holds the pixel
			// is looked for from the smallest up, and the pixel is a
			// literal, and entered, in every cache below that.
			holder, node := 1, uint32(0)
			for ; holder <= maxColorCacheBits; holder++ {
				node = (hash>>(32-holder) | 1<<holder) & (cacheNodes - 1)
				if trial.caches[node] == pixel {
					break
				}
				trial.caches[node] = pixel
				literal(holder, pixel)
			}
			holders = append(holders, uint8(holder))
			// A slot's hits are those of the two slots it splits into, less
			// the pixels its own cache does not hold. So a hit is counted in
			// the largest cache, and marked in the pixel's slot in the cache
			// just smaller than the smallest that holds it, where foldHits
			// takes it away again.
			if holder <= maxColorCacheBits {
				trial.hits[(hash>>(32-maxColorCacheBits)|1<<maxColorCacheBits)&(cacheNodes-1)]++
				trial.hits[node>>1]++
			}
			continue
		}
		symbol, _, _ := prefixEncode(step.length)
		lengths[symbol]++
		symbol, _, _ = prefixEncode(step.distance)
		distances[symbol]++
		// A copy enters its first pixel in the caches and then each one
		// that differs from the one before it.
		end := position + int(step.length)
		trial.insert(pixels[position])
		for _, start := range runs.within(position, end) {
			trial.insert(pixels[start])
		}
		position = end
	}
	iw.holders = holders
	trial.foldHits()
	// A cache only takes literals away, so every size's literal counts are
	// zero wherever those without a cache are, and only the symbols those
	// use are looked at.
	var used [codeAlpha + 1][literalSymbols]uint8
	var usedCount [codeAlpha + 1]int
	for index := range codeAlpha + 1 {
		for symbol, count := range trial.literals[0][index] {
			if count > 0 {
				used[index][usedCount[index]] = uint8(symbol)
				usedCount[index]++
			}
		}
	}
	best, bestCost := 0, math.Inf(1)
	for cacheBits := 0; cacheBits <= maxColorCacheBits; cacheBits++ {
		literals := &trial.literals[cacheBits]
		// The green code's symbols are taken in order: literals, lengths
		// and cache hits.
		var green entropy
		green.addSymbols(&literals[codeGreen], used[codeGreen][:usedCount[codeGreen]])
		green.addCounts(lengths[:])
		if cacheBits > 0 {
			green.addCounts(trial.hits[1<<cacheBits : 2<<cacheBits])
		}
		cost := green.bits()
		for index := codeRed; index <= codeAlpha; index++ {
			var channel entropy
			channel.addSymbols(&literals[index], used[index][:usedCount[index]])
			cost += channel.bits()
		}
		if cost < bestCost {
			best, bestCost = cacheBits, cost
		}
	}
	green := resize(iw.counts[codeGreen], literalSymbols+lengthSymbols+colorCacheSize(best))
	copy(green, trial.literals[best][codeGreen][:])
	copy(green[literalSymbols:], lengths[:])
	copy(green[literalSymbols+lengthSymbols:], trial.hits[1<<best:2<<best])
	iw.counts[codeGreen] = green
	for index := codeRed; index <= codeAlpha; index++ {
		iw.counts[index] = resize(iw.counts[index], literalSymbols)
		copy(iw.counts[index], trial.literals[best][index][:])
	}
	return best
}

// insert enters a pixel in the caches of every size, walking from its slot
// in the largest up to the root.
func (trial *cacheTrial) insert(pixel uint32) {
	for node := pixel*colorCacheMultiply>>(32-maxColorCacheBits) | 1<<maxColorCacheBits; node > 1; node >>= 1 {
		trial.caches[node&(cacheNodes-1)] = pixel
	}
}

// foldHits turns the hits chooseCacheBits counts into every size's: the
// hits of a slot are those of the two slots it splits into, less the
// literals marked there as not held by its cache.
func (trial *cacheTrial) foldHits() {
	for node := 1<<maxColorCacheBits - 1; node > 1; node-- {
		trial.hits[node] = trial.hits[2*node] + trial.hits[2*node+1] - trial.hits[node]
	}
}

// smallLog2 holds the base 2 logarithms of the counts most symbols have.
var smallLog2 = func() (table [1024]float64) {
	for value := range table {
		table[value] = math.Log2(float64(value))
	}
	return table
}()

// log2 is math.Log2 of a count, from the table where it can be.
func log2(count uint32) float64 {
	if count < uint32(len(smallLog2)) {
		return smallLog2[count]
	}
	return math.Log2(float64(count))
}

// entropy gathers counts, taken in symbol order, for an estimate of the
// bits they would be coded in.
type entropy struct {
	total uint32
	used  int
	sum   float64
}

// addCounts takes counts in symbol order. The sums are kept in locals, as
// the estimate always was: an architecture that fuses a multiply and an add
// then fuses them the same way, and the estimate, and so the cache size it
// chooses, stays what it was there.
func (e *entropy) addCounts(counts []uint32) {
	total, used, sum := e.total, e.used, e.sum
	for _, count := range counts {
		if count > 0 {
			total += count
			used++
			sum += float64(count) * log2(count)
		}
	}
	e.total, e.used, e.sum = total, used, sum
}

// addSymbols takes the counts of symbols, which are in order, in the same way.
func (e *entropy) addSymbols(counts *[literalSymbols]uint32, symbols []uint8) {
	total, used, sum := e.total, e.used, e.sum
	for _, symbol := range symbols {
		if count := counts[symbol]; count > 0 {
			total += count
			used++
			sum += float64(count) * log2(count)
		}
	}
	e.total, e.used, e.sum = total, used, sum
}

// bits is the Shannon entropy of the counts, in bits, plus a rough price
// for describing each symbol that occurs.
func (e *entropy) bits() float64 {
	if e.used <= 1 {
		return float64(e.used) * 8
	}
	return float64(e.total)*log2(e.total) - e.sum + float64(e.used)*4
}

// symbolCosts sets costs to the bits each symbol would take under a code
// fitted to counts. The estimate is smoothed so that a symbol the counts
// never saw, and every symbol of an alphabet not used at all, has a price.
func symbolCosts(costs []float32, counts []uint32) {
	// The counts are summed as integers, which is what adding them one by one
	// as floating point amounts to, since every sum is far below 2^53.
	total := uint64(0)
	for _, count := range counts {
		total += uint64(count)
	}
	smoothed := float64(total) + 0.5*float64(len(counts))
	// Most counts are zero, or equal to the count before them, so a price is
	// computed only where the count changes.
	lastCount, lastCost := ^uint32(0), float32(0)
	for symbol, count := range counts {
		if count != lastCount {
			lastCount, lastCost = count, float32(math.Log2(smoothed/(float64(count)+0.5)))
		}
		costs[symbol] = lastCost
	}
}

// priceTokens fills the cost model from the counts, with the runs
// priorCounts found for the same pixels. Every pixel enters the parse as a
// literal without a colour cache, whose size is chosen only once the parse is
// done, so every pixel of a run costs the same.
func (iw *imageWriter) priceTokens(pixels []uint32) {
	prices := &iw.prices
	symbolCosts(prices.green[:], iw.counts[codeGreen])
	symbolCosts(prices.red[:], iw.counts[codeRed])
	symbolCosts(prices.blue[:], iw.counts[codeBlue])
	symbolCosts(prices.alpha[:], iw.counts[codeAlpha])
	symbolCosts(iw.model.distance[:], iw.counts[codeDistance])
	copy(iw.model.length[:], prices.green[literalSymbols:])
	model := &iw.model
	model.literals = resize(model.literals, len(pixels)+1)
	model.literals[0] = 0
	sum := float32(0)
	starts := iw.runStarts
	for index, start := range starts {
		end := len(pixels)
		if index+1 < len(starts) {
			end = int(starts[index+1])
		}
		pixel := pixels[start]
		cost := prices.green[pixel>>8&0xff] + prices.red[pixel>>16&0xff] + prices.blue[pixel&0xff] + prices.alpha[pixel>>24]
		sums := model.literals[int(start)+1 : end+1]
		for position := range sums {
			sum += cost
			sums[position] = sum
		}
	}
}

// priorCounts fills the counts a parse is first priced with: every pixel a
// literal, and copies of every length and distance about as likely as each
// other, a little rarer in all than one pixel in sixty-four. It finds the
// runs of the pixels on the way.
func (iw *imageWriter) priorCounts(pixels []uint32) {
	alphabets := [codeCount]int{literalSymbols + lengthSymbols, literalSymbols, literalSymbols, literalSymbols, distanceSymbols}
	for index := range codeCount {
		iw.counts[index] = resize(iw.counts[index], alphabets[index])
		clear(iw.counts[index])
	}
	// Runs of one pixel are counted at once: counting them one by one would
	// make every step wait for the one before to store the same count.
	green, red, blue, alpha := iw.counts[codeGreen], iw.counts[codeRed][:literalSymbols], iw.counts[codeBlue][:literalSymbols], iw.counts[codeAlpha][:literalSymbols]
	starts := iw.runStarts[:0]
	for start := 0; start < len(pixels); {
		pixel := pixels[start]
		end := start + 1
		for end < len(pixels) && pixels[end] == pixel {
			end++
		}
		starts = append(starts, int32(start))
		run := uint32(end - start)
		green[pixel>>8&0xff] += run
		red[pixel>>16&0xff] += run
		blue[pixel&0xff] += run
		alpha[pixel>>24] += run
		start = end
	}
	iw.runStarts = starts
	share := uint32(len(pixels)/64 + 1)
	for symbol := range lengthSymbols {
		iw.counts[codeGreen][literalSymbols+symbol] = share
	}
	for symbol := range distanceSymbols {
		iw.counts[codeDistance][symbol] = 1
	}
}
