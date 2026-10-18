//go:build !windows

package backend

// saveNameRefused reports whether an error is the host refusing a name rather
// than finding nothing at it, which only Windows answers (see the Windows
// file).
func saveNameRefused(error) bool {
	return false
}
