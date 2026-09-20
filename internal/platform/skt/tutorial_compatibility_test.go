package skt

import (
	"bytes"
	_ "embed"
	"os"
	"strings"
	"testing"

	"github.com/movingwoo/wfeature/internal/jvm/classfile"
)

//go:embed testdata/tutorial-cache.jar
var tutorialCacheJAR []byte

func TestTutorialCacheTransitionJAR(t *testing.T) {
	archive, err := Open(tutorialCacheJAR)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepareTutorialCompatibility(archive)) != 0 {
		t.Fatal("unregistered fixture selected original compatibility")
	}
	original := bytes.Clone(archive.Entries["TutorialCacheMIDlet.class"])
	_, failure := Start(archive, Options{DisableAuthentication: true, Framebuffer: newTestFramebuffer(t, 120, 160)})
	if err := failure; err == nil || !strings.Contains(err.Error(), "ArrayIndexOutOfBoundsException") {
		t.Fatalf("expected stale-cache failure, got %v", err)
	}
	adapted := replaceTutorialPhaseStore(original, "transition", "()V", 1, "TutorialCacheMIDlet", "phase", "names")
	if adapted == nil {
		t.Fatal("fixture adaptation missing")
	}
	if !bytes.Equal(original, archive.Entries["TutorialCacheMIDlet.class"]) {
		t.Fatal("archive bytes changed")
	}
	before, _ := classfile.Parse(original)
	after, err := classfile.Parse(adapted)
	if err != nil {
		t.Fatal(err)
	}
	for i, method := range before.Methods {
		oldCode, newCode := method.CodeAttribute(), after.Methods[i].CodeAttribute()
		if oldCode == nil {
			continue
		}
		if method.Name != "transition" && !bytes.Equal(oldCode.Bytecode, newCode.Bytecode) {
			t.Fatalf("unrelated method %s changed", method.Name)
		}
		if len(oldCode.Bytecode) != len(newCode.Bytecode) || oldCode.MaxStack != newCode.MaxStack || oldCode.MaxLocals != newCode.MaxLocals {
			t.Fatal("method layout changed")
		}
	}
	archive.Entries["TutorialCacheMIDlet.class"] = adapted
	fixed := startLicenseFixture(t, archive, false, nil)
	if err := fixed.RunPending(); err != nil {
		t.Fatal(err)
	}
	value, err := fixed.VM.InvokeStatic("TutorialCacheMIDlet", "result", "()I")
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := value.Int32(); n != 7 {
		t.Fatalf("ability name length = %d", n)
	}
	phase, ok := fixed.MIDlet.FieldValue("TutorialCacheMIDlet.phase:I")
	if !ok {
		t.Fatal("missing phase")
	}
	if n, _ := phase.Int32(); n != 5 {
		t.Fatalf("final phase = %d", n)
	}
}

func TestTutorialCacheRefusesInvalidTransforms(t *testing.T) {
	archive, err := Open(tutorialCacheJAR)
	if err != nil {
		t.Fatal(err)
	}
	original := archive.Entries["TutorialCacheMIDlet.class"]
	for _, pc := range []int{-1, 0, 2, 6, len(original), int(^uint(0) >> 1)} {
		if replaceTutorialPhaseStore(original, "transition", "()V", pc, "TutorialCacheMIDlet", "phase", "names") != nil {
			t.Fatalf("invalid offset %d accepted", pc)
		}
	}
	for end := 0; end < len(original); end++ {
		if replaceTutorialPhaseStore(original[:end], "transition", "()V", 1, "TutorialCacheMIDlet", "phase", "names") != nil {
			t.Fatalf("truncation %d accepted", end)
		}
	}
	if replaceTutorialPhaseStore(original, "transition", "()V", 1, "TutorialCacheMIDlet", "other", "names") != nil {
		t.Fatal("wrong field accepted")
	}
}

func TestLocalTutorialCompatibilityFingerprint(t *testing.T) {
	path := os.Getenv("WFEATURE_SKT_TUTORIAL_ARCHIVE")
	if path == "" {
		t.Skip("set WFEATURE_SKT_TUTORIAL_ARCHIVE to an original local archive")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := Open(data)
	if err != nil {
		t.Fatal(err)
	}
	original := bytes.Clone(archive.Entries["f.class"])
	if len(prepareTutorialCompatibility(archive)) != 1 {
		t.Fatal("original revision not adapted")
	}
	if !bytes.Equal(original, archive.Entries["f.class"]) {
		t.Fatal("original archive changed")
	}
	archive.Entries["extra.class"] = []byte("changed class set")
	if len(prepareTutorialCompatibility(archive)) != 0 {
		t.Fatal("changed revision adapted")
	}
}
