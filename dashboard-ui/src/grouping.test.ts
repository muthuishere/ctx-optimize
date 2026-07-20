import { describe, it, expect } from 'vitest'
import { groupByRepo, rollUpFresh } from './grouping'
import type { StoreInfo } from './types'

const s = (key: string, root: string, nodes: number, fresh = 'fresh', extra: Partial<StoreInfo> = {}): StoreInfo => ({
  key, root, nodes, edges: nodes * 2, fresh, ...extra,
})

describe('rollUpFresh', () => {
  it('mirrors freshness.Overall: any stale wins, then unknown, else fresh', () => {
    expect(rollUpFresh(['fresh', 'unknown', 'stale'])).toBe('stale')
    expect(rollUpFresh(['fresh', 'unknown'])).toBe('unknown')
    expect(rollUpFresh(['fresh', 'fresh'])).toBe('fresh')
    expect(rollUpFresh([])).toBe('unknown')
  })
})

describe('groupByRepo', () => {
  it('folds a monorepo into ONE group and keeps separate repos separate', () => {
    const groups = groupByRepo([
      s('volentis', 'volentis', 100),
      s('volentis/apps/api', 'volentis', 500),
      s('volentis/apps/web', 'volentis', 300),
      s('ctx-optimize', 'ctx-optimize', 2000),
    ])
    expect(groups.map((g) => g.root)).toEqual(['volentis', 'ctx-optimize'])

    const v = groups[0]
    expect(v.self?.key).toBe('volentis')
    expect(v.modules.map((m) => m.key)).toEqual(['volentis/apps/api', 'volentis/apps/web']) // node-count desc
    expect(v.nodes).toBe(900) // 100 + 500 + 300
    expect(v.edges).toBe(1800)
  })

  it('a single-module repo groups to itself with no modules', () => {
    const [g] = groupByRepo([s('brain', 'brain', 42)])
    expect(g.self?.key).toBe('brain')
    expect(g.modules).toHaveLength(0)
    expect(g.nodes).toBe(42)
  })

  it('rolls freshness worst-of across the group', () => {
    const [g] = groupByRepo([
      s('mono', 'mono', 10, 'fresh'),
      s('mono/a', 'mono', 10, 'stale'),
      s('mono/b', 'mono', 10, 'fresh'),
    ])
    expect(g.fresh).toBe('stale')
  })

  it('merges producer counts and sums usage', () => {
    const [g] = groupByRepo([
      s('mono', 'mono', 1, 'fresh', {
        producers: { code: 10, markdown: 2 },
        usage: { total_served: 3, est_tokens_saved: 100, est_cost_saved_usd: 0.5 },
      }),
      s('mono/a', 'mono', 1, 'fresh', {
        producers: { code: 5, tickets: 7 },
        usage: { total_served: 4, est_tokens_saved: 50, est_cost_saved_usd: 0.25 },
      }),
    ])
    expect(g.producers).toEqual({ code: 15, markdown: 2, tickets: 7 })
    expect(g.usage).toEqual({ total_served: 7, est_tokens_saved: 150, est_cost_saved_usd: 0.75 })
  })

  it('reports the most recent gather age and inherits summary/source from the repo store', () => {
    const [g] = groupByRepo([
      s('mono', 'mono', 1, 'fresh', { age_seconds: 900, summary: 'the monorepo', source_path: '/src/mono' }),
      s('mono/a', 'mono', 1, 'fresh', { age_seconds: 60 }),
    ])
    expect(g.ageSeconds).toBe(60)
    expect(g.summary).toBe('the monorepo')
    expect(g.sourcePath).toBe('/src/mono')
  })

  it('groups a module whose repo store is absent (broken/deleted residual)', () => {
    const [g] = groupByRepo([s('mono/a', 'mono', 5), s('mono/b', 'mono', 3)])
    expect(g.self).toBeUndefined()
    expect(g.modules).toHaveLength(2)
    expect(g.nodes).toBe(8)
  })

  it('falls back to the key when root is missing (legacy payload)', () => {
    const [g] = groupByRepo([{ key: 'solo', root: '', nodes: 1, edges: 0, fresh: 'fresh' }])
    expect(g.root).toBe('solo')
    expect(g.self?.key).toBe('solo')
  })
})
