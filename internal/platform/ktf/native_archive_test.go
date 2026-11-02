package ktf

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/wipicrt"
)

// buildNativeInfo assembles a module information file the way a real one is
// laid out, so the parser is exercised against the shape rather than against
// one recorded byte string.
func buildNativeInfo(t *testing.T, spans [][]byte, trailer []uint32) []byte {
	t.Helper()
	const tableOffset = 0x48
	header := make([]byte, tableOffset)
	table := make([]byte, (len(spans)+1)*4)
	body := &bytes.Buffer{}
	start := uint32(tableOffset + len(table))
	for index, span := range spans {
		binary.LittleEndian.PutUint32(table[index*4:], start+uint32(body.Len()))
		body.Write(span)
		_ = index
	}
	binary.LittleEndian.PutUint32(table[len(spans)*4:], start+uint32(body.Len()))
	// The two header sections sit between the fixed fields and the span table.
	// A fixture that leaves them empty would leave the record decoder with
	// nothing to read, so the second one is placed where a real file puts it.
	binary.LittleEndian.PutUint32(header[0x08:], 0x20)
	binary.LittleEndian.PutUint32(header[0x0c:], 0x28)
	binary.LittleEndian.PutUint32(header[0x10:], tableOffset)
	binary.LittleEndian.PutUint32(header[0x14:], uint32(len(spans)))
	binary.LittleEndian.PutUint32(header[0x18:], binary.LittleEndian.Uint32(table))
	out := &bytes.Buffer{}
	out.Write(header)
	out.Write(table)
	out.Write(body.Bytes())
	for _, word := range trailer {
		_ = binary.Write(out, binary.LittleEndian, word)
	}
	out.WriteString(nativeInfoTrailerMagic)
	return out.Bytes()
}

func nativeTextSpan(text string) []byte {
	return append([]byte{0xfe, 0xfe}, backend.EncodeEUCKR(text)...)
}

func nativeTypedSpan(mime string, payload []byte) []byte {
	span := make([]byte, 2)
	binary.LittleEndian.PutUint16(span, uint16(len(mime)+3))
	span = append(span, mime...)
	span = append(span, 0)
	return append(span, payload...)
}

func nativeNumericSpan(words ...uint32) []byte {
	span := make([]byte, len(words)*4)
	for index, word := range words {
		binary.LittleEndian.PutUint32(span[index*4:], word)
	}
	return span
}

// TestParseNativeInfoReadsHeaderFieldsOnTheirStride covers the record stride
// the header sections are written at. Reading them as bare aligned words is
// what made an earlier pass map four times the module's size: the boundary
// between two records reads as a value of its own.
//
// What the records *mean* is not asserted, and that is the point. Tag 6 read as
// the module's page-rounded length until two more archives arrived carrying the
// same header beside modules of two other sizes.
func TestParseNativeInfoReadsHeaderFieldsOnTheirStride(t *testing.T) {
	data := buildNativeInfo(t, [][]byte{nativeNumericSpan(1)}, []uint32{0, 1})
	// Two records in the second section, written the way a real file writes
	// them: a value, a halfword the format leaves spare, and the tag.
	binary.LittleEndian.PutUint32(data[0x28:], 0x03e80001)
	binary.LittleEndian.PutUint16(data[0x2c:], 0)
	binary.LittleEndian.PutUint16(data[0x2e:], 4)
	binary.LittleEndian.PutUint32(data[0x30:], 2*nativePageSize)
	binary.LittleEndian.PutUint16(data[0x34:], 0)
	binary.LittleEndian.PutUint16(data[0x36:], 6)

	info, err := ParseNativeInfo(data)
	if err != nil {
		t.Fatalf("parse native info: %v", err)
	}
	field, ok := info.Field(6)
	if !ok || field.Value != 2*nativePageSize {
		t.Fatalf("tag 6 = %+v (%v), want the value written under it", field, ok)
	}
	if other, ok := info.Field(4); !ok || other.Value != 0x03e80001 {
		t.Errorf("tag 4 = %+v (%v), want the record beside it", other, ok)
	}
	if _, ok := info.Field(3); ok {
		t.Error("a tag no record carries was answered")
	}
}

// TestParseNativeInfoTolerantOfLineEndingsPastItsTrailer covers the shape one
// archive is distributed in: its information file carries CR LF after the
// trailer, from something that moved it in text mode before it was packed. The
// zip entry's CRC is correct, so refusing it turns a whole title away over two
// bytes nothing reads.
func TestParseNativeInfoTolerantOfLineEndingsPastItsTrailer(t *testing.T) {
	good := buildNativeInfo(t, [][]byte{nativeNumericSpan(1, 2, wipicrt.ImageBase)}, []uint32{0x1234, 18933, 0xff})
	info, err := ParseNativeInfo(append(append([]byte{}, good...), '\r', '\n'))
	if err != nil {
		t.Fatalf("parse an information file with line endings past its trailer: %v", err)
	}
	if info.ApplicationID != 18933 {
		t.Errorf("application id = %d, want %d", info.ApplicationID, 18933)
	}
	// Anything else after the trailer is still not this format.
	if _, err := ParseNativeInfo(append(append([]byte{}, good...), 'x')); err == nil {
		t.Error("a file with a byte of its own past the trailer parsed")
	}
}

// TestApplicationIdentifierReadsTheAppletRecord covers what CreateApplication
// is given. The record is picked out by its shape rather than by the word that
// follows the identifier: that word is the image base in one archive and one
// bit away from it in two others, which is what kept those two out.
func TestApplicationIdentifierReadsTheAppletRecord(t *testing.T) {
	for _, testCase := range []struct {
		name string
		last uint32
	}{
		{name: "the 2005 archive's word", last: wipicrt.ImageBase},
		{name: "the later archives' word", last: 0x10100000},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			data := buildNativeInfo(t, [][]byte{
				// The file's first record opens with a round number the module
				// refuses, and it is not the applet record.
				nativeNumericSpan(0x1000, 0, 0, 0, 0, 0, 0x141, 1, 0, 0),
				nativeNumericSpan(0x0102e0a9, 0, 0x3e8, 0, testCase.last),
				// A second, shorter record names an extension rather than the
				// applet.
				nativeNumericSpan(0x0103028a, 0),
			}, []uint32{0x1234, 18933, 0xff})
			archive := &NativeArchive{}
			var err error
			if archive.Info, err = ParseNativeInfo(data); err != nil {
				t.Fatalf("parse native info: %v", err)
			}
			identifier, ok := archive.ApplicationIdentifier()
			if !ok || identifier != 0x0102e0a9 {
				t.Fatalf("ApplicationIdentifier() = %#x (%v), want %#x", identifier, ok, 0x0102e0a9)
			}
		})
	}
}

func TestParseNativeInfo(t *testing.T) {
	icon := []byte("BM\x00\x01icon bytes")
	data := buildNativeInfo(t, [][]byte{
		nativeTypedSpan("image/bmp", icon),
		nativeTextSpan("A Vendor"),
		nativeTextSpan("A Vendor"),
		nativeTextSpan("제목"),
		nativeNumericSpan(1, 2, wipicrt.ImageBase),
	}, []uint32{0x1234, 18933, 0xff})

	info, err := ParseNativeInfo(data)
	if err != nil {
		t.Fatalf("parse native info: %v", err)
	}
	if info.Vendor != "A Vendor" {
		t.Errorf("vendor = %q, want %q", info.Vendor, "A Vendor")
	}
	if info.Name != "제목" {
		t.Errorf("name = %q, want %q", info.Name, "제목")
	}
	if info.ApplicationID != 18933 {
		t.Errorf("application id = %d, want %d", info.ApplicationID, 18933)
	}
	if len(info.Icons) != 1 || info.Icons[0].MIME != "image/bmp" || !bytes.Equal(info.Icons[0].Data, icon) {
		t.Errorf("icons = %+v, want one image/bmp carrying the payload", info.Icons)
	}
	if len(info.Records) != 1 || len(info.Records[0]) != 3 || info.Records[0][2] != wipicrt.ImageBase {
		t.Errorf("records = %+v, want one three-word record ending at the image base", info.Records)
	}
}

func TestParseNativeInfoRejectsDamage(t *testing.T) {
	good := buildNativeInfo(t, [][]byte{nativeNumericSpan(1)}, []uint32{0, 1})
	for _, testCase := range []struct {
		name string
		data []byte
		want string
	}{
		{name: "short", data: good[:0x10], want: "too short"},
		{name: "no trailer", data: append(append([]byte{}, good[:len(good)-4]...), "zzzz"...), want: "does not end with"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := ParseNativeInfo(testCase.data); err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error = %v, want one containing %q", err, testCase.want)
			}
		})
	}

	t.Run("span table past the end", func(t *testing.T) {
		damaged := append([]byte{}, good...)
		binary.LittleEndian.PutUint32(damaged[0x14:], maxNativeInfoSpans)
		if _, err := ParseNativeInfo(damaged); err == nil {
			t.Fatal("a span table running past the file parsed")
		}
	})

	t.Run("a span running into the trailer", func(t *testing.T) {
		damaged := append([]byte{}, good...)
		table := binary.LittleEndian.Uint32(damaged[0x10:])
		// The span table's last entry is where the identity record starts, so a
		// file whose last span runs to the very end leaves no record at all —
		// and the trailer magic is inside the span rather than after it.
		binary.LittleEndian.PutUint32(damaged[table+4:], uint32(len(damaged)))
		if _, err := ParseNativeInfo(damaged); err == nil {
			t.Fatal("a span running into the trailer parsed")
		}
	})

	t.Run("spans out of order", func(t *testing.T) {
		damaged := append([]byte{}, good...)
		table := binary.LittleEndian.Uint32(damaged[0x10:])
		binary.LittleEndian.PutUint32(damaged[table+4:], 0)
		if _, err := ParseNativeInfo(damaged); err == nil {
			t.Fatal("a span ending before it starts parsed")
		}
	})
}

// buildTestZIP packs entries the way a download package is packed, so a test
// that asks what an archive is asks it of a zip rather than of a map.
func buildTestZIP(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	out := &bytes.Buffer{}
	writer := zip.NewWriter(out)
	for name, contents := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		if _, err := entry.Write(contents); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	return out.Bytes()
}

// TestIsNativeArchiveAnswersFromEntryNames covers the question every Host asks
// before it asks anything else. It is answered from the central directory, so
// what it must get right is the shape: the pair that names this package, the
// descriptor that names the other one, and a folder the whole package sits in.
func TestIsNativeArchiveAnswersFromEntryNames(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		files map[string][]byte
		want  bool
	}{
		{
			name:  "a module beside its information file",
			files: map[string][]byte{"title.mif": {1}, "title.mod": {2}, "data.dat": {3}},
			want:  true,
		},
		{
			name:  "the same through a wrapping folder",
			files: map[string][]byte{"Game/title.mif": {1}, "Game/title.mod": {2}, "Game/data.dat": {3}},
			want:  true,
		},
		{
			name:  "the descriptor package is the other generation",
			files: map[string][]byte{adfPath: {1}, "title.mif": {2}, "title.mod": {3}},
			want:  false,
		},
		{
			name:  "a lone module is not enough",
			files: map[string][]byte{"title.mod": {1}},
			want:  false,
		},
		{
			name:  "two modules are not this package either",
			files: map[string][]byte{"title.mif": {1}, "title.mod": {2}, "other.mod": {3}},
			want:  false,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			data := buildTestZIP(t, testCase.files)
			if got := IsNativeArchive(data); got != testCase.want {
				t.Errorf("IsNativeArchive() = %v, want %v", got, testCase.want)
			}
			// The cheap answer and the one an opened package gives have to
			// agree, or a Host would pick a loader the archive then refuses.
			opened, err := readOuterZIP(data)
			if err != nil {
				t.Fatalf("open archive: %v", err)
			}
			if got := IsNativePackage(opened); got != testCase.want {
				t.Errorf("IsNativePackage() = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestIsNativeArchiveRefusesWhatIsNotAnArchive(t *testing.T) {
	if IsNativeArchive([]byte("not a zip")) {
		t.Error("something that is not an archive was claimed")
	}
}
