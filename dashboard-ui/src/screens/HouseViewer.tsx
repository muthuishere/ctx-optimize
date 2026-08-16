import { useEffect, useRef, useState } from 'react'
import { api } from '../api'
import { relationStyle } from '../flowLayout'
import { houseLayout, HVH, HVW, type House, type Room } from '../houseLayout'
import { sanitizeScene } from '../sanitize'
import type { Scene } from '../types'
import { mix, readPalette, rgba } from '../theme'
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


const SANS = '-apple-system,BlinkMacSystemFont,"Segoe UI",Inter,Helvetica,Arial,sans-serif'
const MONO = 'ui-monospace,SFMono-Regular,Menlo,Consolas,monospace'

export default function HouseViewer({ module, root, grain, onRoot }: ViewerProps) {
  const [scene, setScene] = useState<Scene | null>(null)
  const [err, setErr] = useState('')
  const wrap = useRef<HTMLDivElement | null>(null)
  const cv = useRef<HTMLCanvasElement | null>(null)

  useEffect(() => {
    setScene(null)
    setErr('')
    const q = new URLSearchParams({ module })
    if (root) q.set('root', root)
    if (grain) q.set('grain', grain)
    api<Scene>(`/api/scene?${q}`)
      .then((s) => setScene(sanitizeScene(s)))
      .catch((e) => setErr(String(e.message || e)))
  }, [module, root, grain])


  useEffect(() => {
    if (!scene || !cv.current || !wrap.current) return
    const canvas = cv.current
    const host = wrap.current
    const ctx = canvas.getContext('2d', { alpha: false })
    if (!ctx) return
    const pal = readPalette(host)
    const INK = pal.text
    const MUTED = pal.muted
    const WALL = pal.line2
    const ROOM = pal.panel
    const ACC = pal.amber
    const DOT = pal.focus
    const GROUND = pal.ground

    const motion = window.matchMedia
      ? window.matchMedia('(prefers-reduced-motion: reduce)')
      : null
    let still = !!motion?.matches
    const onMotion = () => { still = !!motion?.matches }
    motion?.addEventListener?.('change', onMotion)

    // No camera: the frame is fixed, the chrome stays put, and the picture
    // fills the stage by construction — the width is the unit and the virtual
    // height follows the stage's aspect ratio.
    let SC = 1, W = 0, Hh = 0, vh = HVH
    let H: House = houseLayout(scene, vh)
    const resize = () => {
      const dpr = Math.min(window.devicePixelRatio || 1, 2)
      W = host.clientWidth || 1
      Hh = host.clientHeight || 1
      canvas.width = Math.floor(W * dpr)
      canvas.height = Math.floor(Hh * dpr)
      canvas.style.width = W + 'px'
      canvas.style.height = Hh + 'px'
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
      SC = W / HVW
      vh = Hh / SC
      H = houseLayout(scene, vh)
    }
    const ro = new ResizeObserver(resize)
    ro.observe(host)
    resize()

    const T = (x: number, y: number) => ({ x: x * SC, y: y * SC })
    const S = (v: number) => v * SC

    // Hit regions live in VIRTUAL space: the camera pans and zooms between
    // frames, so a region measured in pixels would drift the moment it did.
    type Hit = { x: number; y: number; w: number; h: number; root: string; kind?: 'reset'; grain?: string }
    let hits: Hit[] = []
    let hover = -1
    const hitAt = (vx: number, vy: number) =>
      hits.findIndex((h) => vx >= h.x && vx <= h.x + h.w && vy >= h.y && vy <= h.y + h.h)
    const toVirtual = (e: PointerEvent) => {
      const r = canvas.getBoundingClientRect()
      return { x: (e.clientX - r.left) / SC, y: (e.clientY - r.top) / SC }
    }
    const onMove = (e: PointerEvent) => {
      const v = toVirtual(e)
      hover = hitAt(v.x, v.y)
      canvas.style.cursor = hover >= 0 ? 'pointer' : 'default'
    }
    const onUp = (e: PointerEvent) => {
      const v = toVirtual(e)
      const i = hitAt(v.x, v.y)
      if (i >= 0 && hits[i].kind !== 'reset') onRoot(hits[i].root, hits[i].grain || '')
    }
    const onLeave = () => { hover = -1; canvas.style.cursor = 'default' }
    canvas.addEventListener('pointermove', onMove)
    canvas.addEventListener('pointerup', onUp)
    canvas.addEventListener('pointerleave', onLeave)

    // The room's NAME is the way in — see FlowViewer.titleLink. A corner badge
    // is discoverable only once you know to look; the name is the first thing
    // read, so it is what should be clickable, and it says so by looking like a
    // link. Only the TEXT is a target, so the rest of the room stays inert.
    function titleLink(
      label: string, x: number, baseY: number, size: number, maxW: number,
      root: string, enterable: boolean, grain = '',
    ): boolean {
      let shown = label
      ctx!.font = `700 ${S(size)}px ${SANS}`
      while (shown.length > 1 && ctx!.measureText(shown).width / SC > maxW) shown = shown.slice(0, -1)
      if (shown !== label) shown = shown.slice(0, -1) + '…'
      const w = ctx!.measureText(shown).width / SC
      let on = false
      if (enterable) {
        hits.push({ x, y: baseY - size, w, h: size * 1.35, root, grain })
        on = hover === hits.length - 1
      }
      text(shown, x, baseY, {
        size, weight: 700,
        color: enterable ? (on ? pal.accent : mix(pal.text, pal.accent, .55)) : INK,
      })
      if (enterable) {
        const u = T(x, baseY + 3.5)
        ctx!.strokeStyle = on ? pal.accent : rgba(pal.accent, .45)
        ctx!.lineWidth = Math.max(1, S(on ? 1.4 : 1))
        ctx!.setLineDash(on ? [] : [S(2.5), S(2.5)])
        ctx!.beginPath(); ctx!.moveTo(u.x, u.y); ctx!.lineTo(u.x + S(w), u.y); ctx!.stroke()
        ctx!.setLineDash([])
      }
      return on
    }

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
        while (s.length > 1 && ctx!.measureText(s).width / SC > max) s = s.slice(0, -1)
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
      return ctx!.measureText(s).width / SC
    }

    // ---- the shell: roof, walls, ground line
    function drawShell() {
      const wallL = 210, wallR = HVW - 210
      // ground
      const g = T(0, H.ground + 26)
      ctx!.strokeStyle = pal.line2; ctx!.lineWidth = Math.max(1, S(2))
      ctx!.beginPath(); ctx!.moveTo(T(60, 0).x, g.y); ctx!.lineTo(T(HVW - 60, 0).x, g.y); ctx!.stroke()
      // hatching under the ground: this is where the foundation sits
      ctx!.strokeStyle = pal.line; ctx!.lineWidth = Math.max(1, S(1))
      for (let x = 70; x < HVW - 60; x += 16) {
        const a = T(x, H.ground + 26), b = T(x - 12, H.ground + 40)
        ctx!.beginPath(); ctx!.moveTo(a.x, a.y); ctx!.lineTo(b.x, b.y); ctx!.stroke()
      }
      // roof
      const apex = T(HVW / 2, H.roof - 74)
      const l = T(wallL - 26, H.roof - 4), r = T(wallR + 26, H.roof - 4)
      ctx!.fillStyle = mix(pal.panel, pal.text, .04)
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
        ctx!.strokeStyle = pal.line; ctx!.lineWidth = Math.max(1, S(1))
        ctx!.beginPath(); ctx!.moveTo(T(198, 0).x, p.y + S(f.h)); ctx!.lineTo(T(HVW - 198, 0).x, p.y + S(f.h)); ctx!.stroke()
        text(f.label, 198, f.y - 7, { size: 8.5, color: pal.dim, spacing: 1.35, weight: 700 })
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
      // The room frame no longer carries hover: the NAME is the target, so the
      // name is what lights up. A whole room highlighting for a link inside it
      // says "click anywhere", which is exactly the confusion that made the
      // card un-draggable in the flow view.
      const pillar = c.hub
      ctx!.save()
      ctx!.shadowColor = 'rgba(0,0,0,.4)'
      ctx!.shadowBlur = S(12); ctx!.shadowOffsetY = S(3)
      ctx!.fillStyle = pillar ? mix(pal.panel, pal.amber, .12) : ROOM
      rr(p.x, p.y, S(r.w), S(r.h), S(6)); ctx!.fill()
      ctx!.restore()
      ctx!.strokeStyle = pillar ? ACC : WALL
      ctx!.lineWidth = Math.max(1, S(pillar ? 1.8 : 1))
      rr(p.x, p.y, S(r.w), S(r.h), S(6)); ctx!.stroke()

      const pad = 12
      text(r.n, r.x + pad, r.y + 22, { size: 12, weight: 400, color: pal.dim, font: MONO })
      // The load-bearing mark rides on the TOP row beside the ordinal. It used
      // to sit at the foot of the room, where it landed on top of the files
      // line in exactly the room that earns it — the hub is often the smallest
      // directory, so it gets the shortest room.
      if (pillar) {
        text('LOAD-BEARING', r.x + pad + 26, r.y + 22, { size: 8, color: ACC, spacing: 1.2, weight: 700 })
      }
      const titleOn = titleLink(c.label, r.x + pad, r.y + 44, 15, r.w - pad * 2, c.dir, c.children > 0 && c.inner > 0, c.enter_grain)
      if (r.h > 74) {
        text(`${c.files} files · ${c.decls} decls`, r.x + pad, r.y + 64,
          { size: 10, color: MUTED, font: MONO, max: r.w - pad * 2 })
      }
      if (r.h > 104 && c.detail) {
        text(c.detail, r.x + pad, r.y + 84, { size: 10.5, color: pal.dim, max: r.w - pad * 2 })
      }
      // in/out, bottom-right — the numbers that put this room on this floor
      if (r.w > 150) {
        text(`↘${c.in}  ${c.out}↗`, r.x + r.w - pad, r.y + r.h - 12,
          { size: 9.5, color: pal.dim, font: MONO, align: 'right' })
      }
      if (c.children > 0 && c.inner > 0) {
        const label = `${c.children} inside`
        const w = measure(label, 9, 700, MONO) + 24
        const bx = r.x + r.w - w - 8, by = r.y + 8
        const q = T(bx, by)
        ctx!.fillStyle = titleOn ? mix(pal.panel, pal.accent, .18) : mix(pal.panel, pal.text, .05)
        rr(q.x, q.y, S(w), S(17), S(8.5)); ctx!.fill()
        ctx!.strokeStyle = titleOn ? pal.accent : pal.line2; ctx!.lineWidth = Math.max(1, S(1))
        rr(q.x, q.y, S(w), S(17), S(8.5)); ctx!.stroke()
        text(label, bx + 8, by + 12, { size: 9, weight: 700, font: MONO, color: titleOn ? pal.accent : pal.muted })
        const cx = bx + w - 10, cy = by + 8.5
        const a1 = T(cx - 2.5, cy - 3.5), b1 = T(cx + 1.5, cy), c1 = T(cx - 2.5, cy + 3.5)
        ctx!.strokeStyle = titleOn ? pal.accent : pal.dim; ctx!.lineWidth = Math.max(1, S(1.5))
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
        ctx!.fillStyle = mix(pal.panel, pal.amber, .10)
        rr(p.x, p.y, S(d.w), S(d.h), S(8)); ctx!.fill()
        ctx!.setLineDash([S(5), S(6)])
        if (!still) ctx!.lineDashOffset = -t * S(22)
        ctx!.strokeStyle = rgba(pal.amber, .7); ctx!.lineWidth = S(1.5)
        rr(p.x, p.y, S(d.w), S(d.h), S(8)); ctx!.stroke()
        ctx!.restore()

        text('DOOR', d.x + 11, d.y + 17, { size: 7.5, color: rgba(pal.amber, .8), spacing: 1.2, weight: 700 })
        text(w.transport, d.x + 11, d.y + 36, { size: 12, weight: 700, font: MONO, max: d.w - 22 })
        text(`${w.total} ports`, d.x + 11, d.y + 54, { size: 10, color: pal.amber, font: MONO, weight: 700 })
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
        ctx!.strokeStyle = rgba(pal.amber, .45); ctx!.lineWidth = Math.max(1, S(1.4))
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
          hits.push({ x, y, w, h: 24, root: c.root, grain: '' })
          const on = hover === hits.length - 1
          ctx!.fillStyle = on ? mix(pal.panel, pal.focus, .22) : mix(pal.panel, pal.text, .05)
          rr(p.x, p.y, S(w), S(24), S(12)); ctx!.fill()
          ctx!.strokeStyle = on ? DOT : pal.line2; ctx!.lineWidth = Math.max(1, S(1))
          rr(p.x, p.y, S(w), S(24), S(12)); ctx!.stroke()
          text(c.label, x + w / 2, y + 16, { size: 11.5, align: 'center', color: on ? DOT : '#6f6a61' })
        } else {
          text(c.label, x + w / 2, y + 16, { size: 11.5, align: 'center', weight: 700, color: INK })
        }
        x += w
        if (!last) { text('/', x + 5, y + 16, { size: 11.5, color: pal.dim }); x += 15 }
      })
    }

    function drawHeader() {
      text(scene!.root ? scene!.root.split('/').pop()! : scene!.title,
        62, 84, { size: 46, weight: 800, max: 620 })
      text('the same derived scene, read as a building', 62, 108, { size: 13, color: pal.muted })
      drawCrumbs()
      const label = 'RESET VIEW'
      const rw = measure(label, 9, 700) + 22
      const rx = HVW - 62 - rw, ry = 140
      hits.push({ x: rx, y: ry, w: rw, h: 22, root: '', kind: 'reset' })
      const ron = hover === hits.length - 1
      const rp = T(rx, ry)
      ctx!.fillStyle = ron ? mix(pal.panel, pal.focus, .22) : mix(pal.panel, pal.text, .05)
      rr(rp.x, rp.y, S(rw), S(22), S(11)); ctx!.fill()
      ctx!.strokeStyle = ron ? DOT : pal.line2; ctx!.lineWidth = Math.max(1, S(1))
      rr(rp.x, rp.y, S(rw), S(22), S(11)); ctx!.stroke()
      text(label, rx + rw / 2, ry + 15, {
        size: 9, weight: 700, align: 'center', spacing: 1.1, color: ron ? DOT : pal.muted,
      })

      const bits = [
        `${scene!.subsystems_shown} of ${scene!.subsystems_total} directories`,
        `${scene!.lifted_shown} of ${scene!.lifted_total} lifted relations`,
      ]
      text(bits.join('  ·  '), HVW - 62, 84, { size: 11.5, color: MUTED, align: 'right', font: MONO })
      text('floor = dependency depth · room width = files · stair = edges, summed · door = a transport',
        HVW - 62, 106, { size: 11.5, color: pal.muted, align: 'right' })
      text(`ctx-optimize · ${scene!.total_nodes.toLocaleString()} nodes · ${scene!.total_edges.toLocaleString()} edges`,
        HVW - 62, 128, { size: 11, color: ACC, align: 'right', weight: 700 })
    }

    function drawNotes() {
      scene!.notes.forEach((n, i) => {
        text(n, HVW - 62, H.vh - 76 + i * 14, { size: 9.6, color: i === 0 ? pal.amber : MUTED, align: 'right' })
      })
    }

    let raf = 0
    const t0 = performance.now()
    const frame = (now: number) => {
      const t = (now - t0) / 1000
      hits = []
      // The ground fills the STAGE: painting only the virtual rect left a
      // border of another colour the moment the camera stopped exactly fitting.
      ctx.fillStyle = GROUND; ctx.fillRect(0, 0, W, Hh)
      if (scene!.empty) {
        drawHeader()
        text(scene!.empty, HVW / 2, H.vh / 2, { size: 16, color: pal.muted, align: 'center', max: 900 })
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
      canvas.removeEventListener('pointermove', onMove)
      canvas.removeEventListener('pointerup', onUp)
      canvas.removeEventListener('pointerleave', onLeave)
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
