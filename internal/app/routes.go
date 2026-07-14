// routes.go — the `routes` verb family, mirroring `languages`: core
// recognizers are embedded (big frameworks + yaml shapes), everything
// call-shaped is a drop-in ROUTE PACK (see internal/extract/code/routepacks.go).
// list: core + discovered packs with their source. add: scaffold a pack by
// name, or fetch one from a GitHub repo / direct .json URL (user-invoked
// fetch — the one sanctioned network path besides remotes). remove: delete a
// pack file, repo first then global.
package app

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/extract/code"
	"github.com/muthuishere/ctx-optimize/internal/grammar"
)

// coreRouteRecognizers documents the embedded channels for `routes list`.
var coreRouteRecognizers = []string{
	"fastapi-route", "flask-route", "express-route", "nestjs-route",
	"angular-route", "react-router-route", "vue-router-route",
	"openapi-route", "drupal-route", "ingress-route",
}

var routePackNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

const routesUsage = "usage: ctx-optimize routes <list | add <name|github-url|json-url> [--global] | remove <name>>"

func cmdRoutes(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", routesUsage)
	}
	sub, rest := args[0], args[1:]
	f := parseFlags(rest)
	repo, err := resolvePath(f)
	if err != nil {
		return err
	}
	switch sub {
	case "list":
		packs, err := code.LoadRoutePacks(repo)
		if err != nil {
			return err
		}
		if f.bools["json"] {
			ps := []map[string]string{}
			for _, p := range packs {
				ps = append(ps, map[string]string{"name": p.Name, "file": p.File})
			}
			return emit(stdout, map[string]any{"core": coreRouteRecognizers, "packs": ps})
		}
		fmt.Fprintf(stdout, "core:  %s\n", strings.Join(coreRouteRecognizers, ", "))
		for _, p := range packs {
			fmt.Fprintf(stdout, "pack:  %s (%s)\n", p.Name, p.File)
		}
		if len(packs) == 0 {
			fmt.Fprintln(stdout, "packs: (none)")
		}
		fmt.Fprintln(stdout, "scaffold one: `ctx-optimize routes add <name>` · install one: `ctx-optimize routes add <github-url>`")
		return nil
	case "add":
		if len(f.args) != 1 {
			return fmt.Errorf("usage: ctx-optimize routes add <name | github-url | url-of-pack.json> [--global]")
		}
		dest := code.RepoRoutesDir(repo)
		where := "repo"
		if f.bools["global"] {
			if dest, err = code.MachineRoutesDir(); err != nil {
				return err
			}
			where = "global"
		}
		arg := f.args[0]
		if strings.HasPrefix(arg, "http://") || strings.HasPrefix(arg, "https://") {
			return routesAddFromURL(arg, dest, stdout)
		}
		if !routePackNameRe.MatchString(arg) {
			return fmt.Errorf("route pack name %q: letters, digits, - and _ only (or pass a URL)", arg)
		}
		p := filepath.Join(dest, arg+".json")
		if _, err := os.Stat(p); err == nil {
			return fmt.Errorf("route pack already exists: %s — edit it, or `routes remove %s` first", p, arg)
		}
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return err
		}
		tmpl := fmt.Sprintf(`{
  "name": %q,
  "_review": true,
  "rules": [
    {"call": "registerRoute", "path_arg": 0, "handler_arg": 1, "method": "GET"}
  ]
}
`, arg)
		if err := os.WriteFile(p, []byte(tmpl), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "scaffolded %s route pack: %s — REVIEW IT (the example rule is live), then `ctx-optimize add` applies it\n", where, p)
		return nil
	case "remove":
		if len(f.args) != 1 {
			return fmt.Errorf("usage: ctx-optimize routes remove <name>")
		}
		name := f.args[0]
		machine, err := code.MachineRoutesDir()
		if err != nil {
			return err
		}
		for _, c := range []struct{ where, dir string }{
			{"repo", code.RepoRoutesDir(repo)},
			{"global", machine},
		} {
			p := filepath.Join(c.dir, name+".json")
			if _, err := os.Stat(p); err != nil {
				continue
			}
			if err := os.Remove(p); err != nil {
				return err
			}
			fmt.Fprintf(stdout, "removed %s route pack: %s\n", c.where, p)
			return nil
		}
		return fmt.Errorf("no route pack %q in %s or %s", name, code.RepoRoutesDir(repo), machine)
	default:
		return fmt.Errorf("unknown routes subcommand %q (add | list | remove)", sub)
	}
}

// routesAddFromURL installs route packs from the network: a direct URL to one
// pack .json, or a github.com/owner/repo URL whose tree carries either
// routes/*.json files or a single top-level route-pack .json.
func routesAddFromURL(rawurl, dest string, stdout io.Writer) error {
	u, err := url.Parse(rawurl)
	if err != nil {
		return fmt.Errorf("routes add %s: %w", rawurl, err)
	}
	if strings.HasSuffix(u.Path, ".json") {
		data, err := httpGetBytes(rawurl)
		if err != nil {
			return err
		}
		pack, err := code.ParseRoutePack(data, rawurl)
		if err != nil {
			return err
		}
		return installRoutePack(dest, pack.Name, data, rawurl, stdout)
	}
	if u.Host != "github.com" {
		return fmt.Errorf("routes add: expected a .json URL or https://github.com/owner/repo, got %s", rawurl)
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
		return fmt.Errorf("download route pack repo: %w", err)
	}
	work, err := os.MkdirTemp("", "ctx-routes-*")
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
	installed, err := installRoutePacksFromTree(top, dest, rawurl, stdout)
	if err != nil {
		return err
	}
	if installed == 0 {
		return fmt.Errorf("no route pack found in %s (looked for routes/*.json and top-level *.json pack files)", rawurl)
	}
	return nil
}

// installRoutePacksFromTree scans a fetched repo tree: every routes/*.json
// must be a valid pack (loud failure — a repo that CLAIMS packs must deliver);
// with no routes/ dir, top-level *.json files that validate as packs install
// (non-pack json like package.json is skipped).
func installRoutePacksFromTree(top, dest, origin string, stdout io.Writer) (int, error) {
	installed := 0
	routesDir := filepath.Join(top, "routes")
	if entries, err := os.ReadDir(routesDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(routesDir, e.Name()))
			if err != nil {
				return installed, err
			}
			pack, err := code.ParseRoutePack(data, origin+" → routes/"+e.Name())
			if err != nil {
				return installed, err
			}
			if err := installRoutePack(dest, pack.Name, data, origin, stdout); err != nil {
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
		pack, err := code.ParseRoutePack(data, origin+" → "+e.Name())
		if err != nil {
			continue // not a route pack (package.json et al) — not claimed, not loud
		}
		if err := installRoutePack(dest, pack.Name, data, origin, stdout); err != nil {
			return installed, err
		}
		installed++
	}
	return installed, nil
}

func installRoutePack(dest, name string, data []byte, origin string, stdout io.Writer) error {
	if !routePackNameRe.MatchString(name) {
		return fmt.Errorf("route pack from %s: name %q: letters, digits, - and _ only", origin, name)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	p := filepath.Join(dest, name+".json")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "installed route pack %q from %s → %s\n", name, origin, p)
	return nil
}

func httpGetBytes(rawurl string) ([]byte, error) {
	resp, err := http.Get(rawurl)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GET %s: %s", rawurl, resp.Status)
	}
	return io.ReadAll(resp.Body)
}
