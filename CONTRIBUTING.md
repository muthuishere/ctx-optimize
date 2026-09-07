# Contributing

Go, built with [Task](https://taskfile.dev). Two binaries (`ctx-optimize` +
`ctx-optimize-adapters`), no LLM, no DB — deterministic by design (see `AGENTS.md` for the
fuller architecture notes if you're touching the store format itself).

## Running the gate

```bash
task ci      # exactly what CI runs: lint, test, build, then smoke-tests both binaries
```

That expands to:

```bash
task lint    # go vet + gofmt -l (a formatting drift is a failure, not something lint fixes for you)
task test    # go test ./...
task build   # both binaries, version-stamped from git
```

Run `task ci` before opening a PR — it's the same gate CI runs, so a green `task ci`
locally means no surprises in review.

## What a good PR looks like

- **Deterministic stays deterministic.** No LLM calls, no network access except the
  explicit `remote push`/`remote pull`/`update` paths — if your change needs either,
  that's a design conversation first, not a PR.
- **Secrets are names, never values.** Commands and adapter scripts reference env var
  *names*; the shell expands them at run time. A PR that writes or prints a secret value
  anywhere (logs, store files, error messages) will be rejected regardless of what else it
  does.
- **gofmt-clean.** `task lint` fails on unformatted files rather than silently
  reformatting them — run `gofmt -w` yourself before pushing.
- **New adapters** (`.ctxoptimize/adapters/*.js|py|sh`) must print batch JSON to stdout;
  there's a `.sample` template already in the repo — copy its shape rather than
  reinventing the output format.

## Filing an issue

If you're reporting a bug in what the graph extracted (a wrong node, a missing edge), run
`ctx-optimize verify "<label or file:L10-L20>"` first — the tool has a built-in mechanism
for confirming whether a citation is actually stale, and that output is more useful in a
bug report than "the graph is wrong."
