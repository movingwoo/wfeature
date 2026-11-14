package skt

import "github.com/movingwoo/wfeature/internal/platform/compatibility"

func (archive *Archive) resumeWithoutRestart() bool {
	return compatibility.HasFix("skt", compatibility.JavaClassSet, archive.clipCodeDigest(), compatibility.SKTResumeWithoutStart)
}

// resumeThroughStart reports whether this class set was written for MIDP's
// reactivation on a handset VM that let a finished thread start again: its
// pauseApp ends a loop, and its startApp checks isAlive and starts that same
// Thread object again. Such a title is resumed through startApp and its VM
// restarts a finished thread; every other title is resumed without a second
// start (resumeStarted) and gets the language's refusal. See
// docs/history/skt.md#skt-resume-2026-11-10.
func (archive *Archive) resumeThroughStart() bool {
	return compatibility.HasFix("skt", compatibility.JavaClassSet, archive.clipCodeDigest(), compatibility.SKTResumeThroughStart)
}
