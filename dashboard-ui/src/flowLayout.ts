// flowLayout turns a derived Scene into virtual-space geometry.
//
// The one rule: NOTHING here invents a relationship. Cards are placed from
// `layer`/`row`, which the server computed as longest-path depth in the lifted
// dependency DAG, so a card's COLUMN is a fact about the graph — the killed
// wall view failed precisely because "position carried no information, it was
// the sort order". A world group is placed under the mean x of the cards that
// actually reach it, so even the outer world's position is derived.

import type { Scene, SceneCard, SceneLink, SceneWorld } from './types'

export const VW = 1600
export const VH = 1000

export interface Box {
  id: string
  kind: 'card' | 'hub' | 'world'
  x: number
  y: number
  w: number
  h: number
  r: number // hub/world radius (0 for cards)
  card?: SceneCard
  world?: SceneWorld
  n: string // the printed ordinal, "01".."NN"
}

export interface Curve {
  link: SceneLink
  p0: { x: number; y: number }
  p1: { x: number; y: number }
  p2: { x: number; y: number }
  p3: { x: number; y: number }
}

export interface Layout {
  /** the virtual height this layout was composed for */
  vh: number
  /** the band the header owns; its CONTENT must scale to fit this */
  headerH: number
  /** where the footer rule sits, in virtual units */
  footerY: number
  boxes: Box[]
  byId: Map<string, Box>
  curves: Curve[]
  maxWeight: number
  /**
   * true when the cards were CLUSTERED rather than placed by dependency depth.
   * A column's x is the longest-path depth in the lifted DAG — but a scene with
   * no directed edges has no DAG, and every card lands in layer 0. Seven cards
   * in one column then overflow a fixed frame and overlap each other, which is
   * what a multi-module repo with no cross-module dependency looked like.
   *
   * Clustering keeps position meaningful without inventing a direction:
   * cards that touch each other are drawn near each other, and modules nothing
   * connects to are set aside rather than padded into a row. The flag travels
   * so the picture can SAY that x is no longer dependency depth here — the
   * killed wall view died of position that meant nothing and did not admit it.
   */
  clustered: boolean
}

// The composition adapts to the STAGE. It used to be a fixed 1600x1000 letter-
// boxed into whatever shape the window was, which left bars down the sides or
// top and bottom and made the scene look small in a large window. The width is
// still the unit (1600), but the height is whatever the stage's aspect ratio
// says it is, so the picture is full by construction rather than by zooming.
const CARD_W_MAX = 236
const CARD_W_MIN = 168
const CARD_H_MAX = 118
// 56 is where a card still shows its ordinal and its NAME and nothing else.
// Below that it stops being a card. It is reached only on a short stage with a
// full column: at vh=700 with five stacked cards and an outer-world band, no
// larger height fits without either overlapping or leaving the frame, and the
// frame is the thing the reader can see.
const CARD_H_MIN = 56
// The bands are PROPORTIONAL, with floors. Fixed 254/112/142 unit bands were
// fine at a virtual height of 1000 and catastrophic below it: a browser window
// 1908 wide and 720 tall gives a virtual height of ~605, at which the header,
// the outer-world band and the footer together claim more than the whole space
// — the card band collapsed to 7 units and the hub landed on the title.
const headerH = (vh: number) => Math.max(132, Math.min(254, vh * 0.26))
const footerH = (vh: number) => Math.max(64, Math.min(112, vh * 0.13))
// An OPEN plate has to fit its heading and at least one row of door names.
// 92 did not: (92-76)/24 rounds to zero rows, so an expanded plate printed
// "showing 0 of 25" — it took the space, hid the names, and told the reader
// the store had nothing, which was false. A plate that cannot show a name is
// strictly worse than the chip it replaced.
const worldH = (vh: number) => Math.max(112, Math.min(WORLD_H, vh * 0.16))
const HUB_R = 100
// The outer world is a PLATE, not a ring: a transport with 205 ports needs its
// door names in rows, and the killed wall view proved a perimeter of names is
// unreadable long before 205. A plate lists a bounded sample in a grid and
// prints "sample N of M" underneath.
// RELATION PALETTE. A curve's colour is the KIND of dependency it stands for,
// not decoration: `calls` is behaviour crossing a boundary, `imports` is only
// structure, `provides`/`consumes` leave the system entirely. They were all the
// same grey with the same blue dots, which threw away the one thing the store
// knows about an edge that its thickness does not say.
export const RELATION_STYLE: Record<string, { line: string; flow: string; label: string; dashed?: boolean }> = {
  calls:    { line: '#b9a9d8', flow: '#6d4ec4', label: '#6d4ec4' },
  imports:  { line: '#c9cec2', flow: '#7f8c74', label: '#7f8c74' },
  provides: { line: '#a8cbb4', flow: '#1f8a55', label: '#1f8a55' },
  consumes: { line: '#e3c48f', flow: '#b9761a', label: '#b9761a' },
  // Module grain (ADR 22). `depends` is the strongest edge in the store — two
  // manifests, both EXTRACTED — so it gets the strongest ink. `shares` means
  // "both call the same external service" and is NOT a call between them; it
  // is drawn dashed and pale so it can never be misread as one.
  depends:  { line: '#9db4d8', flow: '#2f5fa8', label: '#2f5fa8' },
  shares:   { line: '#d8cfc2', flow: '#9a8f7e', label: '#9a8f7e', dashed: true },
  // one module CONSUMES a port another PROVIDES: a real call between them, so
  // it reads like `calls` does everywhere else rather than inventing a colour
}
export const RELATION_FALLBACK = { line: '#cfcabf', flow: '#4a5cd0', label: '#8a857c', dashed: false }
export const relationStyle = (rel: string) => RELATION_STYLE[rel] || RELATION_FALLBACK

// A world group is a CHIP until you ask for it. Expanded plates were taking a
// third of the canvas to show six sampled port names — detail — while the
// modules, which are the actual work, were squeezed into what was left. The
// chip states the transport and the count, which is the part worth seeing at a
// glance; the names are one click away.
export const CHIP_W = 196
export const CHIP_H = 34

export const WORLD_W = 288
export const WORLD_H = 142

/** rectOf is a box's bounding rectangle — the hub's circle included. */
function rectOf(b: Box) {
  return b.kind === 'hub'
    ? { x: b.x - b.r, y: b.y - b.r, w: b.r * 2, h: b.r * 2 }
    : { x: b.x, y: b.y, w: b.w, h: b.h }
}

const PAD = 10

/**
 * resolveOverlaps separates shapes that sit on each other, and keeps every
 * shape inside the frame. It moves boxes vertically only: x carries the
 * dependency depth and must not be negotiated away to make room.
 *
 * Bounded iteration, not a solver: a handful of passes settles a scene of at
 * most seven cards plus a hub plus a few plates, and a layout that could loop
 * is worse than one that leaves a 2px overlap.
 */
function resolveOverlaps(boxes: Box[], top: number, bottom: number) {
  const move = (b: Box, dy: number) => { b.y += dy }
  for (let pass = 0; pass < 24; pass++) {
    let worst = 0
    for (let i = 0; i < boxes.length; i++) {
      for (let j = i + 1; j < boxes.length; j++) {
        const a = rectOf(boxes[i])
        const c = rectOf(boxes[j])
        const ox = Math.min(a.x + a.w, c.x + c.w) - Math.max(a.x, c.x)
        const oy = Math.min(a.y + a.h, c.y + c.h) - Math.max(a.y, c.y)
        if (ox <= -PAD || oy <= -PAD) continue // already clear on one axis
        const need = (oy + PAD) / 2
        if (need <= 0) continue
        worst = Math.max(worst, need)
        // push the lower one down and the higher one up, by half each
        if (a.y + a.h / 2 <= c.y + c.h / 2) { move(boxes[i], -need); move(boxes[j], need) }
        else { move(boxes[i], need); move(boxes[j], -need) }
      }
    }
    // then put everything back inside the frame; this can re-introduce an
    // overlap, which the next pass takes out again
    for (const b of boxes) {
      const r = rectOf(b)
      if (r.y < top) move(b, top - r.y)
      const r2 = rectOf(b)
      if (r2.y + r2.h > bottom) move(b, bottom - (r2.y + r2.h))
    }
    if (worst < 0.5) break
  }
}

function centerOf(b: Box) {
  return b.kind === 'hub' ? { x: b.x, y: b.y } : { x: b.x + b.w / 2, y: b.y + b.h / 2 }
}

// anchor picks the point on a box nearest the direction of travel — the same
// four-sided anchoring the reference uses, chosen instead of hand-authored.
function anchor(b: Box, toward: { x: number; y: number }) {
  const c = centerOf(b)
  const dx = toward.x - c.x
  const dy = toward.y - c.y
  if (b.kind === 'hub') {
    const m = Math.hypot(dx, dy) || 1
    return { x: c.x + (dx / m) * b.r, y: c.y + (dy / m) * b.r }
  }
  if (Math.abs(dx) * b.h >= Math.abs(dy) * b.w) {
    return { x: dx >= 0 ? b.x + b.w : b.x, y: c.y }
  }
  return { x: c.x, y: dy >= 0 ? b.y + b.h : b.y }
}

export function layout(scene: Scene, vh: number = VH, expanded: string = ''): Layout {
  const cards = scene.cards || []
  const worldGroups = scene.world || []
  const bandTop = headerH(vh)
  const footerY = vh - footerH(vh)
  // only the OPEN group needs plate height; the rest are chips
  const wH = worldH(vh)
  const bandH = worldGroups.some((w) => w.id === expanded) ? wH : CHIP_H
  // The outer-world band is only reserved when there IS an outer world. A store
  // with no `port` nodes was leaving a third of the canvas blank and holding it
  // for something that would never draw — the same "an absent feature looks
  // like empty space" failure the missing-boundaries note fixed in the payload.
  const worldY = worldGroups.length > 0 ? footerY - 24 - bandH : 0
  const bandBottom = worldGroups.length > 0 ? worldY - 40 : footerY - 30
  const boxes: Box[] = []
  const byId = new Map<string, Box>()

  const layers = Math.max(1, ...cards.map((c) => c.layer + 1))
  const perLayer = new Map<number, SceneCard[]>()
  for (const c of cards) {
    const l = perLayer.get(c.layer) || []
    l.push(c)
    perLayer.set(c.layer, l)
  }

  const left = 72
  const right = VW - 72
  const span = right - left
  const colW = span / layers
  // Cards must never touch: the column count is derived, so the card width has
  // to follow it rather than the other way round.
  const cardW = Math.max(CARD_W_MIN, Math.min(CARD_W_MAX, colW - 26))

  // Card HEIGHT follows the fullest column, for the same reason width follows
  // the column count. With a fixed frame there is no camera to pan a column
  // that does not fit, so the cards shrink instead of running out of the
  // bottom — five stacked cards plus the outer-world band do not fit in 1000
  // units at full height, and the alternative to shrinking is losing them.
  // A column's budget counts the HUB as what it is: a circle wider than a card.
  // Sizing the hub from a separate constant meant a column holding both asked
  // for more than the band, and the separation pass could only shuffle the
  // shortfall around.
  const HUB_FACTOR = 1.6
  const weightOf = (cs: SceneCard[]) => cs.reduce((a, c) => a + (c.hub ? HUB_FACTOR : 1), 0)
  const tallest = Math.max(1, ...[...perLayer.values()].map((c) => c.length))
  const heaviest = Math.max(1, ...[...perLayer.values()].map(weightOf))
  const band = Math.max(CARD_H_MIN, bandBottom - bandTop)
  // n cards plus (n-1) gaps have to fit the band. Dividing by (n-1) asked for
  // more room than exists — at a 500-unit stage it produced two 118-unit cards
  // for a 147-unit band, and the separation pass then spent every one of its
  // passes failing to fix an impossible layout.
  const GAP_Y = 14
  const cardH = Math.max(
    CARD_H_MIN,
    Math.min(CARD_H_MAX, (band - (tallest - 1) * GAP_Y) / heaviest),
  )
  // The hub is a circle and the only shape that can reach the header; it is
  // sized from the band it has to sit in, not from a constant.
  const hubR = Math.max(40, Math.min(HUB_R, (cardH * HUB_FACTOR) / 2))

  // No topology to draw. One layer means no card depends on another, so there
  // is no left-to-right to read; a column is then a flow chart of nothing, and
  // past four cards it does not even fit the frame — seven modules stacked,
  // overlapping, with the last one past the footer.
  //
  // What IS in the data is who touches whom. So the cards are CLUSTERED:
  // connected ones sit together, and modules nothing connects to are set aside
  // in their own column. Adjacency then means something — it is the only thing
  // position can honestly carry when there is no direction to read.
  const clustered = layers === 1 && cards.length > 3 && !cards.some((c) => c.hub)
  let ordinal = 0
  if (clustered) {
    const band = Math.max(CARD_H_MIN, bandBottom - bandTop)
    const idx = new Map(cards.map((c, i) => [c.id, i]))
    const deg = cards.map(() => 0)
    const pairs: Array<[number, number]> = []
    for (const lk of scene.links || []) {
      const a = idx.get(lk.from), b = idx.get(lk.to)
      if (a === undefined || b === undefined || a === b) continue
      pairs.push([a, b]); deg[a]++; deg[b]++
    }
    const linkedCards = cards.filter((_, i) => deg[i] > 0)
    const loners = cards.filter((_, i) => deg[i] === 0)

    // The lone modules take a narrow column on the right; the connected ones
    // get the rest. With none of either, the split costs nothing.
    const loneW = loners.length > 0 ? Math.min(span * 0.28, CARD_W_MAX + 40) : 0
    const mainW = span - loneW
    const cw = Math.max(CARD_W_MIN, Math.min(CARD_W_MAX,
      mainW / Math.max(2, Math.ceil(Math.sqrt(Math.max(1, linkedCards.length)))) - 26))
    const ch = Math.max(CARD_H_MIN, Math.min(CARD_H_MAX,
      band / Math.max(2, Math.ceil(Math.sqrt(Math.max(1, linkedCards.length)))) - GAP_Y))

    // Seeded on a circle by rank, then a FIXED number of relaxation passes:
    // linked pairs pull together, every pair pushes apart. Deterministic — no
    // clock, no randomness — so the same scene always draws the same picture,
    // which is what makes a saved arrangement worth saving.
    const pos = new Map<string, { x: number; y: number }>()
    const cx0 = left + mainW / 2
    const cy0 = bandTop + band / 2
    const rx = Math.max(cw, mainW / 2 - cw / 2 - 12)
    const ry = Math.max(ch, band / 2 - ch / 2 - 12)
    linkedCards.forEach((c, i) => {
      const t = (i / Math.max(1, linkedCards.length)) * Math.PI * 2
      pos.set(c.id, { x: cx0 + Math.cos(t) * rx * 0.7, y: cy0 + Math.sin(t) * ry * 0.7 })
    })
    for (let pass = 0; pass < 60; pass++) {
      for (const [a, b] of pairs) {
        const pa = pos.get(cards[a].id), pb = pos.get(cards[b].id)
        if (!pa || !pb) continue
        const dx = pb.x - pa.x, dy = pb.y - pa.y
        const d = Math.hypot(dx, dy) || 1
        const want = cw * 1.5
        const k = ((d - want) / d) * 0.06
        pa.x += dx * k; pa.y += dy * k; pb.x -= dx * k; pb.y -= dy * k
      }
      for (let i = 0; i < linkedCards.length; i++) {
        for (let j = i + 1; j < linkedCards.length; j++) {
          const pa = pos.get(linkedCards[i].id)!, pb = pos.get(linkedCards[j].id)!
          const dx = pb.x - pa.x, dy = pb.y - pa.y
          const d = Math.hypot(dx, dy) || 1
          const min = Math.hypot(cw + 24, ch + GAP_Y)
          if (d >= min) continue
          const k = ((d - min) / d) * 0.5
          pa.x += dx * k; pa.y += dy * k; pb.x -= dx * k; pb.y -= dy * k
        }
      }
      for (const c of linkedCards) {
        const p = pos.get(c.id)!
        p.x = Math.min(left + mainW - cw / 2 - 8, Math.max(left + cw / 2 + 8, p.x))
        p.y = Math.min(bandBottom - ch / 2, Math.max(bandTop + ch / 2, p.y))
      }
    }
    for (const c of linkedCards) {
      ordinal++
      const p = pos.get(c.id)!
      const b: Box = { id: c.id, kind: 'card', x: p.x - cw / 2, y: p.y - ch / 2, w: cw, h: ch, r: 0, card: c,
        n: String(cards.indexOf(c) + 1).padStart(2, '0') }
      boxes.push(b); byId.set(b.id, b)
    }
    // The lone column is a column only while there is a cluster to sit beside.
    // With nothing connected at all — a repo of entirely independent modules —
    // it is the whole picture, and squeezing it into 28% of the width stacked
    // six cards into space for three. Then it is a grid across the full frame.
    const laneW = linkedCards.length > 0 ? loneW : span
    const laneX = linkedCards.length > 0 ? left + mainW : left
    const lcols = linkedCards.length > 0
      ? 1
      : Math.min(loners.length, Math.max(1, Math.round(Math.sqrt((loners.length * (span / band)) / 2))))
    const lrows = Math.max(1, Math.ceil(loners.length / lcols))
    const cellW = laneW / lcols
    const lw = Math.max(CARD_W_MIN, Math.min(CARD_W_MAX, cellW - 34))
    // The row step has to fit the band: n cards and (n-1) gaps, never more.
    const lh = Math.max(CARD_H_MIN, Math.min(ch, (band - (lrows - 1) * GAP_Y) / lrows))
    const lstep = lrows > 1 ? Math.max(lh + GAP_Y, (band - lh) / (lrows - 1)) : 0
    loners.forEach((c, i) => {
      const x = laneX + cellW * ((i % lcols) + 0.5) - lw / 2
      const y = bandTop + (lrows > 1 ? Math.floor(i / lcols) * lstep : band / 2 - lh / 2)
      const b: Box = { id: c.id, kind: 'card', x, y, w: lw, h: lh, r: 0, card: c,
        n: String(cards.indexOf(c) + 1).padStart(2, '0') }
      boxes.push(b); byId.set(b.id, b)
    })
  } else
  for (let l = 0; l < layers; l++) {
    const col = (perLayer.get(l) || []).slice().sort((a, b) => a.row - b.row)
    const cx = left + colW * (l + 0.5)
    // Cards must never touch VERTICALLY either. Dividing the band by the count
    // silently overlapped them once a column held four cards: (596-254)/3 is
    // 114 against a card height of 118. cardH is now sized so the fullest
    // column fits, and the step is never allowed below it.
    // bandTop..centerBottom bounds card CENTRES, not their edges. Using the
    // band's bottom directly put the last card's lower half past the footer,
    // where with a fixed frame it is simply gone.
    // bandTop..bandBottom bounds card EDGES; these are centres, so both ends
    // move in by half a card. Using the band directly as the first centre put
    // the top card's upper half above the header, where the separation pass
    // then shoved the whole column down and re-created the overlap it exists
    // to remove.
    // both ends move in by half the TALLEST shape in this column, which is the
    // hub wherever the hub is
    const half = (col.some((c) => c.hub) ? hubR * 2 : cardH) / 2
    const centerTop = bandTop + half
    const centerBottom = bandBottom - half
    const step = col.length > 1
      ? Math.max(half * 2 + GAP_Y, (centerBottom - centerTop) / (col.length - 1))
      : 0
    // A column holding ONE card has a free y: the row slot only orders cards
    // within a layer, so with a single card there is nothing to order. Seven
    // such columns drew a thin horizontal strip across the middle of a tall
    // stage. Alternating them uses the height and pulls the arrows apart —
    // x still carries the dependency depth, which is the part that means
    // something.
    const mid = (centerTop + centerBottom) / 2
    const sway = Math.min((centerBottom - centerTop) / 2, cardH * 0.9)
    const y0 = col.length > 1
      ? centerTop
      : layers > 2 ? mid + (l % 2 === 0 ? -sway : sway) : mid
    col.forEach((c, i) => {
      ordinal++
      const n = String(ordinal).padStart(2, '0')
      const cy = y0 + step * i
      const b: Box = c.hub
        ? { id: c.id, kind: 'hub', x: cx, y: cy, w: 0, h: 0, r: hubR, card: c, n }
        : { id: c.id, kind: 'card', x: cx - cardW / 2, y: cy - cardH / 2, w: cardW, h: cardH, r: 0, card: c, n }
      boxes.push(b)
      byId.set(b.id, b)
    })
  }

  // The world band sits BELOW whatever the cards actually needed. It used to be
  // a fixed Y, so the moment a column grew to fit four cards the plates were
  // underneath them — the fix for one overlap created another.
  let lowest = 0
  for (const b of boxes) {
    lowest = Math.max(lowest, b.kind === 'hub' ? b.y + b.r : b.y + b.h)
  }
  // The plate follows the cards down, but never past the footer: the frame is
  // fixed now, so anything that runs out of the bottom is simply gone. On a
  // short stage a tall column can reach the band — the plate then sits at the
  // lowest position that still fits, and is drawn last so it stays readable.
  const worldTop = Math.min(footerY - bandH - 8, Math.max(worldY, lowest + 30))

  // World groups: x = mean x of the cards that actually link to them, so a
  // transport sits under the subsystems that open it. Collisions are pushed
  // apart, order preserved.
  const worlds = worldGroups
  const linked = new Map<string, number[]>()
  for (const lk of scene.links || []) {
    if (!lk.to.startsWith('world:')) continue
    const from = byId.get(lk.from)
    if (!from) continue
    const arr = linked.get(lk.to) || []
    arr.push(centerOf(from).x)
    linked.set(lk.to, arr)
  }
  const placed = worlds
    .map((w, i) => {
      const xs = linked.get(w.id) || []
      const x = xs.length > 0
        ? xs.reduce((a, b) => a + b, 0) / xs.length
        : left + (span * (i + 0.5)) / Math.max(1, worlds.length)
      return { w, x }
    })
    .sort((a, b) => a.x - b.x)
  const minGap = (expanded ? WORLD_W : CHIP_W) + 34
  for (let i = 1; i < placed.length; i++) {
    if (placed[i].x - placed[i - 1].x < minGap) placed[i].x = placed[i - 1].x + minGap
  }
  const half = (expanded ? WORLD_W : CHIP_W) / 2
  const overflow = placed.length > 0 ? placed[placed.length - 1].x - (right - half) : 0
  if (overflow > 0) for (const p of placed) p.x -= overflow
  for (const p of placed) {
    const x = Math.min(right - half, Math.max(left + half, p.x))
    const open = p.w.id === expanded
    const bw = open ? WORLD_W : CHIP_W
    const b: Box = {
      id: p.w.id, kind: 'world', x: x - bw / 2, y: worldTop,
      w: bw, h: open ? wH : CHIP_H, r: 0, world: p.w, n: '',
    }
    boxes.push(b)
    byId.set(b.id, b)
  }

  // SHAPE COLLISION. Lines may cross — an arrow over an arrow is readable, and
  // forbidding it would mean moving cards away from the columns that carry their
  // meaning. Shapes may NOT: a card under the hub, or the hub on the title, is
  // just lost information.
  //
  // So this is a separation pass, not a layout: it starts from the derived
  // positions and moves the minimum needed to stop shapes sitting on each other
  // or leaving the frame. Cards keep their column (x is left alone) because the
  // column IS the dependency depth; only y is negotiable.
  resolveOverlaps(boxes, bandTop, footerY)

  // Curves. Bend is derived too: parallel links between the same pair fan out
  // by index so a CALLS and an IMPORTS arrow between two cards stay separable.
  const pairSeen = new Map<string, number>()
  const curves: Curve[] = []
  let maxWeight = 1
  for (const lk of scene.links || []) {
    const a = byId.get(lk.from)
    const b = byId.get(lk.to)
    if (!a || !b) continue
    maxWeight = Math.max(maxWeight, lk.weight)
    const key = lk.from + ' ' + lk.to
    const k = pairSeen.get(key) || 0
    pairSeen.set(key, k + 1)
    const p0 = anchor(a, centerOf(b))
    const p3 = anchor(b, centerOf(a))
    const dx = p3.x - p0.x
    const dy = p3.y - p0.y
    const len = Math.hypot(dx, dy) || 1
    const nx = -dy / len
    const ny = dx / len
    const bend = (k === 0 ? 0.08 : k % 2 === 1 ? -0.2 * Math.ceil(k / 2) : 0.2 * (k / 2)) * len
    curves.push({
      link: lk,
      p0,
      p1: { x: p0.x + dx * 0.32 + nx * bend, y: p0.y + dy * 0.32 + ny * bend },
      p2: { x: p0.x + dx * 0.68 + nx * bend, y: p0.y + dy * 0.68 + ny * bend },
      p3,
    })
  }
  return { vh, headerH: bandTop, footerY, boxes, byId, curves, maxWeight, clustered }
}

export function bez(
  p0: { x: number; y: number }, p1: { x: number; y: number },
  p2: { x: number; y: number }, p3: { x: number; y: number }, t: number,
) {
  const u = 1 - t
  const a = u * u * u, b = 3 * u * u * t, c = 3 * u * t * t, d = t * t * t
  return { x: a * p0.x + b * p1.x + c * p2.x + d * p3.x, y: a * p0.y + b * p1.y + c * p2.y + d * p3.y }
}

export function bezT(
  p0: { x: number; y: number }, p1: { x: number; y: number },
  p2: { x: number; y: number }, p3: { x: number; y: number }, t: number,
) {
  const u = 1 - t
  const x = 3 * u * u * (p1.x - p0.x) + 6 * u * t * (p2.x - p1.x) + 3 * t * t * (p3.x - p2.x)
  const y = 3 * u * u * (p1.y - p0.y) + 6 * u * t * (p2.y - p1.y) + 3 * t * t * (p3.y - p2.y)
  const m = Math.hypot(x, y) || 1
  return { x: x / m, y: y / m }
}
