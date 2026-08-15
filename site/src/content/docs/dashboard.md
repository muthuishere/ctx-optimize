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

**Viewer** — a force-directed graph of one module, with a filter panel down the right side
listing every node kind (`route`, `config`, `class`, `document`, `file`, `function`,
`module`, `section`) and every producer, each with a count, so you can switch families on and
off. Drag pans, the wheel zooms, and clicking a node loads its 1-hop neighborhood and merges
it into the view.

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

## The limit worth knowing

**The graph endpoint is budgeted, so large graphs arrive truncated.** The viewer does not
stream a whole module; the server sends a bounded slice and says so. Asked for this repo's own
module, `/api/graph` returned 708 of 4,834 nodes and 2,804 of 10,438 edges with
`"truncated": true`, and the viewer prints the same thing above the canvas —
`nodes 633 / 7663 · server-budgeted`.

That is deliberate: a browser cannot lay out a million-node graph, and the Linux kernel store
on this machine is 2.8M nodes. But it means **the picture is a sample, not the graph**. Click a
node to expand its real neighborhood, and use `affected`, `path` and `change-plan` on the CLI
when you need the complete answer — those read the whole graph and are the ones to trust for
blast radius.

## Endpoints

Read (plain GET, loopback):

| endpoint | returns |
|---|---|
| `/api/stores` | every store: key, root, node/edge counts, freshness, producers, usage |
| `/api/modules` | the module list, with counts and a one-line summary |
| `/api/graph?module=<key>` | a budgeted node/edge slice; `?center=` expands one node |
| `/api/query?module=<key>&q=<terms>` | ranked hits with location, signature and neighbors |
| `/api/usage?module=<key>` | the usage counter for that store |
| `/api/setup` | grammar / route / manifest packs and effective config |
| `/api/audit` | the audit records |
| `/api/token` | the per-process mutation token |

Write (POST/PUT, loopback **and** `X-Ctx-Token`): `/api/repo/add`, `/api/onboard`,
`/api/onboard/confirm`, `/api/store` (delete), `/api/config`, `/api/pack`, `/api/remote/…`.

The read path never creates a store directory — opening the dashboard cannot leave anything
behind on disk.
