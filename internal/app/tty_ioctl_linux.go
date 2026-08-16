//go:build linux

package app

import "syscall"

// TCGETS is the Linux "read the terminal attributes" ioctl.
const ioctlReadTermios = syscall.TCGETS
