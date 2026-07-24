# Design — lever 3 (lazy autosync on query) + `sync` = resync

Status: **ACCEPTED** — 2026-07-24 (owner sign-off this session). Supersedes the
code-shaped parts of `proposal.md` after `spikes.md` reshaped the levers.

**Owner decisions (LOCKED 2026-07-24):**
- **D1 = YES** — `sync` becomes the first-class resync verb (definition + docs +
  `--adapters`/`--all`).
- **Default mode = `off`** — deterministic; opt in per repo/global/env.
- **`sync --all` = FULL refresh incl. sources** — `--all` also dials native
  sources (DB/bucket/queue schemas), so it supersedes D2's "reserve for later."
  `--adapters` = code+local+adapters, no dial. Auto-sync (lever 3) is STILL
  code-only, never `--all`.

Reads the two siblings as given:
- `proposal.md` — the original ADR; its Part-A "AST-per-file cache" thesis was
  **invalidated** by the spike (saves ~78 ms of a ~730 ms sync; deferred).
- `spikes.md` — the measurements. Levers **1** (0-change short-circuit) and **2**
  (persisted wazero compile cache) are BUILT + measured (commit 8319814). This
  doc finalizes the **remaining** piece: **lever 3 — lazy autosync on query**.

## What we're deciding here

1. **`sync` becomes the resync verb** (owner's call this session) — see D1.
2. **Lever 3**: config-gated, code-only auto-resync fired by a read verb, via a
   detached child (mechanism already LOCKED in `proposal.md`).
3. **`sync --adapters` / `--all`** — opt back into the adapter refresh (owner's
   mid-session ask).

Answer quality is a separate, out-of-scope follow-up.

---

## D1 — `sync` IS resync (recommended)

**Owner's framing:** "if resync is already there, sync should be resync only."

**Where it stands today.** `sync` (app.go:73) is literally `add . --no-adapters`
on the cwd. After levers 1+2 that path is *already* an incremental resync:
0-change short-circuits in ~20 ms, a 1-file edit re-parses one file + skips
git-history/wiki when unchanged. So the behavior is here; what's missing is that
`sync` isn't **named/owned** as "the resync verb" and its scope isn't stated.

**Decision (recommended):** make `sync` the first-class **resync** verb, defined
by three properties, and document it as such:

- **Scope = the repo you're in** (cwd / `--path`), never a foreign path — already
  enforced ("sync takes no path"). `add <path>` stays the verb for another repo.
- **Code by default, no dial.** `sync` refreshes the LOCAL producers only
  (code, markdown, manifests, git-history, deplink). It does **not** run adapter
  scripts and does **not** dial native sources — a resync must never touch the
  network or credentials. (Today's `--no-adapters` already gives code+local;
  native sources are only ever refreshed by an explicit `add DBVAR` / `up` /
  `capture`, so `sync` already leaves them alone.)
- **Incremental by construction** — it rides levers 1+2; O(changed files).

This makes the mental model clean and is exactly what lever 3 spawns:

```
add <path>   — (re)gather ANY repo's store, full pipeline (adapters + sources on up/add)
sync         — resync THIS repo, code+local only, incremental, no dial     ← the resync verb
up           — front door: bootstrap-or-refresh, idempotent
```

**Not proposed:** removing/renaming `add` or breaking `sync`'s current CLI. This
is a *definition + docs + one flag*, not a rewrite. `sync` keeps working; we just
commit to what it means.

## D2 — `sync --adapters` / `--all`

Add two opt-in flags to the `sync` verb only:

- `sync --adapters` — resync **and** re-run the declared/dropped adapter scripts
  (drops the internal `--no-adapters`).
- `sync --all` — **full refresh**: code + local + adapters **+ native sources**
  (dials DB/bucket/queue schemas, same path as `up`). Uses credentials by the
  operator's explicit choice — never on a background/auto path.

Tiny change in the `Run` dispatch (app.go:73-78): conditionally append
`--no-adapters` unless `--adapters`/`--all` is present. No new plumbing —
`gatherInto` already takes `skipAdapters`.

> Note: lever-3 auto-sync ALWAYS runs the code-only path regardless of this flag.
> A background read-triggered sync must never run adapter scripts (arbitrary
> commands) or dial. `--adapters` is a **human**, explicit choice.

---

## Lever 3 — lazy autosync on query

### Config surface

- **Project** (committable, team-wide): `.ctxoptimize/config.json` →
  `"autosync": "off" | "lazy" | "block"`. New `Config.Autosync` field.
- **Global** (machine default): `~/ctxoptimize/config.json` → same key. Settable
  via `ctx-optimize config autosync lazy [--project]` (new key in `cmdConfig`;
  values normalize case-insensitively).
- **Env override** (CI/hook): `CTX_OPTIMIZE_AUTOSYNC=off|lazy|block`.
- **Precedence:** env → project → global → **`off`** (default). Unknown value →
  `off` (fail-safe, deterministic-by-default doctrine).

### The trigger is the tree-signature, NOT git-HEAD (important correction)

`proposal.md` Part B said "check freshness first." **Freshness = git HEAD**, which
does not move on an *uncommitted* edit — yet the ADR's own success check is "edit
a file, query twice, get the fresh answer." So the staleness gate for autosync
must be **lever 1's `treeSignature`** (stat-fingerprint of the working tree),
compared against the recorded `Source.TreeSig`. That is the same cheap
stat-scan (~ms) that already gates the short-circuit, and it catches uncommitted
edits. A recompute mismatch (or missing TreeSig) ⇒ stale.

- Cost: one stat-scan over the tree per read verb when `autosync != off`. ~ms on
  500 files, ~tens of ms on 12k. In `off` (default) we do **zero** extra work.

### The two modes

- **`lazy`** (recommended headline): a stale read spawns a **detached child**
  `ctx-optimize sync` and **answers immediately** from the current (slightly
  stale) store; the *next* read sees the refreshed store. **0 ms added query
  latency** — the honest, provable win (spike §3). The read prints a one-line
  freshness note to **stderr** ("store stale — resyncing in background; re-run
  for updated"), so stdout / `--json` stays byte-clean and the agent knows.
- **`block`**: a stale read runs the incremental `sync` **inline first** (progress
  to stderr), then answers — always fresh. After levers 1+2 the delta is cheap,
  but honestly ~300–350 ms on a 1-file edit (the wasm-compile floor, spike §3),
  NOT sub-0.13 s. We ship the honest number.

### Mechanism (LOCKED in proposal.md — restated, no re-litigation)

NO cron, NO daemon, NO watcher. The query process spawns a detached, self-
terminating child and returns. A **PID lockfile** in the store makes concurrent
stale reads no-op the spawn (one sync in flight, never a stampede).

- **Lockfile:** `<store-root>/<rootKey>/.autosync.lock`, created atomically
  (`O_CREATE|O_EXCL`), holds `pid\nstartNanos`. Reclaimed only if the pid is dead
  **and** age > 30 s (closes the parent-exit/child-adopt race), or unconditionally
  if age > 10 min (a wedged sync). Lives inside an EXISTING store dir — the read
  path never *creates* a store (os.Stat-gated), preserving "reads don't write a
  store."
- **Detach, per-GOOS (build-tagged):** Unix `SysProcAttr{Setsid:true}`; Windows
  `CreationFlags: DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP`. Child stdio =
  null. Tested on all three release platforms (spike S6).
- **Child lifecycle:** a hidden `__autosync` verb — reclaims the lock with its own
  live pid, runs the code-only sync in `--dir`, `defer`s lock removal. Spawn is
  behind an injectable seam (`var spawnAutosyncChild = …`) so tests don't fork.
- **Scope LOCKED:** child runs the **code-only** sync (`sync`'s default path).
  Never adapters, never a source dial. A read never triggers network/credentials.

### Determinism / safety (from spikes.md §2, unchanged)

- Two-phase extraction already guarantees `incremental == full` graph
  (Phase-2 call resolution re-runs wholesale). The golden net pins it. Lever 3
  adds no new extraction path — it just *invokes* `sync`, so this holds for free.
- Atomic temp+rename in the store (store.go:551) means a concurrent reader sees
  the whole old or whole new store, never torn (spike S4).

### Read verbs that trigger autosync

The query hot path: `query`/`ask`, `card`, `change-plan`/`plan`, `affected`,
`path`, `explain`, `hubs`, `nodes`, `edges`, `deps`, `routes`, `manifests`,
`export`, `verify`. **Not** `status`/`fresh` (those *report* freshness — auto-
syncing there would hide the very signal they exist to show), not `serve`,
`wiki`, `config`, `log`, and no write verb.

---

## Open questions for the owner (decide before code)

1. **D1** — commit to "`sync` = the resync verb" (definition + docs + `--adapters`
   flag), or leave `sync` as an undocumented fast-lane and only add lever 3?
   *(My lean: yes, do D1 — it's the clean model and costs almost nothing.)*
2. **Default mode** — ship `off` (deterministic, no surprise work — my lean) or
   `lazy` (CodeGraph-parity out of the box)? Env/config always available either
   way.
3. **`--all`** — alias `--adapters` now, reserve "+sources" for later? (my lean:
   yes, don't wire a dial into `--all` yet.)
4. **Wiki on auto-sync** — the child's `sync` regenerates the wiki when the graph
   changed (already cheap-guarded). Keep, or have the auto-sync child skip wiki
   entirely (not on the query hot path)? *(Lean: keep — it's already skipped when
   the graph is unchanged, and a stale wiki is its own small trust gap.)*

## Success check (unchanged from proposal.md, restated)

- `autosync: lazy` — edit a file, query twice → the 2nd query is fresh, no manual
  `sync`; the 1st added **0 ms** and printed a stderr staleness note.
- `autosync: block` — edit, query once → fresh answer; latency = the incremental
  delta (honest ~300 ms floor on 1 file, not sub-0.13 s).
- `off` (default) — byte-identical to today. Reads never write/create a store.
- One sync in flight under concurrent stale reads (lockfile); dead-owner lock
  reclaimed. Cross-platform detach tested on darwin/linux/windows.
- Golden `incremental == full` equality still green (lever 3 adds no extract path).
