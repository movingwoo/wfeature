package lgt

import "testing"

// An array is an object whose block opens with the length and carries the
// elements from one word in, and the element width is what the compiled code
// strides by — so the platform has to size the block by the same number the
// guest shifts by.
func TestJavaArrayBlockCarriesTheLengthAndTheElements(t *testing.T) {
	client := fixtureClient(t)

	for _, want := range []struct {
		code  uint32
		name  string
		bytes uint32
	}{
		{javaArrayByte, "[B", 1},
		{javaArrayChar, "[C", 2},
		{javaArrayShort, "[S", 2},
		{javaArrayInt, "[I", 4},
		{javaArrayLong, "[J", 8},
	} {
		class, err := client.resolveJavaArrayType(1, 0, want.code)
		if err != nil {
			t.Fatalf("resolveJavaArrayType(%d) error = %v", want.code, err)
		}
		if class.Name != want.name || class.ElementBytes != want.bytes {
			t.Errorf("code %d = %q at %d bytes, want %q at %d",
				want.code, class.Name, class.ElementBytes, want.name, want.bytes)
		}
	}

	class, err := client.resolveJavaArrayType(1, 0, javaArrayInt)
	if err != nil {
		t.Fatal(err)
	}
	array, err := client.allocateJavaArray(class.Object, 4)
	if err != nil {
		t.Fatalf("allocateJavaArray() error = %v", err)
	}
	block, err := client.readWord(array + 8)
	if err != nil {
		t.Fatal(err)
	}
	length, err := client.readWord(block)
	if err != nil {
		t.Fatal(err)
	}
	if length != 4 {
		t.Errorf("the array reports length %d, want 4", length)
	}
	// The last element has to be inside the block: a store past it is the
	// corruption this sizing exists to prevent, and the guest's own bounds
	// check will not catch it.
	if err := client.writeWord(block+4+3*4, 0x5a5a5a5a); err != nil {
		t.Errorf("the last element is not writable: %v", err)
	}
}

// An array of arrays is built here, not by the module: the outer array holds
// references to the inner ones, and the type one dimension in is read off the
// name.
func TestJavaMultiArrayBuildsEveryDimension(t *testing.T) {
	client := fixtureClient(t)
	class, err := client.resolveJavaArrayType(2, 0, javaArrayByte)
	if err != nil {
		t.Fatalf("resolveJavaArrayType() error = %v", err)
	}
	if class.Name != "[[B" || class.ElementBytes != 4 {
		t.Fatalf("the outer type is %q at %d bytes", class.Name, class.ElementBytes)
	}
	lengths, err := client.allocateWords([]uint32{3, 5})
	if err != nil {
		t.Fatal(err)
	}
	array, err := client.allocateJavaMultiArray(class.Object, lengths, 2)
	if err != nil {
		t.Fatalf("allocateJavaMultiArray() error = %v", err)
	}
	block, err := client.readWord(array + 8)
	if err != nil {
		t.Fatal(err)
	}
	if outer, err := client.readWord(block); err != nil || outer != 3 {
		t.Fatalf("the outer array reports %d, %v", outer, err)
	}
	for index := uint32(0); index < 3; index++ {
		inner, err := client.readWord(block + 4 + index*4)
		if err != nil || inner == 0 {
			t.Fatalf("inner array %d is %#x, %v", index, inner, err)
		}
		innerBlock, err := client.readWord(inner + 8)
		if err != nil {
			t.Fatal(err)
		}
		if length, err := client.readWord(innerBlock); err != nil || length != 5 {
			t.Errorf("inner array %d reports length %d, %v", index, length, err)
		}
	}
}

// A reference store is a platform call because a primitive store is not, and
// the bound it checks is the array's own.
func TestJavaReferenceStoreIsBoundsChecked(t *testing.T) {
	client := fixtureClient(t)
	class, err := client.resolveJavaArrayType(1, 0, javaArrayInt)
	if err != nil {
		t.Fatal(err)
	}
	array, err := client.allocateJavaArray(class.Object, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.storeJavaArrayReference(array, 1, 0x1234); err != nil {
		t.Fatalf("a store inside the array failed: %v", err)
	}
	if err := client.storeJavaArrayReference(array, 2, 0x1234); err == nil {
		t.Error("a store one past the end was accepted")
	}
}
