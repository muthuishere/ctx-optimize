// A camera for the canvas viewers: pan, zoom, and fit.
//
// Both derived views draw into a fixed virtual space (1600x1000) and used to be
// letterboxed into the stage at whatever scale fit — so a wide window left bars
// down the sides, a short one left them top and bottom, and nothing could be
// moved or magnified. The scene is legible or it is not, and you had no way to
// argue with it.
//
// This is deliberately NOT a second layout: the camera moves the eye, never the
// facts. Where a card sits is still derived; the camera only decides which part
// of that derivation is on screen and how big.
export interface Cam {
  /** scale: virtual units -> css px */
  k: number
  /** translation in css px */
  x: number
  y: number
}

export interface CameraOpts {
  /** the virtual space the scene is composed in */
  vw: number
  vh: number
  /** css px of padding to leave around the scene when fitting */
  pad?: number
  minK?: number
  maxK?: number
}

export function fit(cam: Cam, opts: CameraOpts, W: number, H: number) {
  const pad = opts.pad ?? 0
  const k = Math.min((W - pad * 2) / opts.vw, (H - pad * 2) / opts.vh)
  cam.k = k
  cam.x = (W - opts.vw * k) / 2
  cam.y = (H - opts.vh * k) / 2
}

/**
 * attach wires wheel-zoom and drag-pan onto a canvas.
 *
 * `onPick` is consulted FIRST on pointerdown, in virtual coordinates. Return an
 * object to take the gesture (a card drag, a click target); return null and the
 * camera pans instead. That ordering is what lets one canvas host both without
 * a modifier key: the scene gets first refusal, the camera gets the rest.
 */
export function attach(
  canvas: HTMLCanvasElement,
  cam: Cam,
  opts: CameraOpts,
  handlers: {
    onPick?: (vx: number, vy: number) => 'drag' | 'click' | null
    onDragTo?: (vx: number, vy: number) => void
    onClick?: (vx: number, vy: number) => void
    onHover?: (vx: number, vy: number) => void
    onChange?: () => void
  } = {},
) {
  const minK = opts.minK ?? 0.15
  const maxK = opts.maxK ?? 6

  const toVirtual = (cx: number, cy: number) => {
    const r = canvas.getBoundingClientRect()
    return { x: (cx - r.left - cam.x) / cam.k, y: (cy - r.top - cam.y) / cam.k }
  }

  let mode: 'none' | 'pan' | 'drag' = 'none'
  let last = { x: 0, y: 0 }
  let moved = 0

  const onDown = (e: PointerEvent) => {
    if (e.button !== 0) return
    const v = toVirtual(e.clientX, e.clientY)
    moved = 0
    last = { x: e.clientX, y: e.clientY }
    const pick = handlers.onPick?.(v.x, v.y) ?? null
    if (pick === 'drag') {
      mode = 'drag'
    } else if (pick === 'click') {
      // a click target: decided on pointerUP, so a drag that starts on a card
      // still pans rather than firing navigation under the user's finger
      mode = 'pan'
    } else {
      mode = 'pan'
    }
    canvas.setPointerCapture(e.pointerId)
    canvas.style.cursor = mode === 'drag' ? 'grabbing' : 'grabbing'
  }

  const onMove = (e: PointerEvent) => {
    const v = toVirtual(e.clientX, e.clientY)
    if (mode === 'none') {
      handlers.onHover?.(v.x, v.y)
      return
    }
    const dx = e.clientX - last.x
    const dy = e.clientY - last.y
    moved += Math.abs(dx) + Math.abs(dy)
    last = { x: e.clientX, y: e.clientY }
    if (mode === 'drag') handlers.onDragTo?.(v.x, v.y)
    else { cam.x += dx; cam.y += dy }
    handlers.onChange?.()
  }

  const onUp = (e: PointerEvent) => {
    const wasMode = mode
    mode = 'none'
    try { canvas.releasePointerCapture(e.pointerId) } catch { /* already gone */ }
    canvas.style.cursor = 'default'
    // A gesture under ~4px is a click, not a pan. Without this every click
    // registers as a 1px drag on a trackpad and navigation never fires.
    if (wasMode === 'pan' && moved < 4) {
      const v = toVirtual(e.clientX, e.clientY)
      handlers.onClick?.(v.x, v.y)
    }
    handlers.onChange?.()
  }

  const onWheel = (e: WheelEvent) => {
    e.preventDefault()
    const r = canvas.getBoundingClientRect()
    const px = e.clientX - r.left
    const py = e.clientY - r.top
    // zoom about the cursor: the point under the pointer must not move
    const before = { x: (px - cam.x) / cam.k, y: (py - cam.y) / cam.k }
    const factor = Math.exp(-e.deltaY * 0.0016)
    cam.k = Math.max(minK, Math.min(maxK, cam.k * factor))
    cam.x = px - before.x * cam.k
    cam.y = py - before.y * cam.k
    handlers.onChange?.()
  }

  canvas.addEventListener('pointerdown', onDown)
  canvas.addEventListener('pointermove', onMove)
  canvas.addEventListener('pointerup', onUp)
  canvas.addEventListener('pointercancel', onUp)
  canvas.addEventListener('wheel', onWheel, { passive: false })

  return () => {
    canvas.removeEventListener('pointerdown', onDown)
    canvas.removeEventListener('pointermove', onMove)
    canvas.removeEventListener('pointerup', onUp)
    canvas.removeEventListener('pointercancel', onUp)
    canvas.removeEventListener('wheel', onWheel)
  }
}
