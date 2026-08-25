package wipic

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"hash/adler32"
	"hash/crc32"
	stdimage "image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
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
		if string(data[at+pngChunkTag:at+2*pngChunkTag]) == "IDAT" {
			spans = append(spans, span{at: at + 2*pngChunkTag, length: int(length)})
			total += int(length)
		}
		at = end
	}
	// A zlib stream is a two-byte header, the deflate data, and four bytes of
	// Adler-32; anything shorter than those six bytes is not one.
	if total < 6 {
		return nil, false
	}
	stream := make([]byte, 0, total)
	for _, part := range spans {
		stream = append(stream, data[part.at:part.at+part.length]...)
	}
	// flate reads the deflate data on its own, without the trailer the zlib
	// reader would check — which is the whole point of doing it here.
	inflated, err := io.ReadAll(flate.NewReader(bytes.NewReader(stream[2 : len(stream)-4])))
	if err != nil {
		return nil, false
	}
	computed := adler32.Checksum(inflated)
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

// DecodeStandard decodes with the standard library's own readers, and retries
// over a PNG whose checksums a title rewrote — first the chunk CRCs, then the
// Adler-32 the compressed pixels carry.
//
// It is where the three platforms' image routers end up once they have ruled
// out the formats the standard library does not register — the handset bitmap
// and the BMP with a transparent palette entry — so the tolerance above is the
// same on all of them without any of them stating it.
func DecodeStandard(data []byte) (stdimage.Image, string, error) {
	decoded, format, err := stdimage.Decode(bytes.NewReader(data))
	if err == nil {
		return decoded, format, nil
	}
	if repaired, mended := RepairPNGChecksums(data); mended {
		if decoded, format, retryErr := stdimage.Decode(bytes.NewReader(repaired)); retryErr == nil {
			return decoded, format, nil
		}
		data = repaired
	}
	repaired, mended := RepairPNGPixelChecksum(data)
	if !mended {
		return nil, "", err
	}
	return stdimage.Decode(bytes.NewReader(repaired))
}
