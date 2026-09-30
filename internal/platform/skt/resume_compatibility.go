package skt

import "github.com/movingwoo/wfeature/internal/platform/compatibility"

func (archive *Archive) resumeWithoutRestart() bool {
	return compatibility.HasFix("skt", compatibility.JavaClassSet, archive.clipCodeDigest(), compatibility.SKTResumeWithoutStart)
}
