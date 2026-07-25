# ADR — Docker + Compose recognizers (literal facts only)

Status: **ACCEPTED** — 2026-07-25 (owner: *"see whatever is feasible docker and
docker compose and all"*, under the standing rules: literal only, no performance
regression, no secret ever stored).

## The gap, measured today

`Dockerfile` → **one `config` node**, nothing else. No stages, no base images.

`compose.yaml` → a `config` node + **17 flat `config_key` nodes**: three separate
nodes all labelled `image`, two labelled `volumes`, with `api`/`db`/`cache`
indistinguishable from `ports` or `environment`. Consequences:

1. Services are not modelled as services.
2. **The image VALUES are lost** — only the key `image` is captured, so
   `ghcr.io/acme/api:1.2.3`, `postgres:16`, `redis:7` are nowhere. The k8s lane
   already answers "what images do we run"; compose is blind to the same fact.
3. `depends_on` exists as a key but never as an **edge**, so the service
   dependency graph — the point of compose — is absent.

## Reuse the existing vocabulary — no new kinds for the shared parts

`k8s.go:344-351` already establishes: `image:<ref>` node id, kind `image`, label
= the full ref, and `X --uses_image--> image:<ref>`. Compose and Dockerfile MUST
emit the identical shape so a repo with both k8s manifests and compose converges
on **one** node per image. Relations `depends_on` and `uses_image` already exist.

## Scope — every item is a literal read (the 1%-wrong rule)

### compose (`compose.y{a,}ml`, `docker-compose.y{a,}ml`)

| fact | source | emit |
|---|---|---|
| service | each key under top-level `services:` | node kind **`service`**, id `<file>::service:<name>`, label `<name>` |
| image | `image:` scalar | `service --uses_image--> image:<ref>` + the shared `image` node |
| dependency | `depends_on:` **list form AND map form** (`db: {condition: …}`) | `service --depends_on--> service` (same file) |
| build context | `build: ./api` or `build: {context: …, dockerfile: …}` | resolve the path; if that Dockerfile EXISTS, `service --depends_on--> <dockerfile>` node |
| ports | `ports:` entries | service **metadata** only (`ports: "8080:8080,…"`) — not nodes |

### Dockerfile (`Dockerfile`, `Dockerfile.*`, `*.Dockerfile`)

| fact | source | emit |
|---|---|---|
| stage | `FROM <ref> [AS <name>]` | node kind **`stage`**, id `<file>::stage:<name-or-index>`, label = stage name, else the image ref |
| base image | the `<ref>` in `FROM` | `stage --uses_image--> image:<ref>` |
| stage link | `COPY --from=<stage>` | `stage --depends_on--> stage` |
| exposed ports | `EXPOSE` | stage metadata |

`FROM ... AS x` casing is free-form (`as`/`AS`); `--from=` may name a stage **or**
an image — only emit the edge when it matches a stage declared in the same file
(otherwise it is an image; do not guess).

## Explicitly OUT

- **`environment:` / `env_file` values — never read.** This is the secret surface
  (`DB_URL: postgres://…`, `POSTGRES_PASSWORD`). Today the flat-config lane
  captures only the KEY and no value; this recognizer stores **neither key nor
  value** for env. Simplest safe choice, and it removes the leak path entirely.
- `RUN`/`CMD`/`ENTRYPOINT` command text (not structure, and can embed secrets).
- top-level `volumes:` / `networks:` declarations (thin value).
- `${VAR}` interpolation — a service or image name built from a variable is NOT
  in the file. Emit the literal text as-is; never resolve.
- Compose `extends`, `include`, profiles, and multi-file overrides — resolution is
  inference. Read each file literally and independently.

## Perf

Both file classes are already visited by the manifests walker (`Dockerfile` and
`docker-compose.*` are in `manifestNames`; compose files also match
`configExts`). Cost is parse-only on matched files, no new tree walk. Must be
confirmed by measurement, not assumed.

## Verification required

1. **Golden fixture** pinning: 3 services, images shared with a k8s manifest in
   the same repo (assert ONE `image:` node, two `uses_image` edges from different
   lanes), both `depends_on` forms, a multi-stage Dockerfile with
   `COPY --from`, and a `build:` path that resolves to a real Dockerfile.
2. **Secret test**: a compose file with `POSTGRES_PASSWORD: hunter2` and
   `DB_URL: postgres://u:p@db/app` must produce **no node, label, or metadata
   containing any part of those values**. Write this first.
3. **No junk**: an arbitrary non-compose YAML with a `services:` key must not be
   misread — require the compose filename set, and confirm the existing k8s lane
   still wins on real k8s manifests (both lanes see `.yaml`).
4. `task ci` + `task golden`; corpus tier against the pinned clones. Node counts
   move UP only where expected; existing snapshots reviewed, not re-blessed.
