package launcher

import (
	"bytes"
	"os"
	"strconv"
)

// processStateKnown says processUncollected asks this system about a
// process's state.
const processStateKnown = true

// processUncollected reports whether a pid is a process that has exited and
// that its parent has not yet waited for: its state in /proc is Z, or X while
// it is being collected. The state follows the command name, which is in
// parentheses and may hold either itself, so it is read after the last one.
func processUncollected(pid int) bool {
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return false
	}
	end := bytes.LastIndexByte(stat, ')')
	if end < 0 || end+2 >= len(stat) {
		return false
	}
	state := stat[end+2]
	return state == 'Z' || state == 'X'
}
