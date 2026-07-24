//go:build !windows

package app

import "syscall"

// detachedSysProcAttr fully detaches the auto-sync child from the query process
// (ADR 2026-07-24-lazy-autosync, lever 3 — the "separate channel"): a new
// session (Setsid) means the child outlives the parent and is never killed when
// the parent's process group / terminal goes away. No fork of our own — os/exec
// + Setsid is the portable Unix detach.
func detachedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}

// processAlive reports whether pid names a live process — signal 0 probes
// existence without delivering anything (EPERM still means alive). Used to
// reclaim a lockfile whose owner crashed.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}
