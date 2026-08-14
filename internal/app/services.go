package app

// cmdServices — `ctx-optimize services` (ADR 2026-08-13
// boundary-model-and-defaults, D5): the services registry, where the
// DEPENDENCY is the boundary. `services` / `services list` prints the
// effective merged registry (embedded → machine → repo, same ladder as
// boundaries.json); `services add <file|url>` validates one registry file and
// installs it into the machine dir — network touched ONLY on this explicit
// user command, the `grammar build` posture.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/boundaries"
)

func cmdServices(args []string, stdout io.Writer) error {
	f := parseFlags(args)
	sub := "list"
	rest := f.args
	if len(rest) > 0 {
		sub, rest = rest[0], rest[1:]
	}
	switch sub {
	case "list":
		return servicesList(f.bools["json"], stdout)
	case "add":
		if len(rest) != 1 {
			return fmt.Errorf("usage: services add <file|url>")
		}
		return servicesAdd(rest[0], stdout)
	default:
		return fmt.Errorf("usage: services [list] [--json] | services add <file|url>")
	}
}

func servicesList(asJSON bool, stdout io.Writer) error {
	svcs, err := boundaries.LoadServices(".")
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(svcs))
	for id := range svcs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if asJSON {
		ordered := make(map[string]boundaries.Service, len(svcs))
		for _, id := range ids {
			ordered[id] = svcs[id]
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", " ")
		return enc.Encode(ordered)
	}
	fmt.Fprintf(stdout, "%d services (embedded + machine %s + repo .ctxoptimize/services.json)\n\n",
		len(ids), boundaries.ServicesMachineDir())
	for _, id := range ids {
		s := svcs[id]
		hosts := strings.Join(s.Match.Hosts, " ")
		if hosts == "" {
			hosts = "-"
		}
		fmt.Fprintf(stdout, "  %-14s deps:%-2d hosts: %s", id, len(s.Match.Deps), hosts)
		if s.ConfigHint != "" {
			fmt.Fprintf(stdout, "  env:%s*", s.ConfigHint)
		}
		if len(s.Endpoints) > 0 {
			fmt.Fprintf(stdout, "  endpoints:%d", len(s.Endpoints))
		}
		fmt.Fprintln(stdout)
	}
	return nil
}

func servicesAdd(src string, stdout io.Writer) error {
	var data []byte
	var base string
	if strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://") {
		resp, err := http.Get(src) // user-invoked network only (grammar-build posture)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("fetch %s: %s", src, resp.Status)
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return err
		}
		base = path.Base(src)
	} else {
		var err error
		data, err = os.ReadFile(src)
		if err != nil {
			return err
		}
		base = filepath.Base(src)
	}
	f, err := boundaries.ValidateServiceFile(data)
	if err != nil {
		return fmt.Errorf("invalid services file: %w", err)
	}
	if !strings.HasSuffix(base, ".json") {
		base += ".json"
	}
	dir := boundaries.ServicesMachineDir()
	if dir == "" {
		return fmt.Errorf("cannot resolve services dir (no home?)")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	dst := filepath.Join(dir, base)
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return err
	}
	ids := make([]string, 0, len(f.Services))
	for id := range f.Services {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	fmt.Fprintf(stdout, "installed %s (%d services: %s)\n", dst, len(ids), strings.Join(ids, ", "))
	fmt.Fprintln(stdout, "re-gather with `ctx-optimize add .` to apply")
	return nil
}
