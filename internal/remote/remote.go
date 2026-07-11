// Package remote syncs a store folder to a remote — push/pull, incremental,
// manifest-driven. The remote is NEVER a query target: queries always run on
// the local folder; remotes exist only for sync and reuse (share a store by
// handing over a URL). Two backends, both dependency-free:
//
//	file:///path        any mounted/rsync-able directory (also the test double)
//	s3://bucket/prefix  any S3-compatible endpoint via stdlib SigV4 (AWS, R2,
//	                    Hetzner, MinIO). Credentials come from the standard
//	                    env vars at call time (AWS_ACCESS_KEY_ID,
//	                    AWS_SECRET_ACCESS_KEY, AWS_REGION, AWS_ENDPOINT_URL) —
//	                    never stored, never logged.
//
// Sync protocol: the remote holds the same layout plus manifest.json. Push
// compares local manifest hashes against the remote manifest and uploads only
// changed files, manifest last (so a reader never sees a manifest pointing at
// missing objects). Pull is the mirror image. Pulled hooks/ are written but
// NOT approved — executing them is refresh's job and gated separately.
package remote

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/store"
)

// Backend is the minimal object interface both remotes implement.
type Backend interface {
	Put(key string, data []byte) error
	Get(key string) ([]byte, error) // must return ErrNotFound for absent keys
}

// ErrNotFound signals an absent remote object (e.g. no manifest yet).
var ErrNotFound = fmt.Errorf("remote: object not found")

// Options carries explicit credentials/endpoint from config (already
// ${VAR}-resolved). Empty fields fall back to the standard AWS_* env vars at
// call time. Values live in memory only — never stored, printed, or logged.
type Options struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	Region          string
	Endpoint        string
}

// Open parses a remote URL into a backend using env-var credentials only.
func Open(rawURL string) (Backend, error) { return OpenWith(rawURL, Options{}) }

// OpenWith parses a remote URL into a backend with explicit options.
func OpenWith(rawURL string, opts Options) (Backend, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse remote url: %w", err)
	}
	switch u.Scheme {
	case "file":
		if u.Path == "" {
			return nil, fmt.Errorf("file remote needs a path: file:///dir")
		}
		return &fileBackend{root: u.Path}, nil
	case "s3":
		if u.Host == "" {
			return nil, fmt.Errorf("s3 remote needs a bucket: s3://bucket/prefix")
		}
		return newS3Backend(u.Host, strings.Trim(u.Path, "/"), opts)
	default:
		return nil, fmt.Errorf("unsupported remote scheme %q (file:// or s3://)", u.Scheme)
	}
}

// ---- file backend ----

type fileBackend struct{ root string }

func (f *fileBackend) Put(key string, data []byte) error {
	path := filepath.Join(f.root, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (f *fileBackend) Get(key string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(f.root, filepath.FromSlash(key)))
	if os.IsNotExist(err) {
		return nil, ErrNotFound
	}
	return data, err
}

// ---- sync ----

// Result reports what a push/pull actually moved.
type Result struct {
	Transferred []string `json:"transferred"`
	Skipped     int      `json:"skipped"`
}

// Push uploads changed artifacts. It refreshes the local manifest first so
// what's pushed is exactly what's on disk.
func Push(s *store.Store, b Backend) (*Result, error) {
	local, err := s.UpdateManifest()
	if err != nil {
		return nil, err
	}
	remote, err := remoteManifest(b)
	if err != nil {
		return nil, err
	}
	res := &Result{}
	for rel, entry := range local.Files {
		if remote.Files[rel].Hash == entry.Hash {
			res.Skipped++
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.Dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		if err := b.Put(rel, data); err != nil {
			return nil, fmt.Errorf("push %s: %w", rel, err)
		}
		res.Transferred = append(res.Transferred, rel)
	}
	// Manifest last: a pull that races a push sees old-manifest/old-files or
	// new-manifest/new-files, never a manifest pointing at missing objects.
	data, err := os.ReadFile(filepath.Join(s.Dir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	if err := b.Put("manifest.json", data); err != nil {
		return nil, err
	}
	return res, nil
}

// Pull downloads changed artifacts per the remote manifest. Hooks arrive like
// any file but are inert: nothing in this binary executes them, and refresh
// (which will) gates on explicit approval — pulling a store must never mean
// silently running someone's code.
func Pull(s *store.Store, b Backend) (*Result, error) {
	remote, err := remoteManifest(b)
	if err != nil {
		return nil, err
	}
	local, err := s.UpdateManifest()
	if err != nil {
		return nil, err
	}
	res := &Result{}
	for rel, entry := range remote.Files {
		if local.Files[rel].Hash == entry.Hash {
			res.Skipped++
			continue
		}
		data, err := b.Get(rel)
		if err != nil {
			return nil, fmt.Errorf("pull %s: %w", rel, err)
		}
		path := filepath.Join(s.Dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return nil, err
		}
		res.Transferred = append(res.Transferred, rel)
	}
	if _, err := s.UpdateManifest(); err != nil {
		return nil, err
	}
	return res, nil
}

func remoteManifest(b Backend) (*store.Manifest, error) {
	data, err := b.Get("manifest.json")
	if err == ErrNotFound {
		return &store.Manifest{Files: map[string]store.Entry{}}, nil
	}
	if err != nil {
		return nil, err
	}
	m := &store.Manifest{}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("parse remote manifest: %w", err)
	}
	if m.Files == nil {
		m.Files = map[string]store.Entry{}
	}
	return m, nil
}
