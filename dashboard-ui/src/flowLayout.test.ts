import { describe, expect, it } from 'vitest'
import { VH, VW, cardRows, layout } from './flowLayout'
import type { Scene } from './types'

// A scene shaped by hand: three layers, one hub, two transports. Every
// expectation below is written from THIS object, never captured from layout().
function scene(): Scene {
  return {
    module: 'demo',
    title: 'demo',
    root: '',
    level: 'directory',
    crumbs: [{ label: 'demo', root: '' }],
    questions: [],
    inside: [],
    total_nodes: 100,
    total_edges: 200,
    subsystems_total: 20,
    subsystems_shown: 4,
    lifted_total: 9,
    lifted_shown: 5,
    cards: [
      { id: 'a', label: 'a', dir: 'src/a', files: 3, decls: 9, in: 0, out: 20, ext_in: 0, ext_out: 0, layer: 0, row: 0, detail: 'A', glyph: '⇄', hub: false, children: 2, top: 'Fn', enter_grain: '', inner: 1 },
      { id: 'b', label: 'b', dir: 'src/b', files: 3, decls: 9, in: 20, out: 6, ext_in: 0, ext_out: 0, layer: 0, row: 1, detail: 'B', glyph: '◇', hub: false, children: 0, top: 'Fn', enter_grain: '', inner: 1 },
      { id: 'c', label: 'c', dir: 'src/c', files: 3, decls: 9, in: 26, out: 4, ext_in: 0, ext_out: 0, layer: 1, row: 0, detail: 'C', glyph: '◇', hub: false, children: 3, top: 'Fn', enter_grain: '', inner: 1 },
      { id: 'd', label: 'd', dir: 'src/d', files: 3, decls: 9, in: 30, out: 0, ext_in: 0, ext_out: 0, layer: 2, row: 0, detail: 'D', glyph: '⚙', hub: true, children: 0, top: 'Fn', enter_grain: '', inner: 1 },
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

// Four cards in one column silently overlapped: the band was divided by the
// count, and (596-254)/3 is 114 against a card height of 118. Seen on
// mm/kasan/common.c, where the declaration level puts four cards in a column.
//
// This checks overlap in BOTH axes, not just the vertical gap in a column.
// The original version sorted by y and compared consecutive cards, which only
// holds while every card is in one column — and cards with no dependency
// between them are no longer laid out that way (a repo of unrelated modules is
// a set, not a flow). Two cards side by side read as a huge negative "gap" and
// failed a layout that was correct, while a genuine horizontal overlap would
// have passed. Rectangle intersection is what the rule always meant.
describe('flowLayout never overlaps cards', () => {
  it('leaves every pair of cards clear of each other, however many there are', () => {
    for (const n of [2, 3, 4, 6, 9]) {
      const s = scene()
      s.cards = Array.from({ length: n }, (_, i) => ({
        ...s.cards[0], id: 'c' + i, label: 'c' + i, dir: 'src/c' + i,
        layer: 0, row: i, hub: false,
      }))
      s.links = []
      const boxes = layout(s).boxes.filter((b) => b.kind === 'card')
      for (let i = 0; i < boxes.length; i++) {
        for (let j = i + 1; j < boxes.length; j++) {
          const a = boxes[i], b = boxes[j]
          const ox = Math.min(a.x + a.w, b.x + b.w) - Math.max(a.x, b.x)
          const oy = Math.min(a.y + a.h, b.y + b.h) - Math.max(a.y, b.y)
          expect(ox > 0 && oy > 0,
            `${n} cards: ${a.id} and ${b.id} overlap by ${ox.toFixed(1)}x${oy.toFixed(1)}`).toBe(false)
        }
      }
    }
  })

  it('keeps unconnected modules out of the cluster, and inside the frame', () => {
    const s = scene()
    s.cards = Array.from({ length: 6 }, (_, i) => ({
      ...s.cards[0], id: 'c' + i, label: 'c' + i, dir: 'src/c' + i,
      layer: 0, row: i, hub: false,
    }))
    // c0..c2 touch each other; c3..c5 touch nothing.
    s.links = [
      { from: 'c0', to: 'c1', relation: 'shares', label: 'BOTH CALL', weight: 2 },
      { from: 'c1', to: 'c2', relation: 'shares', label: 'BOTH CALL', weight: 1 },
    ]
    const l = layout(s)
    expect(l.clustered).toBe(true)
    const at = (id: string) => l.byId.get(id)!
    const linkedRight = Math.max(at('c0').x + at('c0').w, at('c1').x + at('c1').w, at('c2').x + at('c2').w)
    for (const id of ['c3', 'c4', 'c5']) {
      expect(at(id).x, `${id} should sit apart from the connected cluster`)
        .toBeGreaterThanOrEqual(linkedRight - at(id).w)
    }
    for (const b of l.boxes.filter((x) => x.kind === 'card')) {
      expect(b.x).toBeGreaterThanOrEqual(0)
      expect(b.x + b.w).toBeLessThanOrEqual(VW)
      expect(b.y + b.h).toBeLessThanOrEqual(l.footerY)
    }
  })

  it('still lays out by dependency depth when there IS a direction to read', () => {
    const s = scene()
    s.cards = Array.from({ length: 6 }, (_, i) => ({
      ...s.cards[0], id: 'c' + i, label: 'c' + i, dir: 'src/c' + i,
      layer: i < 3 ? 0 : 1, row: i % 3, hub: false,
    }))
    s.links = [{ from: 'c0', to: 'c3', relation: 'depends', label: 'DEPENDS', weight: 1 }]
    const l = layout(s)
    expect(l.clustered).toBe(false)
    expect(l.byId.get('c3')!.x).toBeGreaterThan(l.byId.get('c0')!.x)
  })
})

// SHAPES MAY NOT OVERLAP. Lines may — an arrow crossing an arrow is readable,
// and forbidding it would mean moving cards off the columns that carry their
// meaning. A card under the hub, or a world plate on a card, is lost
// information. This replaces a narrower rule ("the world band sits below the
// lowest card") which the separation pass supersedes: the requirement was never
// the ordering, it was that nothing sits on anything.
describe('flowLayout never lets two shapes overlap', () => {
  const rect = (b: { kind: string; x: number; y: number; w: number; h: number; r: number }) =>
    b.kind === 'hub'
      ? { x: b.x - b.r, y: b.y - b.r, w: b.r * 2, h: b.r * 2 }
      : { x: b.x, y: b.y, w: b.w, h: b.h }

  for (const vh of [500, 605, 700, 1000, 1400]) {
    for (const n of [1, 2, 3, 5, 7]) {
      it(`${n} cards at vh=${vh}`, () => {
        const s = scene()
        s.cards = Array.from({ length: n }, (_, i) => ({
          ...s.cards[0], id: 'c' + i, label: 'c' + i, dir: 'src/c' + i,
          layer: i % 3, row: Math.floor(i / 3), hub: i === n - 1,
        }))
        const lay = layout(s, vh)
        for (let i = 0; i < lay.boxes.length; i++) {
          for (let j = i + 1; j < lay.boxes.length; j++) {
            const a = rect(lay.boxes[i]), b = rect(lay.boxes[j])
            const ox = Math.min(a.x + a.w, b.x + b.w) - Math.max(a.x, b.x)
            const oy = Math.min(a.y + a.h, b.y + b.h) - Math.max(a.y, b.y)
            expect(ox > 1 && oy > 1,
              `${lay.boxes[i].id} and ${lay.boxes[j].id} overlap by ${Math.round(ox)}x${Math.round(oy)}`)
              .toBe(false)
          }
        }
      })
    }
  }
})

// With a fixed frame there is no camera to pan to something that ran out of the
// bottom — it is simply gone. The band bounds card CENTRES, and using it as if
// it bounded their edges put the last card's lower half through the footer.
describe('flowLayout keeps everything inside the frame', () => {
  for (const vh of [700, 870, 1000, 1400]) {
    it(`no card crosses the footer at vh=${vh}`, () => {
      for (const n of [1, 2, 3, 4, 5]) {
        const s = scene()
        s.cards = Array.from({ length: n }, (_, i) => ({
          ...s.cards[0], id: 'c' + i, label: 'c' + i, dir: 'src/c' + i,
          layer: 0, row: i, hub: false,
        }))
        const lay = layout(s, vh)
        for (const b of lay.boxes) {
          const bottom = b.kind === 'hub' ? b.y + b.r : b.y + b.h
          expect(bottom, `${n} cards at vh=${vh}: ${b.id} runs past the footer`)
            .toBeLessThanOrEqual(lay.footerY)
          expect(b.y, `${n} cards at vh=${vh}: ${b.id} starts above the header`)
            .toBeGreaterThan(100)
        }
      }
    })
  }
})

// Every card in the volentis screenshot printed two lines of text on one line:
// the module's path and its summary, straight through each other. The rows had
// independent height guards — path at y+91 if h>=100, detail at y+h-15 if
// h>=86 — and between 100 and 118 units those two positions ARE each other.
// Clustered cards live in exactly that band, so it was every card on screen.
describe('a card never prints two lines on one line', () => {
  it('keeps the path and the detail apart at every height a card can be', () => {
    for (let h = 56; h <= 140; h++) {
      const r = cardRows(h)
      if (r.showDir && r.showDetail) {
        expect(r.detailY - r.dirY, `h=${h}: path and detail collide`).toBeGreaterThanOrEqual(12)
      }
      if (r.showDir) {
        expect(r.dirY, `h=${h}: path printed through the name`).toBeGreaterThan(r.titleBase + 6)
      }
      if (r.showDetail) {
        expect(r.detailY, `h=${h}: detail printed through the name`).toBeGreaterThan(r.titleBase + 6)
        expect(r.detailY, `h=${h}: detail printed outside the card`).toBeLessThanOrEqual(h)
      }
    }
  })

  it('drops the path before the detail, because the detail says more', () => {
    // at the height where only one line fits, it is the summary that survives
    const only = Array.from({ length: 90 }, (_, i) => cardRows(i + 56))
      .filter((r) => r.showDetail && !r.showDir)
    expect(only.length).toBeGreaterThan(0)
  })

  it('drops both lines on a card too short for either', () => {
    const r = cardRows(56)
    expect(r.showDir).toBe(false)
    expect(r.tight).toBe(true)
  })
})
