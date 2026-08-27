package detect_test

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/movingwoo/wfeature/internal/platform/detect"
)

func TestArchiveNamesEachPlatformFromItsMarkerEntry(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		entries map[string][]byte
		want    detect.Platform
	}{
		{
			name:    "KTF carries the ADF",
			entries: map[string][]byte{"__adf__": []byte("aid:AI0000\n"), "AI0000.jar": nil},
			want:    detect.KTF,
		},
		{
			name:    "LGT carries app_info",
			entries: map[string][]byte{"app_info": []byte("aid=AI0000\n"), "AI0000.jar": nil},
			want:    detect.LGT,
		},
		{
			name:    "SKT carries a descriptor named after the title",
			entries: map[string][]byte{"0056194389.msd": []byte("MIDlet-1: x,,rpg.Hero\n"), "0056194389.jar": nil},
			want:    detect.SKT,
		},
		{
			name:    "a JAR referencing the SKVM surface is SKT",
			entries: map[string][]byte{"Main.class": []byte("\xca\xfe\xba\xbecom/skt/m/Device")},
			want:    detect.SKT,
		},
		{
			name:    "a JAR referencing com.xce is SKT",
			entries: map[string][]byte{"Main.class": []byte("\xca\xfe\xba\xbecom/xce/io/XFile")},
			want:    detect.SKT,
		},
		{
			// The earlier KTF download package has no descriptor and no JAR:
			// a module information file beside a raw module. It is the same
			// platform — what differs is the package.
			name: "the earlier KTF package is a module beside its information file",
			entries: map[string][]byte{
				"a title/18933.mif": []byte("1fim"),
				"a title/game.mod":  []byte("\x00\x00\x00\x00"),
				"a title/data.bin":  nil,
			},
			want: detect.KTF,
		},
		{
			// One of the pair is not the shape: `.mod` is a common enough
			// extension that claiming an archive for it alone would be a
			// guess rather than a discriminator.
			name:    "a lone module is claimed by nobody",
			entries: map[string][]byte{"game.mod": []byte("\x00")},
			want:    detect.Unknown,
		},
		{
			name: "two modules are not the shape either",
			entries: map[string][]byte{
				"18933.mif": []byte("1fim"),
				"one.mod":   nil,
				"two.mod":   nil,
			},
			want: detect.Unknown,
		},
		{
			// No carrier claims a bare MIDlet. Naming one anyway is what used
			// to hide a detection failure behind a platform that would then
			// fail to load the archive.
			name:    "a plain MIDlet JAR belongs to no vendor",
			entries: map[string][]byte{"Main.class": []byte("\xca\xfe\xba\xbejavax/microedition/lcdui/Canvas")},
			want:    detect.Unknown,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			platform, err := detect.Archive(buildZIP(t, testCase.entries))
			if err != nil {
				t.Fatalf("Archive() error = %v", err)
			}
			if platform != testCase.want {
				t.Fatalf("Archive() = %q, want %q", platform, testCase.want)
			}
		})
	}
}

// A KTF archive packages a JAR full of classes, so the marker has to win over
// anything a class scan would find. It also has to win cheaply: reaching the
// scan at all would mean decompressing tens of megabytes to answer a question
// the central directory already answered.
func TestArchiveMarkerWinsOverTheClassScan(t *testing.T) {
	archive := buildZIP(t, map[string][]byte{
		"__adf__":    []byte("aid:AI0000\n"),
		"Main.class": []byte("com/skt/m/Device"),
	})
	platform, err := detect.Archive(archive)
	if err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	if platform != detect.KTF {
		t.Fatalf("Archive() = %q, want %q", platform, detect.KTF)
	}
}

// The loaders normalize entry names before matching, so an archive written
// with `./` or backslashes still names its platform here — detecting a
// platform whose loader would reject the archive, or missing one it accepts,
// are both wrong in the same way.
func TestArchiveMatchesTheMarkerTheLoadersMatch(t *testing.T) {
	for _, name := range []string{"__ADF__", "./__adf__"} {
		platform, err := detect.Archive(buildZIP(t, map[string][]byte{name: []byte("aid:AI0000\n")}))
		if err != nil {
			t.Fatalf("Archive(%q) error = %v", name, err)
		}
		if platform != detect.KTF {
			t.Fatalf("Archive(%q) = %q, want %q", name, platform, detect.KTF)
		}
	}
}

// The fixtures the platform packages test against are the closest thing to a
// real archive this repository holds, and they are what the browser will hand
// the detector.
func TestArchiveNamesTheRealFixtureJARs(t *testing.T) {
	for _, testCase := range []struct {
		path string
		want detect.Platform
	}{
		{path: filepath.Join("..", "skt", "testdata", "skvm.jar"), want: detect.SKT},
		{path: filepath.Join("..", "skt", "testdata", "canvas.jar"), want: detect.Unknown},
	} {
		data, err := os.ReadFile(testCase.path)
		if err != nil {
			t.Fatalf("read %s: %v", testCase.path, err)
		}
		platform, err := detect.Archive(data)
		if err != nil {
			t.Fatalf("Archive(%s) error = %v", testCase.path, err)
		}
		if platform != testCase.want {
			t.Fatalf("Archive(%s) = %q, want %q", testCase.path, platform, testCase.want)
		}
	}
}

func TestArchiveRejectsWhatIsNotAnArchive(t *testing.T) {
	if _, err := detect.Archive([]byte("this is not a zip")); err == nil {
		t.Fatal("Archive() accepted a non-archive")
	}
}

func buildZIP(t testing.TB, entries map[string][]byte) []byte {
	t.Helper()
	buffer := &bytes.Buffer{}
	writer := zip.NewWriter(buffer)
	for name, contents := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create %q: %v", name, err)
		}
		if _, err := entry.Write(contents); err != nil {
			t.Fatalf("write %q: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close archive: %v", err)
	}
	return buffer.Bytes()
}

// A copy that was unpacked and zipped up again often gains the game's name as
// a containing folder. It is the same archive, and detection used to claim it
// for nobody — which reads to a user as "this file is not a game" rather than
// as "this file is packed differently".
func TestArchiveNamesAPlatformThroughAWrappingFolder(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		entries map[string][]byte
		want    detect.Platform
	}{
		{
			name:    "KTF inside a folder",
			entries: map[string][]byte{"My Game/__adf__": []byte("aid:AI0000\n"), "My Game/AI0000.jar": nil},
			want:    detect.KTF,
		},
		{
			name:    "LGT inside a folder",
			entries: map[string][]byte{"My Game/app_info": []byte("aid=AI0000\n"), "My Game/AI0000.jar": nil},
			want:    detect.LGT,
		},
		{
			name:    "SKT inside a folder",
			entries: map[string][]byte{"My Game/0056194389.msd": []byte("MIDlet-1: x,,rpg.Hero\n"), "My Game/0056194389.jar": nil},
			want:    detect.SKT,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := detect.Archive(buildZIP(t, testCase.entries))
			if err != nil {
				t.Fatalf("Archive() error = %v", err)
			}
			if got != testCase.want {
				t.Fatalf("Archive() = %q, want %q", got, testCase.want)
			}
		})
	}
}

// A marker deeper than one folder still names its platform, because the loader
// roots such an archive at its descriptor: one local copy is a dump of the
// handset's own application directory, which keeps the title under
// `W/apps/<AID>/` and nothing at the root but a file beside it. Detection has
// to agree with what the loader will do.
func TestArchiveClaimsAMarkerBelowMoreThanOneFolder(t *testing.T) {
	entries := map[string][]byte{
		"W/exe_info":               nil,
		"W/apps/AI0000/__adf__":    []byte("aid:AI0000\n"),
		"W/apps/AI0000/AI0000.jar": nil,
	}
	got, err := detect.Archive(buildZIP(t, entries))
	if err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	if got != detect.KTF {
		t.Fatalf("Archive() = %q, want %q", got, detect.KTF)
	}
}

// The earlier KTF package is recognised by a shape rather than a marker: one
// `.mif` beside one `.mod`. That is a guess, and `.mod` is a common enough
// extension that a MIDlet repacked without its `.msd` can wear it by accident,
// so what a class file says it links against is asked first.
func TestTheClassScanWinsOverTheNativePackageShape(t *testing.T) {
	archive := buildZIP(t, map[string][]byte{
		"music.mod":  []byte("tracker module"),
		"icon.mif":   []byte("an image"),
		"Main.class": []byte("com/skt/m/Device"),
	})
	platform, err := detect.Archive(archive)
	if err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	if platform != detect.SKT {
		t.Fatalf("Archive() = %q, want %q — the shape outranked the constant pool", platform, detect.SKT)
	}
}

// With nothing to contradict it, the shape still names the package: a real one
// carries no class files at all, so the scan before it finds nothing.
func TestTheNativePackageShapeStillNamesItWithNoClassesToRead(t *testing.T) {
	archive := buildZIP(t, map[string][]byte{
		"title.mif": []byte("an image"),
		"title.mod": []byte("a module"),
	})
	platform, err := detect.Archive(archive)
	if err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	if platform != detect.KTF {
		t.Fatalf("Archive() = %q, want %q", platform, detect.KTF)
	}
}

// A bag of games is a shape a Host can name, and the shape is "every entry is
// a zip". What must not happen is a real archive being called one: a KTF
// package carries a JAR, and a title that ships a zip of its own data beside
// its descriptor is still a game.
func TestAZipOfZipsIsNamedAndNothingElseIs(t *testing.T) {
	episodes := buildZIP(t, map[string][]byte{
		"ep1.zip": []byte("PK\x05\x06"),
		"ep2.zip": []byte("PK\x05\x06"),
		"ep3.zip": []byte("PK\x05\x06"),
	})
	if !detect.ArchiveOfArchives(episodes) {
		t.Fatal("a zip holding three zips was not named as one")
	}
	platform, err := detect.Archive(episodes)
	if err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	if platform != detect.Unknown {
		t.Fatalf("Archive() = %q, want %q", platform, detect.Unknown)
	}

	for _, testCase := range []struct {
		name    string
		entries map[string][]byte
	}{
		{
			name:    "a game that ships a zip of its own data is a game",
			entries: map[string][]byte{"__adf__": []byte("aid:AI0000\n"), "data.zip": []byte("PK\x05\x06")},
		},
		{
			name:    "an ordinary archive is not a bag",
			entries: map[string][]byte{"app_info": []byte("aid=AI0000\n"), "AI0000.jar": nil},
		},
		{
			name:    "an empty zip is not a bag either",
			entries: map[string][]byte{},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if detect.ArchiveOfArchives(buildZIP(t, testCase.entries)) {
				t.Fatal("named as a zip of zips")
			}
		})
	}

	// The packer's leavings are not entries a person put there.
	bagged := buildZIP(t, map[string][]byte{
		"ep1.zip":            []byte("PK\x05\x06"),
		"__MACOSX/._ep1.zip": []byte("\x00"),
		".DS_Store":          []byte("\x00"),
	})
	if !detect.ArchiveOfArchives(bagged) {
		t.Fatal("a bag with the packer's leavings in it was not named")
	}
}
