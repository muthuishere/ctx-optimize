package app

// First-run install (ADR 2026-08-15-skills-on-by-default, D1).
//
// The adoption gap this closes: `npm install -g @muthuishere/ctx-optimize`
// gives the user a CLI their agent never calls, because `install --skills` is a
// second command nobody runs. The graph gets built, is correct, and is
// invisible.
//
// The mechanism is deliberately NOT an npm postinstall (D0): postinstall is the
// supply-chain shape teams block, `npm ci --ignore-scripts` skips it for exactly
// the users most likely to care, it writes to $HOME before the binary has ever
// run, and it covers only the npm channel. Running the binary is the act of
// consent, and it is the one event every channel shares — npm, go install, the
// tarballs, a bare downloaded binary.
//
// What it may write, and what it may not:
//
//   - the skill (~/.claude/skills/ctx-optimize, ~/.agents/skills/ctx-optimize)
//     and the prompt hooks — yes.
//   - the GLOBAL always-on rule in ~/.claude/CLAUDE.md / ~/.codex/AGENTS.md —
//     NO. That is a larger claim on the user's machine and stays with the
//     explicit `install` verb; first run only says the verb exists (owner
//     decision, 2026-08-15).
//   - anything inside a repo (the CLAUDE.md/AGENTS.md pointer block) — NO.
//     That is a committed, reviewable change to someone else's repository and
//     stays an explicit act, with `init`.
//
// It announces every path it wrote and how to undo it, on ONE line. A tool that
// writes into $HOME silently is not something this project ships, whatever the
// convenience. The announcement goes to STDERR so a first run of
// `query --json` still emits parseable JSON on stdout; the TTY test is on
// stdout, per the ADR, because stdout is what tells us a human is watching.

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/skills"
	"github.com/muthuishere/ctx-optimize/internal/store"
)

// noAutoInstallEnv is the user's off switch, and it is checked before anything
// is read or written.
const noAutoInstallEnv = "CTX_OPTIMIZE_NO_AUTO_INSTALL"

// assumeTTYEnv forces the terminal test to pass. Tests need it (they inject a
// bytes.Buffer, which is never a char device), and it is the honest escape for
// a user whose terminal multiplexer or wrapper hides the tty.
const assumeTTYEnv = "CTX_OPTIMIZE_ASSUME_TTY"

// ciEnvVars: ANY of these set to a non-empty value suppresses the install. A
// container running one `query` in a pipeline must not accrete files in its
// home directory, and the conservative direction here is always "do nothing".
var ciEnvVars = []string{
	"CI", "CONTINUOUS_INTEGRATION", "BUILD_NUMBER",
	"GITHUB_ACTIONS", "GITLAB_CI", "CIRCLECI", "BUILDKITE", "DRONE",
	"JENKINS_URL", "TEAMCITY_VERSION", "TF_BUILD", "APPVEYOR",
	"BITBUCKET_BUILD_NUMBER", "CODEBUILD_BUILD_ID", "HUDSON_URL",
}

// noAutoInstallVerbs never trigger it. `install`/`uninstall`/`update` own this
// surface explicitly (auto-installing under `uninstall` would fight the user);
// `hook-context` and `__autosync` are machine lanes; `version`/`help` are
// diagnostics that must stay inert, because they are what a script or a
// build task runs to check the binary exists.
var noAutoInstallVerbs = map[string]bool{
	"install": true, "uninstall": true, "update": true,
	"hook-context": true, "__autosync": true,
	"version": true, "--version": true, "-v": true,
	"help": true, "--help": true, "-h": true,
}

// firstRunStampPath is the record that makes the second run silent: the
// machine-global config dir the dotenv ladder already uses. It lives OUTSIDE
// the store, so deleting a store does not re-trigger, and outside the skill
// dirs, so `uninstall` (which removes those) is respected rather than undone by
// the next verb.
func firstRunStampPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "ctx-optimize", "auto-install.json"), nil
}

// maybeFirstRunInstall runs after the --help check and before dispatch. It
// never changes the verb's exit code: a failed courtesy install is reported and
// stepped over, not escalated.
func maybeFirstRunInstall(cmd string, stdout, stderr io.Writer) {
	if !firstRunEligible(cmd, stdout) {
		return
	}
	written, err := firstRunInstall()
	if err != nil {
		fmt.Fprintf(stderr, "ctx-optimize: first-run skill install skipped (%v)\n", err)
		return
	}
	if len(written) == 0 {
		return
	}
	fmt.Fprintf(stderr, "ctx-optimize: first run — installed the agent skill + prompt hook, writing %s. Undo with `ctx-optimize uninstall`; skip with %s=1.\n",
		strings.Join(written, ", "), noAutoInstallEnv)
	fmt.Fprintf(stderr, "ctx-optimize: `ctx-optimize install` additionally adds the always-on \"knowledge graph before grep\" rule to your global ~/.claude/CLAUDE.md and ~/.codex/AGENTS.md — first run does not touch those, or any repo.\n")
}

// firstRunEligible answers "would writing to $HOME right now be a surprise?".
// Every branch fails toward NO.
func firstRunEligible(cmd string, stdout io.Writer) bool {
	if os.Getenv(noAutoInstallEnv) != "" {
		return false
	}
	if isCI() {
		return false
	}
	if !isTerminal(stdout) {
		return false
	}
	if noAutoInstallVerbs[cmd] {
		return false
	}
	if _, known := verbFlags[cmd]; !known {
		return false // an alias we do not know, or a typo about to be rejected
	}
	stamp, err := firstRunStampPath()
	if err != nil {
		return false
	}
	if _, err := os.Stat(stamp); err == nil {
		return false // recorded: we have done this once, and once is the deal
	}
	dirs, err := skills.Targets(true)
	if err != nil {
		return false
	}
	for _, d := range dirs {
		if _, err := os.Stat(filepath.Join(d, "SKILL.md")); err == nil {
			return false // already installed by hand; leave it alone
		}
	}
	return true
}

func isCI() bool {
	for _, k := range ciEnvVars {
		if os.Getenv(k) != "" {
			return true
		}
	}
	return false
}

// isTerminal reports whether w is a real terminal — a human watching. A pipe,
// a regular file, `>/dev/null`, a test buffer and a captured CI log all read as
// false.
//
// It asks the kernel (the terminal ioctl, tty_unix.go), NOT the file mode: the
// first cut tested `os.ModeCharDevice`, and /dev/null is a character device, so
// `ctx-optimize add . >/dev/null 2>&1` — the redirect every cron job and
// Dockerfile RUN line uses — read as a terminal and installed 40 files into
// $HOME. That is precisely the case the ADR names, arriving through the one
// redirect people use most in scripts.
func isTerminal(w io.Writer) bool {
	if os.Getenv(assumeTTYEnv) != "" {
		return true
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return isTerminalFD(f.Fd())
}

// firstRunInstall writes the skill, the hooks, and the stamp — and returns
// EVERY path it wrote, so the caller can name them all. It honours the same
// machine-global `skills`/`hooks` settings the explicit verb honours; it never
// reads a repo config, because first run must not depend on where the user
// happened to be standing.
func firstRunInstall() ([]string, error) {
	gcfg := &store.GlobalConfig{}
	if root, err := store.Root(""); err == nil {
		if c, err := store.LoadGlobalConfig(root); err == nil {
			gcfg = c
		}
	}
	skillDirs, err := skills.SkillTargets(gcfg.Skills)
	if err != nil {
		return nil, err
	}
	hookPlats, err := skills.HookPlatforms(gcfg.Hooks)
	if err != nil {
		return nil, err
	}

	var written []string
	for _, d := range skillDirs {
		if err := skills.InstallDir(d); err != nil {
			return nil, err
		}
		written = append(written, d)
	}
	// Claude always (it is the one CLI with a real prompt-hook API and Devin
	// reads the same file); codex/copilot only when their CLI is on PATH — a
	// hook file for a CLI the user does not have is litter.
	for _, h := range []struct {
		plat    string
		install func() (string, bool, error)
	}{
		{"claude", skills.InstallClaudeHook},
		{"codex", skills.InstallCodexHook},
		{"copilot", skills.InstallCopilotHook},
	} {
		if !hookPlats[h.plat] {
			continue
		}
		if h.plat != "claude" && !skills.OnPath(h.plat) {
			continue
		}
		p, changed, err := h.install()
		if err != nil {
			continue // a hook we cannot write is not worth failing the verb over
		}
		if changed {
			written = append(written, p)
		}
	}

	stamp, err := firstRunStampPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(stamp), 0o755); err != nil {
		return nil, err
	}
	body := fmt.Sprintf("{\n  \"at\": %q,\n  \"paths\": [\n", time.Now().UTC().Format(time.RFC3339))
	for i, p := range written {
		sep := ","
		if i == len(written)-1 {
			sep = ""
		}
		body += fmt.Sprintf("    %q%s\n", p, sep)
	}
	body += "  ]\n}\n"
	if err := os.WriteFile(stamp, []byte(body), 0o644); err != nil {
		return nil, err
	}
	return append(written, stamp), nil
}
