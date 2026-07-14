import { useEffect, useRef } from 'react'
import type { Edge, Node } from './types'

// Hand-rolled canvas force layout — ported from the original single-file UI
// (grid-approximated repulsion + springs + mild centering). Zero graph-viz
// dependencies: the physics is ~60 lines and the store graphs it draws are
// server-budgeted, so nothing heavier is warranted.

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
  })

  // Merge incoming graph data into the running simulation: existing nodes
  // keep their positions (expand-on-click grows the picture in place).
  useEffect(() => {
    const st = stateRef.current
    const keep = new Set(nodes.map((n) => n.id))
    for (const id of Array.from(st.sim.keys())) if (!keep.has(id)) st.sim.delete(id)
    const deg = new Map<string, number>()
    for (const e of edges) {
      deg.set(e.source, (deg.get(e.source) || 0) + 1)
      deg.set(e.target, (deg.get(e.target) || 0) + 1)
    }
    const R = Math.sqrt(nodes.length) * 60 + 60
    let i = 0
    for (const n of nodes) {
      const prev = st.sim.get(n.id)
      if (prev) {
        Object.assign(prev, n, { x: prev.x, y: prev.y, vx: prev.vx, vy: prev.vy, deg: deg.get(n.id) || 0 })
      } else {
        const a = (i / Math.max(1, nodes.length)) * Math.PI * 2
        const r = R * (0.3 + 0.7 * ((i * 2654435761 % 997) / 997))
        st.sim.set(n.id, { ...n, x: Math.cos(a) * r, y: Math.sin(a) * r, vx: 0, vy: 0, deg: deg.get(n.id) || 0 })
      }
      i++
    }
    st.edges = edges
    st.colors = colors
    st.selected = selectedId
    st.onSelect = onSelect
    if (!st.fitted && nodes.length > 0) {
      const cv = canvasRef.current
      if (cv) {
        const r = cv.getBoundingClientRect()
        st.view.k = Math.min(r.width, r.height) / (R * 2.4) || 1
        st.view.x = r.width / 2
        st.view.y = r.height / 2
        st.fitted = true
      }
    }
    st.ticking = 300
  }, [nodes, edges, colors, selectedId, onSelect])

  useEffect(() => {
    const cv = canvasRef.current!
    const ctx = cv.getContext('2d')!
    const st = stateRef.current
    let raf = 0
    let alive = true

    const resize = () => {
      const r = cv.getBoundingClientRect()
      cv.width = r.width * devicePixelRatio
      cv.height = r.height * devicePixelRatio
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
      for (const n of nodes) {
        n.vx -= n.x * 0.0009
        n.vy -= n.y * 0.0009
        n.x += Math.max(-8, Math.min(8, n.vx))
        n.y += Math.max(-8, Math.min(8, n.vy))
        n.vx *= 0.85
        n.vy *= 0.85
      }
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
        ctx.strokeStyle = hot ? 'rgba(34,211,238,.9)' : focus ? 'rgba(124,137,166,.07)' : 'rgba(124,137,166,.22)'
        ctx.lineWidth = (hot ? 1.8 : 1) / view.k
        ctx.beginPath()
        ctx.moveTo(a.x, a.y)
        ctx.lineTo(b.x, b.y)
        ctx.stroke()
      }
      for (const n of nodes) {
        const r = 3.5 + Math.min(10, Math.sqrt(n.deg) * 1.4)
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
          ctx.font = `${11 / view.k}px ui-monospace, monospace`
          ctx.lineWidth = 3 / view.k
          ctx.strokeStyle = 'rgba(7,11,20,.85)'
          ctx.strokeText(n.label, n.x + r + 4 / view.k, n.y + 3.5 / view.k)
          ctx.fillStyle = n.id === focus ? '#ffffff' : 'rgba(226,232,244,.92)'
          ctx.fillText(n.label, n.x + r + 4 / view.k, n.y + 3.5 / view.k)
        }
        ctx.globalAlpha = 1
      }
    }

    const loop = () => {
      if (!alive) return
      if (st.ticking > 0) {
        st.ticking--
        step()
      }
      draw()
      raf = requestAnimationFrame(loop)
    }
    raf = requestAnimationFrame(loop)

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
        return
      }
      const n = nodeAt(e)
      st.hovered = n ? n.id : null
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
    }
    cv.addEventListener('mousedown', down)
    window.addEventListener('mousemove', move)
    window.addEventListener('mouseup', up)
    cv.addEventListener('wheel', wheel, { passive: false })

    return () => {
      alive = false
      cancelAnimationFrame(raf)
      window.removeEventListener('resize', resize)
      cv.removeEventListener('mousedown', down)
      window.removeEventListener('mousemove', move)
      window.removeEventListener('mouseup', up)
      cv.removeEventListener('wheel', wheel)
    }
  }, [])

  return <canvas ref={canvasRef} />
}
