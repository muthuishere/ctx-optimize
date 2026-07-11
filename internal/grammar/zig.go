// zig.go acquires the compiler for `grammar build`: zig already on PATH wins;
// otherwise the official prebuilt toolchain is downloaded ONCE into
// ~/ctxoptimize/toolchain/, sha256-verified against ziglang.org's signed
// index, and reused forever. Network happens only inside this explicitly
// invoked build command — never at query/add time.
package grammar

import (
	"archive/tar"
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ulikunitz/xz"
)

const zigVersion = "0.16.0"

// EnsureZig returns a runnable zig binary path.
func EnsureZig(stdout io.Writer) (string, error) {
	if p, err := exec.LookPath("zig"); err == nil {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "ctxoptimize", "toolchain")
	target := zigTarget()
	root := filepath.Join(dir, fmt.Sprintf("zig-%s-%s", target, zigVersion))
	bin := filepath.Join(root, zigBinName())
	if _, err := os.Stat(bin); err == nil {
		return bin, nil
	}

	fmt.Fprintf(stdout, "zig not found — downloading official toolchain %s (%s) once into %s\n", zigVersion, target, dir)
	idx, err := httpGet("https://ziglang.org/download/index.json")
	if err != nil {
		return "", fmt.Errorf("fetch zig index: %w", err)
	}
	var index map[string]map[string]json.RawMessage
	if err := json.Unmarshal(idx, &index); err != nil {
		return "", fmt.Errorf("parse zig index: %w", err)
	}
	entry, ok := index[zigVersion][target]
	if !ok {
		return "", fmt.Errorf("no zig %s build for %s — install zig yourself and re-run", zigVersion, target)
	}
	var meta struct {
		Tarball string `json:"tarball"`
		Shasum  string `json:"shasum"`
	}
	if err := json.Unmarshal(entry, &meta); err != nil {
		return "", err
	}

	data, err := httpGet(meta.Tarball)
	if err != nil {
		return "", fmt.Errorf("download zig: %w", err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != meta.Shasum {
		return "", fmt.Errorf("zig tarball sha256 mismatch — refusing to install")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if strings.HasSuffix(meta.Tarball, ".zip") {
		err = unzipTo(data, dir)
	} else {
		err = untarXZTo(data, dir)
	}
	if err != nil {
		return "", fmt.Errorf("extract zig: %w", err)
	}
	if _, err := os.Stat(bin); err != nil {
		return "", fmt.Errorf("zig extracted but %s missing", bin)
	}
	return bin, nil
}

func zigTarget() string {
	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		arch = "x86_64"
	case "arm64":
		arch = "aarch64"
	}
	os := runtime.GOOS
	if os == "darwin" {
		os = "macos"
	}
	return arch + "-" + os
}

func zigBinName() string {
	if runtime.GOOS == "windows" {
		return "zig.exe"
	}
	return "zig"
}

func httpGet(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// untarXZTo extracts a .tar.xz into dir, sanitizing paths.
func untarXZTo(data []byte, dir string) error {
	xr, err := xz.NewReader(strings.NewReader(string(data)))
	if err != nil {
		return err
	}
	return untarTo(tar.NewReader(xr), dir)
}

func untarTo(tr *tar.Reader, dir string) error {
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		path, err := safeJoin(dir, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		}
	}
}

func unzipTo(data []byte, dir string) error {
	zr, err := zip.NewReader(strings.NewReader(string(data)), int64(len(data)))
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		path, err := safeJoin(dir, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(path, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			rc.Close()
			return err
		}
		if _, err := io.Copy(out, rc); err != nil {
			rc.Close()
			out.Close()
			return err
		}
		rc.Close()
		out.Close()
	}
	return nil
}

// safeJoin rejects path traversal out of dir (zip-slip guard).
func safeJoin(dir, name string) (string, error) {
	path := filepath.Join(dir, filepath.FromSlash(name))
	if !strings.HasPrefix(path, filepath.Clean(dir)+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive entry escapes target dir: %s", name)
	}
	return path, nil
}
