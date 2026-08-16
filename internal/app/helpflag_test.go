package app

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// snapshot fingerprints every file under dirs by path AND content, so a
// rewrite-in-place is caught as well as a create or a delete. Asserting on a
// snapshot rather than on stdout is the point: the defect was a verb that did
// its job while printing something plausible, and only the filesystem can tell
// those apart.
func snapshot(t *testing.T, dirs ...string) string {
	t.Helper()
	var lines []string
	for _, d := range dirs {
		_ = filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() {
				return nil
			}
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			lines = append(lines, fmt.Sprintf("%s %x", p, sha256.Sum256(b)))
			return nil
		})
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// `--help` is in commonFlags, so every verb ACCEPTED it — and every verb's
// handler then IGNORED it. Measured before the fix: `ctx-optimize install
// --help` performed the install (skills for four agents, hooks, the global rule
// written into the home directory) instead of printing usage; `uninstall
// --help` removed them again; `add`, `sync`, `up`, `init`, `wiki` and `update`
// all did their work. `--help` is the flag someone types when they are NOT sure
// they want the effect, which makes this the same class as the unknown flag
// that was silently dropped: a question answered with an action.
//
// Handled centrally now (app.go: wantsHelp), so the guarantee is per-VERB and
// this table is the gate for all of them (ADR 2026-08-15-skills-on-by-default,
// D2).
func TestHelpFlagWritesNothing(t *testing.T) {
	verbs := [][]string{
		{"install"}, {"uninstall"}, {"update"},
		{"add"}, {"sync"}, {"up"}, {"init"}, {"wiki"},
		{"capture"}, {"merge"},
		{"store", "delete"}, {"remote", "push"}, {"remote", "pull"},
	}
	for _, verb := range verbs {
		for _, form := range []string{"--help", "-h"} {
			t.Run(strings.Join(verb, " ")+" "+form, func(t *testing.T) {
				home, store, repo := t.TempDir(), t.TempDir(), t.TempDir()
				t.Setenv("HOME", home)
				t.Setenv("USERPROFILE", home) // windows home resolution
				t.Setenv("CTX_OPTIMIZE_STORE", store)
				if err := os.WriteFile(filepath.Join(repo, "main.go"),
					[]byte("package main\n\nfunc Main() {}\n"), 0o644); err != nil {
					t.Fatal(err)
				}

				before := snapshot(t, home, store, repo)
				args := append(append([]string{}, verb...), "--path", repo, form)
				out, _ := runCLI(t, 0, args...)
				after := snapshot(t, home, store, repo)

				if before != after {
					t.Fatalf("`%s %s` wrote to disk instead of printing usage\nbefore:\n%s\nafter:\n%s",
						strings.Join(verb, " "), form, before, after)
				}
				if !strings.Contains(out, verb[0]) {
					t.Fatalf("`%s %s` printed no usage for the verb:\n%s",
						strings.Join(verb, " "), form, out)
				}
			})
		}
	}
}

// The sharpest case, and the one that is invisible to a snapshot of an empty
// home: `uninstall --help` used to DELETE a real installation. Install for
// real, then ask for help, and require every file to survive byte-for-byte.
func TestUninstallHelpDoesNotUninstall(t *testing.T) {
	home, store := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CTX_OPTIMIZE_STORE", store)

	runCLI(t, 0, "install", "--claude")
	installed := snapshot(t, home)
	if !strings.Contains(installed, "SKILL.md") {
		t.Fatalf("fixture broken — install wrote no skill:\n%s", installed)
	}

	runCLI(t, 0, "uninstall", "--help")
	if after := snapshot(t, home); after != installed {
		t.Fatalf("`uninstall --help` removed the installation\nbefore:\n%s\nafter:\n%s", installed, after)
	}
}
