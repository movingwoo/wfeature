package classfile

import "encoding/binary"

type reader struct {
	data   []byte
	offset int
}

func newReader(data []byte) *reader {
	return &reader{data: data}
}

func (r *reader) remaining() int {
	return len(r.data) - r.offset
}

func (r *reader) take(size int) ([]byte, error) {
	start := r.offset
	if size < 0 || size > r.remaining() {
		return nil, invalid(start, "need %d bytes, have %d", size, r.remaining())
	}
	r.offset += size
	return r.data[start:r.offset], nil
}

func (r *reader) u1() (uint8, error) {
	data, err := r.take(1)
	if err != nil {
		return 0, err
	}
	return data[0], nil
}

func (r *reader) u2() (uint16, error) {
	data, err := r.take(2)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint16(data), nil
}

func (r *reader) u4() (uint32, error) {
	data, err := r.take(4)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(data), nil
}

func (r *reader) u8() (uint64, error) {
	data, err := r.take(8)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(data), nil
}

func (r *reader) sized(length uint32) ([]byte, error) {
	if uint64(length) > uint64(r.remaining()) {
		return nil, invalid(r.offset, "declared length %d exceeds remaining input", length)
	}
	return r.take(int(length))
}
