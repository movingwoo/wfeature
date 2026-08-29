package ktf

import (
	"errors"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm"
)

// Graphics.setRGBPixels takes bytes per line, not elements. The specification
// says so — "한 줄의 이미지가 저장되기 위해서 필요한 바이트 수" — and the WIPI C
// call beside it was already read that way here. Reading it as an element
// pitch multiplies the span by four: one title hands over an 88x102 picture in
// an 8976 element array with a line of 352, which is exactly 88 pixels of four
// bytes and exactly fills the array, and this platform ended the session on its
// first frame.
func TestRGBPixelsCountsBytesPerLine(t *testing.T) {
	client, runtime := newTestRuntime(t)
	graphics, err := runtime.newScreenGraphics()
	if err != nil {
		t.Fatal(err)
	}
	const width, height = 88, 102
	pixels, err := client.JVM().NewArray(jvm.Type{Kind: jvm.TypeInt}, width*height)
	if err != nil {
		t.Fatal(err)
	}
	set := runtimeGraphicsTransferRGBPixels(true)
	call := func(bytesPerLine int32) error {
		_, err := set(runtime, client.JVM(), []jvm.Value{
			jvm.ReferenceValue(graphics),
			jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(width), jvm.IntValue(height),
			jvm.ReferenceValue(pixels), jvm.IntValue(0), jvm.IntValue(bytesPerLine),
		})
		return err
	}

	// The array holds the picture exactly, so the call the title makes fits.
	if err := call(width * 4); err != nil {
		t.Fatalf("a picture that exactly fills its array was refused: %v", err)
	}
	// A zero line is the tightly-packed case and fits too.
	if err := call(0); err != nil {
		t.Fatalf("a zero bytes-per-line was refused: %v", err)
	}

	// A range that really does run past the array is the exception the
	// specification names, not a platform failure — a title guarding its own
	// call with a catch gets nothing from an error the guest cannot see.
	err = call(width * 8)
	var guest *jvm.GuestException
	if !errors.As(err, &guest) || guest.Object == nil || guest.Object.ClassName != "java/lang/ArrayIndexOutOfBoundsException" {
		t.Fatalf("an over-long range answered %v, want an ArrayIndexOutOfBoundsException", err)
	}

	// A null array is the other throw the specification names.
	_, err = set(runtime, client.JVM(), []jvm.Value{
		jvm.ReferenceValue(graphics),
		jvm.IntValue(0), jvm.IntValue(0), jvm.IntValue(width), jvm.IntValue(height),
		jvm.ReferenceValue(nil), jvm.IntValue(0), jvm.IntValue(width * 4),
	})
	if !errors.As(err, &guest) || guest.Object == nil || guest.Object.ClassName != "java/lang/NullPointerException" {
		t.Fatalf("a null array answered %v, want a NullPointerException", err)
	}
}
