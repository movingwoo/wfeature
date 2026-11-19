package lgt

import (
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/platform/detect"
)

func TestExecutableJARSelection(t *testing.T) {
	files := map[string][]byte{
		"app_info":      []byte("AID=missing\n"),
		"resources.jar": zipOf(t, map[string][]byte{"text": []byte("resource")}),
		"program.jar":   fixtureJAR(t),
	}
	if _, err := Open(zipOf(t, files)); err != nil {
		t.Fatal(err)
	}
	files["second.jar"] = fixtureJAR(t)
	if _, err := Open(zipOf(t, files)); err == nil {
		t.Fatal("ambiguous executables accepted")
	}
	files["app_info"] = []byte("AID=program\n")
	if _, err := Open(zipOf(t, files)); err != nil {
		t.Fatal("descriptor priority lost:", err)
	}
}

func TestArchiveKoreanNamesAndResourceAliases(t *testing.T) {
	for _, encode := range []func(string) []byte{func(s string) []byte { return []byte(s) }, backend.EncodeEUCKR} {
		archive, err := Open(zipOf(t, map[string][]byte{
			"app_info":               encode("AID=한글\nName=자료\n"),
			string(encode("한글.jar")): fixtureJAR(t),
			string(encode("자료.bin")): []byte("outer"),
		}))
		if err != nil {
			t.Fatal(err)
		}
		if archive.Descriptor.Fields["Name"] != "자료" {
			t.Fatal("descriptor name not decoded")
		}
		for _, name := range []string{"자료.bin", "/P/자료.bin", "/P/한글/자료.bin"} {
			if data, ok := archive.Resource(name); !ok || string(data) != "outer" {
				t.Fatalf("resource %q = %q, %v", name, data, ok)
			}
		}
	}
}

func TestArchiveRejectsCanonicalNameCollisionsAndTraversal(t *testing.T) {
	for _, names := range [][]string{{"a", "./a"}, {".."}, {"a/../../b"}, {"C:/a"}} {
		files := map[string][]byte{}
		for _, name := range names {
			files[name] = []byte("data")
		}
		if _, err := readZIP(zipOf(t, files), "fixture"); err == nil {
			t.Fatalf("unsafe names accepted: %q", names)
		}
	}
}

func TestInstalledResourcePrefixesResolveFromTheReadOnlyRoot(t *testing.T) {
	for _, prefix := range []string{"P/", "P/fixture/"} {
		archive := &Archive{Descriptor: Descriptor{AID: "fixture"}, Packaged: map[string][]byte{prefix + "data": []byte("installed")}}
		for _, name := range []string{"data", "/P/data", "/P/fixture/data"} {
			if got, exists := archive.Resource(name); !exists || string(got) != "installed" {
				t.Fatalf("%s: resource %s = %q, %t", prefix, name, got, exists)
			}
		}
		archive.Resources = map[string][]byte{"data": []byte("jar")}
		if got, _ := archive.Resource("/P/fixture/data"); string(got) != "jar" {
			t.Fatal("installed alias overrode JAR resource")
		}
	}
}

func TestDescriptorlessSyncExport(t *testing.T) {
	jar := fixtureJAR(t)
	open := func(files map[string][]byte, wantLGT bool) *Archive {
		t.Helper()
		data := zipOf(t, files)
		platform, err := detect.Archive(data)
		if err != nil || (platform == detect.LGT) != wantLGT {
			t.Fatalf("platform = %s, error = %v, want LGT %t", platform, err, wantLGT)
		}
		game, err := Open(data)
		if (err == nil) != wantLGT {
			t.Fatalf("open error = %v, want LGT %t", err, wantLGT)
		}
		return game
	}
	a := open(map[string][]byte{"WEBSYNC1.jar": jar, "big.png": nil}, true)
	b := open(map[string][]byte{"folder/websync1.JAR": jar, "folder/big.png": []byte("icon")}, true)
	if a.Descriptor.AID != "WEBSYNC1" || SaveOwner(a.Descriptor) != SaveOwner(b.Descriptor) ||
		SaveOwner(a.Descriptor) == "WEBSYNC1" || backend.SaveOwnerRefused(SaveOwner(a.Descriptor)) {
		t.Fatal("export has no stable, module-specific save owner", a.Descriptor, b.Descriptor)
	}
	entries, err := readZIP(jar, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	entries[binaryModuleName] = append(entries[binaryModuleName], 0)
	other := open(map[string][]byte{"WEBSYNC1.jar": zipOf(t, entries)}, true)
	if SaveOwner(other.Descriptor) == SaveOwner(a.Descriptor) {
		t.Fatal("two executable revisions shared a save owner")
	}
	open(map[string][]byte{"WEBSYNC1.jar": zipOf(t, map[string][]byte{"binary.mod": []byte("unrelated")})}, false)
	open(map[string][]byte{"WEBSYNC1.jar": jar, "another.jar": jar}, false)
	open(map[string][]byte{"outer/inner/WEBSYNC1.jar": jar}, false)
}
