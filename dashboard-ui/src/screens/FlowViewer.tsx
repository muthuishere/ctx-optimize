import { useEffect, useRef, useState } from 'react'
import { api } from '../api'
import { bez, bezT, layout, VH, VW, type Box } from '../flowLayout'
import type { Scene } from '../types'
import type { ViewerProps } from '../viewers'

// FlowViewer — the derived architecture scene, drawn on a Canvas 2D in a
// virtual 1600x1000 space that scales to the stage.
//
// Every mark is a fact from GET /api/scene:
//   · a card is a DIRECTORY, its column is its longest-path depth in the lifted
//     dependency DAG, its detail line is its highest-degree declarations;
//   · a curve is N real `imports`/`calls` edges lifted to those directories,
//     labelled with the relation and the count — thickness scales with it;
//   · the dashed circles are the outer world: one per transport, holding the
//     port NAMES the boundary lane recorded (never a value), placed under the
//     subsystems that actually open them;
//   · the notes strip prints what is sampled and what is excluded.
//
// Zero external requests: Canvas 2D, system fonts, no image, no CDN.
// prefers-reduced-motion stops every travelling dot and pulse.

const INK = '#1c1b19'
const MUTED = '#8a857c'
const HAIR = '#d6d2c8'
const CARD = '#ffffff'
const ACC = '#e08a1e'
const DOT = '#4a5cd0'
const GROUND = '#fbfaf8'

const SANS = '-apple-system,BlinkMacSystemFont,"Segoe UI",Inter,Helvetica,Arial,sans-serif'
const MONO = 'ui-monospace,SFMono-Regular,Menlo,Consolas,monospace'

export default function FlowViewer({ module }: ViewerProps) {
  const [scene, setScene] = useState<Scene | null>(null)
  const [err, setErr] = useState('')
  const wrap = useRef<HTMLDivElement | null>(null)
  const cv = useRef<HTMLCanvasElement | null>(null)

  useEffect(() => {
    setScene(null)
    setErr('')
    api<Scene>(`/api/scene?module=${encodeURIComponent(module)}`)
      .then(setScene)
      .catch((e) => setErr(String(e.message || e)))
  }, [module])

  useEffect(() => {
    if (!scene || !cv.current || !wrap.current) return
    const canvas = cv.current
    const host = wrap.current
    const ctx = canvas.getContext('2d', { alpha: false })
    if (!ctx) return
    const lay = layout(scene)

    const motion = window.matchMedia
      ? window.matchMedia('(prefers-reduced-motion: reduce)')
      : null
    let still = !!motion?.matches
    const onMotion = () => { still = !!motion?.matches }
    motion?.addEventListener?.('change', onMotion)

    let SC = 1, OX = 0, OY = 0, W = 0, H = 0
    const resize = () => {
      const dpr = Math.min(window.devicePixelRatio || 1, 2)
      W = host.clientWidth || 1
      H = host.clientHeight || 1
      canvas.width = Math.floor(W * dpr)
      canvas.height = Math.floor(H * dpr)
      canvas.style.width = W + 'px'
      canvas.style.height = H + 'px'
      SC = Math.min(W / VW, H / VH)
      OX = (W - VW * SC) / 2
      OY = (H - VH * SC) / 2
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
    }
    const ro = new ResizeObserver(resize)
    ro.observe(host)
    resize()

    const T = (x: number, y: number) => ({ x: OX + x * SC, y: OY + y * SC })
    const S = (v: number) => v * SC

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

    function drawCurves(t: number) {
      for (const c of lay.curves) {
        const { p0, p1, p2, p3, link } = c
        const A = T(p0.x, p0.y), B = T(p1.x, p1.y), C = T(p2.x, p2.y), D = T(p3.x, p3.y)
        const world = link.to.startsWith('world:')
        const heft = 0.9 + 2.2 * Math.sqrt(link.weight / lay.maxWeight)
        ctx!.strokeStyle = world ? '#dcd3c1' : '#cfcabf'
        ctx!.lineWidth = Math.max(1, S(heft))
        ctx!.setLineDash(world ? [S(6), S(6)] : [])
        ctx!.beginPath(); ctx!.moveTo(A.x, A.y); ctx!.bezierCurveTo(B.x, B.y, C.x, C.y, D.x, D.y); ctx!.stroke()
        ctx!.setLineDash([])

        const tg = bezT(p0, p1, p2, p3, 1)
        const ang = Math.atan2(tg.y, tg.x)
        ctx!.save(); ctx!.translate(D.x, D.y); ctx!.rotate(ang)
        ctx!.strokeStyle = '#b6b0a4'; ctx!.lineWidth = Math.max(1, S(1.5))
        ctx!.beginPath(); ctx!.moveTo(-S(10), -S(5.5)); ctx!.lineTo(0, 0); ctx!.lineTo(-S(10), S(5.5)); ctx!.stroke()
        ctx!.restore()

        // travelling dots: count scales with weight, motion honours the OS flag
        const dots = Math.min(4, 1 + Math.round((link.weight / lay.maxWeight) * 3))
        for (let i = 0; i < dots; i++) {
          const u = still ? (i + 0.5) / dots : (t * 0.17 + i / dots) % 1
          const q = bez(p0, p1, p2, p3, u)
          const s = T(q.x, q.y)
          const fade = Math.min(1, Math.sin(Math.PI * Math.min(u, 1 - u) * 2.6) + 0.35)
          ctx!.fillStyle = `rgba(74,92,208,${0.85 * fade})`
          ctx!.beginPath(); ctx!.arc(s.x, s.y, S(3.6), 0, 7); ctx!.fill()
          ctx!.fillStyle = `rgba(74,92,208,${0.12 * fade})`
          ctx!.beginPath(); ctx!.arc(s.x, s.y, S(8), 0, 7); ctx!.fill()
        }

      }
    }

    // Edge labels are a SEPARATE pass, drawn after the cards. A relation label
    // that a card covers is worse than no label — the whole claim of this view
    // is that every arrow is named.
    function drawCurveLabels() {
      for (const c of lay.curves) {
        const { p0, p1, p2, p3, link } = c
        const m = bez(p0, p1, p2, p3, 0.5)
        const mt = bezT(p0, p1, p2, p3, 0.5)
        const off = 17
        const lbl = link.label
        const cnt = String(link.weight)
        const wl = measure(lbl, 9.5, 700) + lbl.length * 1.15
        const wc = measure(cnt, 9.5, 700, MONO)
        const pad = 5
        const total = wl + 7 + wc
        const lx = Math.min(VW - 70 - total / 2, Math.max(70 + total / 2, m.x - mt.y * off))
        const ly = m.y + mt.x * off + 4
        const q = T(lx - total / 2 - pad, ly - 11)
        ctx!.fillStyle = 'rgba(251,250,248,.94)'
        rr(q.x, q.y, S(total + pad * 2), S(15), S(3)); ctx!.fill()
        text(lbl, lx - total / 2, ly, { size: 9.5, weight: 700, color: MUTED, spacing: 1.15 })
        text(cnt, lx + total / 2, ly, { size: 9.5, weight: 700, color: ACC, font: MONO, align: 'right' })
      }
    }

    function drawCard(b: Box) {
      const c = b.card!
      const p = T(b.x, b.y)
      ctx!.save()
      ctx!.shadowColor = 'rgba(30,28,24,.10)'; ctx!.shadowBlur = S(18); ctx!.shadowOffsetY = S(5)
      ctx!.fillStyle = CARD; rr(p.x, p.y, S(b.w), S(b.h), S(12)); ctx!.fill()
      ctx!.restore()
      ctx!.strokeStyle = HAIR; ctx!.lineWidth = Math.max(1, S(1))
      rr(p.x, p.y, S(b.w), S(b.h), S(12)); ctx!.stroke()

      text(b.n, b.x + 18, b.y + 36, { size: 19, weight: 300, color: '#b8b3a8', font: MONO })
      const ip = T(b.x + 18, b.y + 48)
      ctx!.fillStyle = '#f3f1ec'; rr(ip.x, ip.y, S(26), S(26), S(7)); ctx!.fill()
      ctx!.strokeStyle = '#e6e2d9'; ctx!.lineWidth = Math.max(1, S(1))
      rr(ip.x, ip.y, S(26), S(26), S(7)); ctx!.stroke()
      text(c.glyph, b.x + 31, b.y + 66, { size: 13, color: '#6f6a61', align: 'center', font: MONO })

      text(c.label, b.x + 54, b.y + 67, { size: 16, weight: 700, max: b.w - 70 })
      text(c.dir, b.x + 18, b.y + 91, { size: 10.5, color: '#a09a90', font: MONO, max: b.w - 36 })
      text(c.detail || `${c.files} files`, b.x + 18, b.y + b.h - 15,
        { size: 11.5, color: MUTED, max: b.w - 36 })

      // in/out counters, top-right — the numbers that decided the column
      text(`${c.out}↗`, b.x + b.w - 18, b.y + 26, { size: 10, color: '#a8a29a', font: MONO, align: 'right' })
      text(`↘${c.in}`, b.x + b.w - 18, b.y + 40, { size: 10, color: '#a8a29a', font: MONO, align: 'right' })
    }

    function drawHub(b: Box, t: number) {
      const c = b.card!
      const p = T(b.x, b.y)
      const R = S(b.r)
      const pulse = still ? 1 : 1 + Math.sin(t * 1.4) * 0.012
      ctx!.save()
      ctx!.shadowColor = 'rgba(74,92,208,.20)'; ctx!.shadowBlur = S(34)
      ctx!.fillStyle = CARD; ctx!.beginPath(); ctx!.arc(p.x, p.y, R * pulse, 0, 7); ctx!.fill()
      ctx!.restore()
      ctx!.strokeStyle = '#c3c9ee'; ctx!.lineWidth = S(4)
      ctx!.beginPath(); ctx!.arc(p.x, p.y, R * pulse, 0, 7); ctx!.stroke()

      text('MOST DEPENDED ON', b.x, b.y - 46, { size: 8.5, color: MUTED, align: 'center', spacing: 1.3, weight: 700 })
      text(c.label, b.x, b.y - 20, { size: 19, weight: 700, align: 'center', max: b.r * 1.8 })
      text(c.dir, b.x, b.y - 2, { size: 9.5, color: '#a09a90', font: MONO, align: 'center', max: b.r * 1.85 })
      text(c.detail, b.x, b.y + 20, { size: 11, color: MUTED, align: 'center', max: b.r * 1.8 })
      text(`${c.in} in · ${c.out} out`, b.x, b.y + 44, { size: 11, color: DOT, font: MONO, align: 'center', weight: 700 })
      text(`${c.files} files · ${c.decls} decls`, b.x, b.y + 62, { size: 10, color: '#a8a29a', font: MONO, align: 'center' })
    }

    // The outer world: one dashed PLATE per transport, holding a bounded
    // sample of the port NAMES. The killed wall view put 273 names around a
    // perimeter and could not be read; a plate lists a few and says how many it
    // is not showing.
    function drawWorld(b: Box, t: number) {
      const w = b.world!
      const p = T(b.x, b.y)
      ctx!.save()
      ctx!.fillStyle = '#fbf4e8'
      rr(p.x, p.y, S(b.w), S(b.h), S(14)); ctx!.fill()
      ctx!.setLineDash([S(5), S(7)])
      if (!still) ctx!.lineDashOffset = -t * S(26)
      ctx!.strokeStyle = '#e3b877'; ctx!.lineWidth = S(1.6)
      rr(p.x, p.y, S(b.w), S(b.h), S(14)); ctx!.stroke()
      ctx!.restore()

      text('OUTER WORLD', b.x + 14, b.y + 20, { size: 8, color: '#bfa06a', spacing: 1.3, weight: 700 })
      text(w.transport, b.x + 14, b.y + 40, { size: 14, weight: 700, font: MONO, max: b.w - 110 })
      const dir = [w.provides ? `${w.provides} provided` : '', w.consumes ? `${w.consumes} consumed` : '']
        .filter(Boolean).join(' · ')
      text(`${w.total} ports`, b.x + b.w - 14, b.y + 22, { size: 11, color: '#a07a3a', align: 'right', font: MONO, weight: 700 })
      text(dir, b.x + b.w - 14, b.y + 38, { size: 9.5, color: MUTED, align: 'right', font: MONO })

      // door NAMES, two per row. Never a value: a port node has none.
      const cols = 2
      const cw = (b.w - 28 - 8) / cols
      w.sample.forEach((d, i) => {
        const cx2 = b.x + 14 + (i % cols) * (cw + 8)
        const cy = b.y + 54 + Math.floor(i / cols) * 24
        const q = T(cx2, cy)
        ctx!.fillStyle = d.sensitive ? '#fdf3e3' : '#fff'
        rr(q.x, q.y, S(cw), S(20), S(10)); ctx!.fill()
        ctx!.strokeStyle = d.sensitive ? ACC : '#e8e1d3'
        ctx!.lineWidth = Math.max(1, S(d.sensitive ? 1.4 : 1))
        rr(q.x, q.y, S(cw), S(20), S(10)); ctx!.stroke()
        if (d.sensitive) {
          const dp = T(cx2 + 10, cy + 10)
          ctx!.fillStyle = ACC
          ctx!.beginPath(); ctx!.arc(dp.x, dp.y, S(3), 0, 7); ctx!.fill()
        }
        text(d.label, cx2 + (d.sensitive ? 18 : 9), cy + 14, {
          size: 9.5, weight: 600, font: MONO,
          color: d.dynamic ? '#b0a99e' : '#5e594f',
          max: cw - (d.sensitive ? 26 : 18),
        })
      })
      const rows = Math.ceil(w.sample.length / cols)
      const foot = b.y + 54 + rows * 24 + 4
      const bits: string[] = []
      if (w.truncated) bits.push(`sample ${w.sample.length} of ${w.total}`)
      if (w.sensitive > 0) bits.push(`${w.sensitive} sensitive (name only)`)
      if (bits.length > 0) {
        text(bits.join(' · '), b.x + 14, foot + 9, { size: 9, color: MUTED, font: MONO, max: b.w - 28 })
      }
    }

    function drawHeader(t: number) {
      // terminal strip, top-left
      const bw = 720, bh = 46, bx = 62, by = 62
      const p = T(bx, by)
      ctx!.save()
      ctx!.shadowColor = 'rgba(0,0,0,.22)'; ctx!.shadowBlur = S(16); ctx!.shadowOffsetY = S(4)
      ctx!.fillStyle = '#151412'; rr(p.x, p.y, S(bw), S(bh), S(10)); ctx!.fill()
      ctx!.restore()
      let cx = bx + 20
      const segs: [string, string][] = [[scene!.title, '#e6e2d9']]
      for (const s of scene!.stats) segs.push(['|', '#5a564e'], [`${s.text} ${s.label}`, s.label === 'ports' ? '#e0a955' : '#7fd6a0'])
      for (const [s, col] of segs) {
        text(s, cx, by + 30, { size: 13.5, color: col, font: MONO, weight: 600 })
        cx += measure(s, 13.5, 600, MONO) + 9
      }
      if (!still && Math.floor(t * 1.8) % 2) {
        const q = T(cx + 2, by + 15)
        ctx!.fillStyle = '#7fd6a0'; ctx!.fillRect(q.x, q.y, S(8), S(16))
      }

      text(scene!.title, VW - 62, 118, { size: 62, weight: 800, align: 'right', max: 700 })
      text('The architecture, derived from the store — not drawn by hand.', VW - 62, 152,
        { size: 15, color: '#57534b', align: 'right' })
      text(`every card is a directory · every arrow is real ${''}imports/calls edges, summed`,
        VW - 62, 176, { size: 15, weight: 600, align: 'right' })
      text(`ctx-optimize · ${scene!.total_nodes.toLocaleString()} nodes · ${scene!.total_edges.toLocaleString()} edges`,
        VW - 62, 202, { size: 12, color: ACC, align: 'right', weight: 700, spacing: 0.6 })
    }

    function drawFooter() {
      const a = T(0, VH - 112), b2 = T(VW, VH - 112)
      ctx!.strokeStyle = '#e6e2d9'; ctx!.lineWidth = Math.max(1, S(1))
      ctx!.beginPath(); ctx!.moveTo(a.x, a.y); ctx!.lineTo(b2.x, b2.y); ctx!.stroke()

      let x = 62
      const y = VH - 92
      for (const s of scene!.chips) {
        const w = measure(s, 11.5, 500) + 26
        const p = T(x, y)
        ctx!.fillStyle = '#fbfaf7'; rr(p.x, p.y, S(w), S(28), S(14)); ctx!.fill()
        ctx!.strokeStyle = '#e2ddd2'; ctx!.lineWidth = Math.max(1, S(1))
        rr(p.x, p.y, S(w), S(28), S(14)); ctx!.stroke()
        text(s, x + w / 2, y + 19, { size: 11.5, color: '#5e594f', align: 'center' })
        x += w + 10
      }
      // the honesty strip — what is sampled, what is excluded
      scene!.notes.forEach((n, i) => {
        text(n, VW - 62, VH - 88 + i * 14.5, { size: 9.8, color: i === 0 ? '#8a5a12' : MUTED, align: 'right' })
      })
    }

    let raf = 0
    const t0 = performance.now()
    const frame = (now: number) => {
      const t = (now - t0) / 1000
      ctx.fillStyle = '#f4f3ef'; ctx.fillRect(0, 0, W, H)
      const p = T(0, 0)
      ctx.fillStyle = GROUND; ctx.fillRect(p.x, p.y, S(VW), S(VH))
      drawFooter()
      drawCurves(t)
      // Paint order is the legibility contract: curves, then the cards that
      // must never be crossed out, then the labels that name every curve, then
      // the world plates (which own the bottom band outright), then the header.
      for (const b of lay.boxes) {
        if (b.kind === 'card') drawCard(b)
        else if (b.kind === 'hub') drawHub(b, t)
      }
      drawCurveLabels()
      for (const b of lay.boxes) {
        if (b.kind === 'world') drawWorld(b, t)
      }
      drawHeader(t)
      raf = requestAnimationFrame(frame)
    }
    raf = requestAnimationFrame(frame)
    return () => {
      cancelAnimationFrame(raf)
      ro.disconnect()
      motion?.removeEventListener?.('change', onMotion)
    }
  }, [scene])

  return (
    <div className="viewer flow">
      <div className="stage" ref={wrap}>
        <canvas ref={cv} />
        {err && <div className="err flow-msg">{err}</div>}
        {!scene && !err && <div className="flow-msg">deriving the scene…</div>}
        {scene?.empty && <div className="flow-msg">{scene.empty}</div>}
      </div>
    </div>
  )
}
