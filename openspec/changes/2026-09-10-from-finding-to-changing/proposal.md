# ADR 32 — from finding to changing: the store must reach the edit

Status: DRAFT — 2026-09-10. Owner-directed after observing agents fall back to
PowerShell/python scripts to find-and-change, on Windows and macOS alike.
No product code until sign-off.

## The observation

Agents use us to FIND, then abandon us to CHANGE. On Windows that means a
PowerShell script; on macOS, sed or python. The moment they fall back they
re-explore the repo blind, and every token the store saved is spent again.

The owner's instinct was that grep/sed/PowerShell are not actually fast, and
that a prebuilt index should beat them by a wide margin. **Measured today on
the Linux kernel store (2,848,839 nodes / 5,539,232 edges, HEAD binary,
`~/ctx-golden-corpora/linux`), that instinct is half right — and the half that
is wrong is the half that matters for editing.**

| operation | ripgrep | ctx-optimize | |
|---|---|---|---|
| find symbol, signature, doc (`card`) | 2.06 s | **< 0.01 s** | **200×+ faster** |
| callers + blast radius + tests (`change-plan`) | 2.06 s | **< 0.01 s** | **200×+ faster** |
| `explain` · `path` · `hubs` | — | **< 0.01 s** | index path |
| free-text search (`query`) | 2.06 s | 2.96 s | **1.4× slower** |
| reverse impact (`affected`) | 2.06 s | 3.38 s | **1.6× slower** |

(ripgrep: `rg -n blk_mq_submit_bio`, 38 matches in 6 files, best of 3: 2.64 /
2.07 / 2.06 s. ctx-optimize verbs on `blk_mq_submit_bio`, best of 3.)

So the speed is real and already shipped — **where the index is used**. The
problem is not that we are slow. It is that the index reaches three verbs and
stops.

## Three separate failures, not one

**1. Two verbs bypass the index and lose to grep.** Only `card` has an index
path (`cardViaIndex`, `internal/app/app.go:1863` — the sole `*ViaIndex`
function in the codebase). `query` and `affected` deserialize all 2.85M nodes
per invocation. `affected` is the "what breaks if I change this" verb — the
pre-edit question — and it is the slowest thing we ship and slower than the
tool it replaces.

**2. A `calls` edge carries no position, so no edit can be driven from it.**
Fields are exactly `source, target, relation, confidence, weight, metadata`.
The store knows *`cmdAdd` calls `Store.Merge`*; not *where inside `cmdAdd`*.
One struct is the cause — `internal/extract/code/code.go:173`:

```go
type callSite struct {
    callerID string // innermost enclosing decl (or file) id
    callee   string
    recv     string
    file     string
}
```

The tree-sitter walk stands on a call node that HAS byte offsets and discards
them. Worse, `code.go:592-640` dedupes on `seen[callerID\x00targetID]`, so a
caller invoking the target three times emits ONE edge — even with a line
number that would find one site of three, which is worse than grep.

**3. Text is not in the graph at all.** Literal strings, config values,
comments and struct-tag contents are unindexed by design, and the usage card
routes those to grep. A rename spanning Go + SQL + YAML — the normal case —
has no path through the store even in principle.

Declaration nodes carry `L#-L#`. Call sites carry nothing. That asymmetry is
the whole bug, and it is why a 200× speed advantage does not reach the edit.

## What to add, in dependency order

### P0 — put `affected` and `query` on the index (biggest win, no new concepts)

`card` proves the index answers in under 10 ms on a 2.85M-node store.
`affected` answers the same shape of question — walk edges from one resolved
node — and takes 3.38 s because it loads the whole graph first. The index
already exposes `edgesByEndpoint` and `EdgesTouchingOrdered`
(`internal/store/index.go`), which is exactly what a reverse walk needs.

Generalise `cardViaIndex` into an index-backed traversal used by `affected`,
`change-plan --include-ambiguous` and the depth walks. Target: `affected` under
50 ms on the kernel, i.e. 40× faster than ripgrep instead of 1.6× slower.

`query` is a harder case — free text has no key to seek — but it should not
pay a full parse either. Investigate a term→offset index for labels, and keep
the full walk only for the fuzzy tier.

### P1 — put positions on call sites (foundational, small)

Add `line`, `col` and byte range to `callSite`; stop collapsing repeats — keep
one edge per (caller, target) but carry every site, or emit one edge per site.
Everything below is blocked on this and nothing below is hard once it lands.

Touchpoints: `internal/extract/code/code.go` (struct + emission), the edge
metadata contract in `internal/schema`, golden snapshots.

Cost to watch: edges grow. Measure store size and gather time on linux before
and after; if per-site edges are too heavy, carry sites as a metadata list on
one edge.

### P2 — `sites <symbol>` — the verb an agent can edit from

```
ctx-optimize sites Store.Merge --json
```

Every place that must change for that symbol: the declaration, every call
site, every import that names it — each with exact `file:line:col`, ordered,
machine-readable, AMBIGUOUS ones marked and separated.

It must ride the offset index (`internal/store/index.go` — `NodeByID`,
`NodesByLabel`, `edgesByEndpoint`), the same path that makes `card` answer in
under 20 ms on the kernel store. It must NOT go through `query`, which
deserializes the whole graph (4 s on linux, ~97% of it parsing).

**This is THE deliverable.** One call returns the files and the candidate
locations; the agent opens those lines and edits. No tree scan, no
re-exploration, identical output on every OS — which is what removes the
PowerShell/sed fallback without us ever writing a byte.

Candidates are candidates, not instructions: AMBIGUOUS sites are listed under
their own heading and never mixed in with resolved ones, exactly as the
traversal verbs already do.

### P3 — emit an EXECUTABLE EDIT SPEC. We never write. (owner, 2026-09-10)

**Why the agent scripts.** Arithmetic, not ignorance: 38 sites across 6 files
is 38 edit calls or 1 python script, so it writes the script. But look at what
that script actually contains — it FINDS (regex, whole-file scan) and then
REPLACES. The finding half is the dangerous half: it hits comments, string
literals and unrelated same-named symbols, and nothing reviews it.

So we do not stop the agent scripting. **We make the script trivial** by
removing all the logic from it.

```sh
ctx-optimize sites Store.Merge --rename Store.Absorb --json
```

emits one record per site, byte-exact:

```json
{"file":"internal/app/app.go","start":48211,"end":48222,
 "line":1274,"col":13,"old":"Store.Merge","new":"Store.Absorb",
 "confidence":"EXTRACTED"}
```

Rules that make it directly executable:

- **Byte offsets, not line/col arithmetic** — no re-scanning, no regex, no
  ambiguity about which `Merge` on the line.
- **Sorted descending by offset within each file**, so applying them in order
  never invalidates a later offset. The agent does not have to think about
  this; the ordering IS the guarantee.
- **`old` is carried on every record** so the applier can assert before
  writing and abort on drift. A stale store fails loudly instead of corrupting
  a file.
- **AMBIGUOUS sites are in a separate list**, never in the apply set. They are
  reported for the agent to judge. This is the existing traversal doctrine
  applied to edits.

The apply is then a mechanical loop with no logic in it — seek, assert, write.
We **document that one-liner** (python, node, PowerShell) in the usage card so
the agent does not have to invent it, and shipping a small `--exec` helper
stays open (open question 5). Either way the binary writes nothing.

The distinction that matters:

| | today | with the edit spec |
|---|---|---|
| what the agent's script does | finds AND replaces | applies known offsets |
| how sites are found | regex, whole-file scan | tree-sitter, from the store |
| comments / strings / same-named symbols | hit | never in the set |
| unprovable references | silently renamed | excluded, reported |
| failure mode | silent corruption | `old` assert fails, aborts |
| who writes to disk | the script | the script — never us |

We stay read-only, which is what keeps the binary deterministic and auditable.
The agent still owns the change; we make its change correct.

### P4 — the text lane (this is what "across languages and text" means)

The `.txt` decision (`internal/extract/markdown/markdown.go:433`) proved that
indexing prose wholesale is 95% junk and poisons ranking. The answer is not to
reverse it but to index a NARROW, high-value slice as `kind=literal`:

- string literals that are identifier-shaped (`"user_id"`, `"POST /v1/orders"`)
- config keys and values in known formats (yaml/toml/json/env/ini)
- struct tags and annotation arguments
- SQL identifiers inside query strings

Then `sites user_id` spans the Go field, the struct tag, the migration and the
YAML — which is exactly the change grep does badly and no competitor does at
all. Gate it the way `.txt` was gated: measure junk ratio and ranking
displacement on real corpora BEFORE shipping, and drop it if the numbers look
like `.txt` did.

### P5 — make the edit loop fast (already an open ADR, now urgent)

An edit loop re-gathers after every change, so the warm-regather loss is
directly in this path: we lose 6/6 to CodeGraph, 10–31×, and on five of six
corpora our warm run is slower than our cold one. "Change fast" is not
possible while a re-gather costs seconds. The incremental ADR stops being a
benchmark row and becomes a blocker for this lane.

### P6 — the human side

`serve` already onboards a repo in a browser. Extend it with the same output:
show the edit plan, let a person see every site before it is applied. The
boundary census is the most screenshot-able artifact we own; an edit preview
is the most useful one.

### P7 — one call per intent, and a skill that says so (owner, 2026-09-10)

**On pipes: do not build verb-to-verb shell pipes.** Three reasons, one of
them fatal:

1. **Each stage re-loads the store.** Measured on the kernel: `query` 2.96 s,
   `affected` 3.38 s. A three-stage pipe pays three full deserializations of
   2.85M nodes. Composition inside one process pays one.
2. **Pipes are a shell dialect.** bash, zsh and PowerShell disagree about
   quoting and object-vs-text streams. Building the answer on shell syntax
   reintroduces the exact Windows problem this ADR exists to remove.
3. **The repo already chose the better doctrine.** `change-plan` is documented
   as "one call replaces query+card+affected+test-grep". That is composition
   in the binary, and it is correct.

So the direction is MORE composed verbs, not pipes:

- Keep `--json` / `--ndjson` on every verb. That is the pipe that matters —
  ours into the AGENT'S tooling, not ours into ours.
- **Add batch input** so N symbols cost one call, not N: `sites A B C` and
  `--stdin` for a list. This is the real "one command for all" — the round
  trips an agent pays today are per-symbol, not per-stage.
- Each new lane gets ONE verb that answers the whole intent. The refactor lane
  is `sites <symbol> --rename <new> --json`: locations, candidates, ambiguity
  and the edit spec in a single call.

**The skill already teaches intent → one command** (`instructions.md:30-41` is
an intent table). Two gaps to close:

- The refactor lane is absent — there is no row for "I am renaming this".
- **`ctx-optimize search` is buried.** It is documented at `instructions.md:125`
  as a parenthetical fallback, yet it is the direct answer to the Windows
  case that started this ADR: a built-in literal sweep over the extractor's
  own file set, same gitignore, same skip-dirs. Measured on the kernel:
  3.18 s vs ripgrep 2.06 s — slower, but it exists where `rg` does not, which
  is why an agent on Windows reaches for PowerShell instead. **It should be a
  first-class row, not a footnote.** (It is also a candidate for P0's index
  treatment; 3.18 s for a literal sweep is a scan, not a lookup.)

## What we do NOT do

- No LLM in the loop. The binary stays deterministic — `rename` is a
  mechanical span replacement, not a reasoning step.
- No LSP. Type-exact refactors stay Serena's; we do not typecheck, and the
  plan says so where it cannot prove a reference.
- **No writes, at all.** The binary never opens a user's source file for
  writing. `--apply` was considered and rejected: the value is in the exact
  spec, and the moment we write we inherit three operating systems' worth of
  atomicity, permissions and undo semantics for no gain the agent cannot get
  from a five-line apply loop.
- No indexing of all prose. `.txt` settled that with numbers.
- No blocking grep. The agent may still grep; we just stop making it necessary.

## Success check (pre-committed)

On a pinned corpus, an agent asked to rename a symbol with call sites in 3+
files gets every file and every candidate location from **one `sites` call**,
and makes the edits with **zero grep/rg/PowerShell/sed calls** — no tree scan
at any point. Measured on `benchmarks/session/session.py`: fewer tool calls
and lower wall time than the same task done by a grep-armed agent.

Secondary, and the one that proves P0: `affected` on the kernel under 50 ms.
It is 3.38 s today and ripgrep does the equivalent scan in 2.06 s, so the verb
an agent runs immediately before editing is currently the slowest thing we
ship.

## Open questions

1. One edge per call site, or one edge carrying a site list? Decide on
   measured store growth on linux, not taste.
2. RESOLVED 2026-09-10 (owner): we never write. `sites --rename` emits a
   byte-exact, descending-sorted, assert-carrying edit spec; the agent applies
   it with a logic-free loop. `--apply` explicitly rejected.
3. Is the text lane (P4) in this ADR or its own? It carries the only real risk
   of the five, and it is the one that makes a cross-language rename possible.
4. Does `sites` subsume `affected`, or sit beside it? They answer adjacent
   questions ("what breaks" vs "what do I edit") and P0 puts both on the index.
5. Do we ship a tiny `--exec` applier (still not us writing — a documented
   loop the agent runs), or only document the one-liner per platform? Decide
   on whether agents reliably write the loop correctly when told to.


## Addendum — 2026-09-14: P0 shipped; parallel lookups measured and rejected

P0 landed in `c0e68c5`: `affected` answers from the index (linux, interleaved
medians: 3.5 s → 7-95 ms on ordinary walks; the 71k- and 110k-row
`--include-ambiguous` walks break even via a per-level pre-payment budget).
Output is byte-identical to the full load across 20 cases.

**Do not retry parallel lookups on a shared file without mmap.** Fanning a
level out across goroutines (results slotted by position, race-clean) made the
71,519-row walk slower, measured side by side on linux:

| workers | wall | user | sys |
|---|---|---|---|
| 1 | 1.64 s | 1.74 s | 0.74 s |
| 4 | 1.61-1.75 s | 2.36-2.61 s | 4.09-4.81 s |
| 16 | 2.94-3.17 s | 3.41-3.49 s | 34.5-36.0 s |

The cost is kernel time, not Go: GOGC=800 cut GC cycles 100 → 11 with no CPU
change, and giving each worker its OWN file descriptors (a pool of Lookup
sessions) left sys time at 36 s — concurrent preads of one file serialize per
vnode on macOS, not per descriptor. The remaining lever is memory-mapping the
index files, which is platform-specific (Windows needs its own path) and was not
justified for walks that already break even. Reverted; serial walk kept.
