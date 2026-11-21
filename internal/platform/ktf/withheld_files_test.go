package ktf

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

const withheldOptionsClient = "df6a087ca91b09759b9713aa8e040019228154714b1686012e4f1802f9064fe2"

// Only the recorded copy of a recorded name leaves the guest view, whatever
// the name's case; another copy of the name, and a client without a record,
// keep every file.
func TestWithheldPrivateFilesLeaveTheGuestView(t *testing.T) {
	recorded := []byte("the inconsistent record")
	sum := sha256.Sum256(recorded)
	saved := withheldFiles[withheldOptionsClient]
	withheldFiles[withheldOptionsClient] = []withheldFile{{name: "options.dat", sha256: hex.EncodeToString(sum[:])}}
	t.Cleanup(func() { withheldFiles[withheldOptionsClient] = saved })

	view := func(options []byte) map[string][]byte {
		return (&Archive{Files: map[string][]byte{
			"P/options.dat": options, "P/patch.dat": {2}, "p/res/img/ui.bar": {3}, "icon": {4},
		}}).GuestFiles()
	}
	files := withholdPrivateFiles(view(recorded), withheldOptionsClient)
	if _, ok := files["options.dat"]; ok {
		t.Fatal("the withheld record is still in the guest view")
	}
	for _, name := range []string{"patch.dat", "res/img/ui.bar", "icon"} {
		if _, ok := files[name]; !ok {
			t.Fatalf("%s left the guest view", name)
		}
	}
	if upper := withholdPrivateFiles(map[string][]byte{"OPTIONS.DAT": recorded}, withheldOptionsClient); len(upper) != 0 {
		t.Fatal("the withheld name was matched by case")
	}
	if files := withholdPrivateFiles(view([]byte("another capture's record")), withheldOptionsClient); len(files) != 4 {
		t.Fatalf("another copy of the name was withheld: %d files left", len(files))
	}
	if files := withholdPrivateFiles(view(recorded), "unknown"); len(files) != 4 {
		t.Fatalf("a client without a record lost files: %d left", len(files))
	}
	if hasWithheldFiles([]byte("unrecognized client")) {
		t.Fatal("unknown revision matched")
	}
}
