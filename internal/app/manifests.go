// manifests.go — the `manifests` verb family, mirroring `routes`: core
// recognizers are embedded (the big build tools + k8s), everything
// declarative is a drop-in MANIFEST PACK (internal/extract/manifests/packs.go).
// list: core + discovered packs with their source. add: scaffold a pack by
// name, or fetch one from a GitHub repo / direct .json URL. remove: delete a
// pack file, repo first then global.
package app

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/extract/manifests"
	"github.com/muthuishere/ctx-optimize/internal/grammar"
)

// coreManifestRecognizers documents the embedded channels for `manifests list`.
var coreManifestRecognizers = []string{
	"npm-package-json", "maven-pom", "dotnet-csproj", "dotnet-sln",
	"go-mod", "gradle-deps", "k8s-resource",
}

const manifestsUsage = "usage: ctx-optimize manifests <list | add <name|github-url|json-url> [--global] | remove <name>>"

func cmdManifests(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", manifestsUsage)
	}
	sub, rest := args[0], args[1:]
	f := parseFlags(rest)
	repo, err := resolvePath(f)
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		packs, err := manifests.LoadManifestPacks(repo)
		if err != nil {
			return err
		}
		if f.bools["json"] {
			ps := []map[string]string{}
			for _, p := range packs {
				ps = append(ps, map[string]string{"name": p.Name, "file": p.File})
			}
			return emit(stdout, map[string]any{"core": coreManifestRecognizers, "packs": ps})
		}
		fmt.Fprintf(stdout, "core:  %s\n", strings.Join(coreManifestRecognizers, ", "))
		for _, p := range packs {
			fmt.Fprintf(stdout, "pack:  %s (%s)\n", p.Name, p.File)
		}
		if len(packs) == 0 {
			fmt.Fprintln(stdout, "packs: (none)")
		}
		fmt.Fprintln(stdout, "scaffold one: `ctx-optimize manifests add <name>` · install one: `ctx-optimize manifests add <github-url>`")
		return nil
	case "add":
		if len(f.args) != 1 {
			return fmt.Errorf("usage: ctx-optimize manifests add <name | github-url | url-of-pack.json> [--global]")
		}
		dest := manifests.RepoManifestsDir(repo)
		where := "repo"
		if f.bools["global"] {
			if dest, err = manifests.MachineManifestsDir(); err != nil {
				return err
			}
			where = "global"
		}
		arg := f.args[0]
		if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
			return manifestsAddFromURL(arg, dest, stdout)
		}
		if !routePackNameRe.MatchString(arg) {
			return fmt.Errorf("manifest pack name %q: letters, digits, - and _ only (or pass a URL)", arg)
		}
		p := filepath.Join(dest, arg+".json")
		if _, err := os.Stat(p); err == nil {
			return fmt.Errorf("manifest pack already exists: %s — edit it, or `manifests remove %s` first", p, arg)
		}
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return err
		}
		tmpl := fmt.Sprintf(`{
  "name": %q,
  "_review": true,
  "rules": [
    {"file": "*.deps.json", "format": "json", "path": "libraries.*",
     "emit": "dependency", "namespace": %q}
  ]
}
`, arg, arg)
		if err := os.WriteFile(p, []byte(tmpl), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "scaffolded %s manifest pack: %s — REVIEW IT (the example rule is live), then `ctx-optimize add` applies it\n", where, p)
		return nil
	case "remove":
		if len(f.args) != 1 {
			return fmt.Errorf("usage: ctx-optimize manifests remove <name>")
		}
		name := f.args[0]
		machine, err := manifests.MachineManifestsDir()
		if err != nil {
			return err
		}
		for _, c := range []struct{ where, dir string }{
			{"repo", manifests.RepoManifestsDir(repo)},
			{"global", machine},
		} {
			p := filepath.Join(c.dir, name+".json")
			if _, err := os.Stat(p); err != nil {
				continue
			}
			if err := os.Remove(p); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "removed %s manifest pack: %s\n", c.where, p)
			return nil
		}
		return fmt.Errorf("no manifest pack %q in %s or %s", name, manifests.RepoManifestsDir(repo), machine)
	default:
		return fmt.Errorf("unknown manifests subcommand %q (add | list | remove)", sub)
	}
}

// manifestsAddFromURL installs manifest packs from the network: a direct URL
// to one pack .json, or a github.com/owner/repo URL whose tree carries either
// manifests/*.json files or top-level pack .json files (routes-add pattern).
func manifestsAddFromURL(rawurl, dest string, stdout io.Writer) error {
	u, err := url.Parse(rawurl)
	if err != nil {
		return fmt.Errorf("manifests add %s: %w", rawurl, err)
	}
	if strings.HasSuffix(u.Path, ".json") {
		data, err := httpGetBytes(rawurl)
		if err != nil {
			return err
		}
		pack, err := manifests.ParseManifestPack(data, rawurl)
		if err != nil {
			return err
		}
		return installManifestPack(dest, pack.Name, data, rawurl, stdout)
	}
	if u.Host != "github.com" {
		return fmt.Errorf("manifests add: expected a .json URL or https://github.com/owner/repo, got %s", rawurl)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return fmt.Errorf("github url needs owner/repo: %s", rawurl)
	}
	owner, repoName := parts[0], strings.TrimSuffix(parts[1], ".git")
	tarURL := fmt.Sprintf("https://codeload.github.com/%s/%s/tar.gz/HEAD", owner, repoName)
	fmt.Fprintf(stdout, "downloading %s/%s…\n", owner, repoName)
	data, err := httpGetBytes(tarURL)
	if err != nil {
		return fmt.Errorf("download manifest pack repo: %w", err)
	}
	work, err := os.MkdirTemp("", "ctx-manifests-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	if err := grammar.UntarGz(data, work); err != nil {
		return err
	}
	entries, err := os.ReadDir(work)
	if err != nil || len(entries) == 0 {
		return fmt.Errorf("empty repo tarball from %s", rawurl)
	}
	top := filepath.Join(work, entries[0].Name())
	installed, err := installManifestPacksFromTree(top, dest, rawurl, stdout)
	if err != nil {
		return err
	}
	if installed == 0 {
		return fmt.Errorf("no manifest pack found in %s (looked for manifests/*.json and top-level *.json pack files)", rawurl)
	}
	return nil
}

// installManifestPacksFromTree scans a fetched repo tree: every
// manifests/*.json must be a valid pack (loud failure — a repo that CLAIMS
// packs must deliver); with no manifests/ dir, top-level *.json files that
// validate as packs install (non-pack json like package.json is skipped).
func installManifestPacksFromTree(top, dest, origin string, stdout io.Writer) (int, error) {
	installed := 0
	packsDir := filepath.Join(top, "manifests")
	if entries, err := os.ReadDir(packsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(packsDir, e.Name()))
			if err != nil {
				return installed, err
			}
			pack, err := manifests.ParseManifestPack(data, origin+" → manifests/"+e.Name())
			if err != nil {
				return installed, err
			}
			if err := installManifestPack(dest, pack.Name, data, origin, stdout); err != nil {
				return installed, err
			}
			installed++
		}
		return installed, nil
	}
	entries, err := os.ReadDir(top)
	if err != nil {
		return 0, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(top, e.Name()))
		if err != nil {
			return installed, err
		}
		pack, err := manifests.ParseManifestPack(data, origin+" → "+e.Name())
		if err != nil {
			continue // not a manifest pack (package.json et al) — not claimed, not loud
		}
		if err := installManifestPack(dest, pack.Name, data, origin, stdout); err != nil {
			return installed, err
		}
		installed++
	}
	return installed, nil
}

func installManifestPack(dest, name string, data []byte, origin string, stdout io.Writer) error {
	if !routePackNameRe.MatchString(name) {
		return fmt.Errorf("manifest pack from %s: name %q: letters, digits, - and _ only", origin, name)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	p := filepath.Join(dest, name+".json")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "installed manifest pack %q from %s → %s\n", name, origin, p)
	return nil
}
