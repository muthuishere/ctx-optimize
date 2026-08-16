//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd

package app

import (
	"syscall"
	"unsafe"
)

// isTerminalFD asks the kernel whether fd is a TERMINAL, not whether it is a
// character device.
//
// The distinction is the whole point, and it was a real hole: `/dev/null` IS a
// character device, so a `st.Mode()&os.ModeCharDevice != 0` test called it a
// terminal. Measured with a throwaway $HOME and no CI variable set —
//
//	stdout -> regular file   0 files written in $HOME   correct
//	stdout -> pipe (| cat)   0 files written in $HOME   correct
//	stdout -> /dev/null     40 files written in $HOME   WRONG
//
// — which means `ctx-optimize add . >/dev/null 2>&1` in a cron job, a
// Dockerfile RUN line or a systemd unit would have accreted 40 files in the
// home directory of a machine that never asked for them. `>/dev/null` is THE
// redirect scripts use.
//
// The terminal ioctl (TIOCGETA on darwin/BSD, TCGETS on linux — what
// golang.org/x/term does) is the reliable test: only a real tty answers it,
// and /dev/null returns ENOTTY. Hand-rolled here rather than taken as a
// dependency, in the house style of autosync_{unix,windows}.go.
func isTerminalFD(fd uintptr) bool {
	var t syscall.Termios
	_, _, errno := syscall.Syscall6(syscall.SYS_IOCTL, fd,
		uintptr(ioctlReadTermios), uintptr(unsafe.Pointer(&t)), 0, 0, 0)
	return errno == 0
}
