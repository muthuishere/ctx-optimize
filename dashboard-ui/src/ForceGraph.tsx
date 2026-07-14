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

    const draw = () => {
      const view = st.view
      ctx.setTransform(devicePixelRatio, 0, 0, devicePixelRatio, 0, 0)
      ctx.clearRect(0, 0, cv.width, cv.height)
      ctx.translate(view.x, view.y)
      ctx.scale(view.k, view.k)
      const focus = st.selected || st.hovered
      const neigh = neighborSet(focus)
      const nodes = Array.from(st.sim.values())
      const glow = nodes.length <= 600

      for (const e of st.edges) {
        const a = st.sim.get(e.source)
        const b = st.sim.get(e.target)
        if (!a || !b) continue
        const hot = focus && (e.source === focus || e.target === focus)
        let stroke: string
        let lw: number
        if (hot) {
          stroke = 'rgba(74,222,128,.95)'
          lw = 2.1
        } else if (focus) {
          stroke = 'rgba(148,163,184,.06)'
          lw = 1
        } else if (HIGH_IMPACT.has(e.relation)) {
          stroke = 'rgba(110,231,183,.55)' // bright emerald — the load-bearing edges
          lw = 1.7
        } else if (LOW_IMPACT.has(e.relation)) {
          stroke = 'rgba(148,163,184,.13)' // structural scaffolding — thin + dim
          lw = 0.7
        } else {
          stroke = 'rgba(148,163,184,.28)'
          lw = 1
        }
        ctx.strokeStyle = stroke
        ctx.lineWidth = lw / view.k
        ctx.beginPath()
        ctx.moveTo(a.x, a.y)
        ctx.lineTo(b.x, b.y)
        ctx.stroke()
      }
      for (const n of nodes) {
        const r = (SPECIAL.has(n.kind) ? 5 : 3.5) + Math.min(10, Math.sqrt(n.deg) * 1.4)
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
          // Coerce the label defensively: a node with a missing/non-string
          // label (corrupt store, adapter bug) must not break the draw loop.
          const label = typeof n.label === 'string' ? n.label : String(n.label ?? n.id ?? '')
          ctx.font = `${11 / view.k}px ui-monospace, monospace`
          ctx.lineWidth = 3 / view.k
          ctx.strokeStyle = 'rgba(10,12,16,.85)'
          ctx.strokeText(label, n.x + r + 4 / view.k, n.y + 3.5 / view.k)
          ctx.fillStyle = n.id === focus ? '#ffffff' : 'rgba(232,237,244,.92)'
          ctx.fillText(label, n.x + r + 4 / view.k, n.y + 3.5 / view.k)
        }
        ctx.globalAlpha = 1
      }
    }

    // The settle-and-stop loop. It runs only while there is physics left to
    // simulate (st.ticking) or a repaint is pending (needsDraw); otherwise it
    // sets raf=0 and returns, leaving the tab idle until the next wake().
    const loop = () => {
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
