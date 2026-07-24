// Content-hydration spike (openspec/changes/2026-07-24-content-hydration):
// `query`/`card --include-content` attach each hit's verbatim source body,
// read from the file at answer time — never stored. These tests pin the
// three guarantees the ADR asks for: default output unchanged, the flag adds
// content, and a missing file degrades gracefully instead of failing.
package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

func TestQueryIncludeContent(t *testing.T) {
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	repo := t.TempDir()
	src := "package main\n\n// Greet says hi.\nfunc Greet(name string) string {\n\treturn \"hi \" + name\n}\n\nfunc main() {\n\tGreet(\"x\")\n}\n"
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	run := func(wantCode int, args ...string) (string, string) {
		t.Helper()
		var out, errb bytes.Buffer
		code := Run(args, &out, &errb)
		if code != wantCode {
			t.Fatalf("%v: exit %d (want %d): %s", args, code, wantCode, errb.String())
		}
		return out.String(), errb.String()
	}

	run(0, "add", "--path", repo)

	// Default: --json output has NO content field — byte-identical to
	// today's pointer-only shape.
	baseline, _ := run(0, "query", "greet", "--path", repo, "--json")
	var baseRes struct {
		Hits []struct {
			Node struct{ ID string }
			// unmarshal into a raw map to check the key is truly absent,
			// not just empty.
		}
	}
	if err := json.Unmarshal([]byte(baseline), &baseRes); err != nil {
		t.Fatalf("query --json not parseable: %v\n%s", err, baseline)
	}
	if strings.Contains(baseline, `"content"`) || strings.Contains(baseline, `"content_error"`) {
		t.Fatalf("default query --json must omit content fields entirely:\n%s", baseline)
	}

	// Default human render also carries no "content:" section.
	baselineText, _ := run(0, "query", "greet", "--path", repo)
	if strings.Contains(baselineText, "content:") {
		t.Fatalf("default query render must not print content:\n%s", baselineText)
	}

	// --include-content --json: a hit for Greet carries its verbatim body.
	hydrated, _ := run(0, "query", "greet", "--path", repo, "--include-content", "--json")
	var res struct {
		Hits []struct {
			Node struct {
				ID       string
				Location string
			}
			Content      string `json:"content"`
			ContentError string `json:"content_error"`
		}
	}
	if err := json.Unmarshal([]byte(hydrated), &res); err != nil {
		t.Fatalf("query --include-content --json not parseable: %v\n%s", err, hydrated)
	}
	var found bool
	for _, h := range res.Hits {
		if strings.Contains(h.Node.ID, "Greet") || strings.Contains(h.Node.ID, "greet") {
			found = true
			if h.ContentError != "" {
				t.Fatalf("Greet hit should hydrate cleanly, got content_error=%q", h.ContentError)
			}
			if !strings.Contains(h.Content, "func Greet(name string) string") {
				t.Fatalf("hydrated content missing the signature line, got: %q", h.Content)
			}
			if !strings.Contains(h.Content, `return "hi " + name`) {
				t.Fatalf("hydrated content missing the body, got: %q", h.Content)
			}
		}
	}
	if !found {
		t.Fatalf("expected a Greet hit in %+v", res.Hits)
	}

	// --include-content (human render) prints a content: block.
	hydratedText, _ := run(0, "query", "greet", "--path", repo, "--include-content")
	if !strings.Contains(hydratedText, "content:") || !strings.Contains(hydratedText, "func Greet") {
		t.Fatalf("include-content render missing body:\n%s", hydratedText)
	}
}

// TestQueryIncludeContentMissingFile proves a hit whose source file vanished
// after gather degrades to content_error instead of failing the query.
func TestQueryIncludeContentMissingFile(t *testing.T) {
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	repo := t.TempDir()
	src := "package main\n\n// Vanish will be deleted after gather.\nfunc Vanish() {}\n"
	if err := os.WriteFile(filepath.Join(repo, "vanish.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	if code := Run([]string{"add", "--path", repo}, &out, &errb); code != 0 {
		t.Fatalf("add: %s", errb.String())
	}

	// Simulate drift: the file the store points at is now gone.
	if err := os.Remove(filepath.Join(repo, "vanish.go")); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	errb.Reset()
	code := Run([]string{"query", "vanish", "--path", repo, "--include-content", "--json"}, &out, &errb)
	if code != 0 {
		t.Fatalf("query must not fail when a hit's file is missing: exit %d, stderr %s", code, errb.String())
	}
	text := out.String()
	var res struct {
		Hits []struct {
			Node struct{ ID string }
			// json must stay valid even with a content_error present
			ContentError string `json:"content_error"`
			Content      string `json:"content"`
		}
	}
	if err := json.Unmarshal([]byte(text), &res); err != nil {
		t.Fatalf("query --include-content --json must stay valid JSON even on missing file: %v\n%s", err, text)
	}
	var found bool
	for _, h := range res.Hits {
		if strings.Contains(h.Node.ID, "Vanish") || strings.Contains(h.Node.ID, "vanish") {
			found = true
			if h.Content != "" {
				t.Fatalf("missing file must not produce content, got %q", h.Content)
			}
			if h.ContentError == "" {
				t.Fatalf("missing file must set content_error")
			}
		}
	}
	if !found {
		t.Fatalf("expected a Vanish hit in %+v", res.Hits)
	}
}

// TestCardIncludeContent proves card gets the same opt-in flag: default
// stays the truncated bodyHead preview, --include-content returns the full
// verbatim span.
func TestCardIncludeContent(t *testing.T) {
	storeRoot := t.TempDir()
	t.Setenv("CTX_OPTIMIZE_STORE", storeRoot)
	repo := t.TempDir()
	var lines []string
	lines = append(lines, "package main", "", "// Big does many things.", "func Big() string {")
	for i := 0; i < 60; i++ {
		lines = append(lines, "\t_ = 1 // padding")
	}
	lines = append(lines, "\treturn \"end-marker-xyz\"", "}")
	src := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(repo, "big.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	if code := Run([]string{"add", "--path", repo}, &out, &errb); code != 0 {
		t.Fatalf("add: %s", errb.String())
	}

	out.Reset()
	if code := Run([]string{"card", "Big", "--path", repo, "--json"}, &out, &errb); code != 0 {
		t.Fatalf("card: %s", errb.String())
	}
	var baseCard struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal(out.Bytes(), &baseCard); err != nil {
		t.Fatalf("card --json not parseable: %v", err)
	}
	if strings.Contains(baseCard.Body, "end-marker-xyz") {
		t.Fatalf("default card body should be truncated and miss the tail")
	}

	out.Reset()
	if code := Run([]string{"card", "Big", "--path", repo, "--include-content", "--json"}, &out, &errb); code != 0 {
		t.Fatalf("card --include-content: %s", errb.String())
	}
	var fullCard struct {
		Body         string `json:"body"`
		ContentError string `json:"content_error"`
	}
	if err := json.Unmarshal(out.Bytes(), &fullCard); err != nil {
		t.Fatalf("card --include-content --json not parseable: %v", err)
	}
	if fullCard.ContentError != "" {
		t.Fatalf("card --include-content should hydrate cleanly, got %q", fullCard.ContentError)
	}
	if !strings.Contains(fullCard.Body, "end-marker-xyz") {
		t.Fatalf("card --include-content should return the FULL body including the tail, got: %q", fullCard.Body)
	}
}

// TestReadSourceBodyErrors pins the two hardened failure modes: an empty/blank
// source range and a missing file must each return an explicit error (which the
// caller turns into content_error), never a silent empty body.
func TestReadSourceBodyErrors(t *testing.T) {
	repo := t.TempDir()
	// L2 and L3 are blank; L4 has the real declaration.
	if err := os.WriteFile(filepath.Join(repo, "blank.go"),
		[]byte("package main\n\n\nfunc X() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// empty range -> explicit error, not "".
	if _, err := readSourceBody(repo, schema.Node{Source: "blank.go", Location: "L2-L3"}); err == nil || !strings.Contains(err.Error(), "empty source range") {
		t.Fatalf("blank range must error with 'empty source range', got %v", err)
	}

	// missing file -> clean, source-named error.
	if _, err := readSourceBody(repo, schema.Node{Source: "nope.go", Location: "L1"}); err == nil || !strings.Contains(err.Error(), "source file not found") {
		t.Fatalf("missing file must error with 'source file not found', got %v", err)
	}

	// a real, non-empty range still returns the body.
	body, err := readSourceBody(repo, schema.Node{Source: "blank.go", Location: "L4"})
	if err != nil || !strings.Contains(body, "func X") {
		t.Fatalf("valid range must return body, got %q err %v", body, err)
	}
}
