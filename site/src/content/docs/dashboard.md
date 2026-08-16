---
title: The dashboard — `serve`
description: A local page over every store you have gathered. Loopback-only, embedded in the binary, zero external requests, and every mutation audited.
---

`ctx-optimize serve` starts a local web dashboard over the whole store root — every repo you
have ever gathered on this machine, not just the one you are standing in.

```bash
ctx-optimize serve
# dashboard: http://127.0.0.1:4747/  (store root: /Users/you/ctxoptimize) — Ctrl-C to stop
```

`dashboard` is an alias for the same verb. Flags: `--port 4747`, `--host 127.0.0.1`.

It is a viewer first. Everything it can change, the CLI can change too — the dashboard calls
the same command functions the CLI dispatches, so there is no second code path with its own
bugs.

## The seven screens

**Overview** — the landing screen. Totals across every store (repos, modules, nodes, edges,
how many are fresh and how many stale), a breakdown by producer (`code`, `markdown`,
`manifests`, `boundaries`, `deplink`), and a card per repo with its node and edge counts,
freshness badge, and buttons into the viewer and query for it. It also shows a running
estimate of tokens saved, derived from the usage counter each store keeps.

**Repos** — the same set as a flat list, one row per repo and one sub-row per module, each
with **Viewer**, **Query**, **Re-gather** and **Remove**. This is where you re-run a gather
or drop a store you no longer want.

**Onboard** — a three-step wizard, *Path → Scan → Gather*, for pointing the tool at a repo it
has never seen. Scan is read-only: it previews the module layout before anything is written,
and you confirm the module list before the gather runs. It is the same path as
`init --scan --yes` followed by `add .`.

**Query** — the same query engine the agent calls, with a module picker and a text box.
Ranked, cited hits with signatures and neighbors.

**Viewer** — three projections of the same store, picked from a switcher. **Flow** draws the
[derived architecture](/ctx-optimize/see/) (a card is a directory, an arrow is N real edges,
the hub is the most depended-upon directory, the outer world is the boundary ports).
**House** is the same scene as a cutaway building. **Graph** is the original force-directed
view, still budgeted, with a filter panel of every node kind and producer. Click a Flow or
House card to drill — directory → files → declarations. Drag pans, the wheel zooms.

**Settings** — the config keys in effect (`instructions`, `skills`, `hooks`) with what set
each one, the grammar / route / manifest packs discovered on this machine and in the repo,
adapter scripts, and the repo's remote push/pull commands. Every card names the file it is
rendering; the file stays the source of truth.

**Changes** — the audit feed. Every mutation from every door, dashboard and CLI alike, newest
first, with the actor and the target.

## How it is locked down

**Loopback only.** It binds `127.0.0.1:4747` by default.

**Mutations need a per-process token on top of that.** Read endpoints answer a plain GET.
Anything that writes — adding a repo, re-gathering, confirming an onboard, setting a config
key, deleting a store, running a remote push or pull — requires an `X-Ctx-Token` header whose
value comes from `GET /api/token`, which is itself loopback-only. Without it you get a 403:

```bash
$ curl -X POST http://127.0.0.1:4747/api/repo/add -d '{"path":"/tmp/x"}'
{"error":"missing or bad X-Ctx-Token (fetch GET /api/token same-origin)"}   # 403
```

The token is minted per process, so it dies with the server. This check applies even if you
widen `--host`; the loopback check on mutations is by `RemoteAddr` and is not relaxed by that
flag.

**Every mutation is audited.** Each one appends a line to `<store-root>/audit.ndjson` — a
timestamp, whether the actor was `dashboard` or `cli`, the action, the target, and before/after
sha256 where a value changed. It is append-only, sorted-field and git-diffable, and you do not
need the dashboard to read it:

```bash
$ ctx-optimize log
2026-07-26T06:28:33Z  cli       store.delete             chromium
2026-07-26T09:13:45Z  cli       store.delete             chromium
```

`log --json` prints the raw records.

**Zero external requests.** The whole React app is built ahead of time and embedded in the
binary with `go:embed` — the page loads one local JS file, one local CSS file, and an inline
`data:` favicon. There is no CDN, no font host, no analytics, no telemetry. The dashboard
works with the machine offline, and the only network traffic it can generate is a remote
push/pull you explicitly trigger, running the command your own config declares.

## The viewers, and why there are three

The force-directed **Graph** is a hairball on any real store. That is not a rendering
problem — a 9,854-node module cannot be a picture of every symbol. So `serve` derives a
second picture from the same store and lets you switch.

**Flow** is that picture. A card is a directory the author chose. An arrow is N real
`imports`/`calls` edges, lifted and summed — `AMBIGUOUS` excluded, so the arrow is a fact.
A card's column is its longest-path depth in the lifted DAG, so position means the
direction dependencies point. The hub is the most depended-upon directory. The outer
world is the [boundary](/ctx-optimize/boundaries/) ports, grouped by transport, names
only. The footer says what it is hiding: top N of M directories, this is a sample.

**House** is the same derivation projected as a cutaway building. Same cards, same
arrows, same hub. A third viewer is one entry in a registry; the shell never names one.

**Drill.** A leaf directory is not a leaf. Opening a card changes the unit — child
directories, then files, then declarations — so the `includes` and `calls` *inside* a
directory become visible. The cap stays 6 + hub. [The full reading →](/ctx-optimize/see/)

The scene is computed in Go (`GET /api/scene`) and is small on purpose: 7,663 nodes and
14,887 edges become a 4.4 KB payload. Read-only, no token, no audit, and it never
creates a store directory.

## The limit worth knowing

**The graph endpoint is budgeted, so large graphs arrive truncated.** The force-directed
viewer does not stream a whole module; the server sends a bounded slice and says so.
Asked for this repo's own module, `/api/graph` returned 708 of 4,834 nodes and 2,804 of
10,438 edges with `"truncated": true`, and the viewer prints the same thing above the
canvas — `nodes 633 / 7663 · server-budgeted`.

That is deliberate: a browser cannot lay out a million-node graph, and the Linux kernel
store on this machine is 2.8M nodes. Flow and House dodge the problem by *not sending
the graph* — they send the derived scene. For blast radius, still use `affected`,
`path` and `change-plan` on the CLI. Those read the whole graph and are the ones to
trust.

## Endpoints

Read (plain GET, loopback):

| endpoint | returns |
|---|---|
| `/api/stores` | every store: key, root, node/edge counts, freshness, producers, usage |
| `/api/modules` | the module list, with counts and a one-line summary |
| `/api/graph?module=<key>` | a budgeted node/edge slice; `?center=` expands one node |
| `/api/scene?module=<key>` | the derived architecture (cards, arrows, hub, outer world); optional `?root=` to drill |
| `/api/query?module=<key>&q=<terms>` | ranked hits with location, signature and neighbors |
| `/api/usage?module=<key>` | the usage counter for that store |
| `/api/setup` | grammar / route / manifest packs and effective config |
| `/api/audit` | the audit records |
| `/api/token` | the per-process mutation token |

Write (POST/PUT, loopback **and** `X-Ctx-Token`): `/api/repo/add`, `/api/onboard`,
`/api/onboard/confirm`, `/api/store` (delete), `/api/config`, `/api/pack`, `/api/remote/…`.

The read path never creates a store directory — opening the dashboard cannot leave anything
behind on disk.
