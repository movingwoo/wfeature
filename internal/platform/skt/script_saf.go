package skt

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

// scriptSAFObject owns decoded packed pixels. Its index and render attribute
// belong to the SAF object header, independently of animation frame numbers.
type scriptSAFObject struct {
	index, width, height int
	attribute            byte
	flag                 bool
	pixels               []byte
}

// decodeScriptSAFObject reads the body of an object record, including its
// separate two-byte payload length. It does not change decoder or guest state.
// Compression zero requires a separate codec and is intentionally unsupported.
func decodeScriptSAFObject(vm *sgsvm.VM, data []byte, depth int) (scriptSAFObject, int, error) {
	fail := func(err error) (scriptSAFObject, int, error) {
		return scriptSAFObject{}, 0, err
	}
	if len(data) < 7 {
		return fail(fmt.Errorf("SGS SAF object header is truncated"))
	}
	width, height := int(data[1]), int(data[2])
	if width == 0 || height == 0 || (depth != 1 && depth != 2 && depth != 4 && depth != 8) {
		return fail(fmt.Errorf("SGS SAF object dimensions or bit depth are invalid"))
	}
	bits := width * height * depth
	if bits%8 != 0 {
		return fail(fmt.Errorf("SGS SAF object has an incomplete packed byte"))
	}
	size := bits / 8
	length := int(binary.BigEndian.Uint16(data[5:7]))
	if length > len(data)-7 {
		return fail(fmt.Errorf("SGS SAF object payload is truncated"))
	}
	if data[3] != 1 {
		return fail(fmt.Errorf("unsupported SGS SAF object compression %d", data[3]))
	}
	if err := vm.ChargeWork(1 + length + size); err != nil {
		return fail(err)
	}
	reader, err := zlib.NewReader(bytes.NewReader(data[7 : 7+length]))
	if err != nil {
		return fail(fmt.Errorf("SGS SAF object compression: %w", err))
	}
	defer reader.Close()
	// One extra byte detects oversized output while still reaching the checksum
	// for an exact-size stream. Allocation never follows the compressed contents.
	pixels, err := io.ReadAll(io.LimitReader(reader, int64(size)+1))
	if err != nil {
		return fail(fmt.Errorf("SGS SAF object decompression: %w", err))
	}
	if len(pixels) != size {
		return fail(fmt.Errorf("SGS SAF object decoded size %d, expected %d", len(pixels), size))
	}
	return scriptSAFObject{
		index: int(data[0] & 0x7f), width: width, height: height,
		attribute: data[4], flag: data[0]&0x80 != 0, pixels: pixels,
	}, 7 + length, nil
}
