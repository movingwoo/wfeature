package skt

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/sgsvm"
)

func authoredSAFObject(t *testing.T, width, height byte, pixels []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write(pixels); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data := []byte{0x85, width, height, 1, 23, 0, 0}
	binary.BigEndian.PutUint16(data[5:7], uint16(compressed.Len()))
	return append(data, compressed.Bytes()...)
}

func TestScriptSAFObjectDecodesOwnedPackedPixels(t *testing.T) {
	for _, depth := range []int{1, 2, 4, 8} {
		pixels := make([]byte, 8*2*depth/8)
		for i := range pixels {
			pixels[i] = byte(i * 17)
		}
		data := authoredSAFObject(t, 8, 2, pixels)
		length := len(data)
		data = append(data, 42, 43, 44)
		before := bytes.Clone(data)
		object, consumed, err := decodeScriptSAFObject(sgsvm.New(&sgsvm.Program{}, nil), data, depth)
		if err != nil {
			t.Fatal(err)
		}
		if consumed != length || object.index != 5 || !object.flag || object.attribute != 23 || object.width != 8 || object.height != 2 || !bytes.Equal(object.pixels, pixels) {
			t.Fatalf("depth %d: object=%+v consumed=%d", depth, object, consumed)
		}
		if !bytes.Equal(data, before) {
			t.Fatal("decoding changed the source")
		}
		clear(data)
		if !bytes.Equal(object.pixels, pixels) {
			t.Fatal("decoded pixels alias compressed source")
		}
	}
}

func TestScriptSAFObjectRejectsMalformedAndOversizedData(t *testing.T) {
	valid := authoredSAFObject(t, 8, 2, bytes.Repeat([]byte{42}, 16))
	cases := [][]byte{nil, valid[:6], valid[:len(valid)-1]}
	for _, change := range [][2]int{{1, 0}, {2, 0}, {3, 0}, {3, 2}, {5, 255}} {
		data := bytes.Clone(valid)
		data[change[0]] = byte(change[1])
		cases = append(cases, data)
	}
	corrupt := bytes.Clone(valid)
	corrupt[len(corrupt)-1] ^= 1
	cases = append(cases, corrupt)
	for _, size := range []int{0, 15, 17, 1 << 20} {
		cases = append(cases, authoredSAFObject(t, 8, 2, make([]byte, size)))
	}
	for i, data := range cases {
		before := bytes.Clone(data)
		object, consumed, err := decodeScriptSAFObject(sgsvm.New(&sgsvm.Program{}, nil), data, 8)
		if err == nil || consumed != 0 || object.pixels != nil || !bytes.Equal(data, before) {
			t.Fatalf("invalid case %d returned a result or mutated input", i)
		}
	}
	for _, depth := range []int{-1, 0, 3, 16} {
		if _, _, err := decodeScriptSAFObject(sgsvm.New(&sgsvm.Program{}, nil), valid, depth); err == nil {
			t.Fatalf("accepted bit depth %d", depth)
		}
	}
	if _, _, err := decodeScriptSAFObject(sgsvm.New(&sgsvm.Program{}, nil), authoredSAFObject(t, 1, 1, []byte{0}), 1); err == nil {
		t.Fatal("accepted partial packed byte")
	}
}

func TestScriptSAFObjectWorkPreflight(t *testing.T) {
	vm := sgsvm.New(&sgsvm.Program{}, nil)
	vm.ChargeWork(sgsvm.MaxSteps)
	data := authoredSAFObject(t, 8, 2, make([]byte, 16))
	if object, consumed, err := decodeScriptSAFObject(vm, data, 8); err == nil || consumed != 0 || object.pixels != nil {
		t.Fatal("decompression ignored exhausted work budget")
	}
}

func TestScriptSAFObjectMaximumDimensions(t *testing.T) {
	pixels := make([]byte, 255*255)
	for i := range pixels {
		pixels[i] = byte(i*17 + i/8)
	}
	data := authoredSAFObject(t, 255, 255, pixels)
	object, consumed, err := decodeScriptSAFObject(sgsvm.New(&sgsvm.Program{}, nil), data, 8)
	if err != nil || consumed != len(data) || !bytes.Equal(object.pixels, pixels) {
		t.Fatalf("maximum-size object decode: consumed=%d error=%v", consumed, err)
	}
}
