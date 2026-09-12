package skt

type scriptSISRunCode struct {
	bits   uint16
	length uint8
}

// The row is the expected pixel value and the column is the run length minus
// one. These input/output pairs were independently measured across every
// 12-bit input to the original helper. In particular, seven black codes differ
// from the otherwise related T.4 codebook.
var scriptSISRunCodes = [2][64]scriptSISRunCode{
	{
		{0b000111, 6}, {0b0111, 4}, {0b1000, 4}, {0b1011, 4}, // 1-4
		{0b1100, 4}, {0b1110, 4}, {0b1111, 4}, {0b10011, 5}, // 5-8
		{0b10100, 5}, {0b00111, 5}, {0b01000, 5}, {0b001000, 6}, // 9-12
		{0b000011, 6}, {0b110100, 6}, {0b110101, 6}, {0b101010, 6}, // 13-16
		{0b101011, 6}, {0b0100111, 7}, {0b0001100, 7}, {0b0001000, 7}, // 17-20
		{0b0010111, 7}, {0b0000011, 7}, {0b0000100, 7}, {0b0101000, 7}, // 21-24
		{0b0101011, 7}, {0b0010011, 7}, {0b0100100, 7}, {0b0011000, 7}, // 25-28
		{0b00000010, 8}, {0b00000011, 8}, {0b00011010, 8}, {0b00011011, 8}, // 29-32
		{0b00010010, 8}, {0b00010011, 8}, {0b00010100, 8}, {0b00010101, 8}, // 33-36
		{0b00010110, 8}, {0b00010111, 8}, {0b00101000, 8}, {0b00101001, 8}, // 37-40
		{0b00101010, 8}, {0b00101011, 8}, {0b00101100, 8}, {0b00101101, 8}, // 41-44
		{0b00000100, 8}, {0b00000101, 8}, {0b00001010, 8}, {0b00001011, 8}, // 45-48
		{0b01010010, 8}, {0b01010011, 8}, {0b01010100, 8}, {0b01010101, 8}, // 49-52
		{0b00100100, 8}, {0b00100101, 8}, {0b01011000, 8}, {0b01011001, 8}, // 53-56
		{0b01011010, 8}, {0b01011011, 8}, {0b01001010, 8}, {0b01001011, 8}, // 57-60
		{0b00110010, 8}, {0b00110011, 8}, {0b00110100, 8}, {0b11011, 5}, // 61-64
	},
	{
		{0b010, 3}, {0b11, 2}, {0b10, 2}, {0b011, 3}, // 1-4
		{0b0011, 4}, {0b0010, 4}, {0b00011, 5}, {0b000101, 6}, // 5-8
		{0b000100, 6}, {0b0000100, 7}, {0b0000101, 7}, {0b0000111, 7}, // 9-12
		{0b00000100, 8}, {0b00000001, 8}, {0b000011000, 9}, {0b0000010111, 10}, // 13-16
		{0b0000001000, 10}, {0b00001100111, 11}, {0b00001101000, 11}, {0b00001101100, 11}, // 17-20
		{0b00001101110, 11}, {0b00000110111, 11}, {0b00000101000, 11}, {0b00000010111, 11}, // 21-24
		{0b00000011000, 11}, {0b000011001010, 12}, {0b000011001011, 12}, {0b000011001100, 12}, // 25-28
		{0b000011001101, 12}, {0b000001101000, 12}, {0b000001101001, 12}, {0b000001101010, 12}, // 29-32
		{0b000001101011, 12}, {0b000011010010, 12}, {0b000011010011, 12}, {0b000011010100, 12}, // 33-36
		{0b000011010101, 12}, {0b000011010110, 12}, {0b000011010111, 12}, {0b000001101100, 12}, // 37-40
		{0b000001101101, 12}, {0b000011011010, 12}, {0b000011011011, 12}, {0b000001010100, 12}, // 41-44
		{0b000001010101, 12}, {0b000001010110, 12}, {0b000001010111, 12}, {0b000001100100, 12}, // 45-48
		{0b000001100101, 12}, {0b000001010010, 12}, {0b000001010011, 12}, {0b000000100100, 12}, // 49-52
		{0b000000110111, 12}, {0b000000111000, 12}, {0b000000100111, 12}, {0b000000101000, 12}, // 53-56
		{0b000001011000, 12}, {0b000001011001, 12}, {0b000000101011, 12}, {0b000000101100, 12}, // 57-60
		{0b000001011010, 12}, {0b000001100110, 12}, {0b000001100111, 12}, {0b000001111, 9}, // 61-64
	},
}

var scriptSISRunDecodeTable = func() [2][8192]uint8 {
	var table [2][8192]uint8
	for color, codes := range scriptSISRunCodes {
		for index, code := range codes {
			key := uint16(1)<<code.length | code.bits
			table[color][key] = uint8(index + 1)
		}
	}
	return table
}()

func decodeScriptSISRun(r *scriptSISBitReader, color uint) (int, bool) {
	if color > 1 {
		return 0, false
	}
	key := uint16(1)
	for range 12 {
		bit, ok := r.read(1)
		if !ok {
			return 0, false
		}
		key = key<<1 | uint16(bit)
		if run := scriptSISRunDecodeTable[color][key]; run != 0 {
			return int(run), true
		}
	}
	return 0, false
}

func decodeScriptSISCodedTile(r *scriptSISBitReader) ([64]byte, bool) {
	var pixels [64]byte
	color, ok := r.read(1)
	if !ok {
		return pixels, false
	}
	position := 0
	for position < len(pixels) {
		run, ok := decodeScriptSISRun(r, color)
		if !ok || run > len(pixels)-position {
			return [64]byte{}, false
		}
		if color != 0 {
			for pixel := position; pixel < position+run; pixel++ {
				pixels[pixel] = 1
			}
		}
		position += run
		color ^= 1
	}
	return pixels, true
}
