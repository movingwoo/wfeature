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

	// The container that turned up in a local set of packages wears its brand
	// at offset zero rather than in a brand box, and its container box declares
	// a 64-bit size. Neither of those is exotic; both were enough to make the
	// file unrecognisable, which is how a locked package came to be counted as
	// a package this project had failed to load.
	t.Run("the box layout is recognised by a brand at offset zero", func(t *testing.T) {
		header, ok := detect.DCFHeader(dcfSigned(t, 64))
		if !ok {
			t.Fatal("a wrapped container was not recognised")
		}
		if header.Version != 2 {
			t.Errorf("version = %d, want 2", header.Version)
		}
	})
}

// The payload of one of these packages is a JAR beside the descriptor, and a
// wrapper takes its place and keeps its name. Everything else in the archive
// survives, so the marker still says the platform and detection used to answer
// it — which sends a person to a loader that cannot read the bytes, and counts
// the file among the ones this project has work left to do on. It has none.
func TestAPackageWhoseOwnPayloadIsLockedIsNotClaimedForAPlatform(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		marker  string
		payload string
	}{
		{"the descriptor one vendor names exactly", "__adf__", "AI0000.jar"},
		{"the descriptor the other vendor names exactly", "app_info", "AI0000.jar"},
		{"the descriptor named after the title", "TITLE.msd", "TITLE.jar"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			// The container is larger than the front of it detection reads, so
			// this also asks the question the real files ask: whether a header
			// answers the same from a prefix as it would from the whole file.
			data := buildZIP(t, map[string][]byte{
				testCase.marker:  []byte("aid:AI0000\n"),
				testCase.payload: dcfSigned(t, 8<<10),
			})
			platform, reason, err := detect.Classify(data)
			if reason != detect.ReasonDRMWrapped {
				t.Errorf("reason = %q, want %q", reason, detect.ReasonDRMWrapped)
			}
			if platform != detect.Unknown {
				t.Errorf("platform = %q, want %q", platform, detect.Unknown)
			}
			// A person holding one of these is owed the reason it cannot be
			// opened rather than a loader's complaint about the bytes.
			if err == nil {
				t.Fatal("a locked package came back without an error")
			}
			if _, err := detect.Archive(data); err == nil {
				t.Error("Archive opened a locked package without complaint")
			}
		})
	}
}

// The other half of the same claim: a package whose payload is the archive it
// is supposed to be keeps the platform it always had. A detector that reads
// the front of every payload is only worth having if it reads them correctly.
func TestAPackageWhosePayloadIsAnArchiveKeepsItsPlatform(t *testing.T) {
	inner := buildZIP(t, map[string][]byte{"client.bin0": []byte("a game")})
	for _, testCase := range []struct {
		marker string
		want   detect.Platform
	}{
		{"__adf__", detect.KTF},
		{"app_info", detect.LGT},
		{"TITLE.msd", detect.SKT},
	} {
		t.Run(testCase.marker, func(t *testing.T) {
			data := buildZIP(t, map[string][]byte{
				testCase.marker: []byte("aid:AI0000\n"),
				"AI0000.jar":    inner,
			})
			platform, reason, err := detect.Classify(data)
			if err != nil {
				t.Fatalf("classify: %v", err)
			}
			if platform != testCase.want || reason != detect.ReasonClaimed {
				t.Errorf("classify = (%q, %q), want (%q, no reason)", platform, reason, testCase.want)
			}
		})
	}
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

// dcfSigned builds the layout the packages in hand actually use: the brand at
// offset zero rather than in a brand box, then a container box whose size is
// declared in the 64-bit field. The contents are filler — there is no
// encrypted content in this repository and none is needed to answer what these
// ask, which is only what the header says.
func dcfSigned(t testing.TB, contents int) []byte {
	t.Helper()
	file := []byte{'o', 'd', 'c', 'f', 0, 2, 0, 0}
	box := make([]byte, 16)
	// A size of one says the real size is the 64-bit value after the type.
	binary.BigEndian.PutUint32(box, 1)
	copy(box[4:], "odrm")
	binary.BigEndian.PutUint64(box[8:], uint64(16+contents))
	file = append(file, box...)
	return append(file, make([]byte, contents)...)
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
