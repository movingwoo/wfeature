package vp8l

import (
	"math/bits"
	"slices"
)

// maxCodeLength is the longest prefix code the format allows; a code length
// code is at most seven bits, because its lengths are written in three.
const (
	maxCodeLength       = 15
	maxCodeLengthLength = 7
)

// codeLengthOrder is the order the code length code's own lengths are
// written in; trailing zeros in this order are left out.
var codeLengthOrder = [19]uint8{17, 18, 0, 1, 2, 3, 4, 5, 16, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}

// prefixCode is a canonical prefix code ready to write symbols with.
type prefixCode struct {
	// lengths are the code lengths the decoder is told.
	lengths []uint8
	// codes are the codes bit-reversed, because a code is read from its most
	// significant bit and the stream is written from the least.
	codes []uint16
	// sizes are the bits written for a symbol: its length, except that a
	// code with a single symbol costs nothing to use.
	sizes []uint8
}

func (code *prefixCode) reset(alphabet int) {
	code.lengths = resize(code.lengths, alphabet)
	code.codes = resize(code.codes, alphabet)
	code.sizes = resize(code.sizes, alphabet)
	clear(code.lengths)
	clear(code.sizes)
}

// assign derives the canonical codes from the lengths, which are zero but
// for the symbols given, in order.
func (code *prefixCode) assign(symbols []int32) {
	if len(symbols) == 1 {
		code.codes[symbols[0]], code.sizes[symbols[0]] = 0, 0
		return
	}
	var count, next [maxCodeLength + 1]uint16
	for _, symbol := range symbols {
		count[code.lengths[symbol]]++
	}
	value := uint16(0)
	for length := 1; length <= maxCodeLength; length++ {
		value = (value + count[length-1]) << 1
		next[length] = value
	}
	for _, symbol := range symbols {
		length := code.lengths[symbol]
		code.codes[symbol] = bits.Reverse16(next[length]) >> (16 - length)
		code.sizes[symbol] = length
		next[length]++
	}
}

func (code *prefixCode) put(w *bitWriter, symbol int) {
	w.write(uint32(code.codes[symbol]), uint(code.sizes[symbol]))
}

// huffmanBuilder holds what building a code needs, so building one allocates
// nothing once the encoder has warmed up.
type huffmanBuilder struct {
	// symbols are the symbols the counts use, in order; a leaf is known by
	// its place among them.
	symbols []int32
	// keys are the leaves sorted by weight and then by leaf: each is the
	// weight in the upper word and the leaf in the lower, so sorting them is
	// sorting numbers.
	keys []uint64
	// weights are the internal nodes' weights in the order they are made.
	weights []uint32
	// parents is every node's parent: the sorted leaves come first, then the
	// internal nodes in the order they are made.
	parents   []int32
	nodeDepth []uint16
	depths    []uint8
}

// build sets lengths to a prefix code for the counts that is complete and no
// longer than limit. A single used symbol gets length one, which is how the
// format writes a code that spends no bits. When the plain Huffman code is
// too long, every count is raised to a floor that doubles until it fits: rare
// symbols lose a little so that the code stays within the format.
func (builder *huffmanBuilder) build(counts []uint32, limit int, lengths []uint8) {
	builder.use(counts)
	builder.buildUsed(counts, limit, lengths)
}

// use finds the symbols the counts use, for buildUsed, and answers them. The
// symbols answered are the builder's own, valid until it is used again.
func (builder *huffmanBuilder) use(counts []uint32) []int32 {
	symbols := resize(builder.symbols, len(counts))
	used := 0
	for symbol, count := range counts {
		// Every symbol is written and only a used one kept, which spares
		// a branch that the counts would make hard to predict.
		symbols[used] = int32(symbol)
		if count > 0 {
			used++
		}
	}
	builder.symbols = symbols[:used]
	return builder.symbols
}

// buildUsed is build for counts whose used symbols use has just found.
func (builder *huffmanBuilder) buildUsed(counts []uint32, limit int, lengths []uint8) {
	clear(lengths)
	symbols := builder.symbols
	switch len(symbols) {
	case 0:
		return
	case 1:
		lengths[symbols[0]] = 1
		return
	}
	if len(symbols) > 1<<limit {
		panic("vp8l: more symbols than a code of that length can hold")
	}
	floor := uint32(1)
	for !builder.depthsFit(counts, floor, limit) {
		floor *= 2
	}
	for leaf, symbol := range symbols {
		lengths[symbol] = builder.depths[leaf]
	}
}

// depthsFit builds the Huffman tree of the used symbols, each weighing its
// count or floor if that is more, and reports whether no leaf is deeper than
// limit; the depths are left in builder.depths, in leaf order.
func (builder *huffmanBuilder) depthsFit(counts []uint32, floor uint32, limit int) bool {
	leaves := len(builder.symbols)
	keys := resize(builder.keys, leaves)
	builder.keys = keys
	for leaf, symbol := range builder.symbols {
		keys[leaf] = uint64(max(counts[symbol], floor))<<32 | uint64(leaf)
	}
	slices.Sort(keys)
	// Two queues: the sorted leaves and the internal nodes, which are made in
	// order of weight, so the lightest node is always at one of the fronts;
	// a leaf goes first when the two weigh the same.
	weights := resize(builder.weights, leaves-1)
	builder.weights = weights
	parents := resize(builder.parents, 2*leaves-1)
	builder.parents = parents
	nextLeaf, nextInner := 0, 0
	for made := range leaves - 1 {
		weight := uint32(0)
		for range 2 {
			if nextLeaf < leaves && (nextInner >= made || uint32(keys[nextLeaf]>>32) <= weights[nextInner]) {
				weight += uint32(keys[nextLeaf] >> 32)
				parents[nextLeaf] = int32(leaves + made)
				nextLeaf++
			} else {
				weight += weights[nextInner]
				parents[leaves+nextInner] = int32(leaves + made)
				nextInner++
			}
		}
		weights[made] = weight
	}
	// Depths from the root down: a node's parent is always made after it,
	// so walking the nodes backwards visits every parent first.
	nodeDepth := resize(builder.nodeDepth, 2*leaves-1)
	builder.nodeDepth = nodeDepth
	nodeDepth[2*leaves-2] = 0
	for node := 2*leaves - 3; node >= 0; node-- {
		nodeDepth[node] = nodeDepth[parents[node]] + 1
	}
	depths := resize(builder.depths, leaves)
	builder.depths = depths
	fits := true
	for position, key := range keys {
		depth := nodeDepth[position]
		if int(depth) > limit {
			fits = false
		}
		depths[uint32(key)] = uint8(min(depth, 255))
	}
	return fits
}

func resize[T any](slice []T, length int) []T {
	if cap(slice) < length {
		return make([]T, length)
	}
	return slice[:length]
}
