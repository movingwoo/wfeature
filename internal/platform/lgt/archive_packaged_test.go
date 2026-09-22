package lgt

import "testing"

// A handset stored a title's whole download in the title's own directory, so a
// file packaged beside the JAR is one the title can open by name. Three local
// archives ship one — a starting save, a certificate under a Korean directory
// name, and a data file — and dropping them made a title that asks for its
// packaged save write a fresh empty one instead.
func TestPackagedFilesBesideTheJARAreReadable(t *testing.T) {
	archive, err := Open(zipOf(t, map[string][]byte{
		"app_info":      []byte("AID=0102ABCD\nPID=PF000001\nMClass=Fixture\n"),
		"0102ABCD.jar":  fixtureJAR(t),
		"mastercom.sav": []byte("packaged save"),
		"인증파일/cert":     []byte("certificate"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"mastercom.sav", "인증파일/cert"} {
		if _, ok := archive.Resource(name); !ok {
			t.Errorf("the packaged file %q is not readable", name)
		}
	}
	// A leading slash is how a guest names a path, and the case of a name is
	// not something a handset filesystem cared about.
	if _, ok := archive.Resource("/MasterCom.SAV"); !ok {
		t.Error("a packaged file is not found by the path a guest names it with")
	}
}

// The JAR is the application. If both carry a name, the application's own
// resource is the one the game meant.
func TestTheJARWinsANameCollisionWithAPackagedFile(t *testing.T) {
	archive, err := Open(zipOf(t, map[string][]byte{
		"app_info":       []byte("AID=0102ABCD\nPID=PF000001\nMClass=Fixture\n"),
		"0102ABCD.jar":   fixtureJAR(t),
		"data/hello.txt": []byte("the outer one"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	data, ok := archive.Resource("data/hello.txt")
	if !ok {
		t.Fatal("the resource disappeared")
	}
	if string(data) != "packaged" {
		t.Fatalf("resource = %q, want the JAR's own copy", data)
	}
}

// Neither the JAR nor the descriptor is a file the game opens: the loader has
// already read both, and answering with a megabyte of JAR would be a surprise.
func TestTheJARAndDescriptorAreNotThemselvesReadable(t *testing.T) {
	archive, err := Open(zipOf(t, map[string][]byte{
		"app_info":     []byte("AID=0102ABCD\nPID=PF000001\nMClass=Fixture\n"),
		"0102ABCD.jar": fixtureJAR(t),
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0102ABCD.jar", "app_info"} {
		if _, ok := archive.Resource(name); ok {
			t.Errorf("%q is readable as a game file", name)
		}
	}
}

func TestSupplementalApplicationDirectory(t *testing.T) {
	archive, err := Open(zipOf(t, map[string][]byte{
		"app_info":                      []byte("AID=0102ABCD\nPID=PF000001\nMClass=Fixture\n"),
		"0102ABCD.jar":                  fixtureJAR(t),
		"extra/0102abcd/pver.z":         {0, 0},
		"extra/0102abcd/img/tile.idam":  []byte("pixels"),
		"extra/0102abcd/data/hello.txt": []byte("supplement"),
		"extra/0102abcd/options":        []byte("supplement"),
		"P/0102ABCD/options":            []byte("canonical"),
		"extra/other/private":           []byte("other application"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"img/tile.idam", "/IMG/TILE.IDAM", "P/0102ABCD/img/tile.idam"} {
		data, ok := archive.Resource(name)
		if !ok || string(data) != "pixels" {
			t.Fatalf("resource %q = %q, %v", name, data, ok)
		}
	}
	for name, want := range map[string]string{"data/hello.txt": "packaged", "options": "canonical"} {
		data, ok := archive.Resource(name)
		if !ok || string(data) != want {
			t.Fatalf("precedence %q = %q, %v", name, data, ok)
		}
	}
	for _, name := range []string{"private", "../pver.z", "P/0102ABCD/../pver.z"} {
		if _, ok := archive.Resource(name); ok {
			t.Fatalf("unexpected alias %q", name)
		}
	}
	client := fixtureClient(t)
	client.archive = archive
	client.saveStore = newMemorySaveStore()
	if data, ok := client.readFile("pver.z"); !ok || len(data) != 2 {
		t.Fatal("supplement missing at file boundary")
	}
	if names := client.listDirectory("img"); len(names) != 1 || names[0] != "tile.idam" {
		t.Fatalf("supplement directory = %v", names)
	}
	client.writeFile("pver.z", []byte{3, 4})
	if data, ok := client.readFile("pver.z"); !ok || string(data) != string([]byte{3, 4}) {
		t.Fatal("save did not override supplement")
	}
	if data, _ := archive.Resource("pver.z"); string(data) != string([]byte{0, 0}) {
		t.Fatal("archive bytes changed")
	}
	client.removeFile("pver.z")
	if _, ok := client.readFile("pver.z"); ok {
		t.Fatal("deleted supplement reappeared")
	}
}

func TestSupplementalApplicationDirectoryRejectsAmbiguousRoots(t *testing.T) {
	_, err := Open(zipOf(t, map[string][]byte{
		"app_info":          []byte("AID=0102ABCD\nPID=PF000001\nMClass=Fixture\n"),
		"0102ABCD.jar":      fixtureJAR(t),
		"first/0102ABCD/a":  {1},
		"second/0102ABCD/b": {2},
	}))
	if err == nil {
		t.Fatal("ambiguous supplemental roots accepted")
	}
}
