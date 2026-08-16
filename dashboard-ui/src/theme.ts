// The canvas palette, READ FROM THE APP'S OWN CSS VARIABLES.
//
// The derived views were authored as a printed architecture document — cream
// ground, ink text, hairline rules — and they looked good, but they sat inside
// a dark dashboard looking like a foreign window someone had pasted in. Worse,
// they were a second, hand-maintained palette: nothing stopped the app's theme
// moving and the canvas staying where it was.
//
// So the canvas now asks the DOM what the theme is. Every colour here traces
// back to a `--var` in styles.css or is mixed from one, which means a theme
// change is one edit in CSS and both viewers follow. The fallbacks are the
// dark values, used only if a variable is missing entirely.
export interface Palette {
  ground: string   // the surface the scene is drawn on
  sky: string      // outside the scene
  panel: string    // a card face
  panelAlt: string // a highlighted card face
  line: string     // hairlines, card borders
  line2: string    // stronger rules
  text: string     // primary text
  muted: string    // secondary text
  dim: string      // tertiary text / ordinals
  accent: string   // the "this is the one" colour
  amber: string    // the outer world
  focus: string    // interactive highlight
}

const FALLBACK: Palette = {
  ground: '#0f1319', sky: '#0a0c10', panel: '#141a22', panelAlt: '#182231',
  line: '#1f2833', line2: '#2b3644',
  text: '#e8edf4', muted: '#94a3b8', dim: '#64748b',
  accent: '#4ade80', amber: '#fbbf24', focus: '#7c8cff',
}

/**
 * readPalette pulls the live theme off the document. Called once per canvas
 * mount — cheap, and it means a viewer opened after a theme change is correct
 * without any wiring between them.
 */
export function readPalette(el: Element | null): Palette {
  if (typeof window === 'undefined' || !window.getComputedStyle) return FALLBACK
  const cs = window.getComputedStyle(el || document.documentElement)
  const v = (name: string, fb: string) => {
    const got = cs.getPropertyValue(name).trim()
    return got || fb
  }
  return {
    ground: v('--bg2', FALLBACK.ground),
    sky: v('--bg', FALLBACK.sky),
    panel: v('--panel', FALLBACK.panel),
    // A highlighted face has to differ from `panel` on the SAME ground, so it
    // is mixed rather than picked: a second hand-chosen hex is exactly the kind
    // of thing that drifts out of step with the theme.
    panelAlt: mix(v('--panel', FALLBACK.panel), v('--amber', FALLBACK.amber), 0.12),
    line: v('--border', FALLBACK.line),
    line2: v('--border2', FALLBACK.line2),
    text: v('--text', FALLBACK.text),
    muted: v('--muted', FALLBACK.muted),
    dim: v('--dim', FALLBACK.dim),
    accent: v('--accent', FALLBACK.accent),
    amber: v('--amber', FALLBACK.amber),
    focus: FALLBACK.focus,
  }
}

/** mix blends two hex colours. Non-hex input returns `a` unchanged. */
export function mix(a: string, b: string, t: number): string {
  const pa = hex(a), pb = hex(b)
  if (!pa || !pb) return a
  const c = pa.map((x, i) => Math.round(x + (pb[i] - x) * t))
  return '#' + c.map((x) => Math.max(0, Math.min(255, x)).toString(16).padStart(2, '0')).join('')
}

/** rgba builds a canvas fill from a hex colour and an alpha. */
export function rgba(c: string, alpha: number): string {
  const p = hex(c)
  if (!p) return c
  return `rgba(${p[0]},${p[1]},${p[2]},${alpha})`
}

function hex(c: string): [number, number, number] | null {
  const m = /^#?([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(c.trim())
  if (!m) return null
  let h = m[1]
  if (h.length === 3) h = h[0] + h[0] + h[1] + h[1] + h[2] + h[2]
  return [parseInt(h.slice(0, 2), 16), parseInt(h.slice(2, 4), 16), parseInt(h.slice(4, 6), 16)]
}
