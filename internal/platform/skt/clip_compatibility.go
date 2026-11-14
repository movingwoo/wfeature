package skt

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"

	"github.com/movingwoo/wfeature/internal/platform/compatibility"
)

// classSetFixes are the registry fixes an archive's class set selects. Every
// one is keyed by the same digest of all the archive's classes, so a runtime
// reads them together and hashes the classes once, where asking fix by fix
// hashed them four times for every runtime.
type classSetFixes struct {
	// inclusiveSetClip: only this observed original code revision uses
	// inclusive setClip extents. Profile declarations do not establish this
	// behavior. Hash every class so another revision, even one sharing the
	// clipping helper, stays unaffected. Container filenames, ZIP ordering
	// and timestamps do not identify code.
	inclusiveSetClip bool
	// tutorialNameCache: see prepareTutorialCompatibility.
	tutorialNameCache bool
	// resumeWithoutStart: a resume calls nothing (see resume).
	resumeWithoutStart bool
	// resumeThroughStart: this class set was written for MIDP's reactivation
	// on a handset VM that let a finished thread start again: its pauseApp
	// ends a loop, and its startApp checks isAlive and starts that same Thread
	// object again. Such a title is resumed through startApp and its VM
	// restarts a finished thread; every other title is resumed without a
	// second start (resumeStarted) and gets the language's refusal. See
	// docs/history/skt.md#skt-resume-2026-11-10.
	resumeThroughStart bool
}

func (archive *Archive) classSetFixes() classSetFixes {
	digest := archive.clipCodeDigest()
	selects := func(fix string) bool {
		return compatibility.HasFix("skt", compatibility.JavaClassSet, digest, fix)
	}
	return classSetFixes{
		inclusiveSetClip:   selects(compatibility.SKTInclusiveSetClip),
		tutorialNameCache:  selects(compatibility.SKTTutorialNameCache),
		resumeWithoutStart: selects(compatibility.SKTResumeWithoutStart),
		resumeThroughStart: selects(compatibility.SKTResumeThroughStart),
	}
}

func (archive *Archive) clipCodeDigest() string {
	names := make([]string, 0, len(archive.Entries))
	for name := range archive.Entries {
		if strings.HasSuffix(name, ".class") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	digest := sha256.New()
	var size [8]byte
	for _, name := range names {
		data := archive.Entries[name]
		binary.BigEndian.PutUint64(size[:], uint64(len(name)))
		digest.Write(size[:])
		digest.Write([]byte(name))
		binary.BigEndian.PutUint64(size[:], uint64(len(data)))
		digest.Write(size[:])
		digest.Write(data)
	}
	return fmt.Sprintf("%x", digest.Sum(nil))
}
