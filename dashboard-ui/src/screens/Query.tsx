import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { kindColorMap } from '../App'
import type { Module, QueryResult } from '../types'

// Query — the original UI's query affordance as its own screen: search box
// against /api/query, complete hit cards (label, kind, source:location,
// score, neighbors).
export default function Query({ initialModule: rawModule }: { initialModule: string }) {
  const initialModule = decodeURIComponent(rawModule)
  const [mods, setMods] = useState<Module[]>([])
  const [mod, setMod] = useState(initialModule)
  const [q, setQ] = useState('')
  const [res, setRes] = useState<QueryResult | null>(null)
  const [err, setErr] = useState('')

  useEffect(() => {
    api<Module[]>('/api/modules').then((m) => {
      setMods(m)
      if (!initialModule && m.length > 0) setMod(m[0].key)
    }).catch((e) => setErr(String(e.message || e)))
  }, [initialModule])

  const run = async () => {
    if (!q.trim() || !mod) return
    setErr('')
    try {
      setRes(await api<QueryResult>(
        `/api/query?module=${encodeURIComponent(mod)}&q=${encodeURIComponent(q)}`))
    } catch (e: any) {
      setErr(String(e.message || e))
      setRes(null)
    }
  }

  const colors = useMemo(
    () => kindColorMap((res?.hits || []).map((h) => h.node.kind)),
    [res],
  )

  return (
    <div style={{ maxWidth: 860 }}>
      <h2 className="screen">Query — same engine the agent uses</h2>
      <div className="row" style={{ marginBottom: 14 }}>
        <select value={mod} onChange={(e) => { setMod(e.target.value); setRes(null) }}>
          {mods.map((m) => (
            <option key={m.key} value={m.key}>{m.key} ({m.nodes})</option>
          ))}
        </select>
        <input
          type="text" className="grow" placeholder="ask the store… (Enter)"
          value={q} onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && run()}
          autoFocus
        />
        <button className="primary" onClick={run} disabled={!q.trim()}>query</button>
      </div>
      {err && <div className="err">{err}</div>}
      {res && res.hits.length === 0 && <div className="k">no matches</div>}
      {(res?.hits || []).map((h) => (
        <div className="hit" key={h.node.id} style={{ ['--kc' as any]: colors.get(h.node.kind) }}>
          <div>
            <span className="label">{h.node.label}</span>
            <span className="kind">{h.node.kind}</span>
          </div>
          <div className="meta">
            {h.node.source}{h.node.location ? ' ' + h.node.location : ''} · score {h.score}
            {h.node.metadata?.producer ? ` · ${h.node.metadata.producer}` : ''}
          </div>
          {(h.neighbors || []).length > 0 && (
            <div className="nbs">
              {(h.neighbors || []).slice(0, 12).map((n, i) => (
                <span className="chip" key={i}>{n.dir === 'in' ? '←' : '→'} {n.relation} <b>{n.id}</b></span>
              ))}
              {(h.neighbors || []).length > 12 && (
                <span className="chip">… {(h.neighbors || []).length - 12} more</span>
              )}
            </div>
          )}
          <div style={{ marginTop: 5 }}>
            <a href={'#/viewer/' + encodeURIComponent(mod) + '?center=' + encodeURIComponent(h.node.id)}>
              open in viewer →
            </a>
          </div>
        </div>
      ))}
    </div>
  )
}
