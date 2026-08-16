import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fetchScene } from './sceneApi'
import { relationStyle, RELATION_STYLE } from './flowLayout'

const emptyScene = {
  module: 'x', title: 'x', total_nodes: 0, total_edges: 0,
  subsystems_total: 0, subsystems_shown: 0, lifted_total: 0, lifted_shown: 0,
  cards: [], links: [], world: [], stats: [], chips: [], notes: [],
  root: '', level: 'module', crumbs: [], inside: [], questions: [],
}

let calls: string[] = []

function mockFetch(handler: (url: string) => { ok: boolean; body: unknown }) {
  vi.stubGlobal('fetch', (url: string) => {
    calls.push(url)
    const r = handler(url)
    return Promise.resolve({
      ok: r.ok,
      statusText: 'nope',
      json: () => Promise.resolve(r.body),
    } as Response)
  })
}

beforeEach(() => { calls = [] })

describe('fetchScene picks the endpoint from the address', () => {
  it('asks a bare repo name at MODULE grain', async () => {
    mockFetch(() => ({ ok: true, body: emptyScene }))
    await fetchScene('reqsume', '', '')
    expect(calls).toHaveLength(1)
    expect(calls[0]).toContain('/api/repo/scene?repo=reqsume')
  })

  it('falls through to the module scene when the repo has no module index', async () => {
    mockFetch((url) =>
      url.startsWith('/api/repo/scene')
        ? { ok: false, body: { error: 'no module index' } }
        : { ok: true, body: emptyScene })
    const sc = await fetchScene('spinx', '', '')
    expect(calls[0]).toContain('/api/repo/scene')
    expect(calls[1]).toContain('/api/scene?module=spinx')
    expect(sc.level).toBe('module') // whatever the server said, unmodified
  })

  it('never asks the repo endpoint for a module key', async () => {
    mockFetch(() => ({ ok: true, body: emptyScene }))
    await fetchScene('reqsume/apps/api', '', '')
    expect(calls).toHaveLength(1)
    expect(calls[0]).toContain('/api/scene?module=reqsume%2Fapps%2Fapi')
  })

  it('never asks the repo endpoint once a drill is in the address', async () => {
    mockFetch(() => ({ ok: true, body: emptyScene }))
    await fetchScene('reqsume', 'src/lib', '')
    expect(calls).toHaveLength(1)
    expect(calls[0]).toContain('root=src')
    await fetchScene('reqsume', '', 'file')
    expect(calls[1]).toContain('grain=file')
    expect(calls.some((c) => c.startsWith('/api/repo/scene'))).toBe(false)
  })

  it('hands back a scene whose array fields are never null', async () => {
    mockFetch(() => ({ ok: true, body: { ...emptyScene, chips: null, notes: null } }))
    const sc = await fetchScene('reqsume', '', '')
    expect(sc.chips).toEqual([])
    expect(sc.notes).toEqual([])
  })
})

describe('the module-grain relations are drawn as what they are', () => {
  it('gives depends and shares their own ink', () => {
    expect(relationStyle('depends').line).not.toBe(relationStyle('shares').line)
    expect(RELATION_STYLE.depends).toBeDefined()
  })

  it('marks shares dashed, because it is not a call', () => {
    expect(relationStyle('shares').dashed).toBe(true)
    expect(relationStyle('depends').dashed).toBeFalsy()
    expect(relationStyle('calls').dashed).toBeFalsy()
  })
})
