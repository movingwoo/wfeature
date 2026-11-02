package lgt

import (
	"bytes"
	"testing"
)

// searchRecord follows the recognized revision's record search over data from
// offset at, for the record of map want, and answers how it leaves: "found",
// "end" by the exit for a missing record, or "past" where it would read
// beyond data. The rule is the one recordTableName describes.
func searchRecord(data []byte, at int, want byte) string {
	for {
		if at+1 >= len(data) {
			return "past"
		}
		first, second := data[at], data[at+1]
		at += 2
		switch {
		case first == 0x23:
			if second == want {
				return "found"
			}
		case second == 0x24:
			if at >= len(data) {
				return "past"
			}
			third := data[at]
			at++
			if third == 0x8a {
				return "end"
			}
		}
	}
}

func TestRecordTableEndStopsTheSearchFromEveryPosition(t *testing.T) {
	// Two tables in the revision's layout, `23 <map> … 24 <map>`, one odd and
	// one even in length, neither holding the map searched for.
	for _, table := range [][]byte{
		{0x23, 0x00, 0x1c, 0xf1, 0x01, 0x24, 0x00, 0x23, 0x95, 0x1c, 0xf1, 0x24, 0x95},
		{0x23, 0x00, 0x1c, 0xf1, 0x24, 0x00, 0x23, 0x95, 0x1c, 0xf1, 0x24, 0x95},
	} {
		if got := searchRecord(table, 0, 0x7c); got != "past" {
			t.Fatalf("the table as packaged answered %s", got)
		}
		corrected := append(bytes.Clone(table), recordTableEnd...)
		for start := range table {
			if got := searchRecord(corrected, start, 0x7c); got != "end" {
				t.Fatalf("from offset %d of %x the search answered %s", start, table, got)
			}
		}
		if got := searchRecord(corrected, 0, 0x95); got != "found" {
			t.Fatalf("a record the table holds answered %s", got)
		}
	}
}

func TestRecordTableTerminatorCorrectsOnlyTheSelectedArchiveCopy(t *testing.T) {
	client := fixtureClient(t)
	if client.recordTableTerminator {
		t.Fatal("the fixture module was selected")
	}
	if hasRecordTableTerminator([]byte("unrecognized module")) {
		t.Fatal("an unknown revision was selected")
	}
	table := []byte{0x23, 0x00, 0x1c, 0xf1, 0x24, 0x00, 0x23, 0x95, 0x24, 0x95}
	other := []byte{0x23, 0x01, 0x24, 0x01}
	if client.archive.Resources == nil {
		client.archive.Resources = make(map[string][]byte)
	}
	client.archive.Resources[recordTableName] = table
	client.archive.Resources["scp_map.scp"] = other

	if data, ok := client.titleFile(recordTableName); !ok || !bytes.Equal(data, table) {
		t.Fatalf("an unselected module read %x, %v", data, ok)
	}
	client.recordTableTerminator = true
	want := append(bytes.Clone(table), recordTableEnd...)
	for _, name := range []string{recordTableName, "/" + recordTableName, "./" + recordTableName} {
		if data, ok := client.titleFile(name); !ok || !bytes.Equal(data, want) {
			t.Fatalf("%s read %x, %v", name, data, ok)
		}
	}
	if data, ok := client.titleFile("scp_map.scp"); !ok || !bytes.Equal(data, other) {
		t.Fatalf("another resource read %x, %v", data, ok)
	}
	if !bytes.Equal(client.archive.Resources[recordTableName], table) {
		t.Fatal("the archive's own copy changed")
	}

	// A file the title wrote under the name is its own and is read as written.
	key, err := fileSaveKey(recordTableName)
	if err != nil {
		t.Fatal(err)
	}
	written := []byte{0x23, 0x7c, 0x24, 0x7c}
	if client.sessionFiles == nil {
		client.sessionFiles = make(map[string][]byte)
	}
	client.sessionFiles[key] = written
	if data, ok := client.titleFile(recordTableName); !ok || !bytes.Equal(data, written) {
		t.Fatalf("the title's own file read %x, %v", data, ok)
	}
}
