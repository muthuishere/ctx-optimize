# ADR 1 — the boundary model: one `port` kind, config rules, shipped defaults

Status: ACCEPTED 2026-08-13 (owner: "start doing AD1") — IMPLEMENTED 2026-08-14 in f3df12d…e4c26fe (D1-D7, S6, S7; D3 resolved additive: byte-match proved regex cannot reproduce the AST recognizers, so they stay as EXTRACTED truth).
Part 1 of 3: this defines WHAT a boundary is and the defaults the binary ships.
ADR 2 (`2026-08-13-boundary-authoring`) defines how repo-specific rules are
GENERATED and proven. ADR 3 (`2026-08-13-serve-world`) defines the view.

## Context — measured on real stores this week

The store knows what things are, not where they connect at the edges:

| fact | in the store? | measurement |
|---|---|---|
| files / symbols / contains / calls | ✅ | drove a full recursive visualization natively |
| routes | ⚠️ | reqsume: 103 from swagger.yaml, **0 from Go source**; 14 match no package |
| route → handler | ❌ | routes: 103 in-edges, **1 out-edge total** |
| app → app over HTTP | ❌ | `ui → api` = **137 sites** through one wrapper; raw `fetch(` finds 3 (45×) |
| third-party egress | ❌ | openai ·15, gemini ·12, contabo ·4, convertapi ·2, an Azure webhook **in no manifest** |
| SDK-mediated egress | ❌ | `lib/firebase.ts` talks to Google with ZERO host literals — SDK + 6 `VITE_FIREBASE_*` keys |
| storage / cookies / env | ❌ | 74 localStorage sites / 7 keys (2 dynamic) · 36 cookie sites · 87 env vars |
| import resolution | ⚠️ | k8s: **92,902 imports → 1,680 resolves_to = 1.8%** |

Common shape: **code binding to an externally-addressable name — provider side,
consumer side, checked by no compiler.**

A spike adapter proved the pipeline end-to-end (161 port nodes into reqsume via
the existing `add --json` door; `card OPENAI_API_KEY` / `affected
api.openai.com` answered by existing verbs, zero binary changes) — and proved
the failure modes that ADR 2 exists to prevent (recall 12%, vendored-corpus
false positives).

## D1 — one node kind: `port`

```
kind: port
reserved metadata (validated fail-closed, like schema.Validate today):
  direction   provides | consumes
  transport   open dotted namespace — network.http, network.ws, network.grpc,
              network.rpc, messaging.* (queue/topic/pubsub), storage.local,
              storage.session, storage.cookie, storage.db, config.env,
              process.exec, ipc.*, ffi.*, ui.action, ui.screen,
              mobile.deeplink, …   (a string, never a Go enum)
  identifier  the external name, normalized per transport (D5)
  scope       internal | external — decided by JOIN, never by guess:
              internal iff the identifier matches a `provides` port in this
              workspace (that IS the monorepo ui→api link). Recomputed each
              gather, so moving a service in/out of the repo flips it.
  tier        EXTRACTED | INFERRED | AMBIGUOUS — existing tiers, unchanged
relations: provides / consumes   (file|function → port)
```

Routes, egress, storage keys, env vars, spawned processes, queues, GUI actions
and deep links are all instances. Open metadata rides namespaces (`otel.*` per
OpenTelemetry semconv — static graph joins runtime traces on `server.address`;
`pack.*`; `org.*`); un-namespaced unknown keys are rejected. Generic query
door: `nodes --kind port --meta transport=network.ws`. `route` stays emitted
alongside during migration.

**No orphan kinds** (standing rule): a kind ships with the edge that answers a
question, or it does not ship.

## D2 — the mechanism is CONFIG, not code: `boundaries.json`

Declarative rules interpreted **inside the walk the engine already does** — no
second file pass, no code executing in `add`.

```json
{ "version": 1, "boundaries": [
  { "id": "process-exec", "transport": "process.exec", "direction": "consumes",
    "when":    { "ext": [".go", ".ts", ".py"] },
    "exclude": { "path": ["testdata/", "benchmarks/", "node_modules/"] },
    "match": [
      { "re": "exec\\.Command\\(\\s*\"([^\"]+)\"", "identifier": 1 },
      { "re": "child_process\\.(?:spawn|exec)\\(\\s*['\"]([^'\"]+)", "identifier": 1 },
      { "re": "subprocess\\.(?:run|Popen)\\(\\s*\\[\\s*['\"]([^'\"]+)", "identifier": 1 } ],
    "verified": { "…mandatory, defined in ADR 2…" } },
  { "id": "env", "transport": "config.env", "direction": "consumes",
    "match": [ { "re": "os\\.Getenv\\(\\s*\"([A-Z0-9_]+)\"", "identifier": 1 } ],
    "flag": { "when_identifier_matches": "KEY|TOKEN|SECRET|PASSWORD",
              "set": { "sensitive": "true" } } }
]}
```

Precedence — the grammar-pack ladder the product already has:

```
.ctxoptimize/boundaries.json      repo (committed, reviewed)
  ~/ctxoptimize/boundaries/*.json machine
    embedded defaults             in the binary
```

Config beats code on every axis: a JSON diff is reviewable; nothing executes; a
proven repo rule graduates into the embedded defaults **verbatim**; and it
rides the existing visit (the JS spike's second pass cost +7–17% — the ceiling
to beat, not the target). The `.ctxoptimize/adapters/*` door remains as the
escape hatch for the genuinely bespoke, and taking it must be justified.

## D3 — the DEFAULT boundary set (shipped, embedded, pre-verified)

The binary is useful with zero config. v1 defaults, each landing with a
`verified` block measured on the corpus sweep (ADR 2 §verify):

| id | transport | covers |
|---|---|---|
| http-url-literal | network.http | url literals, all main langs |
| env-{go,js,py,java,cs} | config.env | os.Getenv / process.env / import.meta.env / os.environ / Environment.* + `sensitive` flag |
| process-{go,js,py,java,cs} | process.exec | exec.Command / child_process / subprocess / ProcessBuilder / Process.Start |
| webstorage | storage.local/session | get/set/removeItem; computed keys → AMBIGUOUS |
| cookies | storage.cookie | Set-Cookie / http.Cookie / document.cookie |
| websocket | network.ws | new WebSocket / EventSource / SignalR |
| routes-{express,nest,fastapi,flask,react-router} | network.http provides | **migration**: re-express the 681 lines of Go recognizers; MUST byte-match golden snapshots before the Go paths are deleted |
| routes-{gin,chi,echo,spring,aspnet,angular} | network.http provides | net-new coverage, same mechanism |

## D4 — `ctx-optimize search`: cross-OS ground-truth and literal sweeps

`rg` is not assumable and POSIX `grep` does not exist on Windows — a release
target (goreleaser `goos: [darwin, linux, windows]`, npm platform packages).
The binary ships its own:

```
ctx-optimize search <regex> [--ext .go,.ts] [--path dir/] [-c | --files]
```

Pure Go (RE2) — the SAME engine that interprets D2 rules, so a rule and its
verification can never disagree on regex semantics. Rides the existing
ignore-aware walker (`internal/scan` + `internal/extract/ignore`), so searcher
and extractor see the **same file set** — the spike's vendored-corpus false
positive was exactly a file-set disagreement. Deterministic sorted output.
Doubles as the agent-facing literal-sweep tool, removing the last external
tool assumption from the instructions card.

## D5 — services registry: when the DEPENDENCY is the boundary

firebase / gcloud / boto3 / stripe: SDK-mediated egress with no URL in code.
The manifest is the evidence. Embedded `services.json` (top ~30 SaaS):

```json
{ "firebase": {
    "transport": "network.http",
    "match": { "deps": ["npm:firebase", "npm:firebase-admin", "pypi:firebase-admin"],
               "hosts": ["*.googleapis.com", "*.firebaseio.com"] },
    "config_hint": "FIREBASE_" },
  "openai": {
    "match": { "deps": ["npm:openai", "pypi:openai", "go:github.com/sashabaranov/go-openai"],
               "hosts": ["api.openai.com"] },
    "endpoints": [ { "method": "POST", "path": "/v1/chat/completions",
                     "sdk": ["chat.completions.create"] } ] } }
```

- dep present → `port(consumes)` **INFERRED** (dep is real; call may be dead)
- SDK symbol at a call site → **EXTRACTED**, resolved to the endpoint when
  named (`chat.completions.create` ⇒ `POST /v1/chat/completions`, no URL seen)
- `config_hint` attaches the service's config.env ports (firebase's 6 keys)
- `ctx-optimize services add <file|url>` extends it — one validated file,
  network only on explicit user command (the `grammar build` posture). Teams
  register INTERNAL platforms the same way → cross-repo joins.
- generatable from vendors' OpenAPI via the existing openapi connector;
  CycloneDX SaaSBOM export falls out for free.

## D6 — identifier normalization + `drift`

Per-transport normalization (`{id}` ≡ `:id`, trailing slash, case, host/path
split) — without it provides/consumes never join and scope/drift are noise.

`ctx-optimize drift`: where provides, consumes and declared disagree — dead
contract (14 swagger orphans today), undocumented endpoint, key written-never-
read, env read-but-declared-nowhere. **Gate: only EXTRACTED×EXTRACTED pairs are
findings**; lower tiers are listed, never accused.

## D7 — module-specifier resolution (prerequisite fix, store-side)

`imports` → `module://` with 1.8% `resolves_to` on k8s. Resolution (relative,
`@/` aliases, tsconfig paths, go module root) moves into the code producer;
~90K edges become traversable on k8s alone. Cheapest high-value item here.

## Perf budget (guardrail; numbers published either way per docs/CRITIQUE.md)

`add` ≤ +5% wall (rules ride the visit) · store ≤ +5% (reqsume est. +0.2%
nodes / +1.1% edges) · `query`/`card` unchanged · scoreboard only moves up.

Known, out of scope, blocking perf claims at scale: the store-lock gap (two
concurrent `add`s interleave silently; the fixed-temp race is already fixed in
`internal/store` with a regression test) and index/`card` behaviour at 2.85M
nodes — each wants its own ADR/spike.

## Spikes

| # | question | gates |
|---|---|---|
| S1 | D2 interpreter; existing recognizers as rules; **byte-match golden** | D2, D3 migration |
| S2 | `search` verb parity: same counts as rg where rg exists; Windows CI run | D4 |
| S3 | services tier on reqsume: manifest alone lights all five externals + firebase | D5 |
| S4 | normalization: swagger-103 vs code-extracted; how many of 14 orphans are real | D6, first drift finding |
| S5 | D7 on k8s: 1.8% → ? | D7 |
| S6 | corpus sweep — linux (syscall/ioctl world, no HTTP), chromium (mojom), k8s, spring, cpython, cljgo, reqsume, self | defaults' verified blocks |
| S7 | kill test: false-positive rate of shipped defaults, published | credibility |

(Tree-sitter query exports through wazero — old S0 — are deferred entirely:
config rules made them non-blocking.)
