package app

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// firstRunHome gives a test its OWN $HOME and store, and clears every CI
// variable — the real $HOME is never a legitimate target for a test that
// installs into $HOME, and a green CI box would otherwise suppress the very
// behaviour under test.
func firstRunHome(t *testing.T) (home, storeRoot string) {
	t.Helper()
	home, storeRoot = t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // windows home resolution
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	t.Setenv(noAutoInstallEnv, "")
	for _, k := range ciEnvVars {
		t.Setenv(k, "")
	}
	return home, storeRoot
}

// runRaw is runCLI without the exit-code assertion and with stderr kept apart:
// the first-run notice is written to stderr on purpose, so that a first run of
// `query --json` still puts nothing but JSON on stdout.
func runRaw(t *testing.T, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	var out, errb bytes.Buffer
	code = Run(args, &out, &errb)
	return out.String(), errb.String(), code
}

// The adoption gap in one test: install the CLI, run a verb, and the skill an
// agent actually reads must be there — announced, not silent. Then run again
// and the machine must be untouched and quiet, because a tool that re-announces
// itself every invocation is a tool people learn to ignore.
//
// The assertions are on a content-hashed snapshot of $HOME (helpflag_test.go's
// snapshot), not on stdout: the defect class this project keeps hitting is a
// verb that prints something plausible while doing something else, and only the
// filesystem can tell those apart.
func TestFirstRunInstallsTheSkillThenGoesSilent(t *testing.T) {
	home, _ := firstRunHome(t)
	t.Setenv(assumeTTYEnv, "1") // a bytes.Buffer is never a char device

	before := snapshot(t, home)
	if before != "" {
		t.Fatalf("fixture broken — temp HOME is not empty:\n%s", before)
	}

	stdout, stderr, code := runRaw(t, "status")
	if code != 0 {
		t.Fatalf("first run exited %d: %s%s", code, stdout, stderr)
	}
	after := snapshot(t, home)
	if !strings.Contains(after, filepath.Join(home, ".claude", "skills", "ctx-optimize", "SKILL.md")) {
		t.Fatalf("first run installed no skill:\n%s", after)
	}
	if !strings.Contains(after, filepath.Join(home, ".agents", "skills", "ctx-optimize", "SKILL.md")) {
		t.Fatalf("first run skipped the ~/.agents skill dir:\n%s", after)
	}
	// Every path written is named, and the undo is named with them.
	for _, want := range []string{
		filepath.Join(home, ".claude", "skills", "ctx-optimize"),
		filepath.Join(home, ".agents", "skills", "ctx-optimize"),
		filepath.Join(home, ".claude", "settings.json"),
		filepath.Join(home, ".config", "ctx-optimize", "auto-install.json"),
		"ctx-optimize uninstall",
		noAutoInstallEnv,
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("first-run notice does not name %q:\n%s", want, stderr)
		}
	}
	// The owner's decision: the skill and the hook, never the GLOBAL rule.
	if strings.Contains(after, filepath.Join(home, ".claude", "CLAUDE.md")) ||
		strings.Contains(after, filepath.Join(home, ".codex", "AGENTS.md")) {
		t.Fatalf("first run wrote the global always-on rule — that stays with the explicit verb:\n%s", after)
	}
	if !strings.Contains(stderr, "ctx-optimize install") {
		t.Errorf("first run did not tell the user the explicit verb exists:\n%s", stderr)
	}

	// Second run: byte-identical home, and nothing said about installing.
	stdout2, stderr2, code2 := runRaw(t, "status")
	if code2 != 0 {
		t.Fatalf("second run exited %d: %s%s", code2, stdout2, stderr2)
	}
	if again := snapshot(t, home); again != after {
		t.Fatalf("second run wrote to $HOME again\nfirst:\n%s\nsecond:\n%s", after, again)
	}
	if strings.Contains(stderr2, "first run") {
		t.Fatalf("second run re-announced the install:\n%s", stderr2)
	}
}

// The stamp is the record, and it must outrank a missing skill: after
// `uninstall`, the next verb must NOT quietly put the skill back. Removing
// something and having it return is worse than it never leaving.
func TestUninstallIsNotUndoneByTheNextRun(t *testing.T) {
	home, _ := firstRunHome(t)
	t.Setenv(assumeTTYEnv, "1")

	runRaw(t, "status") // first run installs
	out, _ := runCLI(t, 0, "uninstall")
	afterUninstall := snapshot(t, home)
	if strings.Contains(afterUninstall, "SKILL.md") {
		t.Fatalf("fixture broken — uninstall left the skill:\n%s", afterUninstall)
	}
	// The stamp is the one thing uninstall keeps, and it must SAY so — an
	// unexplained file left in $HOME is the shape this whole feature avoids.
	stamp := filepath.Join(home, ".config", "ctx-optimize", "auto-install.json")
	if !strings.Contains(afterUninstall, stamp) {
		t.Fatalf("uninstall removed the first-run record — the next verb would reinstall:\n%s", afterUninstall)
	}
	if !strings.Contains(out, stamp) {
		t.Errorf("uninstall does not say it kept %s:\n%s", stamp, out)
	}

	runRaw(t, "status")
	if again := snapshot(t, home); again != afterUninstall {
		t.Fatalf("a verb reinstalled after an explicit uninstall\nafter uninstall:\n%s\nafter run:\n%s", afterUninstall, again)
	}
}

// Suppression is the half that earns the trust. A container running one query
// in a pipeline must accrete NOTHING in its home directory, and each of these
// is a case where a human is demonstrably not watching (or has said no).
func TestFirstRunIsSuppressed(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		tty  bool
	}{
		{name: "CI=1", env: map[string]string{"CI": "1"}, tty: true},
		{name: "CI=true", env: map[string]string{"CI": "true"}, tty: true},
		{name: "GITHUB_ACTIONS", env: map[string]string{"GITHUB_ACTIONS": "true"}, tty: true},
		{name: "GITLAB_CI", env: map[string]string{"GITLAB_CI": "true"}, tty: true},
		{name: "opt-out", env: map[string]string{noAutoInstallEnv: "1"}, tty: true},
		{name: "not a tty", tty: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home, _ := firstRunHome(t)
			if tc.tty {
				t.Setenv(assumeTTYEnv, "1")
			} else {
				t.Setenv(assumeTTYEnv, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			_, stderr, code := runRaw(t, "status")
			if code != 0 {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			if after := snapshot(t, home); after != "" {
				t.Fatalf("wrote into $HOME anyway:\n%s", after)
			}
			if strings.Contains(stderr, "first run") {
				t.Fatalf("announced an install it must not perform:\n%s", stderr)
			}
		})
	}
}

// The suppression above proves a bytes.Buffer is not a terminal. This proves
// the thing that actually ships: a REAL redirected stdout, through the three
// redirects a script uses. `/dev/null` is the one that was broken — it is a
// CHARACTER DEVICE, so the first cut's `os.ModeCharDevice` test called it a
// terminal, and `ctx-optimize add . >/dev/null 2>&1` in a cron job or a
// Dockerfile RUN line wrote 40 files into $HOME. The kernel's terminal ioctl
// (tty_unix.go) is what tells /dev/null and a tty apart.
func TestFirstRunSuppressedOnRedirectedStdout(t *testing.T) {
	cases := []struct {
		name string
		open func(t *testing.T) *os.File
	}{
		{"regular file", func(t *testing.T) *os.File {
			f, err := os.Create(filepath.Join(t.TempDir(), "out.txt"))
			if err != nil {
				t.Fatal(err)
			}
			return f
		}},
		{"pipe", func(t *testing.T) *os.File {
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			go func() { _, _ = io.Copy(io.Discard, r) }() // never block on a full pipe
			return w
		}},
		{"/dev/null", func(t *testing.T) *os.File {
			f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			return f
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home, _ := firstRunHome(t)
			t.Setenv(assumeTTYEnv, "") // the real detector must decide
			out := tc.open(t)
			defer out.Close()

			var errb bytes.Buffer
			if code := Run([]string{"status"}, out, &errb); code != 0 {
				t.Fatalf("exit %d: %s", code, errb.String())
			}
			if after := snapshot(t, home); after != "" {
				t.Fatalf("stdout redirected to a %s is not a terminal, but $HOME grew:\n%s", tc.name, after)
			}
			if strings.Contains(errb.String(), "first run") {
				t.Fatalf("announced an install it must not perform:\n%s", errb.String())
			}
		})
	}
}

// The escape hatch the other first-run tests depend on: without it, a
// bytes.Buffer can never trigger the install and every positive assertion in
// this file would go vacuously green.
func TestAssumeTTYForcesTheTerminalTest(t *testing.T) {
	var buf bytes.Buffer
	t.Setenv(assumeTTYEnv, "")
	if isTerminal(&buf) {
		t.Fatal("a bytes.Buffer must never read as a terminal")
	}
	t.Setenv(assumeTTYEnv, "1")
	if !isTerminal(&buf) {
		t.Fatalf("%s=1 must force the terminal test on", assumeTTYEnv)
	}
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devnull.Close()
	t.Setenv(assumeTTYEnv, "")
	if isTerminal(devnull) {
		t.Fatal("/dev/null is a character device but NOT a terminal")
	}
}

// `--help` is the flag someone types when they are NOT sure they want the
// effect. helpflag_test.go gates the verbs' own handlers; this gates the
// ORDERING — auto-install runs after wantsHelp, so asking a question can never
// install anything, even on a TTY in a fresh home.
func TestHelpDoesNotTriggerFirstRunInstall(t *testing.T) {
	for _, verb := range []string{"query", "status", "boundaries", "add", "install"} {
		t.Run(verb, func(t *testing.T) {
			home, _ := firstRunHome(t)
			t.Setenv(assumeTTYEnv, "1")

			stdout, stderr, code := runRaw(t, verb, "--help")
			if code != 0 {
				t.Fatalf("exit %d: %s%s", code, stdout, stderr)
			}
			if after := snapshot(t, home); after != "" {
				t.Fatalf("`%s --help` installed into $HOME:\n%s", verb, after)
			}
		})
	}
}

// A first run must not corrupt machine output: the notice goes to stderr, so
// `--json` on stdout stays parseable by whatever is reading it.
func TestFirstRunNoticeKeepsStdoutClean(t *testing.T) {
	home, _ := firstRunHome(t)
	t.Setenv(assumeTTYEnv, "1")

	stdout, stderr, code := runRaw(t, "status", "--json")
	if code != 0 {
		t.Fatalf("exit %d: %s%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "first run") {
		t.Fatalf("fixture broken — no first-run install happened:\n%s", stderr)
	}
	if strings.Contains(stdout, "first run") || !strings.HasPrefix(strings.TrimSpace(stdout), "{") {
		t.Fatalf("the notice leaked into machine output:\n%s", stdout)
	}
	_ = home
}

// The stamp names what it wrote, so a user can see later what landed on their
// machine without re-deriving it.
func TestFirstRunStampRecordsThePaths(t *testing.T) {
	home, _ := firstRunHome(t)
	t.Setenv(assumeTTYEnv, "1")
	runRaw(t, "status")

	stamp := filepath.Join(home, ".config", "ctx-optimize", "auto-install.json")
	data, err := os.ReadFile(stamp)
	if err != nil {
		t.Fatalf("no stamp written: %v", err)
	}
	if !strings.Contains(string(data), filepath.Join(home, ".claude", "skills", "ctx-optimize")) {
		t.Fatalf("stamp does not record what was written:\n%s", data)
	}
}
