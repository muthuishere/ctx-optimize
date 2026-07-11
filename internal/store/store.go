// Package store is the central, file-based artifact store — the "gathered
// world". One folder per module under the store root, everything plain,
// git-diffable text (ndjson/json), so the whole store can push/pull anywhere
// and a remote is never a query target: gather once, refresh cheaply, answer
// from the store.
//
// Layout per module key:
//
//	<root>/<key>/
//	  graph/nodes.ndjson   one schema.Node per line, sorted by id
//	  graph/edges.ndjson   one schema.Edge per line, sorted
//	  wiki/  cards/        (later stories)
//	  hooks/               dynamic adapters — travel with the store
//	  manifest.json        content hashes of every artifact (drives sync)
//	  config.json          store-local config (remote URL, ...)
package store

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// Root resolves the store root: flag > $CTX_OPTIMIZE_STORE > ~/ctxoptimize.
func Root(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if env := os.Getenv("CTX_OPTIMIZE_STORE"); env != "" {
		return env, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	return filepath.Join(home, "ctxoptimize"), nil
}

// ModuleKey derives the store key from a module path: the repo's basename, so
// the layout reads ~/ctxoptimize/<repo-name>/. Custom module names come from
// ctx-optimize.json's "name" field (resolved by the caller), which also
// disambiguates two repos sharing a basename.
func ModuleKey(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", path, err)
	}
	key := sanitizeKey(filepath.Base(abs))
	if key == "" {
		return "", fmt.Errorf("empty module key for %s", path)
	}
	return key, nil
}

// SanitizeKey keeps a module name filesystem- and URL-safe — callers pass
// user-chosen names (ctx-optimize.json "name") through it.
func SanitizeKey(name string) string { return sanitizeKey(name) }

// sanitizeKey keeps a name filesystem- and URL-safe.
func sanitizeKey(name string) string {
	var sb strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			sb.WriteRune(r)
		default:
			sb.WriteByte('-')
		}
	}
	return strings.Trim(sb.String(), "-.")
}

// Store is one module's folder. Open creates the layout if absent (init is
// just Open — pointing at a folder IS the database, no ceremony).
type Store struct {
	Dir string
}

func Open(root, key string) (*Store, error) {
	dir := filepath.Join(root, key)
	for _, sub := range []string{"graph", "wiki", "cards", "hooks"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return nil, fmt.Errorf("create store layout: %w", err)
		}
	}
	return &Store{Dir: dir}, nil
}

// ---- graph persistence ----

func (s *Store) nodesPath() string { return filepath.Join(s.Dir, "graph", "nodes.ndjson") }
func (s *Store) edgesPath() string { return filepath.Join(s.Dir, "graph", "edges.ndjson") }

// Nodes loads all nodes (empty store → empty slice, not an error).
func (s *Store) Nodes() ([]schema.Node, error) {
	var out []schema.Node
	err := readNDJSON(s.nodesPath(), func(line []byte) error {
		var n schema.Node
		if err := json.Unmarshal(line, &n); err != nil {
			return err
		}
		out = append(out, n)
		return nil
	})
	return out, err
}

// Edges loads all edges.
func (s *Store) Edges() ([]schema.Edge, error) {
	var out []schema.Edge
	err := readNDJSON(s.edgesPath(), func(line []byte) error {
		var e schema.Edge
		if err := json.Unmarshal(line, &e); err != nil {
			return err
		}
		out = append(out, e)
		return nil
	})
	return out, err
}

// Merge upserts a validated batch into the graph: nodes replace by id (a
// producer re-emitting a node updates it), edges dedupe by
// (source,target,relation). Files are rewritten sorted so diffs stay stable —
// git-clean is a feature, not an accident.
func (s *Store) Merge(b *schema.Batch) (nodesAdded, edgesAdded int, err error) {
	if err := b.Validate(); err != nil {
		return 0, 0, fmt.Errorf("reject batch: %w", err)
	}
	existing, err := s.Nodes()
	if err != nil {
		return 0, 0, err
	}
	byID := make(map[string]schema.Node, len(existing)+len(b.Nodes))
	for _, n := range existing {
		byID[n.ID] = n
	}
	for _, n := range b.Nodes {
		if n.Metadata == nil {
			n.Metadata = map[string]string{}
		}
		n.Metadata["producer"] = b.Producer
		if _, ok := byID[n.ID]; !ok {
			nodesAdded++
		}
		byID[n.ID] = n
	}
	nodes := make([]schema.Node, 0, len(byID))
	for _, n := range byID {
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })

	oldEdges, err := s.Edges()
	if err != nil {
		return 0, 0, err
	}
	edgeKey := func(e schema.Edge) string { return e.Source + "\x00" + e.Target + "\x00" + e.Relation }
	byKey := make(map[string]schema.Edge, len(oldEdges)+len(b.Edges))
	for _, e := range oldEdges {
		byKey[edgeKey(e)] = e
	}
	for _, e := range b.Edges {
		if e.Metadata == nil {
			e.Metadata = map[string]string{}
		}
		e.Metadata["producer"] = b.Producer
		if _, ok := byKey[edgeKey(e)]; !ok {
			edgesAdded++
		}
		byKey[edgeKey(e)] = e
	}
	edges := make([]schema.Edge, 0, len(byKey))
	for _, e := range byKey {
		edges = append(edges, e)
	}
	sort.Slice(edges, func(i, j int) bool { return edgeKey(edges[i]) < edgeKey(edges[j]) })

	if err := writeNDJSON(s.nodesPath(), len(nodes), func(i int) any { return nodes[i] }); err != nil {
		return 0, 0, err
	}
	if err := writeNDJSON(s.edgesPath(), len(edges), func(i int) any { return edges[i] }); err != nil {
		return 0, 0, err
	}
	return nodesAdded, edgesAdded, nil
}

// ---- manifest ----

// Entry is one artifact's fingerprint. Sync and refresh both key on Hash —
// mtime/size are only a fast-path gate, content hash is the truth.
type Entry struct {
	Hash string `json:"hash"`
	Size int64  `json:"size"`
}

type Manifest struct {
	Files map[string]Entry `json:"files"` // store-relative path → entry
}

func (s *Store) manifestPath() string { return filepath.Join(s.Dir, "manifest.json") }

// UpdateManifest re-fingerprints every artifact file and writes manifest.json.
// The manifest itself and config are excluded (config may hold a remote URL
// that differs per machine; the manifest can't contain its own hash).
func (s *Store) UpdateManifest() (*Manifest, error) {
	m := &Manifest{Files: map[string]Entry{}}
	err := filepath.WalkDir(s.Dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(s.Dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "manifest.json" || rel == "config.json" {
			return nil
		}
		h, size, err := hashFile(path)
		if err != nil {
			return err
		}
		m.Files[rel] = Entry{Hash: h, Size: size}
		return nil
	})
	if err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return m, os.WriteFile(s.manifestPath(), append(data, '\n'), 0o644)
}

// Manifest loads the current manifest (absent → empty).
func (s *Store) Manifest() (*Manifest, error) {
	data, err := os.ReadFile(s.manifestPath())
	if os.IsNotExist(err) {
		return &Manifest{Files: map[string]Entry{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if m.Files == nil {
		m.Files = map[string]Entry{}
	}
	return &m, nil
}

// ---- store-local config ----

type Config struct {
	Remote string `json:"remote,omitempty"` // e.g. s3://bucket/prefix or file:///path
}

func (s *Store) configPath() string { return filepath.Join(s.Dir, "config.json") }

func (s *Store) Config() (*Config, error) {
	data, err := os.ReadFile(s.configPath())
	if os.IsNotExist(err) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse store config: %w", err)
	}
	return &c, nil
}

func (s *Store) SaveConfig(c *Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.configPath(), append(data, '\n'), 0o644)
}

// ---- helpers ----

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func readNDJSON(path string, each func([]byte) error) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<24) // long lines: big doc-comment metadata
	for sc.Scan() {
		line := sc.Bytes()
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		if err := each(line); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	return sc.Err()
}

func writeNDJSON(path string, n int, item func(int) any) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for i := 0; i < n; i++ {
		if err := enc.Encode(item(i)); err != nil {
			f.Close()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path) // atomic swap: readers never see a half-written graph
}
