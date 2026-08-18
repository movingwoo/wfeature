package ktf

import "sort"

// RuntimeJavaClassNames reports every Java class this platform publishes to
// guest code, sorted.
//
// It is the list a scan of what a title *names* has to be compared against,
// and the comparison is worth making because a name outside this list does not
// fail where it is used: the fallback record gives the guest a real class with
// an empty method table, so the omission surfaces as "method … was not found
// from class …" at the first call, which can be an hour into a save. See
// "A class left out of that table still resolves, and answers nothing" in
// `docs/ktf.md`, and `internal/tools/apiscan`, which reads the names out of a
// client image and reports the difference.
func RuntimeJavaClassNames() []string {
	names := make([]string, 0, len(runtimeJavaClasses))
	for name := range runtimeJavaClasses {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
