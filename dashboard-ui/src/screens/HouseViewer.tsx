import { useEffect, useRef, useState } from 'react'
import { api } from '../api'
import { attach, fit, type Cam } from '../camera'
import { relationStyle } from '../flowLayout'
import { houseLayout, HVH, HVW, type House, type Room } from '../houseLayout'
import { sanitizeScene } from '../sanitize'
import type { Scene } from '../types'
import type { ViewerProps } from '../viewers'

// HouseViewer — the same derived scene as the flow view, projected as a CUTAWAY
// BUILDING. Not a second dataset and not a second layout engine: it reads
// /api/scene and re-projects it, so the two views can never disagree about the
// code.
//
// The projection is defended in houseLayout.ts — floor = dependency depth,
// room width = files, stair = lifted edge count, door = a transport on the
// outside wall. The rule the killed wall view failed is the rule here: a
// channel that carries no fact does not get drawn.
//
// One canvas, Canvas 2D, system fonts, zero external requests. Motion honours
// prefers-reduced-motion.

const INK = '#171614'
const MUTED = '#8b8479'
const WALL = '#e4ded1'
const ROOM = '#ffffff'
const ACC = '#d97a19'
const DOT = '#3f52c8'
const SKY = '#eceae4'
const GROUND = '#faf8f4'

const SANS = '-apple-system,BlinkMacSystemFont,"Segoe UI",Inter,Helvetica,Arial,sans-serif'
const MONO = 'ui-monospace,SFMono-Regular,Menlo,Consolas,monospace'

export default function HouseViewer({ module, params }: ViewerProps) {
  const [scene, setScene] = useState<Scene | null>(null)
  const [err, setErr] = useState('')
  const [root, setRoot] = useState(params.get('root') || '')
  const wrap = useRef<HTMLDivElement | null>(null)
  const cv = useRef<HTMLCanvasElement | null>(null)

  useEffect(() => {
    setScene(null)
    setErr('')
    const q = new URLSearchParams({ module })
    if (root) q.set('root', root)
    api<Scene>(`/api/scene?${q}`)
      .then((s) => setScene(sanitizeScene(s)))
      .catch((e) => setErr(String(e.message || e)))
  }, [module, root])

  useEffect(() => {
    const onPop = () => {
      const q = new URLSearchParams(window.location.hash.split('?')[1] || '')
      setRoot(q.get('root') || '')
    }
    window.addEventListener('popstate', onPop)
    return () => window.removeEventListener('popstate', onPop)
  }, [])
  useEffect(() => {
    const q = new URLSearchParams(window.location.hash.split('?')[1] || '')
    if ((q.get('root') || '') === root) return
    if (root) q.set('root', root)
    else q.delete('root')
    const base = window.location.hash.split('?')[0]
    window.history.pushState(null, '', base + (q.toString() ? '?' + q : ''))
  }, [root])

  useEffect(() => {
    if (!scene || !cv.current || !wrap.current) return
    const canvas = cv.current
    const host = wrap.current
    const ctx = canvas.getContext('2d', { alpha: false })
    if (!ctx) return
    const H: House = houseLayout(scene)

    const motion = window.matchMedia
      ? window.matchMedia('(prefers-reduced-motion: reduce)')
      : null
    let still = !!motion?.matches
    const onMotion = () => { still = !!motion?.matches }
    motion?.addEventListener?.('change', onMotion)

    const CAM_OPTS = { vw: HVW, vh: HVH, pad: 24, minK: 0.12, maxK: 8 }
    const cam: Cam = { k: 1, x: 0, y: 0 }
    let W = 0, Hh = 0, fitted = false
    const resize = () => {
      const dpr = Math.min(window.devicePixelRatio || 1, 2)
      W = host.clientWidth || 1
      Hh = host.clientHeight || 1
      canvas.width = Math.floor(W * dpr)
      canvas.height = Math.floor(Hh * dpr)
      canvas.style.width = W + 'px'
      canvas.style.height = Hh + 'px'
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
      if (!fitted) { fit(cam, CAM_OPTS, W, Hh); fitted = true }
    }
    const ro = new ResizeObserver(resize)
    ro.observe(host)
    resize()

    const T = (x: number, y: number) => ({ x: cam.x + x * cam.k, y: cam.y + y * cam.k })
    const S = (v: number) => v * cam.k

    // Hit regions live in VIRTUAL space: the camera pans and zooms between
    // frames, so a region measured in pixels would drift the moment it did.
    type Hit = { x: number; y: number; w: number; h: number; root: string; kind?: 'reset' }
    let hits: Hit[] = []
    let hover = -1
    const hitAt = (vx: number, vy: number) =>
      hits.findIndex((h) => vx >= h.x && vx <= h.x + h.w && vy >= h.y && vy <= h.y + h.h)
    const detach = attach(canvas, cam, CAM_OPTS, {
      onPick: (vx, vy) => (hitAt(vx, vy) >= 0 ? 'click' : null),
      onClick: (vx, vy) => {
        const i = hitAt(vx, vy)
        if (i < 0) return
        if (hits[i].kind === 'reset') { fit(cam, CAM_OPTS, W, Hh); return }
        setRoot(hits[i].root)
      },
      onHover: (vx, vy) => {
        hover = hitAt(vx, vy)
        canvas.style.cursor = hover >= 0 ? 'pointer' : 'default'
      },
    })

    function rr(x: number, y: number, w: number, h: number, r: number) {
      ctx!.beginPath()
      ctx!.moveTo(x + r, y); ctx!.lineTo(x + w - r, y); ctx!.quadraticCurveTo(x + w, y, x + w, y + r)
      ctx!.lineTo(x + w, y + h - r); ctx!.quadraticCurveTo(x + w, y + h, x + w - r, y + h)
      ctx!.lineTo(x + r, y + h); ctx!.quadraticCurveTo(x, y + h, x, y + h - r)
      ctx!.lineTo(x, y + r); ctx!.quadraticCurveTo(x, y, x + r, y); ctx!.closePath()
    }
    type TO = { size?: number; weight?: number; color?: string; font?: string; align?: CanvasTextAlign; spacing?: number; max?: number }
    function text(str: string, x: number, y: number, o: TO = {}) {
      const { size = 14, weight = 400, color = INK, font = SANS, align = 'left', spacing = 0, max = 0 } = o
      const p = T(x, y)
      ctx!.font = `${weight} ${S(size)}px ${font}`
      ctx!.fillStyle = color
      ctx!.textAlign = align
      ctx!.textBaseline = 'alphabetic'
      let s = str
      if (max > 0) {
        while (s.length > 1 && ctx!.measureText(s).width / cam.k > max) s = s.slice(0, -1)
        if (s !== str) s = s.slice(0, -1) + '…'
      }
      if (spacing) {
        let cx = p.x
        const total = [...s].reduce((a, ch) => a + ctx!.measureText(ch).width + S(spacing), 0) - S(spacing)
        if (align === 'center') cx = p.x - total / 2
        if (align === 'right') cx = p.x - total
        ctx!.textAlign = 'left'
        for (const ch of s) { ctx!.fillText(ch, cx, p.y); cx += ctx!.measureText(ch).width + S(spacing) }
      } else ctx!.fillText(s, p.x, p.y)
    }
    const measure = (s: string, size: number, weight = 400, font = SANS) => {
      ctx!.font = `${weight} ${S(size)}px ${font}`
      return ctx!.measureText(s).width / cam.k
    }

    // ---- the shell: roof, walls, ground line
    function drawShell() {
      const wallL = 210, wallR = HVW - 210
      // ground
      const g = T(0, H.ground + 26)
      ctx!.strokeStyle = '#cfc8b8'; ctx!.lineWidth = Math.max(1, S(2))
      ctx!.beginPath(); ctx!.moveTo(T(60, 0).x, g.y); ctx!.lineTo(T(HVW - 60, 0).x, g.y); ctx!.stroke()
      // hatching under the ground: this is where the foundation sits
      ctx!.strokeStyle = '#e0d9ca'; ctx!.lineWidth = Math.max(1, S(1))
      for (let x = 70; x < HVW - 60; x += 16) {
        const a = T(x, H.ground + 26), b = T(x - 12, H.ground + 40)
        ctx!.beginPath(); ctx!.moveTo(a.x, a.y); ctx!.lineTo(b.x, b.y); ctx!.stroke()
      }
      // roof
      const apex = T(HVW / 2, H.roof - 74)
      const l = T(wallL - 26, H.roof - 4), r = T(wallR + 26, H.roof - 4)
      ctx!.fillStyle = '#f3efe6'
      ctx!.beginPath(); ctx!.moveTo(l.x, l.y); ctx!.lineTo(apex.x, apex.y); ctx!.lineTo(r.x, r.y); ctx!.closePath()
      ctx!.fill()
      ctx!.strokeStyle = WALL; ctx!.lineWidth = Math.max(1, S(2)); ctx!.stroke()
      // walls
      ctx!.strokeStyle = WALL; ctx!.lineWidth = Math.max(1, S(3))
      for (const wx of [wallL - 12, wallR + 12]) {
        const a = T(wx, H.roof - 4), b = T(wx, H.ground + 26)
        ctx!.beginPath(); ctx!.moveTo(a.x, a.y); ctx!.lineTo(b.x, b.y); ctx!.stroke()
      }
    }

    // ---- floors: the band label says what the storey MEANS
    function drawFloors() {
      for (const f of H.floors) {
        const p = T(150, f.y)
        ctx!.strokeStyle = '#efeae0'; ctx!.lineWidth = Math.max(1, S(1))
        ctx!.beginPath(); ctx!.moveTo(T(198, 0).x, p.y + S(f.h)); ctx!.lineTo(T(HVW - 198, 0).x, p.y + S(f.h)); ctx!.stroke()
        text(f.label, 198, f.y - 7, { size: 8.5, color: '#b3aca0', spacing: 1.35, weight: 700 })
      }
    }

    // ---- stairs: a lifted edge between floors, thickness = how many
    function drawStairs(t: number) {
      for (const s of H.stairs) {
        const a = { x: s.from.x + s.from.w / 2, y: s.from.y + s.from.h }
        const b = { x: s.to.x + s.to.w / 2, y: s.to.y }
        const heft = 0.8 + 2.4 * Math.sqrt(s.link.weight / H.maxWeight)
        const A = T(a.x, a.y), B = T(b.x, b.y)
        const mid = { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 }
        const M = T(mid.x, mid.y)
        const rs = relationStyle(s.link.relation)
        ctx!.strokeStyle = rs.line
        ctx!.lineWidth = Math.max(1, S(heft))
        ctx!.beginPath()
        ctx!.moveTo(A.x, A.y)
        ctx!.quadraticCurveTo(M.x, M.y, B.x, B.y)
        ctx!.stroke()
        // one travelling mark per stair, so heavier stairs read as busier
        const dots = Math.min(3, 1 + Math.round((s.link.weight / H.maxWeight) * 2))
        for (let i = 0; i < dots; i++) {
          const u = still ? (i + 0.5) / dots : (t * 0.2 + i / dots) % 1
          const x = (1 - u) * (1 - u) * a.x + 2 * (1 - u) * u * mid.x + u * u * b.x
          const y = (1 - u) * (1 - u) * a.y + 2 * (1 - u) * u * mid.y + u * u * b.y
          const q = T(x, y)
          ctx!.globalAlpha = 0.8 * Math.min(1, Math.sin(Math.PI * u) + 0.3)
          ctx!.fillStyle = rs.flow
          ctx!.beginPath(); ctx!.arc(q.x, q.y, S(3.2), 0, 7); ctx!.fill()
          ctx!.globalAlpha = 1
        }
      }
    }

    function drawRoom(r: Room, t: number) {
      const c = r.card
      const p = T(r.x, r.y)
      let on = false
      if (c.children > 0) {
        hits.push({ x: r.x, y: r.y, w: r.w, h: r.h, root: c.dir })
        on = hover === hits.length - 1
      }
      // the hub is the load-bearing room: it gets the pillar treatment
      const pillar = c.hub
      ctx!.save()
      ctx!.shadowColor = on ? 'rgba(63,82,200,.22)' : 'rgba(28,26,22,.07)'
      ctx!.shadowBlur = S(on ? 22 : 12); ctx!.shadowOffsetY = S(3)
      ctx!.fillStyle = pillar ? '#fbf6ec' : ROOM
      rr(p.x, p.y, S(r.w), S(r.h), S(6)); ctx!.fill()
      ctx!.restore()
      ctx!.strokeStyle = on ? DOT : pillar ? ACC : WALL
      ctx!.lineWidth = Math.max(1, S(on || pillar ? 1.8 : 1))
      rr(p.x, p.y, S(r.w), S(r.h), S(6)); ctx!.stroke()

      const pad = 12
      text(r.n, r.x + pad, r.y + 22, { size: 12, weight: 400, color: '#c2bbae', font: MONO })
      // The load-bearing mark rides on the TOP row beside the ordinal. It used
      // to sit at the foot of the room, where it landed on top of the files
      // line in exactly the room that earns it — the hub is often the smallest
      // directory, so it gets the shortest room.
      if (pillar) {
        text('LOAD-BEARING', r.x + pad + 26, r.y + 22, { size: 8, color: ACC, spacing: 1.2, weight: 700 })
      }
      text(c.label, r.x + pad, r.y + 44, { size: 15, weight: 700, max: r.w - pad * 2 })
      if (r.h > 74) {
        text(`${c.files} files · ${c.decls} decls`, r.x + pad, r.y + 64,
          { size: 10, color: MUTED, font: MONO, max: r.w - pad * 2 })
      }
      if (r.h > 104 && c.detail) {
        text(c.detail, r.x + pad, r.y + 84, { size: 10.5, color: '#9c968b', max: r.w - pad * 2 })
      }
      // in/out, bottom-right — the numbers that put this room on this floor
      if (r.w > 150) {
        text(`↘${c.in}  ${c.out}↗`, r.x + r.w - pad, r.y + r.h - 12,
          { size: 9.5, color: '#aaa49a', font: MONO, align: 'right' })
      }
      if (c.children > 0) {
        const label = `${c.children} inside`
        const w = measure(label, 9, 700, MONO) + 24
        const bx = r.x + r.w - w - 8, by = r.y + 8
        const q = T(bx, by)
        ctx!.fillStyle = on ? '#eaedfb' : '#f6f4ef'
        rr(q.x, q.y, S(w), S(17), S(8.5)); ctx!.fill()
        ctx!.strokeStyle = on ? DOT : '#e6e0d4'; ctx!.lineWidth = Math.max(1, S(1))
        rr(q.x, q.y, S(w), S(17), S(8.5)); ctx!.stroke()
        text(label, bx + 8, by + 12, { size: 9, weight: 700, font: MONO, color: on ? DOT : '#8b8479' })
        const cx = bx + w - 10, cy = by + 8.5
        const a1 = T(cx - 2.5, cy - 3.5), b1 = T(cx + 1.5, cy), c1 = T(cx - 2.5, cy + 3.5)
        ctx!.strokeStyle = on ? DOT : '#a8a29a'; ctx!.lineWidth = Math.max(1, S(1.5))
        ctx!.beginPath(); ctx!.moveTo(a1.x, a1.y); ctx!.lineTo(b1.x, b1.y); ctx!.lineTo(c1.x, c1.y); ctx!.stroke()
      }
      void t
    }

    // ---- doors: the outer world, hung on the outside wall
    function drawDoors(t: number) {
      for (const d of H.doors) {
        const w = d.world
        const p = T(d.x, d.y)
        ctx!.save()
        ctx!.fillStyle = '#fdf6ea'
        rr(p.x, p.y, S(d.w), S(d.h), S(8)); ctx!.fill()
        ctx!.setLineDash([S(5), S(6)])
        if (!still) ctx!.lineDashOffset = -t * S(22)
        ctx!.strokeStyle = '#e0b273'; ctx!.lineWidth = S(1.5)
        rr(p.x, p.y, S(d.w), S(d.h), S(8)); ctx!.stroke()
        ctx!.restore()

        text('DOOR', d.x + 11, d.y + 17, { size: 7.5, color: '#c0a06c', spacing: 1.2, weight: 700 })
        text(w.transport, d.x + 11, d.y + 36, { size: 12, weight: 700, font: MONO, max: d.w - 22 })
        text(`${w.total} ports`, d.x + 11, d.y + 54, { size: 10, color: '#a07a3a', font: MONO, weight: 700 })
        const dir = [w.provides ? `${w.provides} out` : '', w.consumes ? `${w.consumes} in` : '']
          .filter(Boolean).join(' · ')
        text(dir, d.x + 11, d.y + 70, { size: 9, color: MUTED, font: MONO, max: d.w - 22 })
        if (w.sensitive > 0) {
          const q = T(d.x + d.w - 15, d.y + 14)
          ctx!.fillStyle = ACC
          ctx!.beginPath(); ctx!.arc(q.x, q.y, S(3.5), 0, 7); ctx!.fill()
        }
        // the threshold line, from the door to the wall
        const wallX = d.side === 'left' ? 198 : HVW - 198
        const a = T(d.side === 'left' ? d.x + d.w : d.x, d.y + d.h / 2)
        const b = T(wallX, d.y + d.h / 2)
        ctx!.strokeStyle = '#e3d6bd'; ctx!.lineWidth = Math.max(1, S(1.4))
        ctx!.setLineDash([S(4), S(5)])
        ctx!.beginPath(); ctx!.moveTo(a.x, a.y); ctx!.lineTo(b.x, b.y); ctx!.stroke()
        ctx!.setLineDash([])
      }
    }

    function drawCrumbs() {
      const crumbs = scene!.crumbs
      if (crumbs.length <= 1) return
      let x = 62
      const y = 128
      crumbs.forEach((c, i) => {
        const last = i === crumbs.length - 1
        const w = measure(c.label, 11.5, last ? 700 : 500) + 22
        if (!last) {
          const p = T(x, y)
          hits.push({ x, y, w, h: 24, root: c.root })
          const on = hover === hits.length - 1
          ctx!.fillStyle = on ? '#eaedfb' : '#f2efe8'
          rr(p.x, p.y, S(w), S(24), S(12)); ctx!.fill()
          ctx!.strokeStyle = on ? DOT : '#e6e0d4'; ctx!.lineWidth = Math.max(1, S(1))
          rr(p.x, p.y, S(w), S(24), S(12)); ctx!.stroke()
          text(c.label, x + w / 2, y + 16, { size: 11.5, align: 'center', color: on ? DOT : '#6f6a61' })
        } else {
          text(c.label, x + w / 2, y + 16, { size: 11.5, align: 'center', weight: 700, color: INK })
        }
        x += w
        if (!last) { text('/', x + 5, y + 16, { size: 11.5, color: '#bdb8ad' }); x += 15 }
      })
    }

    function drawHeader() {
      text(scene!.root ? scene!.root.split('/').pop()! : scene!.title,
        62, 84, { size: 46, weight: 800, max: 620 })
      text('the same derived scene, read as a building', 62, 108, { size: 13, color: '#6d675e' })
      drawCrumbs()
      const label = 'RESET VIEW'
      const rw = measure(label, 9, 700) + 22
      const rx = HVW - 62 - rw, ry = 140
      hits.push({ x: rx, y: ry, w: rw, h: 22, root: '', kind: 'reset' })
      const ron = hover === hits.length - 1
      const rp = T(rx, ry)
      ctx!.fillStyle = ron ? '#eaedfb' : '#f2efe8'
      rr(rp.x, rp.y, S(rw), S(22), S(11)); ctx!.fill()
      ctx!.strokeStyle = ron ? DOT : '#e6e0d4'; ctx!.lineWidth = Math.max(1, S(1))
      rr(rp.x, rp.y, S(rw), S(22), S(11)); ctx!.stroke()
      text(label, rx + rw / 2, ry + 15, {
        size: 9, weight: 700, align: 'center', spacing: 1.1, color: ron ? DOT : '#8b8479',
      })

      const bits = [
        `${scene!.subsystems_shown} of ${scene!.subsystems_total} directories`,
        `${scene!.lifted_shown} of ${scene!.lifted_total} lifted relations`,
      ]
      text(bits.join('  ·  '), HVW - 62, 84, { size: 11.5, color: MUTED, align: 'right', font: MONO })
      text('floor = dependency depth · room width = files · stair = edges, summed · door = a transport',
        HVW - 62, 106, { size: 11.5, color: '#57534b', align: 'right' })
      text(`ctx-optimize · ${scene!.total_nodes.toLocaleString()} nodes · ${scene!.total_edges.toLocaleString()} edges`,
        HVW - 62, 128, { size: 11, color: ACC, align: 'right', weight: 700 })
    }

    function drawNotes() {
      scene!.notes.forEach((n, i) => {
        text(n, HVW - 62, HVH - 76 + i * 14, { size: 9.6, color: i === 0 ? '#8a5a12' : MUTED, align: 'right' })
      })
    }

    let raf = 0
    const t0 = performance.now()
    const frame = (now: number) => {
      const t = (now - t0) / 1000
      hits = []
      ctx.fillStyle = SKY; ctx.fillRect(0, 0, W, Hh)
      const p = T(0, 0)
      ctx.fillStyle = GROUND; ctx.fillRect(p.x, p.y, S(HVW), S(HVH))
      if (scene!.empty) {
        drawHeader()
        text(scene!.empty, HVW / 2, HVH / 2, { size: 16, color: '#57534b', align: 'center', max: 900 })
        raf = requestAnimationFrame(frame)
        return
      }
      drawShell()
      drawFloors()
      drawStairs(t)
      for (const r of H.rooms) drawRoom(r, t)
      drawDoors(t)
      drawHeader()
      drawNotes()
      raf = requestAnimationFrame(frame)
    }
    raf = requestAnimationFrame(frame)
    return () => {
      cancelAnimationFrame(raf)
      ro.disconnect()
      motion?.removeEventListener?.('change', onMotion)
      detach()
    }
  }, [scene])

  return (
    <div className="viewer flow">
      <div className="stage" ref={wrap}>
        <canvas ref={cv} />
        {err && <div className="err flow-msg">{err}</div>}
        {!scene && !err && <div className="flow-msg">drawing the building…</div>}
      </div>
    </div>
  )
}
