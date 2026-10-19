//go:build !darwin && !linux && !windows

package launcher

// processUncollected cannot ask this system about a process's state, so a pid
// that answers the signal is taken for a running process, as it always was.
func processUncollected(int) bool {
	return false
}
