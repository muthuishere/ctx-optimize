# ADR — the wiki stops being a default

Status: **DRAFT** — 2026-07-27. Owner-directed ("can we make default off wiki
seems unnecessary no one uses"). No product code touched yet; this is the
sign-off document, and the backward-compatibility question is the whole of it.

## Context — the measurement that forced this

Cross-tool benchmark re-run on 2026-07-26/27, `v0.11.0-22-g4e30bf1`,
Apple M5 Pro, against linux v6.9 (84,300 files):

| run | wall clock | nodes |
|---|---:|---:|
| `add ~/ctx-golden-corpora/linux` | **1,475.37s** | 2,849,719 |
| `add ~/ctx-golden-corpora/linux --no-wiki` | **157.58s** | 2,849,719 |

**The wiki is 1,317.8s — 89.3% of the cold gather — for an identical graph.**
The store it produces is not even large in proportion:

```
graph/     2.0GB    2.85M nodes / 5.54M edges   ← what every verb reads
wiki/      250MB    60,390 pages                ← 12% of bytes, 89% of time
manifest   8.2MB
```

And it decides the head-to-head. graphify 0.9.12 on the same tree takes 531.97s:

| | ctx-optimize (today) | ctx-optimize `--no-wiki` | graphify |
|---|---:|---:|---:|
| cold, linux 84.3k files | 1,475s | **158s** | 532s |

So the default costs us a 3.4× win and turns it into a 2.8× loss. Nothing about
the answers changes — the wiki has never been the query source.

The cost is **non-linear**, which is why this went unnoticed: on the benchmark
corpora the wiki is free.

| corpus | files | `add` | `add --no-wiki` |
|---|---:|---:|---:|
| corpus-flask | 236 | 0.42s | 0.32s |
| corpus-gin | 130 | 0.36s | 0.40s (noise) |
| linux | 84,300 | 1,475s | 158s |

Every corpus the published benchmark uses is ≤754 files. The regime where the
default is ruinous is exactly the regime we never measured.

This is the same defect #9 recorded from chromium (434,597 pages / 1.7GB in one
directory, 8s just to re-list it on later gathers). #9 made the wiki
*configurable* and left the default alone. That was half a fix: linux and
chromium have no `.ctxoptimize/config.json` at all, so the config lever never
reaches the repos that need it.

## Decision

`Config.WikiEnabled()` (`internal/project/project.go:198-203`) flips: **absent
means DISABLED**. The wiki becomes an opt-in artifact built by the `wiki` verb.

Non-negotiable: **"off" must never mean "unavailable."** `ctx-optimize wiki`
keeps building a complete wiki on demand, which is already how
`internal/app/wikiconfig_test.go:61` pins it.

## Backward compatibility — the actual work

Four populations exist today. The flip must be a no-op for the first and a
loud, recoverable change for the rest.

### 1. Repos scaffolded by `init` / `up` since #9 — NO CHANGE, automatically

`Scaffold` writes `"wiki": true` **explicitly**
(`internal/project/project.go:332-337`), with a comment saying it does so even
though absent already meant enabled. That decision — made for discoverability —
is what makes this flip safe: every repo onboarded since then carries an
explicit `true`, and explicit `true` still wins after the flip. **They keep
their wiki with no action and no notification.**

This is the single most important BC fact in the ADR: the population that opted
in through the supported path is untouched.

### 2. Repos with a config that predates the `wiki` key — LOSE the auto-wiki

A `config.json` written before #9 has no `wiki` key, so it reads as absent, so
the wiki stops regenerating on `add`. Recovery is one of:

- `ctx-optimize wiki` — builds a complete wiki, any time; or
- add `"wiki": true` to `.ctxoptimize/config.json` — restores the old behaviour
  permanently.

### 3. Repos with no `.ctxoptimize/` at all (linux, chromium, any ad-hoc `add <path>`) — LOSE the auto-wiki

This is the population the change is FOR. Same two recovery paths; neither
requires a config file (`wiki` is a verb).

### 4. Stores that already contain a wiki — THE HAZARD

A repo in population 2 or 3 that has run `add` before has `<store>/wiki/` on
disk with real pages. After the flip, `add` no longer refreshes it. The wiki
does not disappear — **it silently goes stale**, and a stale wiki is strictly
worse than no wiki: it reads as current and cites lines that have moved.

The motto applies directly. We can say no ("this store has no wiki") but we must
not be wrong ("here is a wiki", built against code from three commits ago).

Options, to be decided with the owner:

- **(a) Grandfather** — if `<store>/wiki/` exists and the config is silent, keep
  regenerating. Existing users see literally no change; only new/never-wikied
  stores get the fast default. Cost: linux/chromium users who ran `add` once
  keep paying 1,317s until they opt out. Weakest on the measurement that
  motivated the change.
- **(b) Skip and say so (RECOMMENDED)** — stop regenerating, and have `add`
  print one line whenever it skipped a wiki that exists on disk and is now
  older than the graph: `wiki: NOT refreshed (stale since <ts>) — rebuild with
  'ctx-optimize wiki', or set "wiki": true`. Fast default everywhere, and the
  staleness is stated rather than discovered.
- **(c) Delete it** — remove the stale directory on the first skipping gather.
  Honest, no stale bytes; but it destroys generated content the user may have
  linked to, on an upgrade they did not ask for. Deleting user-visible data as
  an upgrade side effect is not ours to do.
- **(d) Leave it silent** — the "just flip the bool" version. Rejected: this is
  precisely the wrong-not-absent failure the project exists to avoid.

Recommendation: **(b)**. It takes the win in every population, and the only
behaviour that changes without a message is a wiki that never existed.

### 5. `sync` / `up` autosync paths — ALREADY off

`internal/app/autosync.go:180,282` already pass `--no-wiki`. Unaffected.

## Also in scope

- **`--wiki` flag** for symmetry with `--no-wiki`: force a wiki for one gather
  without editing config. Cheap, and it makes the default reversible per-run.
- **`Scaffold` stops writing `"wiki": true`.** New repos should get the new
  default. The key stays documented in `instructions.md` as the opt-in. (Note
  this reverses the 2026-07-26 owner request to scaffold `"wiki": true`; that
  request came before the linux measurement existed.)
- Docs carrying the old default: `.ctxoptimize/instructions.md` (managed block),
  `internal/skills/bundled/ctx-optimize/SKILL.md` + `references/sync.md`,
  `docs/VISION.md`, this repo's `CLAUDE.md` pointer block.

## Tests that must change (each is a pinned promise being re-pinned)

- `TestWikiAbsentMeansEnabled` (`internal/app/wikiconfig_test.go`) — inverts to
  `TestWikiAbsentMeansDisabled`. Its comment states the old guarantee ("absent
  means enabled, so no existing repo silently loses its wiki"); the replacement
  must state the new one and the reason the guarantee moved.
- `internal/app/app_test.go:47` asserts `wiki/index.md` exists after a plain
  `add` — must gain an explicit `"wiki": true` or move to the `wiki` verb.
- New: a store with an existing wiki + silent config emits the staleness line
  (option b).
- New: `--wiki` forces generation with no config present.

## Verification

- `task ci` green.
- `task golden` hermetic + corpus tiers; judged floors must not move
  (linux-block 16.5, newtonsoft 13.0) — the wiki is not on the query path, so a
  moved score means something else broke.
- Re-measure `add linux` after the change; expect ~158s, i.e. the `--no-wiki`
  number, and re-state the graphify comparison as a 3.4× win.

## Not claimed

- No claim that "no one uses" the wiki — that is the owner's read of demand, and
  it is the reason for the change, but it is not measured here. What IS measured
  is the price of building it for everyone by default.
- The 89.3% figure is one repo at one size. The small-corpus rows show the cost
  is negligible below ~1k files; the shape between 1k and 84k is unmeasured.
- Query latency at scale (4.16s warm / ~11s cold on the 2.0GB linux graph) is a
  SEPARATE and larger problem. This change does not touch it, and fixing the
  wiki default must not be reported as fixing that.
