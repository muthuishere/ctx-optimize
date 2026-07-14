import { useCallback, useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { kindColorMap, KNOWN_PRODUCERS, producerColorMap, SPECIAL_KINDS } from '../App'
import ForceGraph from '../ForceGraph'
import type { Edge, GraphResponse, Module, Node } from '../types'

// Viewer — force-directed NEIGHBORHOOD graph. The server caps every payload
// (top-N by degree; special kinds + a per-producer sample forced in) and
// clicking a node expands its 1-hop neighborhood via
// /api/graph?center=<id>&depth=1 and merges it in. The whole graph never
// ships. Two independent filter axes ride over the loaded graph: KIND (the
// node's shape of thing) and PRODUCER (who emitted it — code, docs, an
// adapter). A node shows only if BOTH its kind and its producer are enabled.
const LIMIT = 400

const producerOf = (n: Node) => n.metadata?.producer || '(unknown)'

export default function Viewer({ initialModule: rawArg }: { initialModule: string }) {
  const qi = rawArg.indexOf('?')
  const initialModule = decodeURIComponent(qi < 0 ? rawArg : rawArg.slice(0, qi))
  const initialCenter = qi < 0 ? '' : new URLSearchParams(rawArg.slice(qi + 1)).get('center') || ''

  const [mods, setMods] = useState<Module[]>([])
  const [mod, setMod] = useState(initialModule)
  const [nodes, setNodes] = useState<Map<string, Node>>(new Map())
  const [edges, setEdges] = useState<Map<string, Edge>>(new Map())
  const [totals, setTotals] = useState({ nodes: 0, edges: 0, truncated: false })
  const [selected, setSelected] = useState<string | null>(null)
  const [hiddenKinds, setHiddenKinds] = useState<Set<string>>(new Set())
  const [hiddenProducers, setHiddenProducers] = useState<Set<string>>(new Set())
  const [err, setErr] = useState('')

  const merge = useCallback((g: GraphResponse, reset: boolean) => {
    setNodes((prev) => {
      const m = reset ? new Map<string, Node>() : new Map(prev)
      for (const n of g.nodes) m.set(n.id, n)
      return m
    })
    setEdges((prev) => {
      const m = reset ? new Map<string, Edge>() : new Map(prev)
      for (const e of g.edges) m.set(e.source + '\x00' + e.target + '\x00' + e.relation, e)
      return m
    })
    setTotals({ nodes: g.total_nodes, edges: g.total_edges, truncated: g.truncated })
  }, [])

  const load = useCallback(async (key: string, center: string) => {
    setErr('')
    try {
      const base = `/api/graph?module=${encodeURIComponent(key)}`
      const g = await api<GraphResponse>(
        center ? `${base}&center=${encodeURIComponent(center)}&depth=1&limit=${LIMIT}` : `${base}&limit=${LIMIT}`)
      merge(g, true)
      setSelected(center || null)
    } catch (e: any) {
      setErr(String(e.message || e))
    }
  }, [merge])

  useEffect(() => {
    api<Module[]>('/api/modules').then((m) => {
      setMods(m)
      const key = initialModule || (m.length > 0 ? m[0].key : '')
      if (key) {
        setMod(key)
        load(key, initialCenter)
      }
    }).catch((e) => setErr(String(e.message || e)))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const expand = useCallback(async (id: string) => {
    setSelected(id)
    try {
      const g = await api<GraphResponse>(
        `/api/graph?module=${encodeURIComponent(mod)}&center=${encodeURIComponent(id)}&depth=1&limit=${LIMIT}`)
      merge(g, false)
    } catch {
      /* node may be filtered out server-side; selection alone is fine */
    }
  }, [mod, merge])

  // Intersection filter: a node survives only if its kind AND its producer are
  // both enabled. Edges follow the surviving node set.
  const shown = useMemo(() => {
    const list = Array.from(nodes.values()).filter(
      (n) => !hiddenKinds.has(n.kind) && !hiddenProducers.has(producerOf(n)))
    const keep = new Set(list.map((n) => n.id))
    const es = Array.from(edges.values()).filter((e) => keep.has(e.source) && keep.has(e.target))
    return { nodes: list, edges: es }
  }, [nodes, edges, hiddenKinds, hiddenProducers])

  // Legends cover every kind / producer currently loaded (hidden or not) so a
  // filtered-out group stays clickable to bring back. Counts are over the whole
  // loaded graph, so a group's tally doesn't jump as the other axis toggles.
  const legendKinds = useMemo(() => {
    const counts = new Map<string, number>()
    for (const n of nodes.values()) counts.set(n.kind, (counts.get(n.kind) || 0) + 1)
    const keys = Array.from(counts.keys())
    const special = SPECIAL_KINDS.filter((k) => counts.has(k))
    const rest = keys.filter((k) => !SPECIAL_KINDS.includes(k)).sort()
    return { order: [...special, ...rest], counts }
  }, [nodes])

  const legendProducers = useMemo(() => {
    const counts = new Map<string, number>()
    for (const n of nodes.values()) {
      const p = producerOf(n)
      counts.set(p, (counts.get(p) || 0) + 1)
    }
    const keys = Array.from(counts.keys())
    const known = KNOWN_PRODUCERS.filter((p) => counts.has(p))
    const rest = keys.filter((p) => !KNOWN_PRODUCERS.includes(p)).sort()
    return { order: [...known, ...rest], counts }
  }, [nodes])

  const colors = useMemo(() => kindColorMap(legendKinds.order), [legendKinds])
  const pcolors = useMemo(() => producerColorMap(legendProducers.order), [legendProducers])

  const toggleKind = useCallback((k: string) => {
    setHiddenKinds((h) => {
      const n = new Set(h)
      n.has(k) ? n.delete(k) : n.add(k)
      return n
    })
  }, [])
  const toggleProducer = useCallback((p: string) => {
    setHiddenProducers((h) => {
      const n = new Set(h)
      n.has(p) ? n.delete(p) : n.add(p)
      return n
    })
  }, [])
  const resetFilters = useCallback(() => {
    setHiddenKinds(new Set())
    setHiddenProducers(new Set())
  }, [])
  const filtered = hiddenKinds.size > 0 || hiddenProducers.size > 0

  const sel = selected ? nodes.get(selected) : undefined
  const selEdges = useMemo(() => {
    if (!selected) return []
    const out: { dir: string; rel: string; id: string }[] = []
    for (const e of edges.values()) {
      if (e.source === selected) out.push({ dir: '→', rel: e.relation, id: e.target })
      if (e.target === selected) out.push({ dir: '←', rel: e.relation, id: e.source })
    }
    return out
  }, [edges, selected])

  return (
    <div className="viewer">
      <div className="side">
        <div className="controls">
          <div className="kicker">viewer</div>
          <select value={mod} onChange={(e) => { setMod(e.target.value); resetFilters(); load(e.target.value, '') }}>
            {mods.map((m) => (
              <option key={m.key} value={m.key}>{m.key} ({m.nodes})</option>
            ))}
          </select>
          <div className="row" style={{ gap: 6 }}>
            <span className="chip">nodes <b>{shown.nodes.length}</b> / {totals.nodes}</span>
            <span className="chip">edges <b>{shown.edges.length}</b></span>
          </div>
          {totals.truncated && (
            <div className="k" style={{ fontSize: '.78rem' }}>
              server-budgeted — click a node to expand its neighborhood
            </div>
          )}
          {err && <div className="err">{err}</div>}
        </div>
        <div className="detail">
          {!sel && <span className="k">Click a node — its 1-hop neighborhood loads and merges in.</span>}
          {sel && (
            <div>
              <h3>{sel.label}</h3>
              <div className="drow">
                <span className="chip" style={{ borderColor: colors.get(sel.kind), color: colors.get(sel.kind) }}>{sel.kind}</span>
                {sel.file_type && <span className="k"> {sel.file_type}</span>}
              </div>
              <div className="drow"><span className="k">source </span><span className="mono">{sel.source} {sel.location || ''}</span></div>
              <div className="drow">
                <span className="k">producer </span>
                <span className="chip" style={{ borderColor: pcolors.get(producerOf(sel)), color: pcolors.get(producerOf(sel)) }}>{producerOf(sel)}</span>
              </div>
              {selEdges.length > 0 && <hr className="divider" style={{ margin: '12px 0' }} />}
              {selEdges.slice(0, 30).map((x, i) => (
                <div className="drow" key={i}>
                  {x.dir} <span className="k">{x.rel} </span>
                  <span className="nb-link mono" onClick={() => expand(x.id)}>{x.id}</span>
                </div>
              ))}
              {selEdges.length > 30 && <div className="drow k">… {selEdges.length - 30} more</div>}
            </div>
          )}
        </div>
      </div>
      <div className="stage">
        <ForceGraph
          nodes={shown.nodes}
          edges={shown.edges}
          colors={colors}
          selectedId={selected}
          onSelect={(id) => (id ? expand(id) : setSelected(null))}
        />
        {(legendKinds.order.length > 0 || legendProducers.order.length > 0) && (
          <div className="legend">
            <div className="lg-head">
              <span className="lg-title">filters</span>
              {filtered && <span className="lg-reset" onClick={resetFilters} title="show everything">reset</span>}
            </div>
            {legendKinds.order.length > 0 && (
              <div className="lg-group">
                <div className="lg-sub">kinds</div>
                {legendKinds.order.map((k) => (
                  <div className={'lg-row' + (hiddenKinds.has(k) ? ' off' : '')} key={k}
                    onClick={() => toggleKind(k)} title={hiddenKinds.has(k) ? 'show ' + k : 'hide ' + k}>
                    <i style={{ background: colors.get(k), color: colors.get(k) }} />
                    <span className="lg-name">{k}</span>
                    {SPECIAL_KINDS.includes(k) && <span className="lg-star" title="first-class kind">★</span>}
                    <span className="lg-count">{legendKinds.counts.get(k)}</span>
                  </div>
                ))}
              </div>
            )}
            {legendProducers.order.length > 0 && (
              <div className="lg-group">
                <div className="lg-sub">producers</div>
                {legendProducers.order.map((p) => (
                  <div className={'lg-row' + (hiddenProducers.has(p) ? ' off' : '')} key={p}
                    onClick={() => toggleProducer(p)} title={hiddenProducers.has(p) ? 'show ' + p : 'hide ' + p}>
                    <i style={{ background: pcolors.get(p), color: pcolors.get(p) }} />
                    <span className="lg-name">{p}</span>
                    <span className="lg-count">{legendProducers.counts.get(p)}</span>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}
        <div className="note">drag: pan · wheel: zoom · click: expand neighborhood · legend: filter kinds & producers</div>
      </div>
    </div>
  )
}
