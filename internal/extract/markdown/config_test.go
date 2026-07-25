package markdown

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// configKeyNodes runs the config producer over one file's content and returns
// the emitted config_key nodes as "label@location" strings — the identity a
// caller can cite, so a diff here is a diff in the graph.
func configKeyNodes(content string) []string {
	b := &schema.Batch{Producer: ProducerName}
	extractConfig(b, "app.yaml", content)
	var out []string
	for _, n := range b.Nodes {
		if n.Kind == "config_key" {
			out = append(out, n.ID+"|"+n.Label+"@"+n.Location)
		}
	}
	return out
}

func configKeyLabels(content string) []string {
	b := &schema.Batch{Producer: ProducerName}
	extractConfig(b, "app.yaml", content)
	var out []string
	for _, n := range b.Nodes {
		if n.Kind == "config_key" {
			out = append(out, n.Label)
		}
	}
	return out
}

// S1 regression: trailing whitespace on nested lines used to decide whether a
// nested key was indexed at all (measured 9 nodes trimmed vs 4 with trailing
// spaces on the same file). Invisible characters must not change the graph.
func TestConfigNestedKeysAreWhitespaceIndependent(t *testing.T) {
	clean := strings.Join([]string{
		"server:",
		"  port: 8080",
		"  host: localhost",
		"database:",
		"    url: jdbc:pg",
		"    pool:",
		"      size: 10",
		"",
	}, "\n")
	// Same content, trailing spaces/tabs sprinkled on the nested lines.
	trailing := regexp.MustCompile(`(?m)^(  .*[^ \t])$`).ReplaceAllString(clean, "$1   ")
	if trailing == clean {
		t.Fatal("test setup: no trailing whitespace was injected")
	}

	got, want := configKeyNodes(trailing), configKeyNodes(clean)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("trailing whitespace changed the graph:\n with = %v\n without = %v", got, want)
	}
	if len(want) != 7 {
		t.Fatalf("want 7 keys at every depth, got %d: %v", len(want), want)
	}
}

// S1 option (a): keys at EVERY depth are indexed. Locking this in so a future
// change cannot silently revert to top-level-only (which would delete ~2/3 of
// config nodes on a representative application.yml).
func TestConfigIndexesEveryDepth(t *testing.T) {
	content := strings.Join([]string{
		"top: 1",
		"  second: 2",
		"    third: 3",
		"      fourth: 4",
		"",
	}, "\n")
	want := []string{"top", "second", "third", "fourth"}
	if got := configKeyLabels(content); !reflect.DeepEqual(got, want) {
		t.Fatalf("depths lost: got %v want %v", got, want)
	}
}

// S2: a block scalar body is opaque DATA. Nine junk nodes in real k8s files
// came from inside an embedded postgresql.conf — none of its keys may appear.
func TestConfigBlockScalarBodyIsNotIndexed(t *testing.T) {
	content := strings.Join([]string{
		"apiVersion: v1",
		"kind: ConfigMap",
		"data:",
		"  postgresql.conf: |",
		"    listen_addresses = '*'",
		"    max_connections = 100",
		"    shared_buffers = 256MB",
		"",
		"    # a comment, and a blank line above, both inside the block",
		"    wal_level = replica",
		"  pg_hba.conf: |",
		"    host all all 0.0.0.0/0 md5",
		"metadata:",
		"  name: pgconf",
		"",
	}, "\n")
	got := configKeyLabels(content)
	want := []string{"apiVersion", "kind", "data", "postgresql.conf", "pg_hba.conf", "metadata", "name"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("block-scalar body leaked into the graph:\n got  %v\n want %v", got, want)
	}
	for _, junk := range []string{"listen_addresses", "max_connections", "shared_buffers", "wal_level", "host all all 0.0.0.0/0 md5"} {
		for _, g := range got {
			if g == junk {
				t.Fatalf("inner key %q emitted", junk)
			}
		}
	}
}

// S2: every block-scalar indicator form opens a block, and the key AFTER the
// block ends is still indexed (proves the skip terminates).
func TestConfigBlockScalarIndicators(t *testing.T) {
	cases := []struct {
		name   string
		header string
		want   []string
	}{
		{"literal", "script: |", []string{"script", "after"}},
		{"literal-strip", "script: |-", []string{"script", "after"}},
		{"folded", "script: >", []string{"script", "after"}},
		{"folded-keep", "script: >+", []string{"script", "after"}},
		{"explicit-indent", "script: |2", []string{"script", "after"}},
		{"indent-after-chomp", "script: |-2", []string{"script", "after"}},
		{"chomp-after-indent", "script: |2-", []string{"script", "after"}},
		{"header-comment", "script: | # inline shell", []string{"script", "after"}},
		// Not a block scalar: a quoted value, and a plain value.
		{"quoted-pipe", `script: "|"`, []string{"script", "inner", "after"}},
		{"plain-value", "script: run.sh", []string{"script", "inner", "after"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			content := strings.Join([]string{
				tc.header,
				"  inner: not-a-key-when-block",
				"after: yes",
				"",
			}, "\n")
			if got := configKeyLabels(content); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

// S2 edge cases the body must survive: a block as the last thing in the file,
// a block opened inside a list item, and a nested block under a deeper key.
func TestConfigBlockScalarEdgeCases(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "block at end of file, no trailing newline",
			content: "notes: |\n  key = value\n  other = value",
			want:    []string{"notes"},
		},
		{
			name:    "block at end of file with trailing blank lines",
			content: "notes: |\n  key = value\n\n\n",
			want:    []string{"notes"},
		},
		{
			name:    "block inside a list item",
			content: "steps:\n  - run: |\n      cmd = go test\n      flag = -race\n  - name: second\nafter: yes\n",
			// `- run` is rejected as a label (contains a space), which is
			// pre-existing behavior; what matters is the body is skipped.
			want: []string{"steps", "after"},
		},
		{
			name:    "block under a deep key, sibling after it",
			content: "a:\n  b:\n    c: |\n      x = 1\n      y = 2\n    d: kept\n  e: kept\nf: kept\n",
			want:    []string{"a", "b", "c", "d", "e", "f"},
		},
		{
			name:    "two blocks back to back at different depths",
			content: "one: |\n  a = 1\ntwo:\n  three: |\n    b = 2\n  four: kept\n",
			want:    []string{"one", "two", "three", "four"},
		},
		{
			name:    "body line that itself looks like a block header",
			content: "outer: |\n  inner: |\n    deep = 1\nafter: yes\n",
			want:    []string{"outer", "after"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := configKeyLabels(tc.content); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestBlockScalarHeader(t *testing.T) {
	yes := []string{" |", " >", " |-", " |+", " >-", " >+", " |2", " |-2", " |2-", " | # c", " >2+ # c", "|"}
	no := []string{"", " ", " run.sh", ` "|"`, " |x", " |23", " |--", " | value", " >text", " ||", " -", " 8080"}
	for _, v := range yes {
		if !blockScalarHeader(v) {
			t.Errorf("blockScalarHeader(%q) = false, want true", v)
		}
	}
	for _, v := range no {
		if blockScalarHeader(v) {
			t.Errorf("blockScalarHeader(%q) = true, want false", v)
		}
	}
}

func TestIndentWidth(t *testing.T) {
	cases := map[string]int{
		"": 0, "a": 0, "  a": 2, "\t\ta": 2, "    ": 4, " \t x": 3,
		// A `- ` list marker counts as two columns, so a list item's key aligns
		// with its siblings rather than with the dash (the yamlwalk rule).
		"- a":       2,
		"  - a":     4,
		"  - - a":   6,
		"    - a":   6,
		"-":         0, // a bare dash is not a `- key` item
		"- ":        2,
		"  -  a":    5, // the marker plus its extra space: content starts at col 5
		"nodash: 1": 0,
	}
	for in, want := range cases {
		if got := indentWidth(in); got != want {
			t.Errorf("indentWidth(%q) = %d, want %d", in, got, want)
		}
	}
}

// The bug the Newtonsoft.Json corpus tier caught: a block scalar opened by a
// LIST ITEM (`- powershell: |`) measured as indent 0, so the item's sibling
// keys at indent 2 looked like block content and were swallowed. On the real
// azure-pipelines.yml this dropped 9 legitimate keys (`env`, `displayName`,
// `condition`, and the keys nested under `env`) alongside the 11 PowerShell
// lines the skip is meant to remove.
func TestConfigBlockScalarInListItemKeepsSiblingKeys(t *testing.T) {
	// Verbatim shape from Newtonsoft.Json's azure-pipelines.yml.
	got := configKeyLabels(strings.Join([]string{
		"steps:",
		"- powershell: |",
		"    $basePath = resolve-path .",
		"    $keyData = [System.Convert]::FromBase64String($Env:KeyData)",
		"  env:",
		"    KeyData: $(newtonsoft.keyData)",
		"  displayName: 'Prepare signing key'",
		"  condition: and(succeeded(), not(eq(variables['build.reason'], 'PullRequest')))",
		"- powershell: |",
		"    $version = Get-Content .\\Build\\version.json",
		"  displayName: 'Run build'",
	}, "\n"))

	// NOTE `powershell` itself is absent, and that is PRE-EXISTING behavior, not
	// a block-skip failure: the key parsed from `- powershell: |` is
	// "- powershell", which the long-standing ContainsAny(key, "{}\"' ") guard
	// rejects for containing a space. So list-item keys have never been indexed.
	// Indexing them would ADD nodes — coverage, not honesty — so it is out of
	// scope for ADR 2026-07-25-structured-formats. Pinned here as current truth
	// so the next reader knows it is known, not overlooked.
	want := []string{"steps", "env", "KeyData", "displayName",
		"condition", "displayName"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("list-item block scalar swallowed sibling keys\n got: %v\nwant: %v", got, want)
	}
	for _, bad := range []string{"$basePath", "$keyData", "$version"} {
		for _, g := range got {
			if g == bad {
				t.Errorf("PowerShell body line %q was indexed as a config key", bad)
			}
		}
	}
}
