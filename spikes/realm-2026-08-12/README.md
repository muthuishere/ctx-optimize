# The realm spike — 2026-08-12 (pre-ADR-3)

The original Game-of-Thrones-style living-realm viewer, recovered 2026-08-16 from
the session log by replaying the Write + Edit + patch chain deterministically.
This is the version ADR 3 (`openspec/changes/2026-08-13-serve-world`) was written
about and later killed. It is NOT the shipped `HouseViewer.tsx`.

- `ports-ux.html` — the built page (376 KB, self-contained, open it directly).
  Live copy: https://claude.ai/code/artifact/7bca2221-45c5-4ab1-b38f-9d6cf238d449
- `spike7-realm.mjs` — the generator (499 lines). `node spike7-realm.mjs` rebuilds the HTML.
- `tree.json`, `allflows.json` — derived from the reqsume ctx-optimize store.
- `mktree.sh` — rebuilds `tree.json` from `r-nodes.json`.
- `replay.py` — the recovery replayer (kept for provenance).

Regenerate the raw dumps:

    cd ~/muthu/deemwarworkspace/reqsume-workspace/reqsume
    ctx-optimize nodes --json > r-nodes.json
    ctx-optimize edges --json > r-edges.json

## What it does that the shipped house viewer doesn't

Recursive entry (realm -> house -> yard -> folder -> file -> function), typed
messengers where every moving thing is a real call from the store, hover shows
cargo (function names), everything draggable, test files and their messengers in
violet, role-coloured quarters, inbound visitors walking in from the rim,
subtree-aware flow aggregation, postgres as an enterable granary of 19 real
tables, and import signposts.
