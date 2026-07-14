import { useCallback, useEffect, useState } from 'react'
import { api, mutate, stream } from '../api'
import { kindColorMap } from '../App'
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

  if (err) return <div className="screenwrap"><div className="err">{err}</div></div>
  if (!stores) return <div className="screenwrap"><div className="k">loading…</div></div>

  return (
    <div className="screenwrap">
      <div className="head">
        <div className="row">
          <div>
            <div className="kicker">repos</div>
            <h2 className="screen">Every store under the root</h2>
          </div>
          <span className="grow" />
          <a href="#/onboard"><button className="primary">+ Add repo</button></a>
        </div>
      </div>

      {stores.length === 0 && (
        <div className="empty">
          <h3>No stores yet</h3>
          <p>Gather a repository into a knowledge graph — scan the layout, pick the modules, and the store answers what a grep-and-read chain would.</p>
          <a href="#/onboard"><button className="primary">Onboard a repo</button></a>
        </div>
      )}

      {stores.map((s) => {
        const producers = Object.entries(s.producers || {}).sort((a, b) => b[1] - a[1])
        const pcolors = kindColorMap(producers.map(([p]) => p))
        return (
          <div className="card" key={s.key}>
            <div className="card-title">
              <h3>{s.key}</h3>
              <span className={'badge ' + s.fresh}>{s.fresh}</span>
              <span className="grow" />
              <a href={'#/viewer/' + encodeURIComponent(s.key)}><button>Viewer</button></a>
              <a href={'#/query/' + encodeURIComponent(s.key)}><button>Query</button></a>
              <button disabled={!s.source_path || busyKey !== ''} onClick={() => regather(s)}>
                {busyKey === s.key ? 're-gathering…' : 'Re-gather'}
              </button>
              <button className="danger" onClick={() => remove(s)}>Remove</button>
            </div>

            <div className="stat-grid">
              <div className="stat"><b>{s.nodes.toLocaleString()}</b><span>nodes</span></div>
              <div className="stat"><b>{s.edges.toLocaleString()}</b><span>edges</span></div>
              {s.age_seconds ? (
                <div className="stat"><b>{ago(s.age_seconds)}</b><span>gathered ago</span></div>
              ) : null}
            </div>

            {s.summary && <div className="muted" style={{ marginTop: 12, fontSize: '.88rem' }}>{s.summary}</div>}

            <div className="row" style={{ marginTop: 14, gap: 6 }}>
              {s.source_path && <span className="chip">src <b>{s.source_path}</b></span>}
              {producers.map(([p, n]) => (
                <span className="chip" key={p} style={{ borderColor: pcolors.get(p), color: pcolors.get(p) }}>
                  {p} <b>{n}</b>
                </span>
              ))}
            </div>

            {busyKey === s.key && log && <pre className="stream">{log}</pre>}
          </div>
        )
      })}
    </div>
  )
}

function ago(secs: number): string {
  if (secs < 3600) return Math.max(1, Math.round(secs / 60)) + 'm'
  if (secs < 86400) return Math.round(secs / 3600) + 'h'
  return Math.round(secs / 86400) + 'd'
}
