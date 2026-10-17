package wipic

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"hash/adler32"
	"hash/crc32"
	stdimage "image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"

	"github.com/movingwoo/wfeature/internal/backend"
)

// A title edits the PNG it is about to decode, and does not fix the checksum.
//
// The pattern is a sprite sheet recoloured at run time: the game keeps one
// encoded picture in a `byte[]`, writes new entries over its palette, and hands
// the array to the platform's own "make an image out of these bytes" call. A
// handset's decoder reads the chunk it is given; it does not verify the CRC-32
// each chunk carries, so the edit costs nothing there. Go's decoder does verify
// it, and refuses the whole picture over four bytes the title never meant to be
// read.
//
// **The repair is only ever attempted after a decode has already failed.** A
// picture that decodes is decoded by the standard reader with its checks intact,
// so nothing here weakens the path every other image takes; and a chunk whose
// data is genuinely damaged still fails, because rewriting a CRC does not make
// a broken deflate stream inflate.

const (
	pngHeader   = 8
	pngChunkTag = 4
	// maxPNGChunk bounds a chunk length read out of the file. The format's own
	// limit is 2^31-1, and a length past the end of the data is a file that is
	// not walkable rather than a chunk.
	maxPNGChunk = 1 << 28
)

var pngSignature = [pngHeader]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}

// IsPNG reports whether data begins with the PNG signature.
func IsPNG(data []byte) bool {
	if len(data) < pngHeader {
		return false
	}
	for index, want := range pngSignature {
		if data[index] != want {
			return false
		}
	}
	return true
}

// RepairPNGChecksums answers a copy of a PNG with every chunk's CRC-32
// recomputed from the chunk it belongs to, and reports whether any of them
// disagreed with what the file carried.
//
// It answers false — and no copy — for anything that is not a walkable PNG, so
// a caller can tell "there was nothing to repair" from "this is not a picture
// whose only problem was a checksum". The walk stops at the first chunk whose
// length runs past the end of the data, which is the one thing a rewritten
// palette never causes.
func RepairPNGChecksums(data []byte) ([]byte, bool) {
	if !IsPNG(data) {
		return nil, false
	}
	repaired, mended := data, false
	for at := pngHeader; at+2*pngChunkTag+pngChunkTag <= len(data); {
		length := binary.BigEndian.Uint32(data[at:])
		if length > maxPNGChunk {
			return nil, false
		}
		end := at + 2*pngChunkTag + int(length) + pngChunkTag
		if end > len(data) || end < at {
			return nil, false
		}
		carried := binary.BigEndian.Uint32(data[end-pngChunkTag:])
		computed := crc32.ChecksumIEEE(data[at+pngChunkTag : end-pngChunkTag])
		if carried != computed {
			if !mended {
				// The copy is made only when there is something to write into
				// it, so a file with every checksum intact is never duplicated.
				repaired = make([]byte, len(data))
				copy(repaired, data)
				mended = true
			}
			binary.BigEndian.PutUint32(repaired[end-pngChunkTag:], computed)
		}
		at = end
	}
	return repaired, mended
}

// RepairPNGPixelChecksum answers a copy of a PNG whose compressed pixel stream
// carries the Adler-32 the data actually hashes to, and reports whether the
// one in the file disagreed.
//
// **This is the other checksum a picture carries**, and it fails for the same
// reason the chunk CRCs do: the pixels inside an IDAT are a zlib stream, whose
// last four bytes are an Adler-32 of the *uncompressed* bytes. A handset's
// decoder inflates and draws; it never checks that trailer. Go's does, and one
// local title's sprite sheet is refused over it — the deflate stream inflates
// perfectly and only the trailer is wrong.
//
// Repairing it means inflating the stream, which is the work the decode was
// going to do anyway, so it is only ever attempted after a decode has already
// failed. A stream that does not inflate is left alone: nothing is invented
// for a picture that is genuinely damaged.
func RepairPNGPixelChecksum(data []byte) ([]byte, bool) {
	if !IsPNG(data) {
		return nil, false
	}
	// Where each IDAT's payload sits, in order. The zlib stream is their
	// concatenation, so a trailer can straddle two of them.
	type span struct{ at, length int }
	var spans []span
	var header []byte
	total := 0
	for at := pngHeader; at+2*pngChunkTag+pngChunkTag <= len(data); {
		length := binary.BigEndian.Uint32(data[at:])
		if length > maxPNGChunk {
			return nil, false
		}
		end := at + 2*pngChunkTag + int(length) + pngChunkTag
		if end > len(data) || end < at {
			return nil, false
		}
		switch string(data[at+pngChunkTag : at+2*pngChunkTag]) {
		case "IDAT":
			spans = append(spans, span{at: at + 2*pngChunkTag, length: int(length)})
			total += int(length)
		case "IHDR":
			if header == nil {
				header = data[at+2*pngChunkTag : end-pngChunkTag]
			}
		}
		at = end
	}
	// A zlib stream is a two-byte header, the deflate data, and four bytes of
	// Adler-32; anything shorter than those six bytes is not one.
	if total < 6 {
		return nil, false
	}
	// The header says how many bytes the pixels inflate to. A stream that
	// runs past that is refused by the decoder whatever its trailer says, so
	// it is not read further — deflate expands a thousandfold, and a few
	// megabytes of it would otherwise inflate to gigabytes here.
	limit := pngPixelBytes(header)
	if limit == 0 {
		return nil, false
	}
	stream := make([]byte, 0, total)
	for _, part := range spans {
		stream = append(stream, data[part.at:part.at+part.length]...)
	}
	// flate reads the deflate data on its own, without the trailer the zlib
	// reader would check — which is the whole point of doing it here.
	hash := adler32.New()
	inflated, err := io.Copy(hash, io.LimitReader(flate.NewReader(bytes.NewReader(stream[2:len(stream)-4])), int64(limit)+1))
	if err != nil || uint64(inflated) > limit {
		return nil, false
	}
	computed := hash.Sum32()
	if binary.BigEndian.Uint32(stream[len(stream)-4:]) == computed {
		return nil, false
	}
	binary.BigEndian.PutUint32(stream[len(stream)-4:], computed)
	repaired := make([]byte, len(data))
	copy(repaired, data)
	written := 0
	for _, part := range spans {
		copy(repaired[part.at:part.at+part.length], stream[written:written+part.length])
		written += part.length
	}
	// The payload moved, so the chunk CRCs that cover it have to follow.
	mended, _ := RepairPNGChecksums(repaired)
	if mended != nil {
		repaired = mended
	}
	return repaired, true
}

// adam7Passes are the interlace passes as column and row offset, then column
// and row step.
var adam7Passes = [7][4]uint64{{0, 0, 8, 8}, {4, 0, 8, 8}, {0, 4, 4, 8}, {2, 0, 4, 4}, {0, 2, 2, 4}, {1, 0, 2, 2}, {0, 1, 1, 2}}

// pngPixelBytes answers how many bytes the pixel stream behind this IHDR
// inflates to: a filter byte and the packed samples for every row of every
// pass. Zero answers a header that names no picture this package decodes.
func pngPixelBytes(header []byte) uint64 {
	if len(header) != 13 {
		return 0
	}
	width, height := binary.BigEndian.Uint32(header), binary.BigEndian.Uint32(header[4:])
	if width == 0 || height == 0 || width > math.MaxInt32 || height > math.MaxInt32 ||
		checkDecodeSize(int(width), int(height)) != nil {
		return 0
	}
	var samples uint64
	switch header[9] {
	case 0, 3:
		samples = 1
	case 4:
		samples = 2
	case 2:
		samples = 3
	case 6:
		samples = 4
	default:
		return 0
	}
	depth := uint64(header[8])
	if depth != 1 && depth != 2 && depth != 4 && depth != 8 && depth != 16 {
		return 0
	}
	rowBytes := func(columns uint64) uint64 { return 1 + (columns*samples*depth+7)/8 }
	switch header[12] {
	case 0:
		return uint64(height) * rowBytes(uint64(width))
	case 1:
		total := uint64(0)
		for _, pass := range adam7Passes {
			if uint64(width) <= pass[0] || uint64(height) <= pass[1] {
				continue
			}
			columns := (uint64(width) - pass[0] + pass[2] - 1) / pass[2]
			rows := (uint64(height) - pass[1] + pass[3] - 1) / pass[3]
			total += rows * rowBytes(columns)
		}
		return total
	}
	return 0
}

// checkDecodeSize refuses a header naming more pixels than a framebuffer may
// hold, before a decoder allocates the picture it names; the standard readers
// size their buffer from the header and only then find the data missing. It
// is the bound SKT's router already applies to the same header, so all three
// platforms refuse the same pictures, and sixteen million pixels is dozens of
// times the largest handset screen. An empty size is left to the decoder,
// which judges it as it always has.
func checkDecodeSize(width, height int) error {
	if width <= 0 || height <= 0 {
		return nil
	}
	if _, err := backend.RGBAByteLength(width, height); err != nil {
		return fmt.Errorf("decode image dimensions: %w", err)
	}
	return nil
}

// decodeBounded runs the standard readers once the header has passed
// checkDecodeSize. A header the reader cannot parse fails here exactly as the
// full decode would have failed on it.
func decodeBounded(data []byte) (stdimage.Image, string, error) {
	config, _, err := stdimage.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	if err := checkDecodeSize(config.Width, config.Height); err != nil {
		return nil, "", err
	}
	return stdimage.Decode(bytes.NewReader(data))
}

// DecodeStandard decodes with the standard library's own readers, and retries
// over a PNG whose checksums a title rewrote — first the chunk CRCs, then the
// Adler-32 the compressed pixels carry.
//
// It is where the three platforms' image routers end up once they have ruled
// out the formats the standard library does not register — the handset bitmap
// and the BMP with a transparent palette entry — so the tolerance above is the
// same on all of them without any of them stating it.
func DecodeStandard(data []byte) (stdimage.Image, string, error) {
	decoded, format, err := decodeBounded(data)
	if err == nil {
		return decoded, format, nil
	}
	if repaired, mended := RepairPNGChecksums(data); mended {
		if decoded, format, retryErr := decodeBounded(repaired); retryErr == nil {
			return decoded, format, nil
		}
		data = repaired
	}
	repaired, mended := RepairPNGPixelChecksum(data)
	if !mended {
		return nil, "", err
	}
	return decodeBounded(repaired)
}
