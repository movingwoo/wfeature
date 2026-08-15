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
