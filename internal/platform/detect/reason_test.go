package detect_test

import (
	"encoding/binary"
	"testing"

	"github.com/movingwoo/wfeature/internal/platform/detect"
)

// Four different files used to come back as the same answer, and only one of
// the four is work this project can do: a package it did not recognise. The
// reason is what tells them apart in a count.
func TestEveryUnclaimedFileSaysWhyItWasNotClaimed(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		data    []byte
		want    detect.Reason
		wantErr bool
	}{
		{
			name: "a zip with no marker any platform knows",
			data: buildZIP(t, map[string][]byte{"README.txt": []byte("nothing to load here")}),
			want: detect.ReasonNoMarker,
		},
		{
			name: "a zip holding whole packages and nothing else",
			data: buildZIP(t, map[string][]byte{
				"one.zip": []byte("PK\x05\x06"),
				"two.zip": []byte("PK\x05\x06"),
			}),
			want: detect.ReasonArchiveOfArchives,
		},
		{
			name:    "an archive format this names but does not read",
			data:    []byte("Rar!\x1a\x07\x00rest of a RAR file"),
			want:    detect.ReasonKnownFormatUnsupported,
			wantErr: true,
		},
		{
			name:    "a package locked before it was distributed",
			data:    dcfVersion1(t, "application/vnd.oma.drm.content", "cid:00WIPI000000000012"),
			want:    detect.ReasonDRMWrapped,
			wantErr: true,
		},
		{
			name:    "a download that did not finish",
			data:    []byte("PK\x03\x04 and then the connection dropped"),
			want:    detect.ReasonNotAnArchive,
			wantErr: true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			platform, reason, err := detect.Classify(testCase.data)
			if testCase.wantErr && err == nil {
				t.Errorf("a file the reader cannot open came back without an error")
			}
			if !testCase.wantErr {
				if err != nil {
					t.Fatalf("classify: %v", err)
				}
				if platform != detect.Unknown {
					t.Errorf("platform = %q, want %q", platform, detect.Unknown)
				}
			}
			if reason != testCase.want {
				t.Errorf("reason = %q, want %q", reason, testCase.want)
			}
		})
	}
}

// A file a platform claimed has no reason to carry, and the platform Archive
// answers has to stay exactly what it was: every caller that loads a game asks
// that one.
func TestAClaimedArchiveCarriesNoReasonAndAnswersTheSamePlatform(t *testing.T) {
	data := buildZIP(t, map[string][]byte{"__adf__": []byte("aid:AI0000\n"), "AI0000.jar": nil})
	platform, reason, err := detect.Classify(data)
	if err != nil {
		t.Fatalf("classify: %v", err)
	}
	if platform != detect.KTF || reason != detect.ReasonClaimed {
		t.Errorf("classify = (%q, %q), want (%q, no reason)", platform, reason, detect.KTF)
	}
	fromArchive, err := detect.Archive(data)
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if fromArchive != platform {
		t.Errorf("Archive says %q where Classify says %q", fromArchive, platform)
	}
}

// The header of a locked container is in the clear, and reading it is the
// whole of what is done with one: what it declares about itself, and nothing
// from the encrypted payload behind it.
func TestALockedContainerIsRecognisedFromItsHeaderAlone(t *testing.T) {
	t.Run("the header-prefixed layout declares its type and its content id", func(t *testing.T) {
		header, ok := detect.DCFHeader(dcfVersion1(t, "application/vnd.oma.drm.content", "cid:00WIPI000000000012"))
		if !ok {
			t.Fatal("a wrapped container was not recognised")
		}
		if header.Version != 1 {
			t.Errorf("version = %d, want 1", header.Version)
		}
		if header.ContentType != "application/vnd.oma.drm.content" {
			t.Errorf("content type = %q", header.ContentType)
		}
		// The scheme in front of the identifier is the same for every one of
		// these, and what a rights lookup keys on is what follows it.
		if header.ContentID != "00WIPI000000000012" {
			t.Errorf("content id = %q, want it without the scheme", header.ContentID)
		}
	})

	t.Run("the box layout is recognised by its brand", func(t *testing.T) {
		header, ok := detect.DCFHeader(dcfBox(t, "ftyp", append([]byte("odcf"), 0, 0, 0, 2, 'o', 'd', 'c', 'f')))
		if !ok {
			t.Fatal("a wrapped container was not recognised")
		}
		if header.Version != 2 {
			t.Errorf("version = %d, want 2", header.Version)
		}
	})

	t.Run("the box layout is recognised by the container box", func(t *testing.T) {
		file := dcfBox(t, "ftyp", append([]byte("isom"), 0, 0, 0, 1, 'i', 's', 'o', 'm'))
		file = append(file, dcfBox(t, "odrm", []byte("a box this does not descend into"))...)
		if _, ok := detect.DCFHeader(file); !ok {
			t.Fatal("a wrapped container was not recognised")
		}
	})
}

// Three bytes is far too little to claim a file on, so the header has to be
// evidence rather than a coincidence: a file that merely begins with a 1, and
// a file whose declared lengths run past its own end, are not these.
func TestWhatIsNotALockedContainerIsNotClaimedAsOne(t *testing.T) {
	for _, testCase := range []struct {
		name string
		data []byte
	}{
		{"a zip", buildZIP(t, map[string][]byte{"__adf__": []byte("aid:AI0000\n")})},
		{"a file that happens to start with a 1", []byte{1, 2, 3, 4, 5, 6, 7, 8}},
		{"nothing", nil},
		{"lengths that run past the end", []byte{1, 40, 40, 'a', '/', 'b'}},
		{"a declared type that is not a media type", func() []byte {
			file := []byte{1, 4, 4}
			return append(file, []byte("aaaabbbb\x00")...)
		}()},
		{"a box layout that is not this wrapper", dcfBox(t, "ftyp", append([]byte("isom"), 0, 0, 0, 1, 'i', 's', 'o', 'm'))},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if header, ok := detect.DCFHeader(testCase.data); ok {
				t.Errorf("claimed as a wrapped container: %+v", header)
			}
		})
	}
}

// Helpers. Both build a header and nothing else: there is no encrypted content
// in this repository and none is needed to answer the question these ask.

func dcfVersion1(t testing.TB, mediaType, identifier string) []byte {
	t.Helper()
	if len(mediaType) > 255 || len(identifier) > 255 {
		t.Fatalf("a declared string does not fit in its length byte")
	}
	file := []byte{1, byte(len(mediaType)), byte(len(identifier))}
	file = append(file, mediaType...)
	file = append(file, identifier...)
	// The two lengths that follow say how large the headers and the payload
	// are. Nothing reads them, but a real file has them.
	return append(file, 0x00, 0x00)
}

func dcfBox(t testing.TB, kind string, body []byte) []byte {
	t.Helper()
	if len(kind) != 4 {
		t.Fatalf("a box type is four characters, not %q", kind)
	}
	box := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(box, uint32(8+len(body)))
	copy(box[4:], kind)
	return append(box, body...)
}
