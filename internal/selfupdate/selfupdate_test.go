package selfupdate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"0.3.9", "v0.3.10", true},
		{"0.3.9", "v0.3.9", false},
		{"0.3.10", "v0.3.9", false},
		{"0.3.9", "v0.4.0", true},
		{"0.9.9", "v1.0.0", true},
		{"0.3.9", "garbage", false}, // malformed tag never triggers a swap
		{"0.3.9", "v0.4.0-rc1", true},
	}
	for _, c := range cases {
		if got := Newer(c.current, c.latest); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestChannel(t *testing.T) {
	if Channel("/usr/local/lib/node_modules/@muthuishere/ctx-optimize-darwin-arm64/bin/ctx-optimize") != "npm" {
		t.Fatal("node_modules path must classify as npm")
	}
	if Channel("/usr/local/bin/ctx-optimize") != "binary" {
		t.Fatal("plain path must classify as binary")
	}
}

func TestLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/muthuishere/ctx-optimize/releases/latest" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"tag_name":"v9.9.9"}`)
	}))
	defer srv.Close()
	tag, err := Latest(srv.URL)
	if err != nil || tag != "v9.9.9" {
		t.Fatalf("Latest = %q, %v", tag, err)
	}
}

// releaseServer serves checksums.txt + a tar.gz asset holding `payload` as
// the ctx-optimize binary. sumOverride poisons the checksum when non-empty.
func releaseServer(t *testing.T, tag string, payload []byte, sumOverride string) *httptest.Server {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "ctx-optimize", Mode: 0o755, Size: int64(len(payload)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	archive := buf.Bytes()

	asset := AssetName(tag)
	sum := sha256.Sum256(archive)
	sumLine := hex.EncodeToString(sum[:])
	if sumOverride != "" {
		sumLine = sumOverride
	}
	checksums := sumLine + "  " + asset + "\n"

	prefix := "/muthuishere/ctx-optimize/releases/download/" + tag + "/"
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, prefix) {
		case "checksums.txt":
			fmt.Fprint(w, checksums)
		case asset:
			w.Write(archive)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestApplySwap(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("release asset is a zip on windows; the tar lane is what ships here")
	}
	srv := releaseServer(t, "v9.9.9", []byte("NEW BINARY"), "")
	defer srv.Close()

	dir := t.TempDir()
	target := filepath.Join(dir, "ctx-optimize")
	if err := os.WriteFile(target, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Apply(srv.URL, "v9.9.9", target, &out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "NEW BINARY" {
		t.Fatalf("target not swapped: %q", data)
	}
	fi, _ := os.Stat(target)
	if fi.Mode().Perm() != 0o755 {
		t.Fatalf("mode = %v, want 0755", fi.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("leftovers in dir: %v", entries)
	}
}

func TestApplyChecksumMismatchLeavesTarget(t *testing.T) {
	srv := releaseServer(t, "v9.9.9", []byte("EVIL"), strings.Repeat("0", 64))
	defer srv.Close()

	target := filepath.Join(t.TempDir(), "ctx-optimize")
	if err := os.WriteFile(target, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := Apply(srv.URL, "v9.9.9", target, &out)
	if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Fatalf("want sha256 mismatch error, got %v", err)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "OLD" {
		t.Fatal("target modified despite checksum failure")
	}
}
