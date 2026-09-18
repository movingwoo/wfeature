package lgt

import (
	"reflect"
	"testing"
)

func TestStringUTF16SubstringAndCharArray(t *testing.T) {
	client := fixtureClient(t)
	units := []uint16{'A', 0xd83d, 0xde00, 'B', 0xd800}
	object := newTestString(t, client, javaTextOfUnits(units))
	length, err := javaStringLength(client, nil, nil, []uint32{object})
	if err != nil || length != 5 {
		t.Fatalf("length = %d, %v", length, err)
	}
	for _, bounds := range [][2]uint32{{1, 3}, {1, 2}, {2, 3}, {4, 5}} {
		result, err := javaStringSubstring(client, nil, nil, []uint32{object, bounds[0], bounds[1]})
		if err != nil {
			t.Fatal(err)
		}
		array, err := javaStringToCharArray(client, nil, nil, []uint32{result})
		if err != nil {
			t.Fatal(err)
		}
		got, err := client.readJavaArrayChars(array)
		if err != nil || !reflect.DeepEqual(got, units[bounds[0]:bounds[1]]) {
			t.Fatalf("substring %v = %x, %v", bounds, got, err)
		}
	}
	if _, err := javaBufferSetLength(client, nil, nil, []uint32{object, 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := javaBufferAppendChar(client, nil, nil, []uint32{object, 0xde00}); err != nil {
		t.Fatal(err)
	}
	text, _ := client.javaText(object)
	if text != "A😀" {
		t.Fatalf("split pair did not rejoin: %q", text)
	}
}
