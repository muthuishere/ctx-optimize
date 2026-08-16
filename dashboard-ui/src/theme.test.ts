import { describe, expect, it } from 'vitest'
import { mix, rgba } from './theme'

// The canvas palette must trace back to the app's CSS variables — a second,
// hand-maintained palette is how the derived views ended up as a cream document
// pasted into a dark dashboard. These pin the mixing helpers that let one theme
// variable produce the several shades a canvas needs.
describe('theme mixing', () => {
  it('mixes toward the second colour', () => {
    expect(mix('#000000', '#ffffff', 0)).toBe('#000000')
    expect(mix('#000000', '#ffffff', 1)).toBe('#ffffff')
    expect(mix('#000000', '#ffffff', 0.5)).toBe('#808080')
  })

  it('expands 3-digit hex', () => {
    expect(mix('#fff', '#000', 0)).toBe('#ffffff')
  })

  it('returns the input unchanged rather than throwing on a non-hex colour', () => {
    // CSS variables can hold rgb()/hsl()/named colours; a viewer must never die
    // because a theme was written a different way.
    expect(mix('rgb(1,2,3)', '#fff', 0.5)).toBe('rgb(1,2,3)')
    expect(rgba('var(--nope)', 0.5)).toBe('var(--nope)')
  })

  it('builds a canvas rgba from a hex colour', () => {
    expect(rgba('#4ade80', 0.5)).toBe('rgba(74,222,128,0.5)')
  })
})
