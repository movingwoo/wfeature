package vp8l

// lengthToken is one step of a code's lengths as written: a length, or a
// repeat (16), a short run of zeros (17) or a long one (18) with its extra
// bits.
type lengthToken struct {
	symbol, extra uint8
}

// lengthCoder writes the lengths of a prefix code.
type lengthCoder struct {
	tokens  []lengthToken
	counts  [19]uint32
	lengths [19]uint8
	code    prefixCode
}

var lengthExtraBits = [19]uint8{16: 2, 17: 3, 18: 7}

// writeCode builds the prefix code for the counts over an alphabet, writes
// its description, and leaves it in code for the symbols that follow.
func (iw *imageWriter) writeCode(w *bitWriter, code *prefixCode, counts []uint32, alphabet int) {
	code.reset(alphabet)
	symbols := iw.builder.use(counts)
	// One or two symbols under 256 have a short form of their own; so does
	// a code nothing uses, which is written as the single symbol zero.
	if len(symbols) <= 2 && (len(symbols) == 0 || symbols[len(symbols)-1] < literalSymbols) {
		used := [2]int32{}
		usedCount := copy(used[:], symbols)
		if usedCount == 0 {
			usedCount = 1
		}
		w.write(1, 1)
		w.write(uint32(usedCount-1), 1)
		if used[0] < 2 {
			w.write(0, 1)
			w.write(uint32(used[0]), 1)
		} else {
			w.write(1, 1)
			w.write(uint32(used[0]), 8)
		}
		code.lengths[used[0]] = 1
		if usedCount == 2 {
			w.write(uint32(used[1]), 8)
			code.lengths[used[1]] = 1
		}
		code.assign(used[:usedCount])
		return
	}
	iw.builder.buildUsed(counts, maxCodeLength, code.lengths)
	code.assign(symbols)
	w.write(0, 1)
	iw.lengths.write(w, &iw.builder, code.lengths)
}

// write describes lengths with the code length code: runs become repeats,
// and when the lengths end in zeros the count of tokens to read is written
// instead of the zeros, if that is shorter.
func (coder *lengthCoder) write(w *bitWriter, builder *huffmanBuilder, lengths []uint8) {
	coder.tokens = appendLengthTokens(coder.tokens[:0], lengths)
	tokens := coder.tokens
	kept := len(tokens)
	for kept > 0 && (tokens[kept-1].symbol == 0 || tokens[kept-1].symbol >= 17) {
		kept--
	}
	// The count written is at least two.
	kept = max(kept, min(2, len(tokens)))
	countBits := uint32(2)
	for kept > 2 && uint32(kept-2) >= 1<<countBits {
		countBits += 2
	}
	// Each way is planned once; the code of the way taken is the one left
	// in the coder, which takes the full way back when trimming does not pay.
	fullCost, fullHeader := coder.plan(builder, tokens)
	trim := false
	if kept < len(tokens) {
		full := coder.lengths
		trimmedCost, trimmedHeader := coder.plan(builder, tokens[:kept])
		trim = trimmedCost+trimmedHeader+3+countBits < fullCost+fullHeader
		if trim {
			tokens = tokens[:kept]
		} else {
			coder.lengths = full
			coder.assign()
		}
	}
	last := coder.last()
	w.write(uint32(last-4), 4)
	for _, symbol := range codeLengthOrder[:last] {
		w.write(uint32(coder.lengths[symbol]), 3)
	}
	if trim {
		w.write(1, 1)
		w.write((countBits-2)/2, 3)
		w.write(uint32(kept-2), uint(countBits))
	} else {
		w.write(0, 1)
	}
	for _, step := range tokens {
		coder.code.put(w, int(step.symbol))
		if extra := lengthExtraBits[step.symbol]; extra > 0 {
			w.write(uint32(step.extra), uint(extra))
		}
	}
}

// plan builds the code length code for tokens into coder.code and answers
// the bits the tokens take with it and the bits its own lengths take.
func (coder *lengthCoder) plan(builder *huffmanBuilder, tokens []lengthToken) (uint32, uint32) {
	coder.counts = [19]uint32{}
	for _, step := range tokens {
		coder.counts[step.symbol]++
	}
	builder.build(coder.counts[:], maxCodeLengthLength, coder.lengths[:])
	coder.assign()
	// Every token of a symbol costs the same, so the cost is summed by
	// symbol rather than token by token.
	cost := uint32(0)
	for symbol, count := range coder.counts {
		cost += count * (uint32(coder.code.sizes[symbol]) + uint32(lengthExtraBits[symbol]))
	}
	return cost, uint32(coder.last()) * 3
}

// assign makes the coder's code from its lengths.
func (coder *lengthCoder) assign() {
	coder.code.reset(19)
	var symbols [19]int32
	used := 0
	for symbol, length := range coder.lengths {
		coder.code.lengths[symbol] = length
		if length > 0 {
			symbols[used] = int32(symbol)
			used++
		}
	}
	coder.code.assign(symbols[:used])
}

// last answers how many of the code length code's lengths are written: the
// ones up to the last that is not zero in the order they are written, and
// never fewer than four.
func (coder *lengthCoder) last() int {
	last := 4
	for index, symbol := range codeLengthOrder {
		if coder.lengths[symbol] != 0 {
			last = max(last, index+1)
		}
	}
	return last
}

// appendLengthTokens turns lengths into tokens: a run of three or more zeros
// is one or more zero runs, and a length repeated three or more times after
// its first appearance is repeats of it.
func appendLengthTokens(tokens []lengthToken, lengths []uint8) []lengthToken {
	for index := 0; index < len(lengths); {
		value := lengths[index]
		run := 1
		for index+run < len(lengths) && lengths[index+run] == value {
			run++
		}
		index += run
		if value == 0 {
			for run >= 11 {
				step := min(run, 138)
				tokens = append(tokens, lengthToken{18, uint8(step - 11)})
				run -= step
			}
			if run >= 3 {
				tokens = append(tokens, lengthToken{17, uint8(run - 3)})
				run = 0
			}
			for ; run > 0; run-- {
				tokens = append(tokens, lengthToken{0, 0})
			}
			continue
		}
		tokens = append(tokens, lengthToken{value, 0})
		run--
		for run >= 3 {
			step := min(run, 6)
			tokens = append(tokens, lengthToken{16, uint8(step - 3)})
			run -= step
		}
		for ; run > 0; run-- {
			tokens = append(tokens, lengthToken{value, 0})
		}
	}
	return tokens
}
