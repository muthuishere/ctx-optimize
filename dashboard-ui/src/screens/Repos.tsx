import { useCallback, useEffect, useState } from 'react'
import { api, mutate, stream } from '../api'
import type { StoreInfo } from '../types'

export default function Repos() {
  const [stores, setStores] = useState<StoreInfo[] | null>(null)
  const [err, setErr] = useState('')
  const [busyKey, setBusyKey] = useState('')
  const [log, setLog] = useState('')

  const reload = useCallback(() => {
    api<StoreInfo[]>('/api/stores').then(setStores).catch((e) => setErr(String(e.message || e)))
  }, [])
  useEffect(reload, [reload])

  const regather = async (s: StoreInfo) => {
    if (!s.source_path) return
    setBusyKey(s.key)
    setLog('')
    try {
      await stream('/api/repo/add', { path: s.source_path }, (t) => setLog((p) => p + t))
    } catch (e: any) {
      setLog((p) => p + '\nERROR: ' + (e.message || e))
    }
    setBusyKey('')
    reload()
  }

  const remove = async (s: StoreInfo) => {
    if (!window.confirm(`Delete store "${s.key}"?\n\nThis removes the gathered graph (${s.nodes} nodes) from disk. The source repo is untouched; re-onboarding rebuilds it. This action is audited.`)) return
    try {
      await mutate('DELETE', '/api/store', { key: s.key, confirm: true })
      reload()
    } catch (e: any) {
      setErr(String(e.message || e))
    }
  }

  if (err) return <div className="err">{err}</div>
  if (!stores) return <div className="k">loading…</div>

  return (
    <div>
      <div className="row" style={{ marginBottom: 14 }}>
        <h2 className="screen" style={{ margin: 0 }}>Repos — every store under the root</h2>
        <span className="grow" />
        <a href="#/onboard"><button className="primary">+ add repo</button></a>
      </div>
      {stores.length === 0 && (
        <div className="card">
          <span className="k">no stores yet — </span>
          <a href="#/onboard">onboard a repo</a>
          <span className="k"> or run `ctx-optimize add` in one.</span>
        </div>
      )}
      {stores.map((s) => (
        <div className="card" key={s.key}>
          <div className="row">
            <h3 style={{ margin: 0 }}>{s.key}</h3>
            <span className={'badge ' + s.fresh}>{s.fresh}</span>
            <span className="k" style={{ fontSize: 12 }}>
              {s.nodes} nodes · {s.edges} edges
              {s.age_seconds ? ` · gathered ${ago(s.age_seconds)} ago` : ''}
            </span>
            <span className="grow" />
            <a href={'#/viewer/' + encodeURIComponent(s.key)}><button>viewer</button></a>
            <a href={'#/query/' + encodeURIComponent(s.key)}><button>query</button></a>
            <button disabled={!s.source_path || busyKey !== ''} onClick={() => regather(s)}>
              {busyKey === s.key ? 're-gathering…' : 're-gather'}
            </button>
            <button className="danger" onClick={() => remove(s)}>remove</button>
          </div>
          {s.summary && <div className="k" style={{ marginTop: 6, fontSize: 12 }}>{s.summary}</div>}
          <div className="row" style={{ marginTop: 8, gap: 5 }}>
            {s.source_path && <span className="chip">src <b>{s.source_path}</b></span>}
            {Object.entries(s.producers || {}).sort((a, b) => b[1] - a[1]).map(([p, n]) => (
              <span className="chip" key={p}>{p} <b>{n}</b></span>
            ))}
          </div>
          {busyKey === s.key && log && <pre className="stream">{log}</pre>}
        </div>
      ))}
    </div>
  )
}

function ago(secs: number): string {
  if (secs < 3600) return Math.max(1, Math.round(secs / 60)) + 'm'
  if (secs < 86400) return Math.round(secs / 3600) + 'h'
  return Math.round(secs / 86400) + 'd'
}
