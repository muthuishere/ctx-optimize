import { describe, expect, it, vi } from 'vitest'
import { attach, fit, type Cam } from './camera'

// The camera moves the EYE, never the facts — so what is tested here is that a
// gesture maps to the right place, and that the two things sharing one canvas
// (the scene and the camera) never both claim the same gesture.
const canvasStub = () => {
  const listeners = new Map<string, (e: any) => void>()
  const el = {
    style: { cursor: '' },
    getBoundingClientRect: () => ({ left: 0, top: 0, width: 800, height: 500 }),
    addEventListener: (t: string, fn: any) => listeners.set(t, fn),
    removeEventListener: (t: string) => listeners.delete(t),
    setPointerCapture: () => {},
    releasePointerCapture: () => {},
  } as unknown as HTMLCanvasElement
  return { el, fire: (t: string, e: any) => listeners.get(t)?.({ button: 0, preventDefault: () => {}, ...e }) }
}
const OPTS = { vw: 1600, vh: 1000, pad: 0 }

describe('camera', () => {
  it('fits the virtual space into the stage and centres it', () => {
    const cam: Cam = { k: 1, x: 0, y: 0 }
    fit(cam, OPTS, 800, 1000)
    expect(cam.k).toBe(0.5)          // width-limited
    expect(cam.x).toBe(0)
    expect(cam.y).toBe(250)          // centred vertically
  })

  it('zooms about the cursor — the point under the pointer must not move', () => {
    const { el, fire } = canvasStub()
    const cam: Cam = { k: 1, x: 0, y: 0 }
    attach(el, cam, OPTS)
    const before = { x: (400 - cam.x) / cam.k, y: (300 - cam.y) / cam.k }
    fire('wheel', { clientX: 400, clientY: 300, deltaY: -240 })
    expect(cam.k).toBeGreaterThan(1)
    const after = { x: (400 - cam.x) / cam.k, y: (300 - cam.y) / cam.k }
    expect(after.x).toBeCloseTo(before.x, 6)
    expect(after.y).toBeCloseTo(before.y, 6)
  })

  it('pans by exactly the pointer delta', () => {
    const { el, fire } = canvasStub()
    const cam: Cam = { k: 2, x: 10, y: 20 }
    attach(el, cam, OPTS, { onPick: () => null })
    fire('pointerdown', { clientX: 100, clientY: 100, pointerId: 1 })
    fire('pointermove', { clientX: 130, clientY: 90, pointerId: 1 })
    expect(cam.x).toBe(40)
    expect(cam.y).toBe(10)
  })

  it('gives the SCENE first refusal on a gesture', () => {
    const { el, fire } = canvasStub()
    const cam: Cam = { k: 1, x: 0, y: 0 }
    const onDragTo = vi.fn()
    attach(el, cam, OPTS, { onPick: () => 'drag', onDragTo })
    fire('pointerdown', { clientX: 100, clientY: 100, pointerId: 1 })
    fire('pointermove', { clientX: 160, clientY: 100, pointerId: 1 })
    expect(onDragTo).toHaveBeenCalled()
    expect(cam.x).toBe(0)            // the camera did NOT also pan
  })

  it('treats a tiny gesture as a click and a real one as a pan', () => {
    const { el, fire } = canvasStub()
    const onClick = vi.fn()
    const cam: Cam = { k: 1, x: 0, y: 0 }
    attach(el, cam, OPTS, { onPick: () => 'click', onClick })
    // a 1px trackpad wobble is still a click
    fire('pointerdown', { clientX: 200, clientY: 150, pointerId: 1 })
    fire('pointermove', { clientX: 201, clientY: 150, pointerId: 1 })
    fire('pointerup', { clientX: 201, clientY: 150, pointerId: 1 })
    expect(onClick).toHaveBeenCalledTimes(1)
    // a real drag is not
    fire('pointerdown', { clientX: 200, clientY: 150, pointerId: 1 })
    fire('pointermove', { clientX: 260, clientY: 150, pointerId: 1 })
    fire('pointerup', { clientX: 260, clientY: 150, pointerId: 1 })
    expect(onClick).toHaveBeenCalledTimes(1)
  })

  it('reports pointer position in VIRTUAL units, not pixels', () => {
    const { el, fire } = canvasStub()
    const cam: Cam = { k: 0.5, x: 100, y: 50 }
    const seen: number[][] = []
    attach(el, cam, OPTS, { onHover: (x, y) => seen.push([x, y]) })
    fire('pointermove', { clientX: 300, clientY: 150 })
    expect(seen[0]).toEqual([(300 - 100) / 0.5, (150 - 50) / 0.5])
  })
})
