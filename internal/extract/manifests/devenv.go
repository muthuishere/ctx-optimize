// devenv.go — task-runner recognizers: Taskfile.yml (go-task), Makefile, and
// justfile targets become task nodes, joining the npm-scripts/gradle task
// kind so "how do I build/test/run this repo" is answerable from the graph.
// Same doctrine as every core recognizer: single-pass, line-anchored,
// literal-or-silent (a shape we can't confidently read emits nothing).
package manifests

import (
	"fmt"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/extract/yamlwalk"
	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// taskNode emits one task with its containing-file edge — the exact shape
// npm scripts already use, so every task runner lands in one queryable kind.
func taskNode(c *collector, rel, name, namespace, command, desc string, line int) {
	id := rel + "::task:" + name
	md := map[string]string{}
	if command != "" {
		md["command"] = command
	}
	if desc != "" {
		md["desc"] = desc
	}
	loc := ""
	if line > 0 {
		loc = fmt.Sprintf("L%d", line)
	}
	c.node(schema.Node{
		ID: id, Label: namespace + ":" + name, Kind: "task", FileType: "manifest",
		Source: rel, Location: loc, Metadata: md,
	})
	c.edge(schema.Edge{Source: rel, Target: id, Relation: "contains", Confidence: schema.Extracted})
}

// extractTaskfile reads go-task Taskfiles (Taskfile.yml + env variants): every
// child of the top-level `tasks:` map is a task; its `desc:` and first `cmds:`
// list item are carried as metadata.
func extractTaskfile(c *collector, rel string, data []byte) {
	lines := yamlwalk.Parse(strings.Split(string(data), "\n"), 0)
	inTasks := false
	cur := ""
	curLine := 0
	desc, cmd := "", ""
	flush := func() {
		if cur != "" {
			taskNode(c, rel, cur, "task", cmd, desc, curLine)
		}
		cur, desc, cmd = "", "", ""
	}
	for _, l := range lines {
		switch {
		case l.Indent == 0:
			flush()
			inTasks = l.Key == "tasks"
		case inTasks && l.Indent == 2 && l.Key != "" && !l.List:
			flush()
			cur, curLine = l.Key, l.Num
		case inTasks && cur != "" && l.Key == "desc" && desc == "":
			desc = l.Val
		case inTasks && cur != "" && l.List && cmd == "":
			// first item under cmds: (or a bare list) — the representative command
			if l.Val != "" {
				cmd = l.Val
			} else if l.Key != "" { // `- task: other` style
				cmd = l.Key + ": " + l.Val
			}
		}
	}
	flush()
}

// extractMakefile emits one task per rule target. Deliberately narrow: a
// target is a single plain word at column 0 followed by ':' — no variables,
// no pattern rules, no special targets (.PHONY), no assignments. The first
// tab-indented recipe line rides along as the command.
func extractMakefile(c *collector, rel string, data []byte) {
	lines := strings.Split(string(data), "\n")
	for i, raw := range lines {
		if raw == "" || raw[0] == '\t' || raw[0] == '#' || raw[0] == '.' || raw[0] == ' ' {
			continue
		}
		colon := strings.IndexByte(raw, ':')
		if colon <= 0 {
			continue
		}
		name := strings.TrimSpace(raw[:colon])
		rest := raw[colon+1:]
		// := / = assignments, double-colon oddities, computed or pattern names:
		// not confidently a task — skip (literal-or-silent).
		if strings.HasPrefix(rest, "=") || strings.HasPrefix(rest, ":") ||
			strings.ContainsAny(name, " \t$%(){}") {
			continue
		}
		cmd := ""
		if i+1 < len(lines) && strings.HasPrefix(lines[i+1], "\t") {
			cmd = strings.TrimSpace(lines[i+1])
		}
		taskNode(c, rel, name, "make", cmd, "", i+1)
	}
}

// extractJustfile emits one task per just recipe: a plain name (args allowed)
// at column 0 ending in ':'. Settings, comments, and assignments are skipped.
func extractJustfile(c *collector, rel string, data []byte) {
	for i, raw := range strings.Split(string(data), "\n") {
		if raw == "" || raw[0] == '\t' || raw[0] == ' ' || raw[0] == '#' ||
			raw[0] == '[' || strings.HasPrefix(raw, "set ") {
			continue
		}
		colon := strings.IndexByte(raw, ':')
		// `name := value` is an assignment, not a recipe (the '=' rides right
		// after the colon); a recipe's args may carry '=' defaults before it.
		if colon <= 0 || strings.HasPrefix(raw[colon+1:], "=") {
			continue
		}
		fields := strings.Fields(raw[:colon])
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		if strings.ContainsAny(name, "${}%") {
			continue
		}
		taskNode(c, rel, name, "just", "", "", i+1)
	}
}
