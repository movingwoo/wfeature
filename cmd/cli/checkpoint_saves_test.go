package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/backend"
	"github.com/movingwoo/wfeature/internal/serve"
	"github.com/movingwoo/wfeature/internal/session"
	"github.com/movingwoo/wfeature/internal/testfixture"
)

// saveCLIFixture is the authored title that keeps a save, as an archive on
// disk beside the save root the command is pointed at. file is where the
// title's save lands, and slots the directory its quick save is kept in.
type saveCLIFixture struct {
	path, saveRoot, file, slots string
	archive                     []byte
}

func newSaveCLIFixture(t *testing.T) saveCLIFixture {
	t.Helper()
	archive, err := testfixture.KTFSaveArchive()
	if err != nil {
		t.Fatal(err)
	}
	summary, err := session.Inspect(archive)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	fixture := saveCLIFixture{path: filepath.Join(root, "fixture.zip"), saveRoot: filepath.Join(root, "saves"), archive: archive}
	fixture.file = filepath.Join(fixture.saveRoot, summary.SaveOwner, filepath.FromSlash(testfixture.KTFSaveStoreKey))
	fixture.slots = filepath.Join(fixture.saveRoot, ".wfeature-quicksave", "owners", summary.SaveOwner)
	if err := os.WriteFile(fixture.path, archive, 0600); err != nil {
		t.Fatal(err)
	}
	return fixture
}

// saveCLIKey is one press of a key of the title, which is one action.
func saveCLIKey(key int32) string {
	name := string(rune(key))
	return fmt.Sprintf("{\"cmd\":\"key\",\"key\":%q,\"action\":\"press\"}\n{\"cmd\":\"key\",\"key\":%q,\"action\":\"release\"}\n", name, name)
}

// serveSaveCLI runs the commands through `-serve` and answers each response.
func serveSaveCLI(t *testing.T, fixture saveCLIFixture, commands string, arguments ...string) []serve.Response {
	t.Helper()
	var output, diagnostics bytes.Buffer
	arguments = append([]string{"-save", fixture.saveRoot, "-serve"}, arguments...)
	if code := runShared(t.Context(), fixture.path, arguments, strings.NewReader(commands), &output, &diagnostics); code != 0 {
		t.Fatalf("CLI = %d: %s", code, diagnostics.String())
	}
	var responses []serve.Response
	for decoder := json.NewDecoder(&output); decoder.More(); {
		var response serve.Response
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		responses = append(responses, response)
	}
	if len(responses) != strings.Count(commands, "\n") {
		t.Fatalf("%d commands were answered %d times", strings.Count(commands, "\n"), len(responses))
	}
	return responses
}

// The command has no way to write a guest word, so the title's two saves are
// told apart by their length: a save is two parts, and a save emptied and then
// finished is the second part alone. The quick save is taken over the first,
// the game then makes the second, and after a quick load the short save is
// still the save — the game's next write, part A in place, lands in a file of
// four bytes and not in the eight the quick save was taken beside.
func TestCheckpointCLIQuickloadKeepsSaves(t *testing.T) {
	fixture := newSaveCLIFixture(t)
	word := func(value uint32) []byte { return binary.LittleEndian.AppendUint32(nil, value) }
	onDisk := func(when string, want []byte) {
		t.Helper()
		if data, err := os.ReadFile(fixture.file); err != nil || !bytes.Equal(data, want) {
			t.Fatalf("%s: the save on disk is %x (%v), want %x", when, data, err, want)
		}
	}
	every := func(responses []serve.Response) {
		t.Helper()
		for index, response := range responses {
			if !response.OK {
				t.Fatalf("command %d = %+v", index, response)
			}
		}
	}

	every(serveSaveCLI(t, fixture, saveCLIKey(testfixture.KTFSaveActionSave)+"{\"cmd\":\"quicksave\"}\n"+
		saveCLIKey(testfixture.KTFSaveActionTruncate)+saveCLIKey(testfixture.KTFSaveActionFinish)+"{\"cmd\":\"quit\"}\n"))
	onDisk("the save made after the quick save", word(testfixture.KTFSaveMask))

	// Over the running game.
	every(serveSaveCLI(t, fixture, "{\"cmd\":\"quickload\"}\n{\"cmd\":\"quit\"}\n"))
	onDisk("a quick load over a running game", word(testfixture.KTFSaveMask))

	// At startup, in a command that never ran the title.
	var output, diagnostics bytes.Buffer
	if code := runShared(t.Context(), fixture.path, []string{"-save", fixture.saveRoot, "-quickload", "-ticks", "0"}, nil, &output, &diagnostics); code != 0 {
		t.Fatalf("CLI = %d: %s", code, diagnostics.String())
	}
	var result struct {
		Restored bool `json:"restored"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil || !result.Restored {
		t.Fatalf("the command did not start from the quick save: %s (%v)", output.String(), err)
	}
	onDisk("a start from the quick save", word(testfixture.KTFSaveMask))

	// The restored game writes onto the save as it is.
	every(serveSaveCLI(t, fixture, saveCLIKey(testfixture.KTFSaveActionPatch)+"{\"cmd\":\"quit\"}\n", "-quickload"))
	onDisk("a patch after a start from the quick save", word(0))
}

// A quick save an earlier build wrote ends the command with status 1 and a
// reason that names the file, in the shell's language. The file is left as it
// is, a live command goes on running after refusing it, and a new quick save
// is written beside it.
func TestCheckpointCLIRefusesAnEarlierBuildsSlot(t *testing.T) {
	fixture := newSaveCLIFixture(t)
	identity := backend.SaveIdentity(fixture.archive)
	earlierName := fmt.Sprintf("%x.wfq", identity)
	earlier := append([]byte("WFSTATE\x00\x01\x00"), bytes.Repeat([]byte{0x5a}, 2048)...)
	if err := os.MkdirAll(fixture.slots, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.slots, earlierName), earlier, 0600); err != nil {
		t.Fatal(err)
	}
	unchanged := func(when string) {
		t.Helper()
		if data, err := os.ReadFile(filepath.Join(fixture.slots, earlierName)); err != nil || !bytes.Equal(data, earlier) {
			t.Fatalf("%s: the earlier build's quick save was touched (%v)", when, err)
		}
	}

	var output, diagnostics bytes.Buffer
	code := runShared(t.Context(), fixture.path, []string{"-save", fixture.saveRoot, "-quickload", "-ticks", "0"}, nil, &output, &diagnostics)
	if code != 1 || output.Len() != 0 || !strings.Contains(diagnostics.String(), "earlier slot format") || !strings.Contains(diagnostics.String(), earlierName) {
		t.Fatalf("a start from the earlier build's quick save = %d, %q: %s", code, output.String(), diagnostics.String())
	}
	unchanged("a refused start")

	responses := serveSaveCLI(t, fixture, "{\"cmd\":\"quickload\"}\n"+saveCLIKey(testfixture.KTFSaveActionSave)+
		"{\"cmd\":\"quicksave\"}\n{\"cmd\":\"quickload\"}\n{\"cmd\":\"quit\"}\n")
	if responses[0].OK || !strings.Contains(responses[0].Error, earlierName) {
		t.Fatalf("a load of the earlier build's quick save = %+v", responses[0])
	}
	for index, response := range responses[1:] {
		if !response.OK {
			t.Fatalf("command %d after the refused load = %+v", index+1, response)
		}
	}
	unchanged("a new quick save")
	if _, err := os.Stat(filepath.Join(fixture.slots, fmt.Sprintf("%x.v2.wfq", identity))); err != nil {
		t.Fatalf("the new quick save is not beside the earlier one: %v", err)
	}
	if data, err := os.ReadFile(fixture.file); err != nil || !bytes.Equal(data, testfixture.KTFSaveContent(0)) {
		t.Fatalf("the save the running game made is %x (%v)", data, err)
	}
}
