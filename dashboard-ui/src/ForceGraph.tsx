import { useEffect, useRef } from 'react'
import type { Edge, Node } from './types'

// High-impact relations are drawn thick + bright; structural ones thin + dim.
// (Defined here, not imported, to keep ForceGraph free of module cycles.)
const HIGH_IMPACT = new Set(['handles', 'declares', 'selects', 'routes_to',
  'mounts', 'uses_image', 'co_changed_with'])
const LOW_IMPACT = new Set(['contains', 'imports'])
// First-class kinds are drawn slightly larger so they stand out at any degree.
const SPECIAL = new Set(['route', 'dependency', 'task', 'resource', 'image', 'config'])

// MAX_SIM_NODES caps how many nodes the physics/renderer will ever touch. The
// server already budgets its payloads, but the producer-sample fairness pass
// (up to 60 per producer) plus expand-on-click can still stack up — so the
// client defends itself too. Above the cap we keep the highest-degree nodes
// (the useful backbone) and drop the rest; the O(n·neighbours) tick stays cheap
// and the main thread never locks. Kept in sync with the Viewer's own cap so
// the "showing N of M" note is honest.
export const MAX_SIM_NODES = 1200

// BATCH_ABOVE is where the renderer switches from one draw call per primitive
// to one per style class. Both are measured (see draw()), and they do NOT
// produce the same image: stroked individually, translucent edges ACCUMULATE
// where they overlap, so a dense graph reads brighter; stroked as one path per
// class they composite once and read flatter. A pixel diff at 5,232 nodes put
// the difference at 68-95% of inked pixels — far too much to swap in silently.
//
// So the exact path is kept for everything at or below the cap, which is every
// view that exists today (MAX_SIM_NODES = 1200, where per-primitive costs
// ~0.5ms and is not worth changing). Batching exists for the sizes the exact
// path cannot draw at all — 20,000 nodes is 360ms a frame per-primitive and
// 5ms batched. Nobody's current view changes; the cliff above it disappears.
export const BATCH_ABOVE = MAX_SIM_NODES

// Hand-rolled canvas force layout — ported from the original single-file UI
// (grid-approximated repulsion + springs + mild centering). Zero graph-viz
// dependencies: the physics is ~60 lines and the store graphs it draws are
// server-budgeted + client-capped, so nothing heavier is warranted.
//
// The RAF loop SETTLES AND STOPS: each data change seeds a bounded run of
// physics ticks, and once motion falls below a threshold (or the tick budget
// runs out) the loop cancels itself — the tab is not animating a static graph
// forever. Interaction (drag / zoom / hover / expand / filter) wakes it again
// for exactly as many frames as it needs, then it sleeps.

interface SimNode extends Node {
  x: number
  y: number
  vx: number
  vy: number
  deg: number
}

interface Props {
  nodes: Node[]
  edges: Edge[]
  colors: Map<string, string>
  selectedId: string | null
  onSelect: (id: string | null) => void
}

export default function ForceGraph({ nodes, edges, colors, selectedId, onSelect }: Props) {
  const canvasRef = useRef<HTMLCanvasElement>(null)
  const stateRef = useRef({
    sim: new Map<string, SimNode>(),
    edges: [] as Edge[],
    colors: new Map<string, string>(),
    view: { x: 0, y: 0, k: 1 },
    fitted: false,
    selected: null as string | null,
    hovered: null as string | null,
    ticking: 0,
    onSelect: (_: string | null) => {},
    wake: () => {}, // set by the animation effect; wakes a settled/stopped loop
  })

  // Merge incoming graph data into the running simulation: existing nodes
  // keep their positions (expand-on-click grows the picture in place).
  useEffect(() => {
    const st = stateRef.current
    const deg = new Map<string, number>()
    for (const e of edges) {
      deg.set(e.source, (deg.get(e.source) || 0) + 1)
      deg.set(e.target, (deg.get(e.target) || 0) + 1)
    }
    // Defensive client cap: never simulate more than MAX_SIM_NODES. If more
    // arrive, keep the highest-degree backbone (plus the selected node) and
    // drop the tail — the O(n) tick and the O(edges) draw stay bounded.
    let simNodes = nodes
    if (nodes.length > MAX_SIM_NODES) {
      const ranked = [...nodes].sort((a, b) => (deg.get(b.id) || 0) - (deg.get(a.id) || 0))
      simNodes = ranked.slice(0, MAX_SIM_NODES)
      if (selectedId && !simNodes.some((n) => n.id === selectedId)) {
        const sel = nodes.find((n) => n.id === selectedId)
        if (sel) simNodes[simNodes.length - 1] = sel
      }
    }
    const keep = new Set(simNodes.map((n) => n.id))
    for (const id of Array.from(st.sim.keys())) if (!keep.has(id)) st.sim.delete(id)
    const R = Math.sqrt(simNodes.length) * 60 + 60
    let i = 0
    for (const n of simNodes) {
      const prev = st.sim.get(n.id)
      if (prev) {
        Object.assign(prev, n, { x: prev.x, y: prev.y, vx: prev.vx, vy: prev.vy, deg: deg.get(n.id) || 0 })
      } else {
        const a = (i / Math.max(1, simNodes.length)) * Math.PI * 2
        const r = R * (0.3 + 0.7 * ((i * 2654435761 % 997) / 997))
        st.sim.set(n.id, { ...n, x: Math.cos(a) * r, y: Math.sin(a) * r, vx: 0, vy: 0, deg: deg.get(n.id) || 0 })
      }
      i++
    }
    st.edges = edges
    st.colors = colors
    st.selected = selectedId
    st.onSelect = onSelect
    if (!st.fitted && simNodes.length > 0) {
      const cv = canvasRef.current
      if (cv) {
        const r = cv.getBoundingClientRect()
        st.view.k = Math.min(r.width, r.height) / (R * 2.4) || 1
        st.view.x = r.width / 2
        st.view.y = r.height / 2
        st.fitted = true
      }
    }
    // Seed a bounded settle run and wake the (possibly stopped) loop.
    st.ticking = 300
    st.wake()
  }, [nodes, edges, colors, selectedId, onSelect])

  useEffect(() => {
    const cv = canvasRef.current!
    const ctx = cv.getContext('2d')!
    const st = stateRef.current
    let raf = 0
    let alive = true
    let needsDraw = true // a redraw is pending (view/hover changed but physics is idle)

    // wake restarts a stopped loop; requestDraw also flags a one-off repaint.
    // The raf===0 guard keeps exactly one loop alive — no stacking.
    const wake = () => {
      if (alive && raf === 0) raf = requestAnimationFrame(loop)
    }
    const requestDraw = () => {
      needsDraw = true
      wake()
    }
    st.wake = wake

    const resize = () => {
      const r = cv.getBoundingClientRect()
      cv.width = r.width * devicePixelRatio
      cv.height = r.height * devicePixelRatio
      requestDraw()
    }
    resize()
    window.addEventListener('resize', resize)

    const neighborSet = (id: string | null) => {
      const s = new Set<string>()
      if (!id) return s
      for (const e of st.edges) {
        if (e.source === id) s.add(e.target)
        if (e.target === id) s.add(e.source)
      }
      return s
    }

    // neighborSet is an O(E) scan and draw() called it EVERY FRAME. It only
    // changes when the focus changes, so it is cached against exactly that.
    let neighCache: { key: string | null; set: Set<string> } = { key: undefined as never, set: new Set() }
    const neighborsOf = (id: string | null) => {
      if (neighCache.key !== id) neighCache = { key: id, set: neighborSet(id) }
      return neighCache.set
    }

    // The node array was rebuilt from the Map every frame. The Map only changes
    // when the neighbourhood is expanded, so the array is kept and rebuilt then.
    let nodeArr: SimNode[] = []
    const simList = () => {
      if (nodeArr.length !== st.sim.size) nodeArr = Array.from(st.sim.values())
      return nodeArr
    }

    const nodeRadius = (n: SimNode) =>
      (SPECIAL.has(n.kind) ? 5 : 3.5) + Math.min(10, Math.sqrt(n.deg) * 1.4)

    const step = () => {
      const nodes = Array.from(st.sim.values())
      const k = 45
      const cell = 90
      const grid = new Map<string, SimNode[]>()
      for (const n of nodes) {
        const gk = Math.round(n.x / cell) + ':' + Math.round(n.y / cell)
        let b = grid.get(gk)
        if (!b) grid.set(gk, (b = []))
        b.push(n)
      }
      for (const n of nodes) {
        const gx = Math.round(n.x / cell)
        const gy = Math.round(n.y / cell)
        for (let dx = -1; dx <= 1; dx++) {
          for (let dy = -1; dy <= 1; dy++) {
            const bucket = grid.get(gx + dx + ':' + (gy + dy))
            if (!bucket) continue
            for (const m of bucket) {
              if (m === n) continue
              const ddx = n.x - m.x
              const ddy = n.y - m.y
              const d2 = ddx * ddx + ddy * ddy || 1
              if (d2 > cell * cell * 4) continue
              const f = ((k * k) / d2) * 0.6
              n.vx += ddx * f * 0.01
              n.vy += ddy * f * 0.01
            }
          }
        }
      }
      for (const e of st.edges) {
        const a = st.sim.get(e.source)
        const b = st.sim.get(e.target)
        if (!a || !b) continue
        const dx = b.x - a.x
        const dy = b.y - a.y
        const d = Math.sqrt(dx * dx + dy * dy) || 1
        const f = (d - 70) * 0.004
        a.vx += (dx / d) * f * 10
        a.vy += (dy / d) * f * 10
        b.vx -= (dx / d) * f * 10
        b.vy -= (dy / d) * f * 10
      }
      let moved = 0
      for (const n of nodes) {
        n.vx -= n.x * 0.0009
        n.vy -= n.y * 0.0009
        const dx = Math.max(-8, Math.min(8, n.vx))
        const dy = Math.max(-8, Math.min(8, n.vy))
        n.x += dx
        n.y += dy
        n.vx *= 0.85
        n.vy *= 0.85
        const m = Math.abs(dx) + Math.abs(dy)
        if (m > moved) moved = m
      }
      return moved // peak per-node motion this tick — used to detect "settled"
    }

    // BATCHED DRAW. Measured in headless Chrome over a real store and three
    // synthetic graphs (20k/100k/500k nodes, ~2.4 edges per node), at
    // zoom-to-fit so nothing is culled:
    //
    //     nodes     per-primitive     batched     win
    //     20,000       360.7 ms       5.05 ms     71x
    //    100,000     2,043.7 ms      35.40 ms     58x
    //    500,000    10,481.0 ms     455.60 ms     23x
    //
    // The win is BATCHING, not culling — culling the same 20k scene moved 5.34
    // to 5.05 ms, i.e. nothing. In Canvas2D the per-primitive state change costs
    // far more than the geometry, so the rules here are:
    //
    //   · edges are grouped into one path per STYLE CLASS: 5 strokes a frame
    //     instead of one per edge;
    //   · nodes are grouped into one path per COLOUR;
    //   · ctx.font is assigned ONCE, not per label — assigning it re-parses the
    //     string, and it was previously set inside the node loop;
    //   · shadowBlur is set once per colour batch instead of per node;
    //   · the neighbour set is cached against the focus rather than rebuilt
    //     (it is an O(E) scan and it ran every frame);
    //   · the node array is kept, not rebuilt from the Map every frame.
    //
    // Resilience is unchanged in kind but moved: a malformed node or edge is
    // skipped while the batch is BUILT, so one bad item still cannot take out
    // the frame — it just never enters a path.
    const EDGE_CLASSES = [
      { stroke: 'rgba(74,222,128,.95)', lw: 2.1 },   // 0 touches the focus
      { stroke: 'rgba(148,163,184,.06)', lw: 1 },    // 1 dimmed by a focus
      { stroke: 'rgba(110,231,183,.55)', lw: 1.7 },  // 2 high impact
      { stroke: 'rgba(148,163,184,.13)', lw: 0.7 },  // 3 low impact
      { stroke: 'rgba(148,163,184,.28)', lw: 1 },    // 4 everything else
    ]
    const edgeClass = (e: Edge, focus: string | null) => {
      if (focus && (e.source === focus || e.target === focus)) return 0
      if (focus) return 1
      if (HIGH_IMPACT.has(e.relation)) return 2
      if (LOW_IMPACT.has(e.relation)) return 3
      return 4
    }

    const drawBatched = () => {
      const view = st.view
      ctx.setTransform(devicePixelRatio, 0, 0, devicePixelRatio, 0, 0)
      ctx.clearRect(0, 0, cv.width, cv.height)
      ctx.translate(view.x, view.y)
      ctx.scale(view.k, view.k)
      const focus = st.selected || st.hovered
      const neigh = neighborsOf(focus)
      const nodes = simList()
      const glow = nodes.length <= 600

      // viewport in world space, with a margin so half-visible marks still draw
      const mg = 80 / view.k
      const vx0 = -view.x / view.k - mg
      const vy0 = -view.y / view.k - mg
      const vx1 = vx0 + cv.width / devicePixelRatio / view.k + 2 * mg
      const vy1 = vy0 + cv.height / devicePixelRatio / view.k + 2 * mg
      const visible = (p: SimNode) => p.x >= vx0 && p.x <= vx1 && p.y >= vy0 && p.y <= vy1

      // ---- edges, one path per style class
      const buckets: SimNode[][] = EDGE_CLASSES.map(() => [])
      for (const e of st.edges) {
        const a = st.sim.get(e.source)
        const b = st.sim.get(e.target)
        if (!a || !b) continue
        if (!visible(a) && !visible(b)) continue
        buckets[edgeClass(e, focus)].push(a, b)
      }
      for (let i = 0; i < EDGE_CLASSES.length; i++) {
        const pts = buckets[i]
        if (pts.length === 0) continue
        ctx.strokeStyle = EDGE_CLASSES[i].stroke
        ctx.lineWidth = EDGE_CLASSES[i].lw / view.k
        ctx.beginPath()
        for (let j = 0; j < pts.length; j += 2) {
          ctx.moveTo(pts[j].x, pts[j].y)
          ctx.lineTo(pts[j + 1].x, pts[j + 1].y)
        }
        ctx.stroke()
      }

      // ---- nodes, one path per colour, split by whether the focus dims them
      const lit = new Map<string, SimNode[]>()
      const dim = new Map<string, SimNode[]>()
      const labels: SimNode[] = []
      for (const n of nodes) {
        if (!visible(n)) continue
        const c = st.colors.get(n.kind) || '#94a3b8'
        const inFocus = !focus || n.id === focus || neigh.has(n.id)
        const table = inFocus ? lit : dim
        let b = table.get(c)
        if (!b) table.set(c, (b = []))
        b.push(n)
        if (view.k > 0.5 && (n.id === focus || neigh.has(n.id) || (!focus && n.deg > 3))) labels.push(n)
      }
      const paint = (table: Map<string, SimNode[]>, alpha: number, withGlow: boolean) => {
        ctx.globalAlpha = alpha
        for (const [c, list] of table) {
          if (withGlow) {
            ctx.shadowColor = c
            ctx.shadowBlur = 12
          }
          ctx.fillStyle = c
          ctx.beginPath()
          for (const n of list) {
            const r = nodeRadius(n)
            ctx.moveTo(n.x + r, n.y)
            ctx.arc(n.x, n.y, r, 0, Math.PI * 2)
          }
          ctx.fill()
          if (withGlow) ctx.shadowBlur = 0
        }
      }
      paint(lit, 1, glow)
      paint(dim, 0.28, false)
      ctx.globalAlpha = 1
      ctx.shadowBlur = 0

      // ---- the focus ring: one node, so it stays its own draw
      if (focus) {
        const n = st.sim.get(focus)
        if (n) {
          const r = nodeRadius(n)
          ctx.strokeStyle = '#ffffff'
          ctx.lineWidth = 1.6 / view.k
          ctx.beginPath()
          ctx.arc(n.x, n.y, r + 3 / view.k, 0, Math.PI * 2)
          ctx.stroke()
        }
      }

      // ---- labels, with the font set ONCE
      if (labels.length > 0) {
        ctx.font = `${11 / view.k}px ui-monospace, monospace`
        ctx.lineWidth = 3 / view.k
        ctx.strokeStyle = 'rgba(10,12,16,.85)'
        for (const n of labels) {
          // Coerce the label defensively: a node with a missing/non-string
          // label (corrupt store, adapter bug) must not break the draw loop.
          const label = typeof n.label === 'string' ? n.label : String(n.label ?? n.id ?? '')
          const r = nodeRadius(n)
          const lx = n.x + r + 4 / view.k
          const ly = n.y + 3.5 / view.k
          ctx.strokeText(label, lx, ly)
          ctx.fillStyle = n.id === focus ? '#ffffff' : 'rgba(232,237,244,.92)'
          ctx.fillText(label, lx, ly)
        }
      }
    }

    // The EXACT renderer: one draw call per primitive, translucent overlap
    // accumulating exactly as it always has. This is what every view at or
    // below BATCH_ABOVE still gets, so no existing picture changes.
    const drawExact = () => {
      const view = st.view
      ctx.setTransform(devicePixelRatio, 0, 0, devicePixelRatio, 0, 0)
      ctx.clearRect(0, 0, cv.width, cv.height)
      ctx.translate(view.x, view.y)
      ctx.scale(view.k, view.k)
      const focus = st.selected || st.hovered
      const neigh = neighborsOf(focus)
      const nodes = simList()
      const glow = nodes.length <= 600

      for (const e of st.edges) {
       try {
        const a = st.sim.get(e.source)
        const b = st.sim.get(e.target)
        if (!a || !b) continue
        const i = edgeClass(e, focus)
        ctx.strokeStyle = EDGE_CLASSES[i].stroke
        ctx.lineWidth = EDGE_CLASSES[i].lw / view.k
        ctx.beginPath()
        ctx.moveTo(a.x, a.y)
        ctx.lineTo(b.x, b.y)
        ctx.stroke()
       } catch { /* one bad edge is skipped alone — the rest still draw */ }
      }
      for (const n of nodes) {
       try {
        const r = nodeRadius(n)
        const inFocus = !focus || n.id === focus || neigh.has(n.id)
        const c = st.colors.get(n.kind) || '#94a3b8'
        ctx.globalAlpha = inFocus ? 1 : 0.28
        if (glow && inFocus) {
          ctx.shadowColor = c
          ctx.shadowBlur = 12
        } else {
          ctx.shadowBlur = 0
        }
        ctx.fillStyle = c
        ctx.beginPath()
        ctx.arc(n.x, n.y, r, 0, Math.PI * 2)
        ctx.fill()
        ctx.shadowBlur = 0
        if (n.id === focus) {
          ctx.strokeStyle = '#ffffff'
          ctx.lineWidth = 1.6 / view.k
          ctx.beginPath()
          ctx.arc(n.x, n.y, r + 3 / view.k, 0, Math.PI * 2)
          ctx.stroke()
        }
        if (view.k > 0.5 && (n.id === focus || neigh.has(n.id) || (!focus && n.deg > 3))) {
          const label = typeof n.label === 'string' ? n.label : String(n.label ?? n.id ?? '')
          ctx.font = `${11 / view.k}px ui-monospace, monospace`
          ctx.lineWidth = 3 / view.k
          ctx.strokeStyle = 'rgba(10,12,16,.85)'
          ctx.strokeText(label, n.x + r + 4 / view.k, n.y + 3.5 / view.k)
          ctx.fillStyle = n.id === focus ? '#ffffff' : 'rgba(232,237,244,.92)'
          ctx.fillText(label, n.x + r + 4 / view.k, n.y + 3.5 / view.k)
        }
        ctx.globalAlpha = 1
       } catch { /* one bad node is skipped alone — the rest still draw */ }
      }
    }

    const draw = () => (simList().length > BATCH_ABOVE ? drawBatched() : drawExact())

    // The settle-and-stop loop. It runs only while there is physics left to
    // simulate (st.ticking) or a repaint is pending (needsDraw); otherwise it
    // sets raf=0 and returns, leaving the tab idle until the next wake().
    //
    // Declared as a hoisted `function`, NOT a `const` arrow: wake() closes over
    // loop and the resize() call above runs synchronously on mount — long
    // before this point. A const would still be in its temporal dead zone
    // there, throwing "Cannot access 'loop' before initialization" and taking
    // the whole Viewer down through the error boundary.
    function loop() {
      raf = 0
      if (!alive) return
      let busy = false
      try {
        if (st.ticking > 0) {
          st.ticking--
          const moved = step()
          if (moved < 0.15) st.ticking = 0 // layout settled — stop early
          busy = true
        }
        if (busy || needsDraw) {
          draw()
          needsDraw = false
        }
      } catch (e) {
        // A bad frame must not wedge the tab or blank the graph: log, stop
        // physics, and let the loop go idle rather than throwing every frame.
        console.error('ctx-optimize graph draw error:', e)
        st.ticking = 0
        needsDraw = false
      }
      if (st.ticking > 0 || needsDraw) raf = requestAnimationFrame(loop)
    }
    wake()

    const nodeAt = (e: MouseEvent): SimNode | null => {
      const r = cv.getBoundingClientRect()
      const wx = (e.clientX - r.left - st.view.x) / st.view.k
      const wy = (e.clientY - r.top - st.view.y) / st.view.k
      let best: SimNode | null = null
      let bd = 12 / st.view.k + 6
      for (const n of st.sim.values()) {
        const d = Math.hypot(n.x - wx, n.y - wy)
        if (d < bd) {
          bd = d
          best = n
        }
      }
      return best
    }

    let drag: { x: number; y: number; moved: boolean } | null = null
    const down = (e: MouseEvent) => {
      drag = { x: e.clientX, y: e.clientY, moved: false }
      cv.style.cursor = 'grabbing'
    }
    const move = (e: MouseEvent) => {
      if (drag) {
        st.view.x += e.clientX - drag.x
        st.view.y += e.clientY - drag.y
        drag.x = e.clientX
        drag.y = e.clientY
        drag.moved = true
        requestDraw() // pan changed the view — repaint (no physics needed)
        return
      }
      const n = nodeAt(e)
      const hid = n ? n.id : null
      if (hid !== st.hovered) {
        st.hovered = hid
        requestDraw() // hover highlight changed — one repaint, then idle
      }
      cv.style.cursor = n ? 'pointer' : 'grab'
    }
    const up = (e: MouseEvent) => {
      cv.style.cursor = 'grab'
      if (drag && !drag.moved) {
        const n = nodeAt(e)
        st.onSelect(n ? n.id : null)
      }
      drag = null
    }
    const wheel = (e: WheelEvent) => {
      e.preventDefault()
      const r = cv.getBoundingClientRect()
      const mx = e.clientX - r.left
      const my = e.clientY - r.top
      const f = e.deltaY < 0 ? 1.15 : 1 / 1.15
      st.view.x = mx - (mx - st.view.x) * f
      st.view.y = my - (my - st.view.y) * f
      st.view.k *= f
      requestDraw() // zoom changed the view — repaint
    }
    cv.addEventListener('mousedown', down)
    window.addEventListener('mousemove', move)
    window.addEventListener('mouseup', up)
    cv.addEventListener('wheel', wheel, { passive: false })

    return () => {
      alive = false
      cancelAnimationFrame(raf)
      raf = 0
      st.wake = () => {} // detach: a stale merge-effect wake must not revive a dead loop
      window.removeEventListener('resize', resize)
      cv.removeEventListener('mousedown', down)
      window.removeEventListener('mousemove', move)
      window.removeEventListener('mouseup', up)
      cv.removeEventListener('wheel', wheel)
    }
  }, [])

  return <canvas ref={canvasRef} />
}
