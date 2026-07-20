# Tasks
- [x] grouping.ts: pure groupByRepo + rollUpFresh (worst-of, mirrors freshness.Overall); aggregates nodes/edges/producers/usage/age
- [x] grouping.test.ts: monorepo folds to one group, separate repos stay separate, single-module degenerates, freshness worst-of, producer/usage sums, missing residual, legacy empty root
- [x] Repos.tsx: one card per repo, modules inline capped at MODULE_PREVIEW(5) + "+N more"/"show fewer"; residual shown as "(root files)" inside; header "N repos · M modules"
- [x] Overview.tsx: headline counts repos, reports modules separately
- [x] task dashboard-build regenerates the committed dist
- [x] tsc clean; 25 UI tests pass; task ci green
- [x] verified live: 18 flat entries -> 2 repo cards (volentis 17 modules folded, ctx-optimize single)
