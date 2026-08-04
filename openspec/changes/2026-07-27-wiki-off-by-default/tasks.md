# Tasks — wiki off by default

Order matters: the default flip (1) is what makes 2–4 necessary, and the docs
(6) must not ship claiming a default that no longer exists.

## 1. Flip the default

- [x] `internal/project/project.go` — `Config.WikiEnabled()` returns **false**
      when `Wiki` is nil. Rewrite the doc comment: it currently states the old
      guarantee ("absent means enabled, so adding this key never silently turns
      it off for anyone"). The replacement states the new one and why it moved
      (89.3% of a linux gather, `spikes.md` / ADR context table).
- [x] `internal/app/app.go` — `noWiki(f, sc)` gains a `--wiki` force flag that
      beats both the new default and a config `"wiki": false`. Precedence,
      most-specific first: `--wiki` > `--no-wiki` > config > default(off).

## 2. `status` diagnoses the stale wiki

- [x] New helper beside `freshnessReports` — `wikiStaleness(s *store.Store)`:
      `os.Stat(<dir>/wiki/index.md)` and `os.Stat(<dir>/graph/nodes.ndjson)`;
      report stale only when both exist and the wiki's mtime is older.
      **Two `Stat` calls on fixed paths — no `ReadDir`/`WalkDir` on `wiki/`
      (defect #9's 8s listing tax).**
- [x] `cmdStatus` prints the line only when stale, after `fresh:`, naming both
      remedies (`ctx-optimize wiki` / `ctx-optimize wiki --delete`).
- [x] `--json` carries it as a `wiki` key, absent when not stale.

## 3. `wiki --delete`

- [x] `cmdWiki` grows `--delete`: `os.RemoveAll(<dir>/wiki)`, then
      `s.UpdateManifest()` so the wiki entries leave the manifest, then a line
      saying what went and how to get it back.
- [x] Never touches `graph/`. Absent wiki → says so, exit 0, not an error.
- [x] `audit.Append` the mutation, matching `store delete`'s shape.

## 4. Scaffold

- [x] `Scaffold` stops writing `"wiki": true` into new configs (this reverses
      the 2026-07-26 request, which predates the linux measurement). The key
      stays documented in `instructions.md` as the opt-in.

## 5. Tests

- [x] `TestWikiAbsentMeansEnabled` → `TestWikiAbsentMeansDisabled`, comment
      restating the new guarantee and the reason it moved.
- [x] `internal/app/app_test.go:47` — asserts `wiki/index.md` after a plain
      `add`; give it an explicit `"wiki": true` or move it to the `wiki` verb.
- [x] New: `--wiki` forces generation with no config present.
- [x] New: `--wiki` beats a config `"wiki": false`.
- [x] New: stale wiki → `status` prints the line; current wiki → silent;
      **empty `wiki/` dir → silent** (the S2 trap, and the one most likely to
      regress).
- [x] New: `wiki --delete` removes the dir, leaves the graph queryable, and
      `wiki` rebuilds afterwards.

## 6. Docs carrying the old default

- [x] `.ctxoptimize/instructions.md` (managed block — the template; the file
      itself refreshes on the next `init`/`up` version bump)
- [x] `internal/skills/bundled/ctx-optimize/SKILL.md` + `references/sync.md`
- [x] `README.md` (three places claimed "regenerated on every add")
- [x] `CHANGELOG.md`
- [x] `docs/VISION.md` — NO CHANGE NEEDED, verified: it never states the
      default. Its wiki mentions are the product thesis (deterministic wiki as
      FORM) and future S3/parquet plans, neither of which the flip touches.
- [x] this repo's `CLAUDE.md` pointer block
- [x] `--help` text for `wiki` (the `--delete` flag) and the `wiki` config key

## 7. Verification

- [x] `task ci` green.
- [x] `task golden` hermetic + corpus tiers against the pinned clones; judged
      floors unmoved (linux-block 16.5, newtonsoft 13.0). The wiki is not on the
      query path — **a moved score means something else broke, not this.**
- [x] Re-measure `add linux` — **132.36s** (2026-08-04, `v0.11.0-24`, M5 Pro),
      2,849,719 nodes, i.e. the ADR's node count to the unit: the graph really
      is byte-identical and only the wiki left. Beat the predicted ≈158s;
      treat the gap as warm page cache (the corpus tier had just read the tree),
      NOT as a speedup this change delivered. vs graphify's 531.97s that is a
      **4.0× win**, where the old default was a 2.8× loss.
