package manifests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

func gatherDevenv(t *testing.T, files map[string]string) *schema.Batch {
	t.Helper()
	root := t.TempDir()
	for p, content := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func findNode(b *schema.Batch, id string) *schema.Node {
	for i := range b.Nodes {
		if b.Nodes[i].ID == id {
			return &b.Nodes[i]
		}
	}
	return nil
}

func TestTaskfileTasks(t *testing.T) {
	b := gatherDevenv(t, map[string]string{
		"Taskfile.yml": `version: '3'

tasks:
  build:
    desc: Build the CLI
    cmds:
      - go build -o bin/app ./cmd/app

  test:
    cmds:
      - go test ./...

  ci:
    desc: Full gate
    deps: [lint, test]
    cmds:
      - go vet ./...
`,
	})
	for _, want := range []string{"build", "test", "ci"} {
		n := findNode(b, "Taskfile.yml::task:"+want)
		if n == nil {
			t.Fatalf("task %s missing", want)
		}
		if n.Kind != "task" || n.Label != "task:"+want {
			t.Errorf("task %s: kind=%q label=%q", want, n.Kind, n.Label)
		}
	}
	if n := findNode(b, "Taskfile.yml::task:build"); n.Metadata["desc"] != "Build the CLI" ||
		n.Metadata["command"] != "go build -o bin/app ./cmd/app" {
		t.Errorf("build metadata = %v", n.Metadata)
	}
	// env variants are recognized too
	b2 := gatherDevenv(t, map[string]string{
		"Taskfile.dev.yml": "version: '3'\ntasks:\n  deploy:\n    cmds:\n      - ./deploy.sh dev\n",
	})
	if findNode(b2, "Taskfile.dev.yml::task:deploy") == nil {
		t.Error("Taskfile.dev.yml variant not recognized")
	}
}

func TestMakefileTargets(t *testing.T) {
	b := gatherDevenv(t, map[string]string{
		"Makefile": `CC := gcc
CFLAGS = -O2

.PHONY: all clean

all: build

build:
	$(CC) -o app main.c

clean:
	rm -f app

%.o: %.c
	$(CC) -c $<

$(BINDIR)/tool:
	install tool
`,
	})
	for _, want := range []string{"all", "build", "clean"} {
		if findNode(b, "Makefile::task:"+want) == nil {
			t.Errorf("target %s missing", want)
		}
	}
	// literal-or-silent: variables, pattern rules, computed names, .PHONY
	for _, id := range []string{"CC", "CFLAGS", "%.o", "$(BINDIR)/tool", ".PHONY"} {
		if findNode(b, "Makefile::task:"+id) != nil {
			t.Errorf("non-target %q leaked a task node", id)
		}
	}
	if n := findNode(b, "Makefile::task:build"); n.Metadata["command"] != "$(CC) -o app main.c" {
		t.Errorf("build command = %q", n.Metadata["command"])
	}
}

func TestJustfileRecipes(t *testing.T) {
	b := gatherDevenv(t, map[string]string{
		"justfile": `set shell := ["bash", "-c"]

version := "1.0"

# builds the app
build:
	go build ./...

test filter='':
	go test ./... -run '{{filter}}'
`,
	})
	for _, want := range []string{"build", "test"} {
		if findNode(b, "justfile::task:"+want) == nil {
			t.Errorf("recipe %s missing", want)
		}
	}
	for _, id := range []string{"set", "version"} {
		if findNode(b, "justfile::task:"+id) != nil {
			t.Errorf("non-recipe %q leaked a task node", id)
		}
	}
}
