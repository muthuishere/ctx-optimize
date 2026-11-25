package app

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/muthuishere/clicodemode/adapters/cmdtree"
	"github.com/muthuishere/clicodemode/codemode"

	"github.com/muthuishere/ctx-optimize/internal/version"
)

// cmdCode mounts CLI Code Mode (ADR 2026-11-22-code-mode): an agent sends ONE
// program that chains the read-only verbs in a single process. Only verbs that
// never write the store are registered, so a program cannot gather or mutate.
func cmdCode(args []string, stdout, stderr io.Writer) int {
	e := codemode.NewEngine(codeRegistry())
	cli := codemode.NewCLI(e, "ctx-optimize code")
	cli.Stdin, cli.Stdout, cli.Stderr = os.Stdin, stdout, stderr
	return cli.Main(context.Background(), args)
}

// jsFiles are the shipped Code Mode modules: js/x.js is imported as "ctx:x".
// They compose the read-only ops into recipes agents otherwise rebuild by hand.
//
//go:embed js
var jsFiles embed.FS

func codeRegistry() *codemode.Registry {
	reg := codemode.NewRegistry(version.Version)
	ro := func(name, desc string, run cmdtree.Runner) {
		reg.MustRegister(cmdtree.FromRunner(name, desc, run,
			cmdtree.WithJSONResult("--json"),
			cmdtree.WithFlagsLast(), // these verbs parse positionals first
			cmdtree.WithValidate(validateFlags(name)),
			cmdtree.WithCheckResult(rejectFuzzy()),
			cmdtree.WithSideEffects(codemode.SideEffectNone)))
	}
	ro("ctx.query", "Find symbols/docs by 2-4 terms. args: [\"<terms>\", \"--budget\", \"N\"].",
		func(_ context.Context, a []string, o, e io.Writer) error { return cmdQuery(a, o, e) })
	ro("ctx.card", "Inspect one symbol: signature, doc, callers. args: [\"<label>\"].",
		func(_ context.Context, a []string, o, _ io.Writer) error { return cmdCard(a, o) })
	ro("ctx.change_plan", "Before editing a symbol: callers, blast radius, tests to run. args: [\"<symbol>\"].",
		func(_ context.Context, a []string, o, _ io.Writer) error { return cmdChangePlan(a, o) })
	ro("ctx.affected", "Reverse impact of a symbol. args: [\"<symbol>\", \"--depth\", \"2\"].",
		func(_ context.Context, a []string, o, e io.Writer) error { return cmdAffected(a, o, e) })
	ro("ctx.path", "How two symbols connect. args: [\"<a>\", \"<b>\"].",
		func(_ context.Context, a []string, o, _ io.Writer) error { return cmdPath(a, o) })
	ro("ctx.hubs", "Most-connected nodes. args: [\"--top\", \"10\"].",
		func(_ context.Context, a []string, o, e io.Writer) error { return cmdHubs(a, o, e) })
	ro("ctx.search", "Literal/regex sweep of the SOURCE TREE under the cwd (Go RE2); same file set as the extractor. args: [\"<regex>\", \"--ext\", \".go,.ts\", \"--path\", \"dir/\"]; returns [{file,line,text}] (with \"--count\" a number).",
		func(_ context.Context, a []string, o, _ io.Writer) error { return cmdSearch(a, o) })
	ro("ctx.verify", "Check citations before acting on them: node id/exact label or file:L10-L20. args: [\"<claim>\", ...]; throws when a claim does not hold.",
		func(_ context.Context, a []string, o, _ io.Writer) error { return cmdVerify(a, o) })
	reg.MustRegisterModules("ctx", jsFiles, "js")
	return reg
}

// The ops call the cmdX functions directly, so two guards the CLI front door
// provides must be restated for a program (clicodemode WithValidate /
// WithCheckResult, v0.6.1; issue muthuishere/clicodemode#6):
//
//   - a mistyped flag is rejected (checkFlags runs only in Run, so it would be
//     silently ignored);
//   - a name that does not exist resolves to a NEAR node whose stderr note
//     ("resolved via fuzzy") a program never sees. A program wants an error, not
//     a plausible wrong answer, so fuzzy resolution is refused unless the caller
//     passes --fuzzy explicitly.

// opVerb maps an operation name to the CLI verb whose flag set applies.
func opVerb(op string) string {
	verb := strings.TrimPrefix(op, "ctx.")
	if verb == "change_plan" {
		return "change-plan"
	}
	return verb
}

func validateFlags(op string) func(args []string) error {
	verb := opVerb(op)
	return func(args []string) error { return checkFlags(verb, args) }
}

func rejectFuzzy() func(args []string, stdout []byte) error {
	return func(args []string, stdout []byte) error {
		var probe struct {
			ResolvedVia string `json:"resolved_via"`
			Node        *struct {
				ID string `json:"id"`
			} `json:"node"`
		}
		if json.Unmarshal(stdout, &probe) != nil || probe.ResolvedVia != "fuzzy" || hasArg(args, "--fuzzy") {
			return nil
		}
		got := ""
		if probe.Node != nil {
			got = probe.Node.ID
		}
		return fmt.Errorf("%q was not found exactly; the nearest match is %q. Use an exact label from ctx.query, or pass \"--fuzzy\" to accept a near match", firstPositional(args), got)
	}
}

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func firstPositional(args []string) string {
	for _, a := range args {
		if len(a) > 0 && a[0] != '-' {
			return a
		}
	}
	return ""
}
