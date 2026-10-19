package launcher

import "golang.org/x/sys/unix"

// processStateZombie is SZOMB in <sys/proc.h>: exited, awaiting collection by
// the parent.
const processStateZombie = 5

// processUncollected reports whether a pid is a process that has exited and
// that its parent has not yet waited for. A pid the system says nothing about
// is not one.
func processUncollected(pid int) bool {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	return err == nil && int(info.Proc.P_pid) == pid && info.Proc.P_stat == processStateZombie
}
