import { describe, expect, it } from 'vitest'
import { houseLayout, HVW } from './houseLayout'
import type { Scene } from './types'

// The house is a PROJECTION of the same derived scene, so the test is not
// "does it look like a house" — it is "does every visual channel still carry
// the fact it claims to". That is the rule the killed wall view failed.
//
// Fixture, written by hand: three floors, a wide directory and a narrow one on
// the same floor, a hub, one card with children and one without, and a world
// group that must become a door rather than a stair.
function scene(over: Partial<Scene> = {}): Scene {
  return {
    module: 'demo', title: 'demo', root: '', crumbs: [{ label: 'demo', root: '' }],
    total_nodes: 400, total_edges: 900,
    subsystems_total: 12, subsystems_shown: 4,
    lifted_total: 9, lifted_shown: 3,
    cards: [
      { id: 'web', label: 'web', dir: 'src/web', files: 30, decls: 90, in: 0, out: 40,
        layer: 0, row: 0, detail: 'Serve', glyph: '⇄', hub: false, children: 2 },
      { id: 'tiny', label: 'tiny', dir: 'src/tiny', files: 1, decls: 2, in: 0, out: 3,
        layer: 0, row: 1, detail: 'T', glyph: '◇', hub: false, children: 0 },
      { id: 'core', label: 'core', dir: 'src/core', files: 10, decls: 40, in: 40, out: 12,
        layer: 1, row: 0, detail: 'Charge', glyph: '◇', hub: false, children: 0 },
      { id: 'db', label: 'db', dir: 'src/db', files: 4, decls: 12, in: 52, out: 0,
        layer: 2, row: 0, detail: 'Open', glyph: '⚙', hub: true, children: 0 },
    ],
    links: [
      { from: 'web', to: 'core', relation: 'calls', label: 'CALLS', weight: 40 },
      { from: 'core', to: 'db', relation: 'calls', label: 'CALLS', weight: 12 },
      { from: 'web', to: 'world:network.http', relation: 'provides', label: 'PROVIDES', weight: 9 },
    ],
    world: [
      { id: 'world:network.http', transport: 'network.http', total: 9, provides: 9, consumes: 0,
        sensitive: 0, sample: [], truncated: false },
      { id: 'world:config.env', transport: 'config.env', total: 3, provides: 0, consumes: 3,
        sensitive: 1, sample: [], truncated: false },
    ],
    stats: [], chips: [], notes: [],
    ...over,
  }
}

describe('houseLayout', () => {
  it('puts the depended-upon code at the FOUNDATION, entry points at the roof', () => {
    const h = houseLayout(scene())
    const web = h.byId.get('web')!
    const core = h.byId.get('core')!
    const db = h.byId.get('db')!
    // layer 0 (calls everything, nothing calls it) is the top storey
    expect(web.y).toBeLessThan(core.y)
    expect(core.y).toBeLessThan(db.y)
    // and the labels say what the storey means
    expect(h.floors[0].label).toBe('ENTRY')
    expect(h.floors[h.floors.length - 1].label).toBe('FOUNDATION')
  })

  it('makes room width mean files and nothing else', () => {
    const h = houseLayout(scene())
    const web = h.byId.get('web')!   // 30 files
    const tiny = h.byId.get('tiny')! // 1 file, same floor
    expect(web.w).toBeGreaterThan(tiny.w)
    // the one-file directory still gets a readable room rather than a sliver
    expect(tiny.w).toBeGreaterThan(40)
  })

  it('keeps every room inside the walls', () => {
    // many rooms on one floor is where a naive share-out overflows
    const many = scene({
      cards: Array.from({ length: 9 }, (_, i) => ({
        id: 'r' + i, label: 'r' + i, dir: 'src/r' + i, files: i === 0 ? 200 : 1,
        decls: 1, in: 1, out: 1, layer: 0, row: i, detail: '', glyph: '◇',
        hub: false, children: 0,
      })),
      links: [],
    })
    for (const r of houseLayout(many).rooms) {
      expect(r.x).toBeGreaterThanOrEqual(0)
      expect(r.x + r.w).toBeLessThanOrEqual(HVW + 0.001)
    }
  })

  it('turns card-to-card links into stairs and world links into doors', () => {
    const h = houseLayout(scene())
    expect(h.stairs).toHaveLength(2)                       // web→core, core→db
    expect(h.stairs.some((s) => s.link.to.startsWith('world:'))).toBe(false)
    expect(h.doors).toHaveLength(2)
    // busiest transport first, and the two sides alternate
    expect(h.doors[0].world.transport).toBe('network.http')
    expect(h.doors[0].side).toBe('left')
    expect(h.doors[1].side).toBe('right')
    // stair thickness is driven by the real edge count
    expect(h.maxWeight).toBe(40)
  })

  it('survives a scene with no world, no links and no cards', () => {
    const bare = houseLayout(scene({ cards: [], links: [], world: [] }))
    expect(bare.rooms).toEqual([])
    expect(bare.stairs).toEqual([])
    expect(bare.doors).toEqual([])
    // and still reports a ground to draw the building on
    expect(Number.isFinite(bare.ground)).toBe(true)
  })
})

// The first render of this view normalised widths PER FLOOR, so every floor
// filled the building and a 44-file room looked exactly like a 5-file room —
// while the legend on screen said "room width = files". A channel that carries
// no fact is the failure the killed wall view was killed for, so the scale is
// global and this test compares ACROSS floors, which is the case that broke.
describe('houseLayout width is one scale for the whole building', () => {
  it('makes a fat directory wider than a thin one on a DIFFERENT floor', () => {
    const s: Scene = scene({
      cards: [
        { id: 'big', label: 'big', dir: 'src/big', files: 44, decls: 300, in: 0, out: 20,
          layer: 0, row: 0, detail: '', glyph: '◇', hub: false, children: 0 },
        { id: 'small', label: 'small', dir: 'src/small', files: 5, decls: 20, in: 20, out: 0,
          layer: 1, row: 0, detail: '', glyph: '◇', hub: true, children: 0 },
      ],
      links: [{ from: 'big', to: 'small', relation: 'calls', label: 'CALLS', weight: 20 }],
      world: [],
    })
    const h = houseLayout(s)
    const big = h.byId.get('big')!
    const small = h.byId.get('small')!
    expect(big.w).toBeGreaterThan(small.w * 1.5)
    // and a floor that is not the heaviest must NOT reach the walls
    expect(small.w).toBeLessThan(HVW - 2 * 210 - 1)
  })

  it('still lets a one-file room be read', () => {
    const h = houseLayout(scene())
    expect(h.byId.get('tiny')!.w).toBeGreaterThan(80)
  })
})
