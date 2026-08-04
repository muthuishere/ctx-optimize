# Spikes — what the stale-wiki remedy has to survive

Run 2026-08-04 on `v0.11.0-23-g8621d6d`, Apple M5 Pro, against a scratch store
(`CTX_OPTIMIZE_STORE` in a temp dir) plus this repo as the 4.3k-node corpus.
Every number below is measured; the ADR's 1,475s/158s linux pair is carried
forward from 2026-07-26 and NOT re-run here.

## S1 — the staleness signal exists and is one `stat`, not a walk

The question: after the flip, can `status` tell "wiki older than graph" apart
from "wiki current" without paying defect #9's directory-listing tax (chromium:
8s just to re-list 434,597 pages)?

Yes. Both artifacts are written by atomic rename, so their mtimes are exactly
the two events we need to compare:

```
add .                     → wiki/index.md   mtime 1785864826
(edit, commit)
add . --force --no-wiki   → graph/nodes.ndjson mtime 1785864841
```

15s apart, in the right direction, from two `os.Stat` calls on fixed paths —
`graph/nodes.ndjson` and `wiki/index.md`. No `ReadDir`, no `WalkDir`, so the
cost is O(1) regardless of whether the wiki has 5 pages or 434,597.

**Consequence for the design:** the check MUST read `wiki/index.md` specifically
and MUST NOT enumerate `wiki/`. A `ReadDir` here would reintroduce #9 into a
verb people run constantly.

## S2 — "wiki dir exists" is NOT "wiki exists"

This repo's own store is the counter-example:

```
~/ctxoptimize/wkdemo/wiki/    → 0 entries
```

`store.New` pre-creates `graph/ wiki/ cards/ hooks/` unconditionally
(`internal/store/store.go:130`), so an empty `wiki/` is the NORMAL state for
every repo that has only ever gathered with `--no-wiki` — which, after this
change, is every repo by default.

**Consequence:** keying the status line off `os.Stat(wiki/)` would print a
staleness warning on virtually every store in existence, about a wiki that was
never built. The predicate must be `wiki/index.md` exists — the file
`wiki.Generate` always writes — not the directory.

## S3 — the skip line already exists; only the staleness half is missing

`add --no-wiki` today already prints:

```
wiki: skipped — the graph is the query source; build it any time with `ctx-optimize wiki`
```

So option (b)'s "say so" is half-built. What it does not say is whether a
*stale wiki is sitting on disk right now*. That is the sentence worth adding,
and S1 shows `status` can compute it for free.

**Consequence:** no new message plumbing in the gather path. The gather line
stays as-is; `status` gains the one line that needs disk state.

## S4 — a stale wiki is a recurring tax, not just a misleading artifact

`UpdateManifest` (`internal/store/store.go:433-477`) re-fingerprints every file
under the store on every gather, and its skip list covers only
`manifest.json` / `config.json` / `source.json` / `sources.json` / `*.tmp`.
`wiki/` is not skipped — so a `--no-wiki` gather still SHA-256s the whole stale
wiki it just refused to regenerate. Confirmed directly: after
`add --force --no-wiki`, the manifest still carried all 562 wiki entries.

Measured, `add --force --no-wiki` on this repo, 3 runs each:

| wiki on disk | wall clock |
|---|---:|
| none | 0.55 / 0.57 / 0.57 s |
| 562 pages, 2.3MB | 0.56 / 0.57 / 0.57 s |
| 60,000 pages, 234MB (synthetic, ≈ linux's 60,390 / 250MB) | 1.79 / 1.62 / 1.69 s |

**≈1.1s per gather, forever, for an artifact nothing reads.** Small next to the
1,317s generation cost the ADR is built on — this is not a headline number and
must not be reported as one. What it does settle is a design question: the user
needs a way to *remove* the stale wiki, not merely be told it is stale, because
"leave it and ignore it" keeps costing something on every future gather.

**Consequence:** the remedy is two halves — `status` diagnoses, and a scoped
delete removes. Diagnosis alone leaves a permanent tax with no escape hatch
short of deleting the entire graph.

## S5 — why the escape hatch cannot be `store delete`

`store delete` (`internal/app/app.go:1234-1301` → `internal/store/delete.go`)
removes the target store's own artifacts and, with `--nested`, every module
store beneath it. Its guard requires a `graph/` dir to exist — i.e. it is
explicitly a *whole-store* verb.

Pointing a user at it to clean up a wiki costs them the graph every verb reads
(4,307 nodes here, 2.85M on linux) plus a re-gather, to reclaim an artifact
they were not using. It is option (c)'s destructiveness with collateral damage.

`wiki/` is self-contained — nothing outside it links in, `wiki.Generate`
rebuilds it in full from the graph in 0.082s on this repo (562 pages) — so a
scoped removal is both safe and reversible.

**Consequence:** `wiki --delete` removes `wiki/` and refreshes the manifest.
The graph is untouched, and `ctx-optimize wiki` puts the pages back.

## Not measured

- The 1k–84k file regime for wiki GENERATION cost is still unmeasured; the ADR
  already states this and nothing here changes it.
- The synthetic 60k-page wiki matches linux's page count and byte size but not
  its page-size distribution; the ≈1.1s should be read as an order of
  magnitude, not a pinned figure.
