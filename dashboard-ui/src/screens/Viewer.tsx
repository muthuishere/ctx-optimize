import { useCallback, useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { kindColorMap, SPECIAL_KINDS } from '../App'
import ForceGraph from '../ForceGraph'
import type { Edge, GraphResponse, Module, Node } from '../types'

// Viewer — force-directed NEIGHBORHOOD graph. The server caps every payload
// (top-N by degree); clicking a node expands its 1-hop neighborhood via
// /api/graph?center=<id>&depth=1 and merges it in. The whole graph never
// ships.
const LIMIT = 400

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
  const [producer, setProducer] = useState('')
  const [hidden, setHidden] = useState<Set<string>>(new Set())
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

  const producers = useMemo(() => {
    const s = new Set<string>()
    for (const n of nodes.values()) s.add(n.metadata?.producer || '(unknown)')
    return Array.from(s).sort()
  }, [nodes])

  const shown = useMemo(() => {
    let list = Array.from(nodes.values())
    if (producer) list = list.filter((n) => (n.metadata?.producer || '(unknown)') === producer)
    list = list.filter((n) => !hidden.has(n.kind))
    const keep = new Set(list.map((n) => n.id))
    const es = Array.from(edges.values()).filter((e) => keep.has(e.source) && keep.has(e.target))
    return { nodes: list, edges: es }
  }, [nodes, edges, producer, hidden])

  // Legend covers every kind currently in the graph (hidden or not) so a
  // filtered-out kind stays clickable to bring back. Special kinds lead.
  const legendKinds = useMemo(() => {
    const s = new Set<string>()
    for (const n of nodes.values()) {
      if (producer && (n.metadata?.producer || '(unknown)') !== producer) continue
      s.add(n.kind)
    }
    const special = SPECIAL_KINDS.filter((k) => s.has(k))
    const rest = Array.from(s).filter((k) => !SPECIAL_KINDS.includes(k)).sort()
    return [...special, ...rest]
  }, [nodes, producer])

  const colors = useMemo(() => kindColorMap(legendKinds), [legendKinds])
  const toggleKind = useCallback((k: string) => {
    setHidden((h) => {
      const n = new Set(h)
      n.has(k) ? n.delete(k) : n.add(k)
      return n
    })
  }, [])
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
          <select value={mod} onChange={(e) => { setMod(e.target.value); setProducer(''); setHidden(new Set()); load(e.target.value, '') }}>
            {mods.map((m) => (
              <option key={m.key} value={m.key}>{m.key} ({m.nodes})</option>
            ))}
          </select>
          <select value={producer} onChange={(e) => setProducer(e.target.value)}>
            <option value="">all producers</option>
            {producers.map((p) => (
              <option key={p} value={p}>{p}</option>
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
              <div className="drow"><span className="k">producer </span>{sel.metadata?.producer || ''}</div>
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
        {legendKinds.length > 0 && (
          <div className="legend">
            <div className="lg-title">kinds — click to filter</div>
            {legendKinds.map((k) => (
              <div className={'lg-row' + (hidden.has(k) ? ' off' : '')} key={k}
                onClick={() => toggleKind(k)} title={hidden.has(k) ? 'show ' + k : 'hide ' + k}>
                <i style={{ background: colors.get(k), color: colors.get(k) }} />
                {k}
                {SPECIAL_KINDS.includes(k) && <span className="lg-star" title="first-class kind">★</span>}
              </div>
            ))}
          </div>
        )}
        <div className="note">drag: pan · wheel: zoom · click: expand neighborhood · legend: filter kinds</div>
      </div>
    </div>
  )
}
