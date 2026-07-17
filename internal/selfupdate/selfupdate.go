// Package selfupdate updates the ctx-optimize binary itself. Like the zig
// toolchain download in internal/grammar, this is sanctioned network ONLY
// because the user explicitly invoked it (`ctx-optimize update`) — the
// binary never checks for updates in the background, ever.
//
// Channels:
//   - npm     — the binary lives under node_modules (the wrapper's platform
//     package): delegate to `npm install -g` so the wrapper and its
//     optionalDependencies stay in sync; never touch the file ourselves.
//   - binary  — a goreleaser release binary anywhere else: download the
//     platform asset from GitHub Releases, verify sha256 against
//     checksums.txt, swap atomically.
//   - dev / anything unrecognized — leave it alone (the app layer skips the
//     binary lane for 0.0.0-dev builds).
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const repo = "muthuishere/ctx-optimize"

var client = &http.Client{Timeout: 60 * time.Second}

// Channel classifies how the running binary was installed: "npm" when it
// lives under node_modules (the wrapper's platform package), else "binary".
func Channel(exe string) string {
	if strings.Contains(filepath.ToSlash(exe), "/node_modules/") {
		return "npm"
	}
	return "binary"
}

// Latest asks the GitHub releases API for the newest tag (e.g. "v0.3.9").
// apiBase is overridable (CTX_OPTIMIZE_UPDATE_API) for tests and mirrors.
func Latest(apiBase string) (string, error) {
	req, err := http.NewRequest("GET", apiBase+"/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GET releases/latest: %s", resp.Status)
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	if rel.TagName == "" {
		return "", fmt.Errorf("releases/latest: empty tag_name")
	}
	return rel.TagName, nil
}

// Newer reports whether latest (a "vX.Y.Z" tag or bare version) is a higher
// semver than current. Non-numeric parts compare as 0 — a malformed tag
// never triggers a swap.
func Newer(current, latest string) bool {
	c, l := parse(current), parse(latest)
	for i := 0; i < 3; i++ {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

func parse(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 { // strip prerelease/build
		v = v[:i]
	}
	var out [3]int
	for i, p := range strings.SplitN(v, ".", 3) {
		if i > 2 {
			break
		}
		n, _ := strconv.Atoi(p)
		out[i] = n
	}
	return out
}

// AssetName is the goreleaser archive for this platform:
// ctx-optimize_<version>_<os>_<arch>.tar.gz (.zip on windows).
func AssetName(tag string) string {
	ver := strings.TrimPrefix(tag, "v")
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("ctx-optimize_%s_%s_%s.%s", ver, runtime.GOOS, runtime.GOARCH, ext)
}

// Apply downloads the tag's platform asset from dlBase, verifies its sha256
// against the release's checksums.txt, extracts the binary, and swaps it
// over target (rename dance: target → .old, new → target, .old removed).
// Any failure leaves target untouched. dlBase is overridable
// (CTX_OPTIMIZE_UPDATE_DL) for tests and mirrors.
func Apply(dlBase, tag, target string, out io.Writer) error {
	asset := AssetName(tag)
	base := dlBase + "/" + repo + "/releases/download/" + tag + "/"

	sums, err := httpGet(base + "checksums.txt")
	if err != nil {
		return fmt.Errorf("fetch checksums.txt: %w", err)
	}
	want := ""
	for _, line := range strings.Split(string(sums), "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[1] == asset {
			want = f[0]
			break
		}
	}
	if want == "" {
		return fmt.Errorf("%s not in checksums.txt — no release build for %s/%s", asset, runtime.GOOS, runtime.GOARCH)
	}

	fmt.Fprintf(out, "downloading %s\n", asset)
	data, err := httpGet(base + asset)
	if err != nil {
		return fmt.Errorf("download %s: %w", asset, err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != want {
		return fmt.Errorf("%s sha256 mismatch — refusing to install", asset)
	}

	isZip := strings.HasSuffix(asset, ".zip")
	bin, err := extractBinary(data, isZip, "ctx-optimize")
	if err != nil {
		return err
	}
	if err := swapBinary(bin, target); err != nil {
		return err
	}
	fmt.Fprintf(out, "binary: %s → %s\n", tag, target)

	// The archive also carries the ctx-optimize-adapters companion (releases
	// since the native-sources split). Swap it beside the main binary; absent
	// in the archive (older tag) → note and move on, never fail the update.
	compName := "ctx-optimize-adapters"
	if strings.HasSuffix(target, ".exe") {
		compName += ".exe"
	}
	comp, err := extractBinary(data, isZip, "ctx-optimize-adapters")
	if err != nil {
		fmt.Fprintf(out, "companion: not in %s release archive — skipped\n", tag)
		return nil
	}
	compTarget := filepath.Join(filepath.Dir(target), compName)
	if err := swapBinary(comp, compTarget); err != nil {
		return fmt.Errorf("companion install: %w", err)
	}
	fmt.Fprintf(out, "companion: %s → %s\n", tag, compTarget)
	return nil
}

// swapBinary atomically replaces target with bin (temp write → rename dance).
// A missing target (first companion install) is created fresh; any failure
// leaves an existing target untouched.
func swapBinary(bin []byte, target string) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".ctx-optimize-new-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		os.Remove(tmpName)
		return err
	}
	old := target + ".old"
	hadOld := true
	if err := os.Rename(target, old); err != nil {
		if !os.IsNotExist(err) {
			os.Remove(tmpName)
			return err
		}
		hadOld = false // first install of this binary — nothing to back up
	}
	if err := os.Rename(tmpName, target); err != nil {
		if hadOld {
			os.Rename(old, target) // roll back
		}
		os.Remove(tmpName)
		return err
	}
	if hadOld {
		os.Remove(old) // best effort (fails on windows while running — harmless)
	}
	return nil
}

// extractBinary pulls the named executable out of the release archive.
func extractBinary(data []byte, isZip bool, name string) ([]byte, error) {
	names := map[string]bool{name: true, name + ".exe": true}
	if isZip {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if names[filepath.Base(f.Name)] {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, fmt.Errorf("binary not found in release zip")
	}
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("binary not found in release tarball")
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeReg && names[filepath.Base(hdr.Name)] {
			return io.ReadAll(tr)
		}
	}
}

func httpGet(url string) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}
