// Package zipentry holds the conventions every platform archive's entry
// names follow.
package zipentry

import "strings"

// A platform archive names its marker at the root — `__adf__`, `app_info`, a
// descriptor beside the JAR — and the loaders look those up by exact name. A
// copy that has been unpacked and zipped up again often gains the game's name
// as a containing folder, which is the same archive as far as any of this is
// concerned and an unrecognisable one as far as the lookups are concerned. The
// failure is total rather than partial: detection claims the archive for
// nobody and a loader handed it reports a missing marker.
//
// **Removing a shared directory cannot damage an archive that works.** If
// every entry is inside one directory then the marker is not at the root, and
// an archive whose marker is not at the root is one no loader here can read
// today. So the rule applies exactly to archives that are already failing.
//
// A JAR is unaffected without needing to be excluded: its entries never all
// sit under one directory, because `META-INF/` is always beside the rest.

// Directory answers the single directory every name lies inside, or the empty
// string when they do not share one. Names are expected already normalised to
// forward slashes, which every caller here does before asking.
//
// Only one level is removed. An archive wrapped twice is not something any
// copy has been seen to do, and stripping until the names run out would eat a
// real directory from an archive that genuinely keeps everything in one.
func Directory(names []string) string {
	if len(names) == 0 {
		return ""
	}
	prefix := ""
	for _, name := range names {
		head, rest, nested := strings.Cut(name, "/")
		// A name at the root settles it: there is no directory they all share.
		if !nested || rest == "" {
			return ""
		}
		if head == "" || head == "." || head == ".." {
			return ""
		}
		if prefix == "" {
			prefix = head
			continue
		}
		if head != prefix {
			return ""
		}
	}
	return prefix
}

// Unwrap removes the shared directory from a set of read entries, or answers
// them unchanged. A rename that would collide with another entry abandons the
// whole attempt rather than dropping one of the two: an archive that
// contradicts itself is better reported by the loader that was going to look
// its marker up than silently reduced here.
func Unwrap(entries map[string][]byte) map[string][]byte {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	directory := Directory(names)
	if directory == "" {
		return entries
	}
	unwrapped := make(map[string][]byte, len(entries))
	for name, contents := range entries {
		stripped := name[len(directory)+1:]
		if _, taken := unwrapped[stripped]; taken {
			return entries
		}
		unwrapped[stripped] = contents
	}
	return unwrapped
}
