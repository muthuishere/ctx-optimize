import { useEffect, useRef, useState } from 'react'
import { arrangementKey, clearArrangement, loadArrangement, saveArrangement } from '../arrangement'
import { fetchScene } from '../sceneApi'
import { bez, bezT, CHIP_H, layout, relationStyle, VW, type Box } from '../flowLayout'
import type { Scene } from '../types'
import { mix, readPalette, rgba } from '../theme'
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

const ZERO = { dx: 0, dy: 0 }

const SANS = '-apple-system,BlinkMacSystemFont,"Segoe UI",Inter,Helvetica,Arial,sans-serif'
const MONO = 'ui-monospace,SFMono-Regular,Menlo,Consolas,monospace'

export default function FlowViewer({ module, root, grain, onRoot, onModule }: ViewerProps) {
  const [scene, setScene] = useState<Scene | null>(null)
  const [err, setErr] = useState('')
  const wrap = useRef<HTMLDivElement | null>(null)
  const cv = useRef<HTMLCanvasElement | null>(null)

  // A level with one card is a chooser wearing a diagram — the server says so
  // and names where the content is, and the address moves there. Doing it here
  // rather than in the fetch keeps the URL honest: the reader lands on, and can
  // share, the store they are actually looking at.
  useEffect(() => {
    if (scene?.redirect) onModule(scene.redirect)
  }, [scene, onModule])

  useEffect(() => {
    setScene(null)
    setErr('')
    fetchScene(module, root, grain)
      .then((s) => setScene(s))
      .catch((e) => setErr(String(e.message || e)))
  }, [module, root, grain])


  useEffect(() => {
    if (!scene || !cv.current || !wrap.current) return
    const canvas = cv.current
    const host = wrap.current
    const ctx = canvas.getContext('2d', { alpha: false })
    if (!ctx) return
    // The palette comes from the app's CSS variables, so the canvas can never
    // drift out of step with the dashboard around it.
    const pal = readPalette(host)
    const INK = pal.text
    const MUTED = pal.muted
    const HAIR = pal.line
    const CARD = pal.panel
    const ACC = pal.amber
    const DOT = pal.focus
    const GROUND = pal.ground

    const motion = window.matchMedia
      ? window.matchMedia('(prefers-reduced-motion: reduce)')
      : null
    let still = !!motion?.matches
    const onMotion = () => { still = !!motion?.matches }
    motion?.addEventListener?.('change', onMotion)

    // NO CAMERA. The frame does not move: the diagram fills the stage, the
    // header, footer and notes stay exactly where they are, and the only thing
    // a drag moves is the shape under the pointer. Panning the whole scene took
    // the title and the honesty notes with it, which is not a view of anything.
    //
    // "Fully occupied" is by construction rather than by zooming: the width is
    // the unit (1600 virtual units = the stage width) and the virtual HEIGHT is
    // whatever the stage's aspect ratio makes it, so there is never a letterbox
    // to zoom out of.
    let SC = 1, W = 0, H = 0, vh = 1000
    // Which outer-world group is open. Collapsed, the band is a strip of chips
    // and the cards get the room back; expanded, one group shows its door names.
    let openWorld = loadArrangement(arrangementKey('flow', module, root, grain))?.openWorld || ''
    let lay = layout(scene, vh, openWorld)
    const relayout = () => { lay = layout(scene, vh, openWorld) }
    const resize = () => {
      const dpr = Math.min(window.devicePixelRatio || 1, 2)
      W = host.clientWidth || 1
      H = host.clientHeight || 1
      canvas.width = Math.floor(W * dpr)
      canvas.height = Math.floor(H * dpr)
      canvas.style.width = W + 'px'
      canvas.style.height = H + 'px'
      ctx.setTransform(dpr, 0, 0, dpr, 0, 0)
      SC = W / VW
      vh = H / SC
      relayout()
    }
    const ro = new ResizeObserver(resize)
    ro.observe(host)
    resize()

    const T = (x: number, y: number) => ({ x: x * SC, y: y * SC })
    const S = (v: number) => v * SC

    // Hit regions are rebuilt every frame in VIRTUAL units; the transform is a
    // plain scale, so screen->virtual is one division.
    type Hit = {
      x: number; y: number; w: number; h: number; root: string
      kind: 'card' | 'crumb' | 'reset' | 'world'
      /** the grain this target opens at; '' means infer from root */
      grain?: string
      /** a different STORE to open; set on the repo crumb above a module */
      module?: string
    }
    let hits: Hit[] = []
    let hover = -1
    const hitAt = (vx: number, vy: number) =>
      hits.findIndex((h) => vx >= h.x && vx <= h.x + h.w && vy >= h.y && vy <= h.y + h.h)

    // A dragged box carries an OFFSET from its derived position, never a
    // replacement for it: the layout stays the source of truth, and Reset puts
    // everything back exactly where the store put it.
    // The arrangement is the reader's, so it survives a reload. It is stored
    // as OFFSETS from the derived layout, which is what makes it safe to keep:
    // the picture underneath is still derived, and RESET VIEW is one click.
    const akey = arrangementKey('flow', module, root, grain)
    const saved = loadArrangement(akey)
    const nudged = new Map<string, { dx: number; dy: number }>(
      Object.entries(saved?.nudged || {}))
    const off = (id: string) => nudged.get(id) || ZERO
    const remember = () => saveArrangement(akey, {
      nudged: Object.fromEntries(nudged), openWorld,
    })
    let dragging: { id: string; ox: number; oy: number } | null = null
    let moved = 0

    const toVirtual = (e: PointerEvent) => {
      const r = canvas.getBoundingClientRect()
      return { x: (e.clientX - r.left) / SC, y: (e.clientY - r.top) / SC }
    }
    const boxAt = (vx: number, vy: number) => {
      for (let i = lay.boxes.length - 1; i >= 0; i--) {
        const b = lay.boxes[i]
        const o = off(b.id)
        if (b.kind === 'hub') {
          if (Math.hypot(vx - (b.x + o.dx), vy - (b.y + o.dy)) <= b.r) return b
        } else if (vx >= b.x + o.dx && vx <= b.x + o.dx + b.w &&
                   vy >= b.y + o.dy && vy <= b.y + o.dy + b.h) return b
      }
      return null
    }

    const onDown = (e: PointerEvent) => {
      if (e.button !== 0) return
      const v = toVirtual(e)
      moved = 0
      if (hitAt(v.x, v.y) >= 0) return // a link: decided on pointerup
      const b = boxAt(v.x, v.y)
      if (!b) return
      const o = off(b.id)
      dragging = { id: b.id, ox: v.x - o.dx, oy: v.y - o.dy }
      canvas.setPointerCapture(e.pointerId)
      canvas.style.cursor = 'grabbing'
    }
    const onMove = (e: PointerEvent) => {
      const v = toVirtual(e)
      if (dragging) {
        moved += 1
        nudged.set(dragging.id, { dx: v.x - dragging.ox, dy: v.y - dragging.oy })
        return
      }
      hover = hitAt(v.x, v.y)
      canvas.style.cursor = hover >= 0 ? 'pointer' : boxAt(v.x, v.y) ? 'grab' : 'default'
    }
    const onUp = (e: PointerEvent) => {
      const wasDragging = dragging
      dragging = null
      try { canvas.releasePointerCapture(e.pointerId) } catch { /* already gone */ }
      canvas.style.cursor = 'default'
      if (wasDragging) {
        remember()
        return
      }
      const v = toVirtual(e)
      const i = hitAt(v.x, v.y)
      if (i < 0) return
      if (hits[i].kind === 'reset') {
        nudged.clear()
        openWorld = ''
        clearArrangement(akey)
        relayout()
        return
      }
      if (hits[i].kind === 'world') {
        openWorld = openWorld === hits[i].root ? '' : hits[i].root
        remember()
        relayout()
        return
      }
      // Leaving the store is not a directory move. The repo crumb above a
      // module says so explicitly rather than being inferred from the label.
      if (hits[i].module) { onModule(hits[i].module!); return }
      // At module grain a card names a STORE, not a directory of this one.
      if (scene.level === 'module') { onModule(hits[i].root); return }
      onRoot(hits[i].root, hits[i].grain || '')
    }
    const onLeave = () => { hover = -1; canvas.style.cursor = 'default' }
    canvas.addEventListener('pointerdown', onDown)
    canvas.addEventListener('pointermove', onMove)
    canvas.addEventListener('pointerup', onUp)
    canvas.addEventListener('pointercancel', onUp)
    canvas.addEventListener('pointerleave', onLeave)

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
        const { link } = c
        // A dragged card takes its arrows with it: each endpoint moves by its
        // own box's offset and the control points by the average, so a curve
        // never detaches from the thing it describes.
        const oa = off(link.from), ob = off(link.to)
        const shift = (q: { x: number; y: number }, d: { dx: number; dy: number }) =>
          d === ZERO ? q : { x: q.x + d.dx, y: q.y + d.dy }
        const mid = { dx: (oa.dx + ob.dx) / 2, dy: (oa.dy + ob.dy) / 2 }
        const p0 = shift(c.p0, oa), p3 = shift(c.p3, ob)
        const p1 = shift(c.p1, mid), p2 = shift(c.p2, mid)
        const A = T(p0.x, p0.y), B = T(p1.x, p1.y), C = T(p2.x, p2.y), D = T(p3.x, p3.y)
        const world = link.to.startsWith('world:')
        const rs = relationStyle(link.relation)
        const heft = 0.9 + 2.2 * Math.sqrt(link.weight / lay.maxWeight)
        ctx!.strokeStyle = rs.line
        ctx!.lineWidth = Math.max(1, S(heft))
        ctx!.setLineDash(world || rs.dashed ? [S(6), S(6)] : [])
        ctx!.beginPath(); ctx!.moveTo(A.x, A.y); ctx!.bezierCurveTo(B.x, B.y, C.x, C.y, D.x, D.y); ctx!.stroke()
        ctx!.setLineDash([])

        // An arrowhead is a claim of DIRECTION. `shares` has none — it says two
        // modules touch the same external service, and which of them is "from"
        // is an artefact of sort order. Drawing a head there would turn a
        // coincidence into a call, which is exactly what the world view was
        // killed for.
        if (link.relation !== 'shares') {
          const tg = bezT(p0, p1, p2, p3, 1)
          const ang = Math.atan2(tg.y, tg.x)
          ctx!.save(); ctx!.translate(D.x, D.y); ctx!.rotate(ang)
          ctx!.strokeStyle = rs.flow; ctx!.lineWidth = Math.max(1, S(1.5))
          ctx!.beginPath(); ctx!.moveTo(-S(10), -S(5.5)); ctx!.lineTo(0, 0); ctx!.lineTo(-S(10), S(5.5)); ctx!.stroke()
          ctx!.restore()
        }

        // travelling dots: count scales with weight, motion honours the OS flag
        const dots = Math.min(4, 1 + Math.round((link.weight / lay.maxWeight) * 3))
        for (let i = 0; i < dots; i++) {
          const u = still ? (i + 0.5) / dots : (t * 0.17 + i / dots) % 1
          const q = bez(p0, p1, p2, p3, u)
          const s = T(q.x, q.y)
          const fade = Math.min(1, Math.sin(Math.PI * Math.min(u, 1 - u) * 2.6) + 0.35)
          ctx!.globalAlpha = 0.85 * fade
          ctx!.fillStyle = rs.flow
          ctx!.beginPath(); ctx!.arc(s.x, s.y, S(3.6), 0, 7); ctx!.fill()
          ctx!.globalAlpha = 0.14 * fade
          ctx!.beginPath(); ctx!.arc(s.x, s.y, S(8), 0, 7); ctx!.fill()
          ctx!.globalAlpha = 1
        }

      }
    }

    // Edge labels are a SEPARATE pass, drawn after the cards. A relation label
    // that a card covers is worse than no label — the whole claim of this view
    // is that every arrow is named.
    // COLLISION. Edge labels used to be placed at the curve's midpoint and
    // drawn regardless of what was already there — so on a dense scene they
    // landed on top of the cards, and the "N inside" badge landed on top of a
    // card's own detail line. Every card, badge and placed label now registers
    // its rectangle here, and a label tries a short ladder of offsets along the
    // curve normal before giving up. Giving up means NOT drawing it: an
    // unreadable label is worse than an absent one, and the count it carries is
    // also printed on the card.
    let occupied: { x: number; y: number; w: number; h: number }[] = []
    const hitsRect = (a: { x: number; y: number; w: number; h: number }) =>
      occupied.some((b) => a.x < b.x + b.w && a.x + a.w > b.x && a.y < b.y + b.h && a.y + a.h > b.y)
    const occupy = (x: number, y: number, w: number, h: number) => occupied.push({ x, y, w, h })

    function drawCurveLabels() {
      for (const c of lay.curves) {
        const { link } = c
        const oa = off(link.from), ob = off(link.to)
        const shift = (q: { x: number; y: number }, d: { dx: number; dy: number }) =>
          d === ZERO ? q : { x: q.x + d.dx, y: q.y + d.dy }
        const mid = { dx: (oa.dx + ob.dx) / 2, dy: (oa.dy + ob.dy) / 2 }
        const p0 = shift(c.p0, oa), p3 = shift(c.p3, ob)
        const p1 = shift(c.p1, mid), p2 = shift(c.p2, mid)
        const m = bez(p0, p1, p2, p3, 0.5)
        const mt = bezT(p0, p1, p2, p3, 0.5)
        const off0 = 17
        const lbl = link.label
        const cnt = String(link.weight)
        const wl = measure(lbl, 9.5, 700) + lbl.length * 1.15
        const wc = measure(cnt, 9.5, 700, MONO)
        const pad = 5
        const total = wl + 7 + wc
        // walk out along the normal, both ways, until the box is clear
        let lx = 0, ly = 0, placed = false
        for (const d of [off0, -off0, off0 * 2, -off0 * 2, off0 * 3, -off0 * 3]) {
          lx = Math.min(VW - 70 - total / 2, Math.max(70 + total / 2, m.x - mt.y * d))
          ly = m.y + mt.x * d + 4
          if (!hitsRect({ x: lx - total / 2 - pad, y: ly - 11, w: total + pad * 2, h: 15 })) {
            placed = true
            break
          }
        }
        if (!placed) continue
        occupy(lx - total / 2 - pad, ly - 11, total + pad * 2, 15)
        const q = T(lx - total / 2 - pad, ly - 11)
        ctx!.fillStyle = rgba(pal.ground, .92)
        rr(q.x, q.y, S(total + pad * 2), S(15), S(3)); ctx!.fill()
        const rs = relationStyle(link.relation)
        text(lbl, lx - total / 2, ly, { size: 9.5, weight: 700, color: rs.label, spacing: 1.15 })
        text(cnt, lx + total / 2, ly, { size: 9.5, weight: 700, color: ACC, font: MONO, align: 'right' })
        // A count is not an explanation. "SHARES 12" between a ui and an api
        // reads as "the ui calls the api" — it is twelve THIRD PARTIES both of
        // them call, and only the names say so. The server sends them; this
        // prints them under the label where the reader is already looking.
        if (link.detail) {
          const dw = measure(link.detail, 8.5, 500)
          const dx = Math.min(VW - 70 - dw / 2, Math.max(70 + dw / 2, lx))
          if (!hitsRect({ x: dx - dw / 2 - 3, y: ly + 3, w: dw + 6, h: 12 })) {
            occupy(dx - dw / 2 - 3, ly + 3, dw + 6, 12)
            const dq = T(dx - dw / 2 - 3, ly + 3)
            ctx!.fillStyle = rgba(pal.ground, .92)
            rr(dq.x, dq.y, S(dw + 6), S(12), S(3)); ctx!.fill()
            text(link.detail, dx, ly + 12, { size: 8.5, color: MUTED, align: 'center' })
          }
        }
      }
    }

    // A card with subdirectories is a door. registerDoor gives it its hit region
    // and returns whether the pointer is currently on it, so the affordance and
    // the click target can never disagree.
    // titleLink draws a card's name and, when there is something inside,
    // makes the NAME the way in. A badge in the corner is discoverable only
    // once you know to look for it; the name is the first thing read, so it is
    // the thing that should be clickable — and it is styled like a link
    // (accent colour, underline on hover) so it advertises that itself.
    //
    // Only the TEXT is registered, not the card: the rest of the card body has
    // to stay free for dragging the shape.
    // What a click on this card OPENS. At directory grain that is the card's
    // directory; at module grain the card is a different STORE and its key is
    // the id, not the path shown underneath. Sending `dir` there navigated to
    // "apps/ui" — a store that does not exist — and the drill silently did
    // nothing, which the browser suite caught and no unit test could.
    const enterKey = (c: { id: string; dir: string }) =>
      scene!.level === 'module' ? c.id : c.dir

    function titleLink(
      label: string, x: number, baseY: number, size: number, maxW: number,
      root: string, enterable: boolean, align: CanvasTextAlign = 'left', grain = '',
    ): boolean {
      let shown = label
      ctx!.font = `700 ${S(size)}px ${SANS}`
      while (shown.length > 1 && ctx!.measureText(shown).width / SC > maxW) shown = shown.slice(0, -1)
      if (shown !== label) shown = shown.slice(0, -1) + '…'
      const w = ctx!.measureText(shown).width / SC
      const lx = align === 'center' ? x - w / 2 : x
      let on = false
      if (enterable) {
        hits.push({ x: lx, y: baseY - size, w, h: size * 1.35, root, kind: 'card', grain })
        on = hover === hits.length - 1
      }
      text(shown, x, baseY, {
        size, weight: 700, align,
        color: enterable ? (on ? pal.accent : mix(pal.text, pal.accent, .55)) : INK,
      })
      if (enterable) {
        // the underline: dotted when idle, solid on hover — a link that only
        // reveals itself on hover is a link nobody finds
        const u = T(lx, baseY + 3.5)
        ctx!.strokeStyle = on ? pal.accent : rgba(pal.accent, .45)
        ctx!.lineWidth = Math.max(1, S(on ? 1.4 : 1))
        ctx!.setLineDash(on ? [] : [S(2.5), S(2.5)])
        ctx!.beginPath(); ctx!.moveTo(u.x, u.y); ctx!.lineTo(u.x + S(w), u.y); ctx!.stroke()
        ctx!.setLineDash([])
      }
      return on
    }

    // The badge is a SECOND target, never the whole card. Registering the
    // card made every card with children un-draggable: the gesture hit a click
    // region, the camera took it, and the entire frame panned instead of the
    // shape moving. Card body = move the shape; badge = go inside.
    function registerDoor(b: Box, x: number, y: number, w: number, h: number): boolean {
      // Children says things exist inside; Inner says there is something to
      // SEE. A header holding eleven declarations that never reference each
      // other is not a door — offering one promises a screen whose only
      // content is "nothing to draw".
      if (!b.card || b.card.children <= 0 || b.card.inner <= 0) return false
      hits.push({ x, y, w, h, root: enterKey(b.card), kind: 'card', grain: b.card.enter_grain })
      return hover === hits.length - 1
    }

    // drawOutside prints the edges whose other end is off-scene, as a chip
    // rather than an arrow. Silent when there are none: a zero here is not a
    // fact worth the space.
    function outsideLabel(c: { ext_in: number; ext_out: number }): string {
      const bits: string[] = []
      if (c.ext_in > 0) bits.push(`↘${c.ext_in.toLocaleString()}`)
      if (c.ext_out > 0) bits.push(`${c.ext_out.toLocaleString()}↗`)
      return bits.length === 0 ? '' : bits.join(' ') + ' out'
    }
    const outsideWidth = (c: { ext_in: number; ext_out: number }) => {
      const l = outsideLabel(c)
      return l === '' ? 0 : measure(l, 9, 700, MONO) + 16
    }

    function drawOutside(c: { ext_in: number; ext_out: number }, x: number, y: number, maxW: number) {
      const label = outsideLabel(c)
      if (label === '') return
      const w = Math.min(maxW, measure(label, 9, 700, MONO) + 16)
      const p = T(x, y)
      ctx!.fillStyle = mix(pal.panel, pal.amber, .12)
      rr(p.x, p.y, S(w), S(15), S(7.5)); ctx!.fill()
      ctx!.strokeStyle = rgba(pal.amber, .5); ctx!.lineWidth = Math.max(1, S(1))
      rr(p.x, p.y, S(w), S(15), S(7.5)); ctx!.stroke()
      text(label, x + w / 2, y + 11, {
        size: 9, weight: 700, font: MONO, align: 'center', color: pal.amber, max: w - 8,
      })
    }

    function drawCard(b0: Box) {
      const o = off(b0.id)
      const b = o === ZERO ? b0 : { ...b0, x: b0.x + o.dx, y: b0.y + o.dy }
      const c = b.card!
      const p = T(b.x, b.y)
      const badgeW = c.children > 0 ? measure(`${c.children} inside`, 9.5, 700, MONO) + 26 : 0
      const on = registerDoor(b, b.x + b.w - 14 - badgeW, b.y + 12, badgeW, 19)
      ctx!.save()
      ctx!.shadowColor = on ? rgba(pal.focus, .5) : 'rgba(0,0,0,.45)'
      ctx!.shadowBlur = S(on ? 26 : 18); ctx!.shadowOffsetY = S(5)
      ctx!.fillStyle = CARD; rr(p.x, p.y, S(b.w), S(b.h), S(12)); ctx!.fill()
      ctx!.restore()
      ctx!.strokeStyle = on ? DOT : HAIR; ctx!.lineWidth = Math.max(1, S(on ? 1.8 : 1))
      rr(p.x, p.y, S(b.w), S(b.h), S(12)); ctx!.stroke()
      occupy(b.x, b.y, b.w, b.h)

      // A compressed card keeps the ordinal and the NAME and drops the rest:
      // the glyph tile, the path and the detail line all need room the card no
      // longer has, and a name half outside its own box is worse than no glyph.
      const tight = b.h < 84
      text(b.n, b.x + 18, b.y + (tight ? b.h / 2 + 5 : 36),
        { size: tight ? 13 : 19, weight: 300, color: pal.dim, font: MONO })
      if (tight) {
        const on2 = titleLink(c.label, b.x + 46, b.y + b.h / 2 + 5, 14, b.w - 62, enterKey(c), c.children > 0 && c.inner > 0, 'left', c.enter_grain)
        if (c.children > 0) drawEnter(b.x + b.w - 10, b.y + 6, c.children, on || on2, 'right')
        return
      }
      const ip = T(b.x + 18, b.y + 48)
      ctx!.fillStyle = mix(pal.panel, pal.text, .06); rr(ip.x, ip.y, S(26), S(26), S(7)); ctx!.fill()
      ctx!.strokeStyle = pal.line2; ctx!.lineWidth = Math.max(1, S(1))
      rr(ip.x, ip.y, S(26), S(26), S(7)); ctx!.stroke()
      text(c.glyph, b.x + 31, b.y + 66, { size: 13, color: pal.muted, align: 'center', font: MONO })

      const titleOn = titleLink(c.label, b.x + 54, b.y + 67, 16, b.w - 70, enterKey(c), c.children > 0 && c.inner > 0, 'left', c.enter_grain)
      // A short card drops its lower lines rather than drawing them over each
      // other: cardH shrinks to fit a full column, and the fixed offsets that
      // were fine at 118 units are not at 74.
      if (b.h >= 100) {
        text(c.dir, b.x + 18, b.y + 91, { size: 10.5, color: pal.dim, font: MONO, max: b.w - 36 })
      }
      // The outside chip and the detail line share the bottom row, so the
      // detail yields exactly the width the chip takes. Drawn over the path
      // subtitle, the chip hid the one thing that says WHICH file a card is.
      const outW = outsideWidth(c)
      if (b.h >= 86) {
        text(c.detail || `${c.files} files`, b.x + 18, b.y + b.h - 15,
          { size: 11.5, color: MUTED, max: b.w - 36 - (outW > 0 ? outW + 10 : 0) })
      }
      drawOutside(c, b.x + b.w - 18 - outW, b.y + b.h - 26, outW)

      // in/out counters, top-right — the numbers that decided the column
      text(`${c.out}↗`, b.x + b.w - 18, b.y + 26, { size: 10, color: pal.dim, font: MONO, align: 'right' })
      text(`↘${c.in}`, b.x + b.w - 18, b.y + 40, { size: 10, color: pal.dim, font: MONO, align: 'right' })
      // Traffic that LEAVES the picture. It is never an arrow — the other end
      // is not on screen, and an arrow to nothing is what the world view was
      // killed for — but inside one file a function's 3 local callers can be a
      // hundred repo-wide, and that is the number that says whether it matters.

      if (c.children > 0 && c.inner > 0) {
        // Top strip, beside the ordinal — the only band on a card that carries
        // no text of its own. Drawn AFTER the title so it can share its hover.
        drawEnter(b.x + b.w - 14, b.y + 12, c.children, on || titleOn, 'right')
      } else if (c.children > 0) {
        // still a true fact, just not a door
        text(`${c.children} inside · no links`, b.x + b.w - 14, b.y + 24,
          { size: 9, color: pal.dim, font: MONO, align: 'right', max: b.w - 60 })
      }
    }

    // drawEnter is the one "you can go in here" mark, used by cards and the hub
    // alike so the affordance reads the same wherever it appears.
    function drawEnter(x: number, y: number, n: number, on: boolean, align: 'right' | 'center') {
      const label = `${n} inside`
      const w = measure(label, 9.5, 700, MONO) + 26
      const bx = align === 'right' ? x - w : x - w / 2
      const p = T(bx, y)
      ctx!.fillStyle = on ? mix(pal.panel, pal.focus, .22) : mix(pal.panel, pal.text, .05)
      rr(p.x, p.y, S(w), S(19), S(9.5)); ctx!.fill()
      ctx!.strokeStyle = on ? DOT : pal.line2; ctx!.lineWidth = Math.max(1, S(1))
      rr(p.x, p.y, S(w), S(19), S(9.5)); ctx!.stroke()
      text(label, bx + 9, y + 13.5, { size: 9.5, weight: 700, font: MONO, color: on ? DOT : pal.muted })
      const cx = bx + w - 11, cy = y + 9.5
      const a = T(cx - 2.5, cy - 4), b2 = T(cx + 2, cy), c2 = T(cx - 2.5, cy + 4)
      ctx!.strokeStyle = on ? DOT : pal.dim; ctx!.lineWidth = Math.max(1, S(1.6))
      ctx!.beginPath(); ctx!.moveTo(a.x, a.y); ctx!.lineTo(b2.x, b2.y); ctx!.lineTo(c2.x, c2.y); ctx!.stroke()
    }

    function drawHub(b0: Box, t: number) {
      const o = off(b0.id)
      const b = o === ZERO ? b0 : { ...b0, x: b0.x + o.dx, y: b0.y + o.dy }
      const c = b.card!
      const p = T(b.x, b.y)
      const R = S(b.r)
      const pulse = still ? 1 : 1 + Math.sin(t * 1.4) * 0.012
      const badgeW = c.children > 0 ? measure(`${c.children} inside`, 9.5, 700, MONO) + 26 : 0
      const on = registerDoor(b, b.x - badgeW / 2, b.y + b.r - 34, badgeW, 19)
      ctx!.save()
      ctx!.shadowColor = rgba(pal.focus, .35); ctx!.shadowBlur = S(on ? 46 : 34)
      ctx!.fillStyle = CARD; ctx!.beginPath(); ctx!.arc(p.x, p.y, R * pulse, 0, 7); ctx!.fill()
      ctx!.restore()
      ctx!.strokeStyle = on ? DOT : rgba(pal.focus, .65); ctx!.lineWidth = S(4)
      ctx!.beginPath(); ctx!.arc(p.x, p.y, R * pulse, 0, 7); ctx!.stroke()
      occupy(b.x - b.r, b.y - b.r, b.r * 2, b.r * 2)

      // The hub's lines are budgeted against its RADIUS: it shrinks with the
      // band now, and a fixed six lines overflowed a small circle — the file
      // count ran outside the ring and the badge sat on top of it.
      const roomy = b.r >= 84
      const tight = b.r < 64
      text('MOST DEPENDED ON', b.x, b.y - (roomy ? 46 : 34),
        { size: 8.5, color: MUTED, align: 'center', spacing: 1.3, weight: 700 })
      const titleOn = titleLink(c.label, b.x, b.y - (roomy ? 20 : 12),
        roomy ? 19 : 15, b.r * 1.7, enterKey(c), c.children > 0 && c.inner > 0, 'center', c.enter_grain)
      if (!tight) {
        text(c.dir, b.x, b.y + (roomy ? -2 : 4),
          { size: 9.5, color: pal.dim, font: MONO, align: 'center', max: b.r * 1.75 })
      }
      if (roomy) {
        text(c.detail, b.x, b.y + 20, { size: 11, color: MUTED, align: 'center', max: b.r * 1.7 })
      }
      // One line, inside the circle. A chip and a files/decls line below it
      // ran outside the ring: a circle's usable width shrinks as you go down,
      // and a fixed-width chip at +74 in a 94-radius circle has 116 units of
      // room and needed more.
      const out = outsideLabel(c)
      text(`${c.in} in · ${c.out} out` + (out ? `  ·  ${out}` : ''),
        b.x, b.y + (roomy ? 42 : 26),
        { size: roomy ? 10.5 : 9, color: DOT, font: MONO, align: 'center', weight: 700,
          max: b.r * 1.55 })
      if (roomy) {
        text(`${c.files} files · ${c.decls} decls`, b.x, b.y + 60,
          { size: 9.5, color: pal.dim, font: MONO, align: 'center', max: b.r * 1.4 })
      }
      if (c.children > 0 && c.inner > 0) drawEnter(b.x, b.y + b.r - (roomy ? 30 : 22), c.children, on || titleOn, 'center')
    }

    // The outer world: one dashed PLATE per transport, holding a bounded
    // sample of the port NAMES. The killed wall view put 273 names around a
    // perimeter and could not be read; a plate lists a few and says how many it
    // is not showing.
    function drawWorld(b0: Box, t: number) {
      const o = off(b0.id)
      const b = o === ZERO ? b0 : { ...b0, x: b0.x + o.dx, y: b0.y + o.dy }
      const w = b.world!
      const p = T(b.x, b.y)
      hits.push({ x: b.x, y: b.y, w: b.w, h: b.h, root: w.id, kind: 'world' })
      const on = hover === hits.length - 1
      const open = b.h > CHIP_H + 1

      ctx!.save()
      ctx!.fillStyle = mix(pal.panel, pal.amber, on ? .18 : .10)
      rr(p.x, p.y, S(b.w), S(b.h), S(open ? 8 : CHIP_H / 2)); ctx!.fill()
      ctx!.setLineDash([S(5), S(6)])
      if (!still) ctx!.lineDashOffset = -t * S(22)
      ctx!.strokeStyle = rgba(pal.amber, on ? .95 : .7); ctx!.lineWidth = S(1.5)
      rr(p.x, p.y, S(b.w), S(b.h), S(open ? 8 : CHIP_H / 2)); ctx!.stroke()
      ctx!.restore()

      if (!open) {
        // COLLAPSED: the transport, the count, and a hint that there is more.
        // Everything a glance needs; the names are one click away and were
        // costing a third of the canvas.
        text(w.transport, b.x + 13, b.y + 22, { size: 11.5, weight: 700, font: MONO, max: b.w - 84 })
        text(`${w.total} ${on ? '▾' : '▸'}`, b.x + b.w - 13, b.y + 22,
          { size: 11, weight: 700, font: MONO, align: 'right', color: pal.amber })
        if (w.sensitive > 0) {
          const q = T(b.x + b.w - 46, b.y + 17)
          ctx!.fillStyle = ACC
          ctx!.beginPath(); ctx!.arc(q.x, q.y, S(3), 0, 7); ctx!.fill()
        }
        return
      }

      text('OUTER WORLD', b.x + 14, b.y + 20, { size: 8, color: rgba(pal.amber, .8), spacing: 1.3, weight: 700 })
      text(w.transport, b.x + 14, b.y + 40, { size: 14, weight: 700, font: MONO, max: b.w - 110 })
      const dir = [w.provides ? `${w.provides} provided` : '', w.consumes ? `${w.consumes} consumed` : '']
        .filter(Boolean).join(' · ')
      text(`${w.total} ports ▾`, b.x + b.w - 14, b.y + 22,
        { size: 11, color: pal.amber, align: 'right', font: MONO, weight: 700 })
      text(dir, b.x + b.w - 14, b.y + 38, { size: 9.5, color: MUTED, align: 'right', font: MONO })

      const cols = 2
      const cw = (b.w - 28 - 8) / cols
      // At least one row, always: the plate is only open because the reader
      // asked for the names, and opening it to show none answers nothing.
      const rowsFit = Math.max(1, Math.floor((b.h - 76) / 24))
      const shown = w.sample.slice(0, rowsFit * cols)
      shown.forEach((d, i) => {
        const cx2 = b.x + 14 + (i % cols) * (cw + 8)
        const cy = b.y + 54 + Math.floor(i / cols) * 24
        const q = T(cx2, cy)
        ctx!.fillStyle = d.sensitive ? mix(pal.panel, pal.amber, .16) : mix(pal.panel, pal.text, .05)
        rr(q.x, q.y, S(cw), S(20), S(10)); ctx!.fill()
        ctx!.strokeStyle = d.sensitive ? ACC : pal.line2
        ctx!.lineWidth = Math.max(1, S(d.sensitive ? 1.4 : 1))
        rr(q.x, q.y, S(cw), S(20), S(10)); ctx!.stroke()
        if (d.sensitive) {
          const dp = T(cx2 + 10, cy + 10)
          ctx!.fillStyle = ACC
          ctx!.beginPath(); ctx!.arc(dp.x, dp.y, S(3), 0, 7); ctx!.fill()
        }
        text(d.label, cx2 + (d.sensitive ? 18 : 9), cy + 14, {
          size: 9.5, weight: 600, font: MONO,
          color: d.dynamic ? pal.dim : pal.muted,
          max: cw - (d.sensitive ? 26 : 18),
        })
      })
      const bits: string[] = []
      if (shown.length < w.total) bits.push(`showing ${shown.length} of ${w.total}`)
      if (w.sensitive > 0) bits.push(`${w.sensitive} sensitive (name only)`)
      if (bits.length > 0) {
        const foot = b.y + 54 + Math.ceil(shown.length / cols) * 24 + 4
        text(bits.join(' · '), b.x + 14, Math.min(foot + 9, b.y + b.h - 6),
          { size: 9, color: MUTED, font: MONO, max: b.w - 28 })
      }
    }

    // The trail out. Every crumb is clickable including the first, so there is
    // no level you can enter and not leave — checked server-side too
    // (TestDrillCrumbsAlwaysLeadOut), because a dead end is worse than no
    // drill-down at all.
    function drawCrumbs() {
      const crumbs = scene!.crumbs
      // One crumb is the store's own name and leads nowhere — except when it
      // names another store, which is the whole point of the repo crumb.
      if (crumbs.length <= 1 && !crumbs.some((c) => c.module)) return
      let x = 62
      const y = hy(126)
      crumbs.forEach((c, i) => {
        const last = i === crumbs.length - 1
        const w = measure(c.label, 11.5, last ? 700 : 500) + 22
        if (!last) {
          const p = T(x, y)
          hits.push({ x, y, w, h: 24, root: c.root, kind: 'crumb', grain: '', module: c.module })
          const on = hover === hits.length - 1
          ctx!.fillStyle = on ? mix(pal.panel, pal.focus, .22) : mix(pal.panel, pal.text, .05)
          rr(p.x, p.y, S(w), S(24), S(12)); ctx!.fill()
          ctx!.strokeStyle = on ? DOT : pal.line2; ctx!.lineWidth = Math.max(1, S(1))
          rr(p.x, p.y, S(w), S(24), S(12)); ctx!.stroke()
          text(c.label, x + w / 2, y + 16, {
            size: 11.5, align: 'center', color: on ? DOT : pal.muted, weight: 500,
          })
        } else {
          text(c.label, x + w / 2, y + 16, { size: 11.5, align: 'center', weight: 700, color: INK })
        }
        x += w
        if (!last) {
          text('/', x + 5, y + 16, { size: 11.5, color: pal.dim })
          x += 15
        }
      })
    }

    // Reset: the scene is derived, so there is always a canonical arrangement
    // to go back to. Pan, zoom and a dragged card are all the user's, and this
    // is how they hand it back.
    // wrapText breaks a string to a width in VIRTUAL units.
    function wrapText(str: string, maxW: number, size: number): string[] {
      ctx!.font = `400 ${S(size)}px ${SANS}`
      const words = str.split(' ')
      const out: string[] = []
      let line = ''
      for (const word of words) {
        const next = line ? line + ' ' + word : word
        if (ctx!.measureText(next).width / SC > maxW && line) { out.push(line); line = word }
        else line = next
      }
      if (line) out.push(line)
      return out.slice(0, 5)
    }

    // The way OUT, on the same surface as the message. The crumbs already lead
    // back, but they sit at the top of the page while the dead end is in the
    // middle of it — and a reader who has just been told there is nothing here
    // should not have to go looking for the exit.
    function drawGoUp(y: number) {
      const crumbs = scene!.crumbs
      if (crumbs.length < 2) return
      const parent = crumbs[crumbs.length - 2]
      const label = `↑  back to ${parent.label}`
      const w = measure(label, 12, 600) + 30
      const x = VW / 2 - w / 2
      hits.push({ x, y, w, h: 30, root: parent.root, kind: 'crumb', grain: '' })
      const on = hover === hits.length - 1
      const p = T(x, y)
      ctx!.fillStyle = on ? mix(pal.panel, pal.focus, .22) : mix(pal.panel, pal.text, .06)
      rr(p.x, p.y, S(w), S(30), S(15)); ctx!.fill()
      ctx!.strokeStyle = on ? DOT : pal.line2; ctx!.lineWidth = Math.max(1, S(1))
      rr(p.x, p.y, S(w), S(30), S(15)); ctx!.stroke()
      text(label, x + w / 2, y + 20, {
        size: 12, weight: 600, align: 'center', color: on ? DOT : pal.muted,
      })
    }

    function drawReset() {
      const label = 'RESET VIEW'
      const w = measure(label, 9, 700) + 22
      const x = VW - 62 - w, y = hy(224)
      hits.push({ x, y, w, h: 22, root: '', kind: 'reset' })
      const on = hover === hits.length - 1
      const p = T(x, y)
      ctx!.fillStyle = on ? mix(pal.panel, pal.focus, .22) : mix(pal.panel, pal.text, .05)
      rr(p.x, p.y, S(w), S(22), S(11)); ctx!.fill()
      ctx!.strokeStyle = on ? DOT : pal.line2; ctx!.lineWidth = Math.max(1, S(1))
      rr(p.x, p.y, S(w), S(22), S(11)); ctx!.stroke()
      text(label, x + w / 2, y + 15, {
        size: 9, weight: 700, align: 'center', spacing: 1.1, color: on ? DOT : pal.muted,
      })
    }

    // The header CONTENT scales with the band it owns. Its y positions were
    // fixed at 118/152/176/202/224 while the band became proportional, so on a
    // short stage the band said 148 units and the text ran to 246 — straight
    // through the top of the hub.
    function hy(v: number) { return v * (lay.headerH / 254) }
    function hs(v: number) { return v * Math.max(0.72, Math.min(1, lay.headerH / 254)) }

    function drawHeader(t: number) {
      // terminal strip, top-left
      const bw = hs(720), bh = hs(46), bx = 62, by = hy(62)
      const p = T(bx, by)
      ctx!.save()
      ctx!.shadowColor = 'rgba(0,0,0,.22)'; ctx!.shadowBlur = S(16); ctx!.shadowOffsetY = S(4)
      ctx!.fillStyle = mix(pal.sky, pal.text, .04); rr(p.x, p.y, S(bw), S(bh), S(10)); ctx!.fill()
      ctx!.restore()
      let cx = bx + 20
      const segs: [string, string][] = [[scene!.title, pal.line2]]
      for (const s of scene!.stats) segs.push(['|', pal.dim], [`${s.text} ${s.label}`, s.label === 'ports' ? '#e0a955' : pal.accent])
      for (const [s, col] of segs) {
        text(s, cx, by + hs(30), { size: hs(13.5), color: col, font: MONO, weight: 600 })
        cx += measure(s, hs(13.5), 600, MONO) + 9
      }
      if (!still && Math.floor(t * 1.8) % 2) {
        const q = T(cx + 2, by + hs(15))
        ctx!.fillStyle = pal.accent; ctx!.fillRect(q.x, q.y, S(8), S(hs(16)))
      }

      drawCrumbs()
      drawReset()

      text(scene!.root ? scene!.root.split('/').pop()! : scene!.title,
        VW - 62, hy(118), { size: hs(62), weight: 800, align: 'right', max: 700 })
      text('The architecture, derived from the store — not drawn by hand.', VW - 62, hy(152),
        { size: hs(15), color: pal.muted, align: 'right' })
      // What an arrow MEANS changes with the grain, and the strip has to say
      // which: at module grain it is two manifests agreeing on a package name,
      // not an import.
      text(scene!.level === 'module'
        ? 'every card is a module · every arrow is a package one declares and another publishes'
        : `every card is a ${scene!.level} · every arrow is real imports/calls edges, summed`,
        VW - 62, hy(176), { size: hs(15), weight: 600, align: 'right' })
      text(`ctx-optimize · ${scene!.total_nodes.toLocaleString()} nodes · ${scene!.total_edges.toLocaleString()} edges`,
        VW - 62, hy(202), { size: hs(12), color: ACC, align: 'right', weight: 700, spacing: 0.6 })
    }

    function drawFooter() {
      const a = T(0, lay.footerY), b2 = T(VW, lay.footerY)
      ctx!.strokeStyle = pal.line2; ctx!.lineWidth = Math.max(1, S(1))
      ctx!.beginPath(); ctx!.moveTo(a.x, a.y); ctx!.lineTo(b2.x, b2.y); ctx!.stroke()

      let x = 62
      const y = lay.footerY + 20
      for (const s of scene!.chips) {
        const w = measure(s, 11.5, 500) + 26
        const p = T(x, y)
        ctx!.fillStyle = mix(pal.panel, pal.text, .04); rr(p.x, p.y, S(w), S(28), S(14)); ctx!.fill()
        ctx!.strokeStyle = pal.line2; ctx!.lineWidth = Math.max(1, S(1))
        rr(p.x, p.y, S(w), S(28), S(14)); ctx!.stroke()
        text(s, x + w / 2, y + 19, { size: 11.5, color: pal.muted, align: 'center' })
        x += w + 10
      }
      // the honesty strip — what is sampled, what is excluded, and what the
      // POSITIONS mean, which changes with the layout the scene earned
      const notes = lay.clustered
        ? ['nothing here depends on anything else here, so there is no left-to-right to read — '
           + 'cards that touch each other are drawn together, and modules nothing connects to sit apart',
           ...scene!.notes]
        : scene!.notes
      notes.forEach((n, i) => {
        text(n, VW - 62, lay.footerY + 24 + i * 14.5, { size: 9.8, color: i === 0 ? pal.amber : MUTED, align: 'right' })
      })
    }

    let raf = 0
    const t0 = performance.now()
    const frame = (now: number) => {
      const t = (now - t0) / 1000
      // Hit regions are geometry, and geometry is rebuilt every frame.
      hits = []
      occupied = []
      // The ground fills the STAGE, not the virtual rect. Painting only the
      // virtual rect left a border of a different colour around the scene the
      // moment the camera stopped exactly fitting it.
      ctx.fillStyle = GROUND; ctx.fillRect(0, 0, W, H)
      if (scene!.empty) {
        // A dead end still gets its header and its trail: the way out has to be
        // on the same surface as the message saying you have hit a wall.
        drawHeader(t)
        // wrapped, and drawn once: a DOM copy of this message used to sit on
        // top of the canvas copy, each truncating differently
        const lines = wrapText(scene!.empty, 900, 15)
        const top = lay.vh / 2 - (lines.length * 22) / 2 - 24
        lines.forEach((ln, i) => {
          text(ln, VW / 2, top + i * 22, { size: 15, color: pal.muted, align: 'center' })
        })
        // What this level HOLDS, as somewhere to go. Telling someone there are
        // two subdirectories and then making them back out and hunt for them is
        // the same failure as a badge nobody can find.
        const inside = scene!.inside
        if (inside.length > 0) {
          const widths = inside.map((c) => measure(c.label, 12, 600) + 28)
          const total = widths.reduce((a, b) => a + b + 10, -10)
          let x = VW / 2 - total / 2
          const y = top + lines.length * 22 + 16
          inside.forEach((c, i) => {
            const w = widths[i]
            hits.push({ x, y, w, h: 28, root: c.root, kind: 'card', grain: '' })
            const on = hover === hits.length - 1
            const q = T(x, y)
            ctx.fillStyle = on ? mix(pal.panel, pal.accent, .22) : mix(pal.panel, pal.text, .08)
            rr(q.x, q.y, S(w), S(28), S(14)); ctx.fill()
            ctx.strokeStyle = on ? pal.accent : rgba(pal.accent, .45)
            ctx.lineWidth = Math.max(1, S(1))
            rr(q.x, q.y, S(w), S(28), S(14)); ctx.stroke()
            text(c.label, x + w / 2, y + 19, {
              size: 12, weight: 600, align: 'center',
              color: on ? pal.accent : mix(pal.text, pal.accent, .55),
            })
            x += w + 10
          })
          text('go straight in', VW / 2, y + 48,
            { size: 10, color: pal.dim, align: 'center', spacing: 1 })
        }
        drawGoUp(inside.length > 0 ? top + lines.length * 22 + 82 : top + lines.length * 22 + 26)
        raf = requestAnimationFrame(frame)
        return
      }
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
      canvas.removeEventListener('pointerdown', onDown)
      canvas.removeEventListener('pointermove', onMove)
      canvas.removeEventListener('pointerup', onUp)
      canvas.removeEventListener('pointercancel', onUp)
      canvas.removeEventListener('pointerleave', onLeave)
    }
  }, [scene])

  return (
    <div className="viewer flow">
      <div className="stage" ref={wrap}>
        <canvas ref={cv} />
        {err && <div className="err flow-msg">{err}</div>}
        {!scene && !err && <div className="flow-msg">deriving the scene…</div>}
      </div>
    </div>
  )
}
