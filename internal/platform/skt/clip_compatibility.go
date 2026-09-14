package skt

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"

	"github.com/movingwoo/wfeature/internal/platform/compatibility"
)

// Only this observed original code revision uses inclusive setClip extents.
// Profile declarations do not establish this behavior. Hash every class so
// another revision, even one sharing the clipping helper, stays unaffected.
// Container filenames, ZIP ordering and timestamps do not identify code.
func (archive *Archive) legacyClipEndpoints() bool {
	return compatibility.HasFix("skt", compatibility.JavaClassSet, archive.clipCodeDigest(), compatibility.SKTInclusiveSetClip)
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
