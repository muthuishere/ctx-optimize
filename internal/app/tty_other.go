//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd

package app

// Windows (and anything else without the terminal ioctl) resolves to "not a
// terminal", so the first-run install simply never fires unasked there. Failing
// toward doing nothing is the doctrine: a missed convenience costs one
// documented command (`ctx-optimize install`), while a wrong "yes" writes into
// someone's home directory. CTX_OPTIMIZE_ASSUME_TTY=1 is the explicit opt-in.
func isTerminalFD(uintptr) bool { return false }
