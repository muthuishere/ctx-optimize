// Native sources (ADR 2026-07-17-bundled-adapter-templates): an env var
// holding a URL is the whole contract. This file wires the source lane into
// the verbs — `add <NAME>` (capture + record), `capture <NAME>` (Batch to
// stdout, no store write), the `adapters` catalog surface, and the slow lane
// `up` runs after the gather. argv carries NAMES only (H2); resolved values
// live in memory for the dial and are scrubbed from every output.
package app

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/muthuishere/ctx-optimize/internal/project"
	"github.com/muthuishere/ctx-optimize/internal/sources"
	"github.com/muthuishere/ctx-optimize/internal/store"
	"github.com/muthuishere/ctx-optimize/internal/wiki"
)

// sourcesMode maps --sources=always|never (default: the 24h TTL rule, M3).
func sourcesMode(f *flags) (string, error) {
	switch v := f.strs["sources"]; v {
	case "", "ttl":
		return sources.ModeTTL, nil
	case "always":
		return sources.ModeAlways, nil
	case "never":
		return sources.ModeNever, nil
	default:
		return "", fmt.Errorf("bad --sources %q (always | never)", v)
	}
}

// warnTrackedEnv fires the loud warning when a secret-bearing .env file is
// TRACKED in git — an already-exposed secret store must not be silently
// built upon. Detection is `git ls-files --error-unmatch` (the gitignore
// trap: the index wins over a later ignore rule).
func warnTrackedEnv(repo string, stdout io.Writer) {
	for _, rel := range sources.TrackedEnvFiles(repo) {
		fmt.Fprintf(stdout, "WARNING: %s is TRACKED in git — secrets in it are already exposed; untrack it: git rm --cached %s\n", rel, rel)
	}
}

// cmdAddSource is the source lane of `add`: capture via the registry, merge
// into the store, and — on success ONLY — record the name in config sources
// (idempotent), so `up` refreshes it from now on.
func cmdAddSource(f *flags, name string, stdout io.Writer) error {
	sc, err := resolveScope(f)
	if err != nil {
		return err
	}
	storeRoot, err := store.Root(f.strs["store"])
	if err != nil {
		return err
	}
	s, err := store.Open(storeRoot, sc.rootKey)
	if err != nil {
		return err
	}
	warnTrackedEnv(sc.rootDir, stdout)
	outcomes, err := sources.Run([]string{name}, sc.rootDir, s, sources.Options{Mode: sources.ModeAlways}, stdout)
	if err != nil {
		return err
	}
	oc := outcomes[0]
	if oc.Status != sources.StatusCaptured {
		return fmt.Errorf("source %s not captured: %s", oc.ID, oc.Detail)
	}
	// Record on success only (H4) — a teammate's `up` now refreshes it.
	cfg, err := project.Load(sc.rootDir)
	if err != nil {
		return err
	}
	recorded := false
	for _, e := range cfg.Sources {
		if e == name {
			recorded = true
			break
		}
	}
	if !recorded {
		cfg.Sources = append(cfg.Sources, name)
		if err := project.Save(sc.rootDir, cfg); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "recorded %s in %s sources — refreshed on every up (commit it)\n", name, project.FileName)
	}
	pages, err := wiki.Generate(s)
	if err != nil {
		return err
	}
	if _, err := s.UpdateManifest(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wiki: %d pages → %s\n", pages, filepath.Join(s.Dir, "wiki"))
	return nil
}

// cmdCapture is the composition/debug primitive: one connector, Batch JSON
// on stdout, no store write. Takes a NAME only — never a raw URL on argv.
func cmdCapture(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	if len(f.args) != 1 || !sources.IsEnvName(f.args[0]) {
		return fmt.Errorf("usage: ctx-optimize capture <ENV_NAME> — names only on argv (^[A-Z_][A-Z0-9_]*$); the value (a URL) lives in the environment or .ctxoptimize/.env")
	}
	sc, err := resolveScope(f)
	if err != nil {
		return err
	}
	b, err := sources.CaptureOnly(f.args[0], sc.rootDir)
	if err != nil {
		return err
	}
	return emit(stdout, b)
}

// upSources is the slow lane `up` runs after the gather: every recorded
// source, parallel dial + serial merge, under the 24h TTL rule
// (--sources=always|never overrides; --strict fails on unset vars), then the
// reconcile report (undeclared source producers; prune with --prune-sources).
func upSources(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	mode, err := sourcesMode(f)
	if err != nil {
		return err
	}
	sc, err := resolveScope(f)
	if err != nil {
		return err
	}
	cfg, err := project.Load(sc.rootDir)
	if err != nil {
		return err
	}
	storeRoot, err := store.Root(f.strs["store"])
	if err != nil {
		return err
	}
	// ZERO added cost when this repo never had sources: no declared entries
	// AND no capture stamps ⇒ nothing to dial, nothing to reconcile.
	if len(cfg.Sources) == 0 {
		stamps, err := sources.SourceStamps(filepath.Join(storeRoot, filepath.FromSlash(sc.rootKey)))
		if err != nil || len(stamps) == 0 {
			return nil
		}
	}
	s, err := store.Open(storeRoot, sc.rootKey)
	if err != nil {
		return err
	}
	// Reconcile even when no sources remain declared (as long as stamps say
	// some existed) — deleting the LAST entry must still surface its ghost.
	orphans, pruned, err := sources.Reconcile(s, cfg.Sources, f.bools["prune-sources"])
	if err != nil {
		return err
	}
	switch {
	case len(orphans) > 0 && f.bools["prune-sources"]:
		fmt.Fprintf(stdout, "pruned %d nodes from undeclared sources: %s\n", pruned, strings.Join(orphans, ", "))
	case len(orphans) > 0:
		fmt.Fprintf(stdout, "source producers no longer declared in config: %s — `ctx-optimize up --prune-sources` removes their nodes\n", strings.Join(orphans, ", "))
	}
	if len(cfg.Sources) == 0 {
		return nil // zero added cost on the gather path
	}
	warnTrackedEnv(sc.rootDir, stdout)
	outcomes, err := sources.Run(cfg.Sources, sc.rootDir, s, sources.Options{Mode: mode, Strict: f.bools["strict"]}, stdout)
	if err != nil {
		return err
	}
	captured := 0
	for _, oc := range outcomes {
		if oc.Status == sources.StatusCaptured {
			captured++
		}
	}
	if captured > 0 || pruned > 0 {
		pages, err := wiki.Generate(s)
		if err != nil {
			return err
		}
		if _, err := s.UpdateManifest(); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "wiki: %d pages → %s\n", pages, filepath.Join(s.Dir, "wiki"))
	}
	if f.bools["strict"] {
		return sources.StrictError(outcomes)
	}
	return nil
}

// sourcesStatusLine renders each recorded source's staleness for status/up
// ("BILLING_DB_URL captured 2h ago"). Empty when nothing was ever captured.
func sourcesStatusLine(storeDir string, now time.Time) string {
	stamps, err := sources.SourceStamps(storeDir)
	if err != nil || len(stamps) == 0 {
		return ""
	}
	ids := make([]string, 0, len(stamps))
	for id := range stamps {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		age := time.Duration(now.Unix()-stamps[id]) * time.Second
		if age < 0 {
			age = 0
		}
		parts = append(parts, fmt.Sprintf("%s captured %s ago", id, sources.FormatAge(age)))
	}
	return strings.Join(parts, " · ")
}

// listSources renders the recorded sources + supported schemes for the
// `adapters` catalog (names/skeletons only — entries are var-shaped by the
// load gate).
func listSources(cfg *project.Config, stdout io.Writer) {
	if len(cfg.Sources) > 0 {
		fmt.Fprintln(stdout, "sources (recorded in config.json, refreshed on up):")
		for _, e := range cfg.Sources {
			fmt.Fprintf(stdout, "  %s\n", sources.SourceID(e))
		}
	} else {
		fmt.Fprintln(stdout, "no sources recorded — `ctx-optimize add <ENV_NAME>` captures a database/bucket/queue/API by env-var name")
	}
	fmt.Fprintf(stdout, "schemes: %s, or a file path — setup card: ctx-optimize adapters help <scheme>\n",
		strings.Join(sources.SupportedSchemes(), " "))
}
