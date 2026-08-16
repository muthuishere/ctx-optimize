import { describe, expect, it } from 'vitest'
import { legendFor, relationStyle, transportStyle } from './flowLayout'
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

describe('the legend is built from what is on screen', () => {
  it('names every mark the scene actually draws, once each', () => {
    const rows = legendFor(sceneWith([
      { from: 'a', to: 'b', relation: 'shares', label: 'BOTH CALL', weight: 3, transport: 'network.http' },
      { from: 'c', to: 'd', relation: 'shares', label: 'BOTH CALL', weight: 1, transport: 'network.http' },
      { from: 'a', to: 'b', relation: 'shares', label: 'BOTH READ', weight: 2, transport: 'config.env' },
      { from: 'e', to: 'f', relation: 'depends', label: 'DEPENDS', weight: 1 },
    ]))
    expect(rows.map((r) => r.label)).toEqual([
      'config.env', 'depends', 'network.http',
    ])
    // The curve is labelled with the transport; the SENTENCE that makes sense
    // of it lives here, because "BOTH CALL 12" tried to be that sentence in
    // two words and read as "the ui calls the api".
    const http = rows.find((r) => r.label === 'network.http')!
    expect(http.meaning).toContain('NOT a call between them')
  })

  it('says nothing about marks the scene does not contain', () => {
    const rows = legendFor(sceneWith([
      { from: 'e', to: 'f', relation: 'depends', label: 'DEPENDS', weight: 1 },
    ]))
    expect(rows).toHaveLength(1)
    expect(rows[0].label).toBe('depends')
    expect(rows[0].meaning).toBe('declares a package the other publishes')
    expect(rows[0].arrow).toBe(true)
    expect(rows[0].dashed).toBe(false)
  })

  it('is empty when nothing is drawn, rather than a key to an empty picture', () => {
    expect(legendFor(sceneWith([]))).toEqual([])
  })

  it('shows a symmetric link without an arrowhead, matching the canvas', () => {
    const rows = legendFor(sceneWith([
      { from: 'a', to: 'b', relation: 'shares', label: 'BOTH CALL', weight: 3, transport: 'network.http' },
    ]))
    expect(rows[0].arrow).toBe(false)
    expect(rows[0].dashed).toBe(true)
  })

  it('draws an outer-world link dashed, because it leaves the system', () => {
    const rows = legendFor(sceneWith([
      { from: 'a', to: 'world:network.http', relation: 'consumes', label: 'HTTP', weight: 9, transport: 'network.http' },
    ]))
    expect(rows[0].dashed).toBe(true)
    expect(rows[0].meaning).toContain('reaches out to a service')
  })

  it('separates a link to the outer world from one to another module', () => {
    // Same transport, same colour, two different claims — one row each, or the
    // key says the two marks mean the same thing when they do not.
    const rows = legendFor(sceneWith([
      { from: 'a', to: 'b', relation: 'shares', label: 'HTTP', weight: 3, transport: 'network.http' },
      { from: 'a', to: 'world:network.http', relation: 'consumes', label: 'HTTP', weight: 9, transport: 'network.http' },
    ]))
    expect(rows).toHaveLength(2)
    expect(new Set(rows.map((r) => r.meaning)).size).toBe(2)
  })

  it('names the transport, never a verb the reader has to decode', () => {
    const rows = legendFor(sceneWith([
      { from: 'a', to: 'b', relation: 'shares', label: 'HTTP', weight: 3, transport: 'network.http' },
    ]))
    expect(rows[0].label).toBe('network.http')
  })
})
