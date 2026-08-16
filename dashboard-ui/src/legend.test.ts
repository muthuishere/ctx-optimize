import { describe, expect, it } from 'vitest'
import { legendFor, relationStyle, shapeNote, transportStyle } from './flowLayout'
import type { Scene, SceneLink } from './types'

function sceneWith(links: SceneLink[]): Scene {
  return {
    module: 'm', title: 'm', total_nodes: 0, total_edges: 0,
    subsystems_total: 0, subsystems_shown: 0, lifted_total: 0, lifted_shown: 0,
    cards: [], links, world: [], stats: [], chips: [], notes: [],
    root: '', level: 'module', crumbs: [], inside: [], questions: [],
  }
}

describe('a line says what KIND of thing it is', () => {
  it('colours by transport, not by relation alone', () => {
    // `calls` over HTTP and `calls` over a spawned process are both calls and
    // are not the same thing to anyone reading the picture.
    const http = relationStyle('calls', 'network.http')
    const proc = relationStyle('calls', 'process.exec')
    expect(http.line).not.toBe(proc.line)
  })

  it('keeps the relation ink when there is no transport to speak of', () => {
    // manifest- and code-derived links have no boundary behind them
    expect(relationStyle('depends')).toEqual(relationStyle('depends', ''))
  })

  it('files an unseen transport by its family rather than dropping it to grey', () => {
    // a build that has never heard of queue.kafka still knows it is a queue
    expect(transportStyle('queue.kafka')).toEqual(transportStyle('queue.rabbitmq'))
    expect(transportStyle('network.grpc')).toEqual(transportStyle('network.http'))
  })

  it('leaves a family it cannot name grey, rather than inventing a colour', () => {
    expect(transportStyle('telepathy.psychic').line).toBe(relationStyle('nonsense').line)
  })
})

describe('the legend is one row per MODE', () => {
  it('names each mode once, however many ways it is drawn', () => {
    // The same transport appearing as a shared third party, as a directed
    // call, and as a reach out of the repo is ONE colour and ONE row. The
    // previous key was a row per (mode × claim) and put `network.http` on
    // screen three times — ten rows to explain four colours.
    const rows = legendFor(sceneWith([
      { from: 'a', to: 'b', relation: 'shares', label: 'HTTP', weight: 3, transport: 'network.http' },
      { from: 'c', to: 'd', relation: 'calls', label: 'HTTP', weight: 1, transport: 'network.http' },
      { from: 'a', to: 'world:network.http', relation: 'consumes', label: 'HTTP', weight: 9, transport: 'network.http' },
      { from: 'a', to: 'world:network.http', relation: 'provides', label: 'HTTP', weight: 4, transport: 'network.http' },
      { from: 'a', to: 'b', relation: 'shares', label: 'ENV', weight: 2, transport: 'config.env' },
      { from: 'e', to: 'f', relation: 'depends', label: 'DEPENDS', weight: 1 },
    ]))
    expect(rows.map((r) => r.label)).toEqual(['config.env', 'depends', 'network.http'])
  })

  it('says what a mode IS, for a reader who has not met the vocabulary', () => {
    const rows = legendFor(sceneWith([
      { from: 'a', to: 'b', relation: 'shares', label: 'HTTP', weight: 3, transport: 'network.http' },
      { from: 'a', to: 'b', relation: 'shares', label: 'ENV', weight: 3, transport: 'config.env' },
    ]))
    expect(rows.find((r) => r.label === 'network.http')!.meaning).toBe('calls over the network')
    expect(rows.find((r) => r.label === 'config.env')!.meaning).toBe('settings read from the environment')
  })

  it('says nothing about modes the scene does not contain', () => {
    const rows = legendFor(sceneWith([
      { from: 'e', to: 'f', relation: 'depends', label: 'DEPENDS', weight: 1 },
    ]))
    expect(rows).toHaveLength(1)
    expect(rows[0].label).toBe('depends')
  })

  it('is empty when nothing is drawn, rather than a key to an empty picture', () => {
    expect(legendFor(sceneWith([]))).toEqual([])
  })

  it('names the mode, never a verb the reader has to decode', () => {
    const rows = legendFor(sceneWith([
      { from: 'a', to: 'b', relation: 'shares', label: 'HTTP', weight: 3, transport: 'network.http' },
    ]))
    expect(rows[0].label).toBe('network.http')
  })
})

// The claim a line makes is in its SHAPE, and that is now explained once for
// the whole key instead of once per mode.
describe('the shape note explains direction, once', () => {
  it('distinguishes the three shapes a scene can draw', () => {
    const note = shapeNote(sceneWith([
      { from: 'a', to: 'b', relation: 'depends', label: 'DEPENDS', weight: 1 },
      { from: 'a', to: 'b', relation: 'shares', label: 'HTTP', weight: 3, transport: 'network.http' },
      { from: 'a', to: 'world:network.http', relation: 'consumes', label: 'HTTP', weight: 9, transport: 'network.http' },
    ]))
    expect(note).toContain('solid')
    expect(note).toContain('leaves the repo')
    expect(note).toContain('NOT each other')
  })

  it('says nothing when every line on screen has the same shape', () => {
    // A key to a distinction the reader cannot see is worse than no key.
    expect(shapeNote(sceneWith([
      { from: 'a', to: 'b', relation: 'depends', label: 'DEPENDS', weight: 1 },
    ]))).toBe('')
    expect(shapeNote(sceneWith([]))).toBe('')
  })
})
