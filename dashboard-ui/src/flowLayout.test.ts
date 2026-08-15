import { describe, expect, it } from 'vitest'
import { layout, VH, VW } from './flowLayout'
import type { Scene } from './types'

// A scene shaped by hand: three layers, one hub, two transports. Every
// expectation below is written from THIS object, never captured from layout().
function scene(): Scene {
  return {
    module: 'demo',
    title: 'demo',
    total_nodes: 100,
    total_edges: 200,
    subsystems_total: 20,
    subsystems_shown: 4,
    lifted_total: 9,
    lifted_shown: 5,
    cards: [
      { id: 'a', label: 'a', dir: 'src/a', files: 3, decls: 9, in: 0, out: 20, layer: 0, row: 0, detail: 'A', glyph: '⇄', hub: false },
      { id: 'b', label: 'b', dir: 'src/b', files: 3, decls: 9, in: 20, out: 6, layer: 0, row: 1, detail: 'B', glyph: '◇', hub: false },
      { id: 'c', label: 'c', dir: 'src/c', files: 3, decls: 9, in: 26, out: 4, layer: 1, row: 0, detail: 'C', glyph: '◇', hub: false },
      { id: 'd', label: 'd', dir: 'src/d', files: 3, decls: 9, in: 30, out: 0, layer: 2, row: 0, detail: 'D', glyph: '⚙', hub: true },
    ],
    links: [
      { from: 'a', to: 'c', relation: 'calls', label: 'CALLS', weight: 20 },
      { from: 'b', to: 'c', relation: 'calls', label: 'CALLS', weight: 6 },
      { from: 'c', to: 'd', relation: 'calls', label: 'CALLS', weight: 4 },
      { from: 'a', to: 'world:network.http', relation: 'provides', label: 'PROVIDES', weight: 12 },
      { from: 'a', to: 'world:config.env', relation: 'consumes', label: 'CONSUMES', weight: 3 },
    ],
    world: [
      { id: 'world:network.http', transport: 'network.http', total: 40, provides: 40, consumes: 0, sensitive: 0, sample: [{ label: '/a', direction: 'provides', sensitive: false, dynamic: false }], truncated: true },
      { id: 'world:config.env', transport: 'config.env', total: 5, provides: 0, consumes: 5, sensitive: 1, sample: [{ label: 'API_KEY', direction: 'consumes', sensitive: true, dynamic: false }], truncated: true },
    ],
    stats: [{ label: 'nodes', text: '100' }],
    chips: ['40 network.http'],
    notes: ['top 4 of 20 …'],
  }
}

const box = (l: ReturnType<typeof layout>, id: string) => {
  const b = l.byId.get(id)
  if (!b) throw new Error('no box ' + id)
  return b
}
const cx = (b: { kind: string; x: number; w: number }) => (b.kind === 'hub' ? b.x : b.x + b.w / 2)

describe('flowLayout', () => {
  it('turns layer into x — position carries the dependency direction', () => {
    const l = layout(scene())
    // a and b share layer 0 and must share a column
    expect(cx(box(l, 'a'))).toBe(cx(box(l, 'b')))
    expect(cx(box(l, 'a'))).toBeLessThan(cx(box(l, 'c')))
    expect(cx(box(l, 'c'))).toBeLessThan(cx(box(l, 'd')))
  })

  it('turns row into y and never overlaps two cards in a column', () => {
    const l = layout(scene())
    const a = box(l, 'a')
    const b = box(l, 'b')
    expect(a.y + a.h).toBeLessThanOrEqual(b.y)
  })

  it('draws a curve for every link, both card-to-card and card-to-world', () => {
    const s = scene()
    const l = layout(s)
    expect(l.curves).toHaveLength(s.links.length)
    for (const c of l.curves) {
      for (const p of [c.p0, c.p1, c.p2, c.p3]) {
        expect(Number.isFinite(p.x)).toBe(true)
        expect(Number.isFinite(p.y)).toBe(true)
      }
    }
    // the relation label rides on the curve — an unlabelled arrow is the
    // failure mode the killed wall view had
    expect(l.curves.map((c) => c.link.label).sort())
      .toEqual(['CALLS', 'CALLS', 'CALLS', 'CONSUMES', 'PROVIDES'])
  })

  it('keeps every box inside the virtual canvas', () => {
    const l = layout(scene())
    for (const b of l.boxes) {
      const left = b.kind === 'hub' ? b.x - b.r : b.x
      const right = b.kind === 'hub' ? b.x + b.r : b.x + b.w
      const top = b.kind === 'hub' ? b.y - b.r : b.y
      const bottom = b.kind === 'hub' ? b.y + b.r : b.y + b.h
      expect(left).toBeGreaterThanOrEqual(0)
      expect(right).toBeLessThanOrEqual(VW)
      expect(top).toBeGreaterThanOrEqual(0)
      expect(bottom).toBeLessThanOrEqual(VH)
    }
  })

  it('never lets two world plates overlap', () => {
    const l = layout(scene())
    const plates = l.boxes.filter((b) => b.kind === 'world').sort((a, b) => a.x - b.x)
    expect(plates).toHaveLength(2)
    for (let i = 1; i < plates.length; i++) {
      expect(plates[i - 1].x + plates[i - 1].w).toBeLessThanOrEqual(plates[i].x)
    }
  })

  it('survives a malformed payload with the arrays missing entirely', () => {
    // A truncated or older /api/scene response must not throw inside the
    // renderer: a blank canvas beats a blank tab.
    const l = layout({ module: 'x', title: 'x' } as unknown as Scene)
    expect(l.boxes).toHaveLength(0)
    expect(l.curves).toHaveLength(0)
  })
})
