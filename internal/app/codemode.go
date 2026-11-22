package app

import (
	"context"
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
		// cmdtree would PREPEND --json; these verbs parse positionals first, so
		// the flag is appended here and WithJSONResult gets "" (parse-only).
		withJSON := func(ctx context.Context, a []string, o, e io.Writer) error {
			return run(ctx, append(append([]string{}, a...), "--json"), o, e)
		}
		reg.MustRegister(cmdtree.FromRunner(name, desc, withJSON,
			cmdtree.WithJSONResult(""),
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
