// Repo grouping (ADR 2026-07-19-dashboard-repo-grouping).
//
// /api/stores returns a FLAT list: a monorepo's modules arrive as peers of
// genuinely separate repos (`volentis`, `volentis/apps/api`, `ctx-optimize`,
// …), which made an 18-module monorepo look like 18 products. Every
// StoreInfo already carries `root` (the top-level store key), so grouping is
// a pure client-side fold — no API change.
//
// A single-module repo groups to exactly one member whose key === root, and
// renders as it always did.
import type { StoreInfo, Usage } from './types'

export interface RepoGroup {
  /** Top-level store key — the repo/product identity. */
  root: string
  /** The repo's own store (residual for a monorepo), when it has one. */
  self?: StoreInfo
  /** Module stores under this repo, node-count desc. Excludes `self`. */
  modules: StoreInfo[]
  /** Every store in the group (self first when present) — what aggregates sum. */
  all: StoreInfo[]
  nodes: number
  edges: number
  /** Worst-of roll-up, mirroring internal/freshness.Overall. */
  fresh: string
  /** Merged producer counts across the group. */
  producers: Record<string, number>
  /** Summed usage; undefined when no member reported any. */
  usage?: Usage
  /** Most recent gather age in the group (smallest age), when known. */
  ageSeconds?: number
  /** Navigator summary from the repo's own store, when present. */
  summary?: string
  /** Source path from the repo's own store, else the first member that has one. */
  sourcePath?: string
}

/** any stale ⇒ stale; else any unknown ⇒ unknown; else fresh (empty ⇒ unknown). */
export function rollUpFresh(states: string[]): string {
  if (states.length === 0) return 'unknown'
  let sawUnknown = false
  for (const s of states) {
    if (s === 'stale') return 'stale'
    if (s !== 'fresh') sawUnknown = true
  }
  return sawUnknown ? 'unknown' : 'fresh'
}

/**
 * groupByRepo folds the flat store list into one entry per top-level repo,
 * preserving the API's ordering of roots (first appearance wins) so the view
 * stays stable across reloads.
 */
export function groupByRepo(stores: StoreInfo[]): RepoGroup[] {
  const byRoot = new Map<string, StoreInfo[]>()
  for (const s of stores) {
    // `root` is the first path segment; fall back to the key so a malformed
    // or legacy payload still groups to itself instead of vanishing.
    const root = s.root || s.key
    const list = byRoot.get(root)
    if (list) list.push(s)
    else byRoot.set(root, [s])
  }

  const out: RepoGroup[] = []
  for (const [root, members] of byRoot) {
    const self = members.find((m) => m.key === root)
    const modules = members
      .filter((m) => m.key !== root)
      .sort((a, b) => b.nodes - a.nodes || a.key.localeCompare(b.key))
    const all = self ? [self, ...modules] : modules

    const producers: Record<string, number> = {}
    for (const m of all) {
      for (const [p, n] of Object.entries(m.producers || {})) {
        producers[p] = (producers[p] || 0) + n
      }
    }

    let usage: Usage | undefined
    for (const m of all) {
      if (!m.usage) continue
      usage = usage
        ? {
            total_served: usage.total_served + m.usage.total_served,
            est_tokens_saved: usage.est_tokens_saved + m.usage.est_tokens_saved,
            est_cost_saved_usd: usage.est_cost_saved_usd + m.usage.est_cost_saved_usd,
          }
        : { ...m.usage }
    }

    const ages = all.map((m) => m.age_seconds).filter((a): a is number => typeof a === 'number' && a > 0)

    out.push({
      root,
      self,
      modules,
      all,
      nodes: all.reduce((n, m) => n + m.nodes, 0),
      edges: all.reduce((n, m) => n + m.edges, 0),
      fresh: rollUpFresh(all.map((m) => m.fresh)),
      producers,
      usage,
      ageSeconds: ages.length ? Math.min(...ages) : undefined,
      summary: self?.summary || all.find((m) => m.summary)?.summary,
      sourcePath: self?.source_path || all.find((m) => m.source_path)?.source_path,
    })
  }
  return out
}

/** How many modules render inline before the "+N more" expander. */
export const MODULE_PREVIEW = 5

/** How many module chips the Overview card previews before "+N more". */
export const OVERVIEW_MODULE_PREVIEW = 4
