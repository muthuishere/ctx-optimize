package app

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// `affected` has an index fast path; it may make the verb quick, never
// different. Answer every flag combination with the index, delete the index
// (forcing the full load), answer again, and require identical bytes.
func TestAffectedIndexMatchesFullLoad(t *testing.T) {
	src := "package main\n\n" +
		"func main() { Alpha(); Beta() }\n\n" +
		"func Alpha() { Beta(); Gamma() }\n\n" +
		"func Beta() { Gamma() }\n\n" +
		"func Gamma() {}\n\n" +
		"type Store struct{}\n\n" +
		"func (s *Store) Merge() { Gamma() }\n"
	repo, storeRoot, key := setupIncremental(t, map[string]string{
		"go.mod":       "module ex\n\ngo 1.22\n",
		"main.go":      src,
		"main_test.go": "package main\n\nimport \"testing\"\n\nfunc TestGamma(t *testing.T) { Gamma() }\n",
	})
	mustIndex(t, storeRoot, key, "before the comparison")

	cases := [][]string{
		{"Gamma"},
		{"Gamma", "--json"},
		{"Gamma", "--ndjson"},
		{"Gamma", "--depth", "1"},
		{"Gamma", "--depth", "4"},
		{"Gamma", "--relation", "calls"},
		{"Gamma", "--include-ambiguous"},
		{"Gamma", "--kind", "test"}, // refused by the fast path: must still match
		{"Store.Merge"},
		{"gamm"}, // not an exact name: falls through to fuzzy on both
	}
	// Exit code is part of the answer: `gamm` is not a name, and both paths must
	// refuse it the same way.
	type result struct {
		out, err string
		code     int
	}
	run := func(c []string) result {
		args := append([]string{"affected"}, c...)
		args = append(args, "--path", repo, "--store", storeRoot)
		var out, errb bytes.Buffer
		code := Run(args, &out, &errb)
		return result{out.String(), errb.String(), code}
	}
	indexed := make([]result, len(cases))
	for i, c := range cases {
		indexed[i] = run(c)
	}
	if indexed[0].code != 0 || len(indexed[0].out) == 0 {
		t.Fatal("precondition: affected Gamma produced no output")
	}

	if err := os.RemoveAll(filepath.Join(storeRoot, key, "graph", "index")); err != nil {
		t.Fatal(err)
	}
	if openStoreAt(t, storeRoot, key).IndexCurrent() {
		t.Fatal("precondition: the index should be gone")
	}
	for i, c := range cases {
		if got := run(c); got != indexed[i] {
			t.Errorf("affected %v differs between index and full load\n index : %+v\n full  : %+v", c, indexed[i], got)
		}
	}
}
