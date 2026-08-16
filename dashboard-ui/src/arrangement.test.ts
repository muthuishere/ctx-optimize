import { beforeEach, describe, expect, it, vi } from 'vitest'
import { arrangementKey, clearArrangement, loadArrangement, saveArrangement } from './arrangement'

beforeEach(() => localStorage.clear())

describe('a reader’s arrangement survives a reload', () => {
  it('round-trips offsets and the open plate', () => {
    const k = arrangementKey('flow', 'reqsume', '', '')
    saveArrangement(k, { nudged: { 'apps/ui': { dx: 40, dy: -12 } }, openWorld: 'world:config.env' })
    const got = loadArrangement(k)
    expect(got?.nudged['apps/ui']).toEqual({ dx: 40, dy: -12 })
    expect(got?.openWorld).toBe('world:config.env')
  })

  it('keys every drilled level separately', () => {
    const top = arrangementKey('flow', 'reqsume', '', '')
    const deep = arrangementKey('flow', 'reqsume', 'src/lib', '')
    const other = arrangementKey('house', 'reqsume', '', '')
    expect(new Set([top, deep, other]).size).toBe(3)
    saveArrangement(top, { nudged: { a: { dx: 1, dy: 1 } }, openWorld: '' })
    expect(loadArrangement(deep)).toBeNull()
    expect(loadArrangement(other)).toBeNull()
  })

  it('forgets an arrangement that has nothing in it', () => {
    const k = arrangementKey('flow', 'x', '', '')
    saveArrangement(k, { nudged: { a: { dx: 1, dy: 1 } }, openWorld: '' })
    saveArrangement(k, { nudged: {}, openWorld: '' })
    expect(loadArrangement(k)).toBeNull()
  })

  it('is cleared by RESET, so a bad drag is never permanent', () => {
    const k = arrangementKey('flow', 'x', '', '')
    saveArrangement(k, { nudged: { a: { dx: 900, dy: 900 } }, openWorld: '' })
    clearArrangement(k)
    expect(loadArrangement(k)).toBeNull()
  })

  it('discards a malformed record rather than trusting it', () => {
    const k = arrangementKey('flow', 'x', '', '')
    localStorage.setItem(k, 'not json')
    expect(loadArrangement(k)).toBeNull()
    localStorage.setItem(k, JSON.stringify({ nudged: null }))
    expect(loadArrangement(k)).toBeNull()
    // a NaN offset would put a card nowhere at all, with nothing on screen to
    // explain why — dropped, while the well-formed entries beside it are kept
    localStorage.setItem(k, JSON.stringify({ nudged: { a: { dx: 'x', dy: 2 }, b: { dx: 3, dy: 4 } } }))
    const got = loadArrangement(k)
    expect(got?.nudged.a).toBeUndefined()
    expect(got?.nudged.b).toEqual({ dx: 3, dy: 4 })
  })

  it('never breaks the viewer when storage is unavailable', () => {
    const boom = () => { throw new Error('QuotaExceededError') }
    const spySet = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(boom)
    const spyGet = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(boom)
    const k = arrangementKey('flow', 'x', '', '')
    expect(() => saveArrangement(k, { nudged: { a: { dx: 1, dy: 1 } }, openWorld: '' })).not.toThrow()
    expect(loadArrangement(k)).toBeNull()
    spySet.mockRestore()
    spyGet.mockRestore()
  })

  it('bounds what it keeps, so one session cannot fill the quota', () => {
    for (let i = 0; i < 60; i++) {
      saveArrangement(arrangementKey('flow', 'm' + i, '', ''),
        { nudged: { a: { dx: i, dy: i } }, openWorld: '' })
    }
    let kept = 0
    for (let i = 0; i < localStorage.length; i++) {
      if (localStorage.key(i)?.startsWith('ctxopt.arrange.v1:')) kept++
    }
    expect(kept).toBeLessThanOrEqual(40)
    // the most recent are the ones that survive
    expect(loadArrangement(arrangementKey('flow', 'm59', '', ''))).not.toBeNull()
  })
})
