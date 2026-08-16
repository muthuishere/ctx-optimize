// houseLayout turns a derived Scene into a CUTAWAY BUILDING.
//
// The rule that killed the wall view applies here in full: a visual channel
// that carries no fact is decoration, and decoration is what "a map with no
// routes is a list in a costume" was about. So every channel is pinned to
// something the store measured:
//
//   floor number   = card.layer, the longest-path depth in the lifted
//                    dependency DAG. Depended-upon code sits at the BOTTOM,
//                    because that is what a foundation is — the hub is the
//                    ground floor, and the entry points are the roof.
//   room width     = files in that directory (bounded), so a fat room is a
//                    fat directory and nothing else.
//   room order     = card.row, the same slot the flow view uses.
//   stair weight   = the lifted edge count between two floors.
//   door on the    = one port transport, with the number of ports on it. Doors
//   outside wall     are on the OUTSIDE because that is what a boundary is.
//   load-bearing   = the hub, drawn as the pillar the floors rest on.
//
// Nothing here invents a relationship, and nothing is placed by eye.
import type { Scene, SceneCard, SceneLink, SceneWorld } from './types'

export const HVW = 1600
export const HVH = 1000

export interface Room {
  card: SceneCard
  x: number
  y: number
  w: number
  h: number
  floor: number
  n: string
}

export interface Stair {
  link: SceneLink
  from: Room
  to: Room
}

export interface Door {
  world: SceneWorld
  x: number
  y: number
  w: number
  h: number
  side: 'left' | 'right'
}

export interface House {
  rooms: Room[]
  byId: Map<string, Room>
  stairs: Stair[]
  doors: Door[]
  floors: { y: number; h: number; layer: number; label: string }[]
  ground: number
  roof: number
  maxWeight: number
}

const MARGIN_X = 210 // the strip outside the walls, where the doors hang
const TOP = 236
const BOTTOM = 812
const ROOM_GAP = 14
const FLOOR_GAP = 10

export function houseLayout(scene: Scene): House {
  const cards = scene.cards || []
  const links = scene.links || []

  // Floors, deepest layer at the BOTTOM. layer 0 is the most dependent code
  // (it calls everything and nothing calls it) so it belongs at the top: a
  // building is read from its foundation up, and so is a dependency graph.
  const layers = Array.from(new Set(cards.map((c) => c.layer))).sort((a, b) => a - b)
  const nFloors = Math.max(1, layers.length)
  const span = BOTTOM - TOP
  const floorH = (span - FLOOR_GAP * (nFloors - 1)) / nFloors

  const floors = layers.map((layer, i) => ({
    layer,
    // i = 0 is layer 0, drawn at the TOP
    y: TOP + i * (floorH + FLOOR_GAP),
    h: floorH,
    label: i === layers.length - 1 ? 'FOUNDATION' : i === 0 ? 'ENTRY' : `LEVEL ${layers.length - 1 - i}`,
  }))

  const left = MARGIN_X
  const right = HVW - MARGIN_X
  const inner = right - left

  const rooms: Room[] = []
  const byId = new Map<string, Room>()
  let ordinal = 0

  for (const f of floors) {
    const on = cards.filter((c) => c.layer === f.layer).sort((a, b) => a.row - b.row)
    if (on.length === 0) continue
    // Width is proportional to FILES, floored so a one-file directory is still
    // a readable room, then normalised to the floor. A room's width is the only
    // thing on screen that says "this directory is big".
    const weights = on.map((c) => Math.max(1, c.files))
    const total = weights.reduce((a, b) => a + b, 0)
    const usable = inner - ROOM_GAP * (on.length - 1)
    const minW = Math.min(120, usable / on.length)
    let x = left
    on.forEach((c, i) => {
      const raw = (weights[i] / total) * usable
      const w = Math.max(minW, raw)
      ordinal++
      const r: Room = {
        card: c, x, y: f.y, w, h: f.h, floor: f.layer,
        n: String(ordinal).padStart(2, '0'),
      }
      rooms.push(r)
      byId.set(c.id, r)
      x += w + ROOM_GAP
    })
    // The min-width floor can overflow the wall; squeeze the row back inside so
    // a room never hangs outside the building it is in.
    const overflow = x - ROOM_GAP - right
    if (overflow > 0) {
      const row = rooms.filter((r) => r.floor === f.layer)
      const k = (inner - ROOM_GAP * (row.length - 1)) / (inner - ROOM_GAP * (row.length - 1) + overflow)
      let cx = left
      for (const r of row) { r.w *= k; r.x = cx; cx += r.w + ROOM_GAP }
    }
  }

  const stairs: Stair[] = []
  let maxWeight = 1
  for (const l of links) {
    const from = byId.get(l.from)
    const to = byId.get(l.to)
    if (!from || !to) continue // world links become doors, not stairs
    stairs.push({ link: l, from, to })
    if (l.weight > maxWeight) maxWeight = l.weight
  }

  // Doors hang on the outside wall, alternating sides, ordered by how many
  // ports they carry — the busiest boundary sits nearest the ground.
  const doors: Door[] = []
  const worlds = (scene.world || []).slice().sort((a, b) => b.total - a.total)
  worlds.forEach((w, i) => {
    const side = i % 2 === 0 ? 'left' : 'right'
    const slot = Math.floor(i / 2)
    const h = 88
    const y = TOP + 40 + slot * (h + 22)
    doors.push({
      world: w, side, h, w: MARGIN_X - 46,
      x: side === 'left' ? 24 : HVW - MARGIN_X + 22,
      y: Math.min(y, BOTTOM - h),
    })
  })

  return {
    rooms, byId, stairs, doors, floors,
    ground: floors.length ? floors[floors.length - 1].y + floors[floors.length - 1].h : BOTTOM,
    roof: TOP,
    maxWeight,
  }
}
