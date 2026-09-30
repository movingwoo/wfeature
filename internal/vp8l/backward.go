package vp8l

import (
	"math/bits"
	"slices"
)

// A backward reference copies at most this many pixels.
const maxCopyLength = 4096

// A distance is written as a prefix code over at most this value.
const maxDistanceValue = 1 << 20

// token is one step of an image's pixels in scan order: a literal pixel, or a
// copy of length pixels from distance pixels back. The distance is the one
// written, a neighbourhood code or the scan distance plus 120.
type token struct {
	length   uint32
	distance uint32
}

// neighbourhood is the table of RFC 9649 section 3.6.2.2.1: the offsets, in
// columns to the left and rows up, that the distance codes 1 to 120 name.
var neighbourhood = [120][2]int8{
	{0, 1}, {1, 0}, {1, 1}, {-1, 1}, {0, 2}, {2, 0}, {1, 2},
	{-1, 2}, {2, 1}, {-2, 1}, {2, 2}, {-2, 2}, {0, 3}, {3, 0},
	{1, 3}, {-1, 3}, {3, 1}, {-3, 1}, {2, 3}, {-2, 3}, {3, 2},
	{-3, 2}, {0, 4}, {4, 0}, {1, 4}, {-1, 4}, {4, 1}, {-4, 1},
	{3, 3}, {-3, 3}, {2, 4}, {-2, 4}, {4, 2}, {-4, 2}, {0, 5},
	{3, 4}, {-3, 4}, {4, 3}, {-4, 3}, {5, 0}, {1, 5}, {-1, 5},
	{5, 1}, {-5, 1}, {2, 5}, {-2, 5}, {5, 2}, {-5, 2}, {4, 4},
	{-4, 4}, {3, 5}, {-3, 5}, {5, 3}, {-5, 3}, {0, 6}, {6, 0},
	{1, 6}, {-1, 6}, {6, 1}, {-6, 1}, {2, 6}, {-2, 6}, {6, 2},
	{-6, 2}, {4, 5}, {-4, 5}, {5, 4}, {-5, 4}, {3, 6}, {-3, 6},
	{6, 3}, {-6, 3}, {0, 7}, {7, 0}, {1, 7}, {-1, 7}, {5, 5},
	{-5, 5}, {7, 1}, {-7, 1}, {4, 6}, {-4, 6}, {6, 4}, {-6, 4},
	{2, 7}, {-2, 7}, {7, 2}, {-7, 2}, {3, 7}, {-3, 7}, {7, 3},
	{-7, 3}, {5, 6}, {-5, 6}, {6, 5}, {-6, 5}, {8, 0}, {4, 7},
	{-4, 7}, {7, 4}, {-7, 4}, {8, 1}, {8, 2}, {6, 6}, {-6, 6},
	{8, 3}, {5, 7}, {-5, 7}, {7, 5}, {-7, 5}, {8, 4}, {6, 7},
	{-6, 7}, {7, 6}, {-7, 6}, {8, 5}, {7, 7}, {-7, 7}, {8, 6},
	{8, 7},
}

// neighbourhoodCodes is the inverse of neighbourhood: the code for an offset
// of x columns left, from -7 to 8, and y rows up, from 0 to 7, or zero.
var neighbourhoodCodes = func() (codes [8][16]uint8) {
	for index, offset := range neighbourhood {
		codes[offset[1]][offset[0]+7] = uint8(index + 1)
	}
	return codes
}()

// distanceCode answers the code a scan distance is written as in an image of
// the given width. An offset in the neighbourhood gets its short code; which
// offset a distance is depends on the width, so both the row it reaches and
// the one below are tried.
func distanceCode(distance, width int) uint32 {
	up := distance / width
	left := distance - up*width
	best := uint32(distance + 120)
	if up < 8 && left <= 8 {
		if code := neighbourhoodCodes[up][left+7]; code != 0 {
			best = min(best, uint32(code))
		}
	}
	if right := left - width; right >= -7 && up+1 < 8 {
		if code := neighbourhoodCodes[up+1][right+7]; code != 0 {
			best = min(best, uint32(code))
		}
	}
	return best
}

// prefixEncode splits a length or distance code, one or more, into the
// prefix symbol that is entropy coded and the extra bits written as they are.
func prefixEncode(value uint32) (symbol, extraBits, extra uint32) {
	value--
	if value < 4 {
		return value, 0, 0
	}
	high := uint32(bits.Len32(value)) - 1
	second := (value >> (high - 1)) & 1
	extraBits = high - 1
	return 2*high + second, extraBits, value & (1<<extraBits - 1)
}

// matcher finds backward references with hash chains: one over short runs
// of pixels, which finds the short copies that most of a picture is made of,
// and one over longer runs, which reaches repeated tiles far back without
// wading through every place two pixels recur.
//
// Its tables are kept from image to image. A chain holds positions plus a
// base, and anything below the base was entered for an earlier image and
// ends a chain, so moving the base past an image empties every chain at
// once: clearing the heads for every image instead would cost more than
// parsing a small one.
type matcher struct {
	// heads holds each chain's most recent position for every hash, and
	// previous, for every position, the one entered before it with the same
	// hash, both plus base.
	heads    [len(chains{})][]uint32
	previous [len(chains{})][]uint32
	// base is what the image's positions are entered plus, and next the
	// base of the image after it.
	base, next uint32
	// inserted is how many of the image's positions the chains hold.
	inserted int
	// spans are the chains' run lengths and ends the number of positions a
	// whole run starts at, none for a chain that is not searched. short is
	// the chain with more of them, whose run is the shorter, and long the
	// other.
	spans, ends [len(chains{})]int
	short, long int
	// nearCodes are the codes of the distances shorter than eight rows of an
	// image nearWidth wide, the only ones a neighbourhood code can name.
	nearCodes []uint32
	nearWidth int
	// lease counts the images parsed with the tables above, and ticket is
	// the count this matcher left there. An Encoder copied by value shares
	// the tables but not base, next or nearWidth, which say what is in them;
	// the lease is shared as the tables are, so a copy that finds another has
	// used them since its own last image makes tables of its own.
	lease  *uint64
	ticket uint64
	// candidates are the distances a search measures.
	candidates []int
	// cheapestCopy is the least a copy can cost, and reach where the pixels
	// from the position searched stop costing no more than that as literals.
	cheapestCopy float32
	reach        int
	// The image being parsed and what it is parsed with.
	pixels []uint32
	width  int
	search chains
	model  *costModel
}

// chain is one hash chain's run length and how many of its most recent
// positions a search compares; a depth of zero leaves it out.
type chain struct {
	span, depth int
}

type chains [2]chain

const matcherHashBits = 15

// hashRun carries the state of a run's hash over more of its pixels. A run
// starts from a state of zero, and its hash is the top matcherHashBits of the
// state after all its pixels, so a run's hash carries on from the state of
// any shorter run it starts with.
func hashRun(state uint32, pixels []uint32) uint32 {
	for _, pixel := range pixels {
		state = (state ^ pixel) * 0x9e3779b1
		state ^= state >> 15
	}
	return state
}

// costModel prices tokens in bits. What a pixel costs as a literal does not
// depend on how the pixels before it were parsed, because every pixel enters
// the colour cache in scan order either way, so literal prices are kept as
// running sums.
type costModel struct {
	literals []float32
	length   [lengthSymbols]float32
	distance [distanceSymbols]float32
}

// copyCost is the price of a copy of length pixels from a distance whose
// code has the given prefix symbol and extra bits.
func (model *costModel) copyCost(length, distanceSymbol, distanceBits uint32) float32 {
	symbol, extraBits, _ := prefixEncode(length)
	return model.length[symbol] + float32(extraBits) + model.distance[distanceSymbol] + float32(distanceBits)
}

// references parses pixels, width to a row, into tokens, pricing them with
// model. At each position it looks at copies from the pixel to the left, the
// row above and the most recent positions in each chain that start with the
// same pixels. The copy that saves the most bits over writing its pixels as
// literals wins, and only if it saves any, since a short copy from far away
// can cost more than the pixels it stands for; and a copy is put off by a
// pixel when the one starting there saves more.
func (m *matcher) references(tokens []token, pixels []uint32, width int, search chains, model *costModel) []token {
	m.prepare(pixels, width, search, model)
	count := len(pixels)
	for position := 0; position < count; {
		length, code, worth := m.find(position)
		if worth > 0 && length < maxCopyLength && position+1 < count {
			if next, nextCode, nextWorth := m.find(position + 1); nextWorth > worth {
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
	m.pixels, m.model = nil, nil
	return tokens
}

// prepare empties the chains for an image to parse.
func (m *matcher) prepare(pixels []uint32, width int, search chains, model *costModel) {
	count := len(pixels)
	m.pixels, m.width, m.search, m.model = pixels, width, search, model
	if m.lease == nil || *m.lease != m.ticket {
		m.lease = new(uint64)
		m.heads = [len(chains{})][]uint32{}
		m.nearCodes, m.nearWidth = nil, 0
		m.next = 0
	}
	*m.lease++
	m.ticket = *m.lease
	if m.next == 0 || uint64(m.next)+uint64(count) > 1<<32-1 {
		// The first image, or one whose positions the base cannot move past
		// any more: the heads are cleared, and the base starts over above
		// the zero a new head holds.
		for index := range m.heads {
			clear(m.heads[index])
		}
		m.next = 1
	}
	m.base = m.next
	m.next += uint32(count)
	m.inserted = 0
	// No copy costs less than the cheapest length symbol and the cheapest
	// distance symbol with no extra bits, and every price is at least zero.
	m.cheapestCopy = slices.Min(model.length[:]) + slices.Min(model.distance[:])
	m.reach = 0
	for index, chain := range search {
		m.spans[index], m.ends[index] = chain.span, 0
		if chain.depth == 0 || chain.span > count {
			continue
		}
		m.heads[index] = resize(m.heads[index], 1<<matcherHashBits)
		m.previous[index] = resize(m.previous[index], count)
		m.ends[index] = count - chain.span + 1
	}
	m.short, m.long = 0, 1
	if m.ends[1] > m.ends[0] {
		m.short, m.long = 1, 0
	}
	// A distance of eight rows or more has no neighbourhood code, so only
	// the shorter ones need a table, and only as far as the image reaches.
	near := min(8*width, count)
	if m.nearWidth != width || len(m.nearCodes) < near {
		m.nearCodes = resize(m.nearCodes, near)
		for distance := 1; distance < near; distance++ {
			m.nearCodes[distance] = distanceCode(distance, width)
		}
		m.nearWidth = width
	}
}

// insertBefore enters every position before end in the chains, so that a
// search from end finds all of them. Both chains' runs start at the
// position, so the long run's hash carries on from the short one's.
func (m *matcher) insertBefore(end int) {
	pixels, base := m.pixels, m.base
	short, long := m.short, m.long
	shortSpan, longSpan := m.spans[short], m.spans[long]
	shortEnd, longEnd := min(end, m.ends[short]), min(end, m.ends[long])
	shortHeads, longHeads := m.heads[short], m.heads[long]
	shortPrevious, longPrevious := m.previous[short], m.previous[long]
	// A position whose whole run lies inside a run of one colour is left
	// out of both chains. skipSpan is the longest run a searched chain has,
	// and at least the pixel itself, which a run of none still ends at.
	skipSpan := max(shortSpan, 1)
	if m.ends[long] > 0 {
		skipSpan = max(skipSpan, longSpan)
	}
	for position := m.inserted; position < shortEnd; position++ {
		// Inside a run of one colour every position hashes alike and the
		// pixel to the left is the copy to take, so the run's positions
		// after its first are left out.
		hasLong := position < longEnd
		leaveShort, leaveLong := false, false
		if position > 0 && pixels[position-1] == pixels[position] {
			before := pixels[position-1]
			// Runs of one colour hold most of a picture's positions, so the
			// end of this one is found first and every position whose runs
			// lie inside it is passed over at once. The run is followed no
			// further than end needs, which leaves the rest of it to the
			// next call instead of walking it again there.
			runEnd, scanEnd := position+1, min(len(pixels), end+skipSpan)
			for runEnd < scanEnd && pixels[runEnd] == before {
				runEnd++
			}
			if last := min(runEnd-skipSpan, end-1); last > position {
				position = last
				continue
			}
			leaveShort = before == pixels[position+shortSpan-1]
			leaveLong = hasLong && before == pixels[position+longSpan-1]
			if leaveShort && (leaveLong || !hasLong) {
				continue
			}
		}
		state := hashRun(0, pixels[position:position+shortSpan])
		if !leaveShort {
			hash := state >> (32 - matcherHashBits)
			shortPrevious[position] = shortHeads[hash]
			shortHeads[hash] = base + uint32(position)
		}
		if hasLong && !leaveLong {
			state = hashRun(state, pixels[position+shortSpan:position+longSpan])
			hash := state >> (32 - matcherHashBits)
			longPrevious[position] = longHeads[hash]
			longHeads[hash] = base + uint32(position)
		}
	}
	m.inserted = max(m.inserted, end)
}

// find answers the best copy at position and the bits it saves; a copy not
// worth taking saves nothing.
func (m *matcher) find(position int) (bestLength int, bestCode uint32, bestWorth float32) {
	pixels, model, base := m.pixels, m.model, m.base
	// The candidates are the pixel to the left, the row above, and the most
	// recent positions in each chain whose run matches the one here. The
	// runs here are hashed before the positions up to here are entered,
	// which measured faster than hashing them after.
	var hashes [len(chains{})]uint32
	if short, long := m.short, m.long; position < m.ends[short] {
		shortSpan, longSpan := m.spans[short], m.spans[long]
		state := hashRun(0, pixels[position:position+shortSpan])
		hashes[short] = state >> (32 - matcherHashBits)
		if position < m.ends[long] {
			state = hashRun(state, pixels[position+shortSpan:position+longSpan])
			hashes[long] = state >> (32 - matcherHashBits)
		}
	}
	m.insertBefore(position)
	// A copy whose pixels cost no more as literals than the cheapest copy is
	// never worth taking. Where literals are cheap, as the transparent pixels
	// of an update are, that rules out every copy shorter than dozens of
	// pixels, which is most of what the candidates offer there. reach never
	// moves back, since a later start leaves fewer bits in the same pixels.
	limit := min(maxCopyLength, len(pixels)-position)
	m.reach = max(m.reach, position)
	for m.reach < position+limit && model.literals[m.reach+1]-model.literals[position] <= m.cheapestCopy {
		m.reach++
	}
	// No copy of floor pixels or fewer is worth taking, so a candidate is
	// only measured if it matches the pixel just past them, and when no copy
	// can reach that pixel there is nothing to find. These comparisons hold
	// in float32 as they would exactly, because every price is at least zero
	// and rounding is monotone.
	floor := m.reach - position
	if floor >= limit {
		return 0, 0, 0
	}
	candidates := append(m.candidates[:0], 1, m.width)
	for index, chain := range m.search {
		if position >= m.ends[index] {
			continue
		}
		previous := m.previous[index]
		candidate := m.heads[index][hashes[index]]
		for tries := 0; candidate >= base && tries < chain.depth; tries++ {
			entry := int(candidate - base)
			candidates = append(candidates, position-entry)
			candidate = previous[entry]
		}
	}
	m.candidates = candidates
	target := pixels[position : position+limit]
	// What the loop reads of the tables is held here, where the compiler
	// keeps it in registers.
	nearCodes, literals := m.nearCodes, model.literals[position:]
	bestDistanceCost := float32(0)
	for index, distance := range candidates {
		// The chains are searched only while the best copy falls short of
		// the limit.
		if index >= 2 && bestLength >= limit {
			break
		}
		if distance <= 0 || distance > position || distance+120 > maxDistanceValue {
			continue
		}
		source := pixels[position-distance:][:len(target)]
		if source[floor] != target[floor] {
			continue
		}
		code := uint32(distance + 120)
		if distance < len(nearCodes) {
			code = nearCodes[distance]
		}
		distanceSymbol, distanceBits, _ := prefixEncode(code)
		distanceCost := model.distance[distanceSymbol] + float32(distanceBits)
		// A candidate that cannot reach past the best copy so far is only
		// worth measuring if its distance is cheaper to write.
		if bestLength > 0 && (bestLength >= len(target) || target[bestLength] != source[bestLength]) && distanceCost >= bestDistanceCost {
			continue
		}
		length := 0
		for length < len(target) && source[length] == target[length] {
			length++
		}
		if length == 0 {
			continue
		}
		if worth := literals[length] - literals[0] - model.copyCost(uint32(length), distanceSymbol, distanceBits); worth > bestWorth {
			bestLength, bestCode, bestWorth = length, code, worth
			bestDistanceCost = distanceCost
		}
	}
	return bestLength, bestCode, bestWorth
}
