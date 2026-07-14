// Package grammar is `ctx-optimize grammar build` — turn ANY tree-sitter
// grammar into a drop-in pack (<name>.wasm + <name>.json) with no shell
// script and nothing preinstalled: grammar source comes from a local dir or a
// GitHub tarball (no git), the compiler is zig (PATH or auto-downloaded,
// see zig.go), the tree-sitter runtime is fetched once and cached, and the
// JSON mapping skeleton is auto-suggested from the grammar's own
// node-types.json. The shim is embedded — the same one the bundled build
// uses, so packs and the embed share one ABI.
package grammar

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

//go:embed assets/shim.c
var shimC []byte

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)

// runtimeTarball pins the tree-sitter runtime the pack is compiled against.
const runtimeTarball = "https://codeload.github.com/tree-sitter/tree-sitter/tar.gz/refs/tags/v0.26.0"

type Options struct {
	Source string   // known name, local grammar dir, or https://github.com/<owner>/<repo>
	Name   string   // pack name; default: grammar.json "name"
	OutDir string   // default ~/ctxoptimize/grammars
	Ref    string   // git ref for GitHub tarballs (default HEAD)
	Exts   []string // seed extensions for the suggested mapping
}

// Build produces <out>/<name>.wasm and, if absent, a suggested <name>.json.
func Build(opts Options, stdout io.Writer) (wasmPath, cfgPath string, err error) {
	// A bare known name resolves through the registry.
	if k, ok := KnownGrammars[opts.Source]; ok {
		if opts.Ref == "" {
			opts.Ref = k.Ref
		}
		if len(opts.Exts) == 0 {
			opts.Exts = k.Exts
		}
		opts.Source = k.URL
	}
	zig, err := EnsureZig(stdout)
	if err != nil {
		return "", "", err
	}

	work, err := os.MkdirTemp("", "ctx-grammar-*")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(work)

	srcDir, err := resolveGrammarDir(opts.Source, opts.Ref, work, stdout)
	if err != nil {
		return "", "", err
	}
	name := opts.Name
	if name == "" {
		if name, err = grammarName(srcDir); err != nil {
			return "", "", err
		}
	}
	// The name reaches file paths AND generated C — it must be an identifier.
	if !nameRe.MatchString(name) {
		return "", "", fmt.Errorf("grammar name %q is not a plain identifier ([A-Za-z0-9_]) — pass --name", name)
	}

	runtimeDir, err := ensureRuntime(stdout)
	if err != nil {
		return "", "", err
	}

	outDir := opts.OutDir
	if outDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", err
		}
		outDir = filepath.Join(home, "ctxoptimize", "grammars")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", "", err
	}

	// Compilation inputs: embedded shim + generated one-grammar table +
	// runtime + parser (+scanner). All exec'd directly — no shell anywhere.
	shimPath := filepath.Join(work, "shim.c")
	if err := os.WriteFile(shimPath, shimC, 0o644); err != nil {
		return "", "", err
	}
	langtab := fmt.Sprintf(`#include <tree_sitter/api.h>
extern const TSLanguage *tree_sitter_%s(void);
const TSLanguage *co_lang_by_id(int id) { return id == 0 ? tree_sitter_%s() : 0; }
`, name, name)
	langtabPath := filepath.Join(work, "langtab.c")
	if err := os.WriteFile(langtabPath, []byte(langtab), 0o644); err != nil {
		return "", "", err
	}

	wasmPath = filepath.Join(outDir, name+".wasm")
	args := []string{
		"cc", "-target", "wasm32-wasi", "-mexec-model=reactor", "-O2",
		"-Wl,--export-dynamic", "-Wl,--initial-memory=67108864", "-Wl,--max-memory=1073741824",
		"-I", filepath.Join(runtimeDir, "lib", "include"),
		"-I", filepath.Join(runtimeDir, "lib", "src"),
		"-I", filepath.Join(srcDir, "src"),
		shimPath, langtabPath,
		filepath.Join(runtimeDir, "lib", "src", "lib.c"),
		filepath.Join(srcDir, "src", "parser.c"),
	}
	if _, err := os.Stat(filepath.Join(srcDir, "src", "scanner.c")); err == nil {
		args = append(args, filepath.Join(srcDir, "src", "scanner.c"))
	}
	args = append(args, "-o", wasmPath)

	fmt.Fprintf(stdout, "compiling %s (zig cc → wasm32-wasi)…\n", name)
	cmd := exec.Command(zig, args...)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("zig cc failed: %w\n%s", err, tail(errb.String(), 2000))
	}

	// Config: never overwrite a hand-tuned mapping; suggest from
	// node-types.json when absent.
	cfgPath = filepath.Join(outDir, name+".json")
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		cfg, serr := Suggest(name, srcDir, opts.Exts)
		if serr != nil {
			fmt.Fprintf(stdout, "note: could not suggest a mapping (%v) — write %s yourself\n", serr, cfgPath)
		} else if err := os.WriteFile(cfgPath, cfg, 0o644); err != nil {
			return "", "", err
		} else {
			fmt.Fprintf(stdout, "suggested mapping written: %s — REVIEW IT (decls/names/calls/imports guessed from node-types.json)\n", cfgPath)
		}
	} else {
		fmt.Fprintf(stdout, "kept existing mapping: %s\n", cfgPath)
	}
	return wasmPath, cfgPath, nil
}

// resolveGrammarDir accepts a local dir or a GitHub URL (tarball download, no
// git). Returns the directory containing src/parser.c.
func resolveGrammarDir(source, ref, work string, stdout io.Writer) (string, error) {
	if fi, err := os.Stat(source); err == nil && fi.IsDir() {
		return findParserDir(source)
	}
	if !strings.HasPrefix(source, "https://github.com/") {
		return "", fmt.Errorf("source must be a local grammar dir or a github.com URL, got %q", source)
	}
	parts := strings.Split(strings.TrimPrefix(source, "https://github.com/"), "/")
	if len(parts) < 2 {
		return "", fmt.Errorf("github url needs owner/repo: %s", source)
	}
	owner, repo := parts[0], strings.TrimSuffix(parts[1], ".git")
	if ref == "" {
		ref = "HEAD"
	} else {
		ref = "refs/heads/" + ref
	}
	url := fmt.Sprintf("https://codeload.github.com/%s/%s/tar.gz/%s", owner, repo, ref)
	fmt.Fprintf(stdout, "downloading %s/%s…\n", owner, repo)
	data, err := httpGet(url)
	if err != nil {
		return "", fmt.Errorf("download grammar: %w", err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	dst := filepath.Join(work, "grammar")
	if err := untarTo(tar.NewReader(gz), dst); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(dst)
	if err != nil || len(entries) == 0 {
		return "", fmt.Errorf("empty grammar tarball")
	}
	return findParserDir(filepath.Join(dst, entries[0].Name()))
}

// UntarGz extracts a gzipped tarball into dir, sanitizing paths — shared by
// grammar builds and route-pack installs (`routes add <github-url>`).
func UntarGz(data []byte, dir string) error {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}
	return untarTo(tar.NewReader(gz), dir)
}

// findParserDir locates src/parser.c at the root or one level down
// (tree-sitter-typescript style multi-grammar repos).
func findParserDir(root string) (string, error) {
	if _, err := os.Stat(filepath.Join(root, "src", "parser.c")); err == nil {
		return root, nil
	}
	entries, _ := os.ReadDir(root)
	var subs []string
	for _, e := range entries {
		if e.IsDir() {
			if _, err := os.Stat(filepath.Join(root, e.Name(), "src", "parser.c")); err == nil {
				subs = append(subs, e.Name())
			}
		}
	}
	switch len(subs) {
	case 1:
		return filepath.Join(root, subs[0]), nil
	case 0:
		return "", fmt.Errorf("no src/parser.c under %s — is the grammar generated? (repos without committed parsers need `tree-sitter generate` first)", root)
	default:
		return "", fmt.Errorf("multiple grammars in %s (%s) — point at the subdirectory you want", root, strings.Join(subs, ", "))
	}
}

func grammarName(srcDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(srcDir, "src", "grammar.json"))
	if err != nil {
		return "", fmt.Errorf("read grammar.json for the name (or pass --name): %w", err)
	}
	var g struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &g); err != nil || g.Name == "" {
		return "", fmt.Errorf("grammar.json has no name — pass --name")
	}
	return g.Name, nil
}

// ensureRuntime downloads and caches the tree-sitter runtime source once.
func ensureRuntime(stdout io.Writer) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "ctxoptimize", "toolchain", "tree-sitter-runtime")
	if _, err := os.Stat(filepath.Join(dir, "lib", "src", "lib.c")); err == nil {
		return dir, nil
	}
	fmt.Fprintf(stdout, "downloading tree-sitter runtime (once)…\n")
	data, err := httpGet(runtimeTarball)
	if err != nil {
		return "", fmt.Errorf("download runtime: %w", err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	tmp := dir + ".tmp"
	os.RemoveAll(tmp)
	if err := untarTo(tar.NewReader(gz), tmp); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(tmp)
	if err != nil || len(entries) == 0 {
		return "", fmt.Errorf("empty runtime tarball")
	}
	os.RemoveAll(dir)
	if err := os.Rename(filepath.Join(tmp, entries[0].Name()), dir); err != nil {
		return "", err
	}
	os.RemoveAll(tmp)
	return dir, nil
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
