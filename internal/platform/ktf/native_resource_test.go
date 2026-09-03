package ktf

import (
	"encoding/binary"
	"testing"
)

// buildResourceFile assembles a resource file the way a real one is laid out,
// so the parser is exercised against the shape rather than against one
// recorded byte string. Each group covers a run of consecutive numbers, and
// the items of the whole file are one array the groups index into.
func buildResourceFile(groups []nativeResourceGroup, items [][]byte) []byte {
	const headerSize = nativeResourceHeaderSize
	groupTable := uint32(headerSize)
	indexTable := groupTable + uint32(len(groups))*nativeResourceGroupStride
	first := indexTable + uint32(len(items)+1)*4

	out := make([]byte, first)
	binary.LittleEndian.PutUint16(out, nativeResourceVersion)
	binary.LittleEndian.PutUint16(out[2:], 1)
	binary.LittleEndian.PutUint16(out[4:], 1)
	binary.LittleEndian.PutUint16(out[6:], uint16(len(groups)))
	binary.LittleEndian.PutUint32(out[0x08:], groupTable)
	binary.LittleEndian.PutUint32(out[0x0c:], uint32(len(groups))*nativeResourceGroupStride)
	binary.LittleEndian.PutUint32(out[0x10:], indexTable)
	binary.LittleEndian.PutUint32(out[0x14:], uint32(len(items)))
	binary.LittleEndian.PutUint32(out[0x18:], first)
	for index, group := range groups {
		record := out[groupTable+uint32(index)*nativeResourceGroupStride:]
		binary.LittleEndian.PutUint16(record, group.Kind)
		binary.LittleEndian.PutUint16(record[2:], group.First)
		binary.LittleEndian.PutUint16(record[4:], group.Span)
		binary.LittleEndian.PutUint16(record[6:], group.Item)
	}
	offset := first
	for index, item := range items {
		binary.LittleEndian.PutUint32(out[indexTable+uint32(index)*4:], offset)
		out = append(out, item...)
		offset += uint32(len(item))
	}
	binary.LittleEndian.PutUint32(out[indexTable+uint32(len(items))*4:], offset)
	return out
}

// TestNativeResourceFileAnswersByNumber covers the map from a number to a run
// of bytes. The groups are runs rather than a list, which is what a file with
// thirteen groups and a hundred items is: consecutive numbers share a group and
// their distance into it is their distance into the items.
func TestNativeResourceFileAnswersByNumber(t *testing.T) {
	data := buildResourceFile(
		[]nativeResourceGroup{
			{Kind: 6, First: 4000, Span: 1, Item: 0},
			{Kind: 6, First: 5001, Span: 0, Item: 2},
			{Kind: 6, First: 7000, Span: 2, Item: 3},
		},
		[][]byte{
			[]byte("four thousand"),
			[]byte("four thousand and one"),
			[]byte("five thousand and one"),
			[]byte("seven thousand"),
			[]byte("seven thousand and one"),
			[]byte("seven thousand and two"),
		},
	)
	file, err := parseNativeResourceFile(data)
	if err != nil {
		t.Fatalf("parse resource file: %v", err)
	}
	for _, testCase := range []struct {
		number uint16
		want   string
	}{
		{number: 4000, want: "four thousand"},
		{number: 4001, want: "four thousand and one"},
		{number: 5001, want: "five thousand and one"},
		{number: 7000, want: "seven thousand"},
		{number: 7002, want: "seven thousand and two"},
	} {
		item, ok := file.item(6, testCase.number)
		if !ok || string(item) != testCase.want {
			t.Errorf("item(6, %d) = %q (%v), want %q", testCase.number, item, ok, testCase.want)
		}
	}
	for _, number := range []uint16{3999, 4002, 5000, 5002, 7003} {
		if item, ok := file.item(6, number); ok {
			t.Errorf("item(6, %d) = %q, want no answer", number, item)
		}
	}
	// The kind is part of the question: a number one kind carries is not a
	// number another kind carries.
	if _, ok := file.item(7, 4000); ok {
		t.Error("a number of one kind answered for another")
	}
}

// TestNativeResourceFileRefusesWhatIsNotOne covers the other half of the same
// call: the title opens its saves and its own data through the file interface,
// so a name handed to the resource loader that is not a resource file has to be
// a no rather than nine bytes of one.
func TestNativeResourceFileRefusesWhatIsNotOne(t *testing.T) {
	good := buildResourceFile(
		[]nativeResourceGroup{{Kind: 6, First: 1, Span: 0, Item: 0}},
		[][]byte{[]byte("one")},
	)
	for _, testCase := range []struct {
		name    string
		damage  func([]byte) []byte
		wantErr string
	}{
		{
			name:    "too short for its header",
			damage:  func(data []byte) []byte { return data[:8] },
			wantErr: "too short",
		},
		{
			name: "another format entirely",
			damage: func(data []byte) []byte {
				out := append([]byte(nil), data...)
				binary.LittleEndian.PutUint16(out, 0x4d42)
				return out
			},
			wantErr: "version",
		},
		{
			name: "a group table past the end",
			damage: func(data []byte) []byte {
				out := append([]byte(nil), data...)
				binary.LittleEndian.PutUint32(out[0x08:], uint32(len(out)))
				return out
			},
			wantErr: "group table",
		},
		{
			name: "an index table past the end",
			damage: func(data []byte) []byte {
				out := append([]byte(nil), data...)
				binary.LittleEndian.PutUint32(out[0x10:], uint32(len(out)))
				return out
			},
			wantErr: "index table",
		},
		{
			name: "items out of order",
			damage: func(data []byte) []byte {
				out := append([]byte(nil), data...)
				index := binary.LittleEndian.Uint32(out[0x10:])
				binary.LittleEndian.PutUint32(out[index+4:], 0)
				return out
			},
			wantErr: "before it starts",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := parseNativeResourceFile(testCase.damage(good)); err == nil {
				t.Fatalf("error = nil, want one containing %q", testCase.wantErr)
			}
		})
	}
}

// TestNativeLoadResourceHandsBackTheItem covers the pair the module reaches
// for its own data: it asks for a numbered item of a named file and gets a
// block, reads it, and hands the block back.
func TestNativeLoadResourceHandsBackTheItem(t *testing.T) {
	item := []byte("MMMD" + "a sound the title plays")
	platform := newTestNativePlatform(t, map[string][]byte{
		"a title/sound.bar": buildResourceFile(
			[]nativeResourceGroup{{Kind: 6, First: 5001, Span: 0, Item: 0}},
			[][]byte{item},
		),
		"a title/save.dat": []byte("not a resource file"),
	})
	name := nativeString(t, platform, "sound.bar")

	block := nativeCall(t, platform.loadResource, 0, name, 5001, 6)
	if block == 0 {
		t.Fatal("the loader answered nothing for a number the file carries")
	}
	if got := nativeRead(t, platform, block, len(item)); string(got) != string(item) {
		t.Errorf("block holds %q, want %q", got, item)
	}
	if got := platform.client.blocks[block]; got == 0 {
		t.Error("the block did not come out of the arena, so nothing can give it back")
	}
	nativeCall(t, platform.freeResource, 0, block)
	if _, ok := platform.client.blocks[block]; ok {
		t.Error("the block was not given back")
	}

	// A number the file does not carry, and a name that is not a resource file
	// at all, are both answered with nothing and both leave a note behind.
	if got := nativeCall(t, platform.loadResource, 0, name, 5002, 6); got != 0 {
		t.Errorf("a number the file does not carry answered %#x", got)
	}
	save := nativeString(t, platform, "save.dat")
	if got := nativeCall(t, platform.loadResource, 0, save, 5001, 6); got != 0 {
		t.Errorf("a name that is not a resource file answered %#x", got)
	}
	if len(platform.ResourceNotes()) != 2 {
		t.Errorf("notes = %q, want one for each request that went unanswered", platform.ResourceNotes())
	}
}
