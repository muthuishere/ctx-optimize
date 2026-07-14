import { describe, expect, it } from 'vitest'
import { safeDecode, sanitizeEdge, sanitizeNode } from './sanitize'

// These encode the exact shapes that used to white-screen the viewer: a node
// with no id, non-string label/kind, a stray '%' in an id. Each must now
// degrade to a clean node / dropped node / raw string — never throw.

describe('sanitizeNode', () => {
  it('keeps a well-formed node unchanged', () => {
    const n = { id: 'a', label: 'A', kind: 'function', source: 'x.go', location: 'L1-L2', metadata: { producer: 'code' } }
    expect(sanitizeNode(n)).toEqual(n)
  })

  it('drops a node with no id (the crash case)', () => {
    expect(sanitizeNode({ label: 'orphan' })).toBeNull()
    expect(sanitizeNode({ id: null })).toBeNull()
    expect(sanitizeNode(null)).toBeNull()
    expect(sanitizeNode(undefined)).toBeNull()
  })

  it('coerces a non-string label to a string instead of leaving it to crash the canvas', () => {
    expect(sanitizeNode({ id: 'a', label: 123 })!.label).toBe('123')
    expect(sanitizeNode({ id: 'a', label: { weird: true } })!.label).toBe('[object Object]')
  })

  it('falls back a missing/null label to the id', () => {
    expect(sanitizeNode({ id: 'a' })!.label).toBe('a')
    expect(sanitizeNode({ id: 'a', label: null })!.label).toBe('a')
  })

  it('coerces a non-string kind so Set/Map lookups stay safe', () => {
    expect(sanitizeNode({ id: 'a', kind: 42 })!.kind).toBe('42')
    expect(sanitizeNode({ id: 'a' })!.kind).toBe('')
  })

  it('normalizes bad optional fields to undefined', () => {
    const n = sanitizeNode({ id: 'a', source: 5, location: {}, file_type: [], metadata: 'nope' })!
    expect(n.source).toBeUndefined()
    expect(n.location).toBeUndefined()
    expect(n.file_type).toBeUndefined()
    expect(n.metadata).toBeUndefined()
  })

  it('stringifies a numeric id (ids are always strings downstream)', () => {
    expect(sanitizeNode({ id: 7 })!.id).toBe('7')
  })

  it('never throws on arbitrary junk', () => {
    for (const junk of [0, '', false, [], {}, { id: '' }, { id: 'x', metadata: null }]) {
      expect(() => sanitizeNode(junk)).not.toThrow()
    }
  })
})

describe('sanitizeEdge', () => {
  it('keeps a well-formed edge', () => {
    expect(sanitizeEdge({ source: 'a', target: 'b', relation: 'calls' }))
      .toEqual({ source: 'a', target: 'b', relation: 'calls' })
  })

  it('drops an edge missing an endpoint', () => {
    expect(sanitizeEdge({ source: 'a', relation: 'calls' })).toBeNull()
    expect(sanitizeEdge({ target: 'b' })).toBeNull()
    expect(sanitizeEdge(null)).toBeNull()
  })

  it('coerces endpoints and defaults a missing relation', () => {
    expect(sanitizeEdge({ source: 1, target: 2 })).toEqual({ source: '1', target: '2', relation: '' })
  })
})

describe('safeDecode', () => {
  it('decodes valid percent-encoding', () => {
    expect(safeDecode('a%2Fb')).toBe('a/b')
  })

  it('returns the raw string on a malformed escape instead of throwing (the render crash)', () => {
    expect(() => safeDecode('100%')).not.toThrow()
    expect(safeDecode('100%')).toBe('100%')
    expect(safeDecode('GET /users/%')).toBe('GET /users/%')
  })
})
