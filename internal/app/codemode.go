package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

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

func codeRegistry() *codemode.Registry {
	reg := codemode.NewRegistry(version.Version)
	ro := func(name, desc string, run cmdtree.Runner) {
		run = strictVerb(name, run)
		reg.MustRegister(cmdtree.FromRunner(name, desc, run,
			cmdtree.WithJSONResult("--json"),
			cmdtree.WithFlagsLast(), // these verbs parse positionals first
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
	return reg
}

// strictVerb gives a code op the same guard rails the CLI front door has and a
// program cannot see otherwise. The ops call the cmdX functions directly, so
// without this a mistyped flag is silently ignored (checkFlags runs only in Run)
// and a name that does not exist resolves to a NEAR node whose stderr note
// ("resolved via fuzzy") never reaches the program. A program wants an error,
// not a plausible wrong answer: fuzzy resolution is refused unless the caller
// passes --fuzzy explicitly.
func strictVerb(op string, run cmdtree.Runner) cmdtree.Runner {
	verb := op[len("ctx."):]
	if verb == "change_plan" {
		verb = "change-plan"
	}
	return func(ctx context.Context, args []string, stdout, stderr io.Writer) error {
		if err := checkFlags(verb, args); err != nil {
			return err
		}
		var buf bytes.Buffer
		if err := run(ctx, args, &buf, stderr); err != nil {
			return err
		}
		var probe struct {
			ResolvedVia string `json:"resolved_via"`
			Node        *struct {
				ID string `json:"id"`
			} `json:"node"`
		}
		if json.Unmarshal(buf.Bytes(), &probe) == nil && probe.ResolvedVia == "fuzzy" && !hasArg(args, "--fuzzy") {
			got := ""
			if probe.Node != nil {
				got = probe.Node.ID
			}
			return fmt.Errorf("%s: %q was not found exactly; the nearest match is %q. Use an exact label from ctx.query, or pass \"--fuzzy\" to accept a near match", op, firstPositional(args), got)
		}
		_, err := stdout.Write(buf.Bytes())
		return err
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
