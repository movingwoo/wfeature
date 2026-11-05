package skt

import (
	_ "embed"
	"strings"
	"testing"
	"time"

	"github.com/movingwoo/wfeature/internal/backend"
)

//go:embed testdata/library-members.jar
var libraryMembersJAR []byte

// libraryMembersLines is what LibraryMembers answers, line by line: the same
// lines internal/jvm's own test of the fixture holds it to. Where the charset
// decides the bytes the fixture compares the runtime with itself, so this
// platform's EUC-KR answers them as the JVM's UTF-8 does; the UTF-8 writer's
// bytes are named, and here they are what proves the writer asked for UTF-8
// rather than taking the platform's charset.
var libraryMembersLines = []string{
	"true,false,true,false,true,false,false,true,true,true,false,NPE|true,false,true,false,false,NPE|true/false",
	"16,34,19,5,12,100,100,202,NASE|null#7true-+ce12345678901,10987654321ec+-eurt7#llun,SIOOBE,SIOOBE,NPE,true",
	"3,1,-1,-1,0,1,-1,AIOOBE,IOOBE|[#1, null, #2, #1, [x]]|[]|true,true,false,NPE,{one=#1, two=2},{a=1, b=2, c=3},abc,2,{}",
	"true,true,NPE,false,IOE,IOE|true,ab가b가c,false,IOE,IOE,false,IOE",
	"h/i!/abc/cde/,true,SIOOBE,NPE,NPE|true,true,true,c3a9f09f9880,IOE,UEE,NPE,NPE",
	"hié가,hié가,hié가,20,1,UTFDFE,UTFDFE,EOF,NPE",
	"LibraryMembers$Made,1,InstantiationException,InstantiationException,IllegalAccessException,IllegalStateException:refused,InstantiationException,InstantiationException,java.util.Vector",
}

func startLibraryMembers(t *testing.T) *Runtime {
	t.Helper()
	archive, err := Open(libraryMembersJAR)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	options := testRuntimeOptions(t)
	options.SaveStore = backend.NewDirectorySaveStore(t.TempDir())
	runtime, err := Start(archive, options)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { runtime.transition("test cleanup", StateDestroyed) })
	return runtime
}

// The CLDC members the class library shares with KTF — String's region
// tests, the rest of StringBuffer, Vector's and Hashtable's searches and
// text, the readers' mark pair, the writers, the static readUTF,
// ByteArrayOutputStream's text and Class.newInstance — reached from a MIDlet's
// startApp in a JAR, the way a title reaches them. Each was a member such a
// title linked to nothing before.
func TestLibraryMembersAnswerFromAMIDletJAR(t *testing.T) {
	runtime := startLibraryMembers(t)
	var report string
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(time.Millisecond) {
		if report = fixtureString(t, runtime, "LibraryMembersMIDlet", "report"); report != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("startApp did not report within 10s")
		}
	}
	got := strings.Split(report, "\n")
	if len(got) != len(libraryMembersLines) {
		t.Fatalf("report has %d lines, want %d:\n%s", len(got), len(libraryMembersLines), report)
	}
	for index, want := range libraryMembersLines {
		if got[index] != want {
			t.Errorf("line %d =\n  %q\nwant\n  %q", index+1, got[index], want)
		}
	}
}

// Runtime.exit is what the class library's System.exit calls, and here it
// ends the MIDlet the way System.exit does: on the teardown notifyDestroyed
// takes, not as a failed method.
func TestRuntimeExitEndsTheMIDletAsSystemExitDoes(t *testing.T) {
	runtime := startLibraryMembers(t)
	if _, err := runtime.VM.InvokeStatic("LibraryMembersMIDlet", "quit", "()V"); err != nil {
		t.Fatalf("quit() error = %v", err)
	}
	if err := runtime.RunPending(); err != nil {
		t.Fatalf("RunPending() error = %v", err)
	}
	if state := runtime.State(); state != StateDestroyed {
		t.Fatalf("state after Runtime.exit = %v, want destroyed", state)
	}
}
