//go:build windows

package app

import "syscall"

// Windows process-creation flags (not all surfaced as named syscall consts
// across Go versions, so pin the values). DETACHED_PROCESS gives the child no
// console; CREATE_NEW_PROCESS_GROUP detaches it from the parent's Ctrl-C group.
// Together they are the Windows analogue of Setsid.
const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
)

// detachedSysProcAttr detaches the auto-sync child on Windows (no fork/Setsid):
// a new process group with no inherited console, so it survives the parent
// exiting (ADR 2026-07-24-lazy-autosync, lever 3).
func detachedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: detachedProcess | createNewProcessGroup}
}

// processAlive: Windows has no cheap signal-0 probe, so the lockfile falls back
// to age-based reclaim only (a crashed child's lock clears after the staleness
// window). Reporting "alive" here means we never race-reclaim a live sync.
func processAlive(pid int) bool { return pid > 0 }
