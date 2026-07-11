# Design — incremental, scalable wiki + graph

The mechanism behind the "incremental scalable wiki" requirement. Combines
graphify's cache economics with citenexus's per-page storage, and adds the piece
neither has: **the re-distill unit is the community/cluster, keyed by a
`member_hash`.** Result: edit one file → re-distill only the communities whose
membership actually changed. No global wiki rewrite, no whole-corpus LLM call.

## Invalidation — content hash, not mtime

- Identity of a source unit = **SHA256 of contents**. `mtime`+`size` is only a
  fast-path gate (skip the read+hash when unchanged) — a `stat-index`.
- Two hash fields per file: **`ast_hash`** (cheap structural extraction) and
  **`distill_hash`** (LLM work), so a structural change never forces an LLM
  re-bill and vice-versa.
- **Structural cache is version-namespaced** by extractor version (a code change
  to an extractor invalidates it). **LLM/distill cache is UNVERSIONED** — a
  release must not re-bill unchanged units.

## Storage layout (folder or S3; query reads only the manifest)

```
graph/graph.json                  # or sharded per-community node/edge files
wiki/index.json                   # LIGHT manifest: page_id, title, keywords, links,
                                  #   summary, member_hash, source_hashes[]  (NO node refs)
wiki/pages/{community_id}.json    # full page (with refs) — fetched only on match
wiki/pages/{community_id}.md      # human-browsable
wiki/log.md                       # append-only journal
cache/ast/v{ver}/{filehash}.json  # structural extraction cache (versioned)
cache/distill/{member_hash}.json  # per-community distilled page cache (unversioned)
manifest.json                     # per-file {mtime, size, ast_hash, distill_hash}
```

- One wiki page ⇄ one community. `member_hash` = hash of the sorted set of member
  node content-hashes **+ the distiller prompt version**. A page is "current"
  iff its `member_hash` matches — a pure lookup, no recompute.
- Query loads `wiki/index.json` only; a full page is fetched on hit. Scales to
  large repos and to S3 because the hot path is the light manifest.

## Incremental update flow (only touched units recomputed)

1. **detect** — stat fast-path → hash slow-path → set of changed/deleted files.
2. **re-extract** only those files; pull unchanged files' AST from `cache/ast`.
3. **reconcile into the existing graph** — evict nodes/edges for changed+deleted
   files, keep the rest; re-run community detection but **pin community IDs to the
   previous run** (remap) so untouched communities keep their id → keep their page.
4. **worklist** — recompute each affected community's `member_hash`; the set whose
   hash changed is the re-distill worklist.
5. **re-distill only the worklist** — for each: `cache/distill/{member_hash}.json`
   hit → reuse (no LLM); miss → one per-community LLM call (injected `models.*`
   client), write `wiki/pages/{id}.*`, upsert exactly that row in `index.json`,
   append one `log.md` line.
6. Everything not on the worklist is read straight from the store — never
   re-serialized, never re-billed.

## Deliberately narrow (targeted, not broad)

- One page per community; deterministic fallback page when no LLM is configured.
- No god-node article sprawl, no HTML/report generation in this path.
- Distiller is injected + optional; failure degrades to the deterministic page.

## Provenance

graphify: content-hash cache + unversioned semantic cache + file-granular
re-extract + reconcile-into-existing-graph (`detect.py`, `cache.py`, `watch.py`).
citenexus-Python: per-page objects + light `index.json` + `WikiStore.integrate_document`
upserting one page (`wiki/store.py`). New piece: per-community `member_hash` distill
cache — the unit neither source uses.
