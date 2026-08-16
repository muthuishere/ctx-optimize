package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// discloseRepo is a small real store built through the real gather, plus a port
// node injected through the universal door so the fixture carries the exact
// shape the defect was reported against (served routes are `port` nodes with
// direction=provides — there is no `route` kind).
func discloseRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	files := map[string]string{
		"go.mod":     "module acme\n",
		"main.go":    "package main\n\nfunc Alpha() {}\n\nfunc main() { Alpha() }\n",
		"README.md":  "# Acme\n\nA thing.\n",
		"lib/lib.go": "package lib\n\nfunc Beta() {}\n",
	}
	for p, c := range files {
		full := filepath.Join(repo, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runCLI(t, 0, "add", "--path", repo)

	batch := `{"producer":"boundaries","nodes":[
	  {"id":"port:network.http:>/checkout","label":"/checkout","kind":"port",
	   "file_type":"boundary","source":"port://network.http//checkout",
	   "metadata":{"direction":"provides","transport":"network.http","identifier":"/checkout","scope":"external"}}],"edges":[]}`
	bf := filepath.Join(t.TempDir(), "batch.json")
	if err := os.WriteFile(bf, []byte(batch), 0o644); err != nil {
		t.Fatal(err)
	}
	runCLI(t, 0, "add", "--json", bf, "--path", repo)
	return repo
}

// THE defect (instance 3): `nodes --kind route` printed "(0 nodes)", exit 0, and
// an agent following our own authoring guide concluded the repo serves nothing.
// It must now say the kind does not exist and name the kinds that do — while
// still exiting 0, because an empty result is a legitimate answer.
func TestNodesUnknownKindDisclosesKindsPresent(t *testing.T) {
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	repo := discloseRepo(t)

	out, _ := runCLI(t, 0, "nodes", "--kind", "route", "--path", repo) // exit 0 asserted by runCLI
	if !strings.Contains(out, "(0 nodes)") {
		t.Fatalf("count line lost: %s", out)
	}
	if !strings.Contains(out, `no node in this store has kind "route"`) {
		t.Fatalf("no disclosure for an impossible --kind: %s", out)
	}
	if !strings.Contains(out, "kinds present:") {
		t.Fatalf("disclosure names nothing that exists: %s", out)
	}
	// The kind the reader actually wanted must be in the list.
	if !strings.Contains(out, "port") || !strings.Contains(out, "file") {
		t.Fatalf("kinds present list is missing real kinds: %s", out)
	}
	// Sorted and deterministic.
	line := disclosureLine(t, out, "kinds present:")
	vals := strings.Split(line, ", ")
	for i := 1; i < len(vals); i++ {
		if vals[i-1] >= vals[i] {
			t.Fatalf("kinds present not sorted: %q", line)
		}
	}

	// edges --relation has the same shape.
	out, _ = runCLI(t, 0, "edges", "--relation", "serves", "--path", repo)
	if !strings.Contains(out, `no edge in this store has relation "serves"`) ||
		!strings.Contains(out, "relations present:") {
		t.Fatalf("edges --relation disclosure missing: %s", out)
	}

	// --where with a key no node carries.
	out, _ = runCLI(t, 0, "nodes", "--where", "directon=provides", "--path", repo)
	if !strings.Contains(out, `carries the key "directon"`) || !strings.Contains(out, "keys present:") {
		t.Fatalf("--where key disclosure missing: %s", out)
	}
	// --where with a real key and a value no node carries.
	out, _ = runCLI(t, 0, "nodes", "--where", "direction=provded", "--path", repo)
	if !strings.Contains(out, `has direction="provded"`) || !strings.Contains(out, "values present for direction:") {
		t.Fatalf("--where value disclosure missing: %s", out)
	}
}

// The guard that stops the feature becoming noise: a REAL kind narrowed to
// nothing by the rest of the predicate is a legitimate empty answer and must
// come back bare. If this test ever fails, readers will start ignoring the note
// and the disclosure is worth nothing.
func TestLegitimateEmptyIsNotDecorated(t *testing.T) {
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	repo := discloseRepo(t)

	cases := [][]string{
		{"nodes", "--kind", "file", "--label", "no-such-file-anywhere"},
		{"nodes", "--kind", "port", "--where", "kind=file"}, // both values real, conjunction empty
		{"edges", "--relation", "contains", "--from", "nothing-with-this-id"},
		{"nodes", "--kind", "function", "--file-type", "document"}, // both real, no overlap
	}
	for _, args := range cases {
		out, errOut := runCLI(t, 0, append(args, "--path", repo)...)
		if strings.Contains(out, "no node in this store") || strings.Contains(out, "no edge in this store") ||
			strings.Contains(out, "present:") {
			t.Fatalf("%v decorated a legitimate empty result:\n%s", args, out)
		}
		if strings.Contains(errOut, "filter_disclosure") {
			t.Fatalf("%v leaked a disclosure to stderr:\n%s", args, errOut)
		}
	}
}

// Machine output: stdout stays exactly the document it was (a bare JSON array
// for `nodes`), and the disclosure arrives on stderr as the documented
// `filter_disclosure` field. Prose on stdout here would corrupt every parse.
func TestDisclosureIsAJSONFieldAndNeverCorruptsStdout(t *testing.T) {
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	repo := discloseRepo(t)

	out, errOut := runCLI(t, 0, "nodes", "--kind", "route", "--path", repo, "--json")
	var arr []map[string]any
	if err := json.Unmarshal([]byte(out), &arr); err != nil {
		t.Fatalf("stdout is no longer a parseable JSON array: %v\n%s", err, out)
	}
	if len(arr) != 0 {
		t.Fatalf("want an empty array, got %d records", len(arr))
	}
	var env struct {
		FilterDisclosure struct {
			Misses []struct {
				Dimension string   `json:"dimension"`
				Stream    string   `json:"stream"`
				Value     string   `json:"value"`
				Present   []string `json:"present"`
				Message   string   `json:"message"`
			} `json:"misses"`
		} `json:"filter_disclosure"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(errOut)), &env); err != nil {
		t.Fatalf("stderr is not the filter_disclosure envelope: %v\n%s", err, errOut)
	}
	if len(env.FilterDisclosure.Misses) != 1 {
		t.Fatalf("want exactly one miss: %s", errOut)
	}
	m := env.FilterDisclosure.Misses[0]
	if m.Dimension != "kind" || m.Value != "route" || m.Stream != "node" {
		t.Fatalf("wrong miss payload: %+v", m)
	}
	if len(m.Present) == 0 || m.Message == "" {
		t.Fatalf("miss carries no suggestion or message: %+v", m)
	}

	// --ndjson: the stream stays record-per-line (here, zero lines).
	out, errOut = runCLI(t, 0, "nodes", "--kind", "route", "--path", repo, "--ndjson")
	if strings.TrimSpace(out) != "" {
		t.Fatalf("ndjson stdout must stay empty: %q", out)
	}
	if !strings.Contains(errOut, `"filter_disclosure"`) {
		t.Fatalf("ndjson disclosure missing from stderr: %s", errOut)
	}

	// A legitimate empty in machine mode writes nothing to stderr at all.
	_, errOut = runCLI(t, 0, "nodes", "--kind", "file", "--label", "zzz-nope", "--path", repo, "--json")
	if strings.Contains(errOut, "filter_disclosure") {
		t.Fatalf("legitimate empty decorated in machine mode: %s", errOut)
	}
}

// Every other verb that takes the shared predicate carries it too.
func TestDisclosureAcrossPredicateVerbs(t *testing.T) {
	t.Setenv("CTX_OPTIMIZE_STORE", t.TempDir())
	repo := discloseRepo(t)

	for _, args := range [][]string{
		{"report", "--kind", "route"},
		{"hubs", "--kind", "route"},
		{"query", "alpha", "--kind", "route"},
		{"affected", "Alpha", "--kind", "route"},
	} {
		out, _ := runCLI(t, 0, append(args, "--path", repo)...)
		if !strings.Contains(out, `no node in this store has kind "route"`) {
			t.Fatalf("%v: no disclosure:\n%s", args, out)
		}
	}
	// export dumps a data document, so its disclosure is stderr-only in every
	// format.
	out, errOut := runCLI(t, 0, "export", "--kind", "route", "--path", repo)
	if strings.Contains(out, "no node in this store") {
		t.Fatalf("export polluted its data document: %s", out)
	}
	if !strings.Contains(errOut, `"filter_disclosure"`) {
		t.Fatalf("export disclosure missing from stderr: %s", errOut)
	}
	if err := json.Unmarshal([]byte(out), &map[string]any{}); err != nil {
		t.Fatalf("export stdout no longer parseable: %v", err)
	}
}

func disclosureLine(t *testing.T, out, marker string) string {
	t.Helper()
	for _, l := range strings.Split(out, "\n") {
		if i := strings.Index(l, marker); i >= 0 {
			return strings.TrimSpace(l[i+len(marker):])
		}
	}
	t.Fatalf("marker %q not found in:\n%s", marker, out)
	return ""
}
