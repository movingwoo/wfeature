package skt

import (
	"bytes"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// These handsets' default charset is EUC-KR, and a title reaches it through
// `String.getBytes()`: the bytes go straight into the title's own text
// renderer, which indexes a glyph table with them. UTF-8 makes that index a
// different number, and an in-game menu died drawing one Korean label while
// the labels beside it — byte arrays out of the title's own resources — drew.
func TestPlatformCharsetIsTheHandsets(t *testing.T) {
	runtime := startConnectorFixture(t)
	text := "네트워크 등록"
	want := []byte{0xb3, 0xd7, 0xc6, 0xae, 0xbf, 0xf6, 0xc5, 0xa9, 0x20, 0xb5, 0xee, 0xb7, 0xcf}

	result, err := runtime.VM.InvokeVirtual(runtime.VM.NewString(text), "getBytes", "()[B")
	if err != nil {
		t.Fatalf("String.getBytes() error = %v", err)
	}
	object, err := result.Reference()
	if err != nil {
		t.Fatal(err)
	}
	got, err := jvm.ByteArraySnapshot(object)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("getBytes() = % x, want % x", got, want)
	}

	// The other direction is the same charset, because a title also builds
	// Strings out of the bytes it packed itself.
	if decoded := decodePlatformBytes(want); decoded != text {
		t.Fatalf("decodePlatformBytes(% x) = %q, want %q", want, decoded, text)
	}
}
