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
  const [ran, setRan] = useState(false)

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
      setRan(true)
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
    <div className="screenwrap">
      <div className="head">
        <div className="kicker">query</div>
        <h2 className="screen">Same engine the agent uses</h2>
        <p className="screen-sub">Ranked, cited hits with signatures and neighbors — lexical IDF + prefix + trigram tiers, budgeted.</p>
      </div>

      <div className="row" style={{ marginBottom: 20 }}>
        <select value={mod} onChange={(e) => { setMod(e.target.value); setRes(null); setRan(false) }}>
          {mods.map((m) => (
            <option key={m.key} value={m.key}>{m.key} ({m.nodes})</option>
          ))}
        </select>
        <input
          type="text" className="grow" placeholder="ask the store…  (Enter to run)"
          value={q} onChange={(e) => setQ(e.target.value)}
          onKeyDown={(e) => e.key === 'Enter' && run()}
          autoFocus
        />
        <button className="primary" onClick={run} disabled={!q.trim()}>Query</button>
      </div>

      {err && <div className="err">{err}</div>}

      {!ran && !err && (
        <div className="empty">
          <h3>Ask the store anything</h3>
          <p>Where is X, how does Y work, who calls Z. Pick a module and type a question — the store answers in one hop.</p>
        </div>
      )}

      {ran && res && res.hits.length === 0 && (
        <div className="empty">
          <h3>No matches</h3>
          <p>Nothing in <b>{mod}</b> matched "{q}". Try broader terms or a different module.</p>
        </div>
      )}

      {(res?.hits || []).map((h) => (
        <div className="hit" key={h.node.id} style={{ ['--kc' as any]: colors.get(h.node.kind) }}>
          <div>
            <span className="label">{h.node.label}</span>
            <span className="kind">{h.node.kind}</span>
          </div>
          <div className="meta">
            {h.node.source}{h.node.location ? ' ' + h.node.location : ''} · <span className="score">score {h.score}</span>
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
          <div style={{ marginTop: 10 }}>
            <a href={'#/viewer/' + encodeURIComponent(mod) + '?center=' + encodeURIComponent(h.node.id)}>
              open in viewer →
            </a>
          </div>
        </div>
      ))}
    </div>
  )
}
