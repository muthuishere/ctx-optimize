//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package app

import "syscall"

// TIOCGETA is the BSD/darwin "read the terminal attributes" ioctl.
const ioctlReadTermios = syscall.TIOCGETA
