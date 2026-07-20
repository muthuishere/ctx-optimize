import { useState } from 'react'
import { mutate, stream } from '../api'
import { kindColorMap } from '../App'
import { useStores } from '../stores'
import { groupByRepo, MODULE_PREVIEW } from '../grouping'
import type { StoreInfo } from '../types'

export default function Repos() {
  const { stores, err: loadErr, refreshing, reload } = useStores()
  const [err, setErr] = useState('')
  const [busyKey, setBusyKey] = useState('')
  const [log, setLog] = useState('')
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})

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

  const showErr = err || loadErr
  if (!stores) return showErr
    ? <div className="screenwrap"><div className="err">{showErr}</div></div>
    : <div className="screenwrap"><div className="k">loading…</div></div>

  // One card per REPO: a monorepo's modules belong inside their product, not
  // beside it as peers (ADR 2026-07-19-dashboard-repo-grouping).
  const groups = groupByRepo(stores)
  const moduleCount = groups.reduce((n, g) => n + g.modules.length, 0)

  return (
    <div className="screenwrap">
      <div className="head">
        <div className="row">
          <div>
            <div className="kicker">repos</div>
            <h2 className="screen">
              {groups.length} {groups.length === 1 ? 'repo' : 'repos'}
              {moduleCount > 0 && <> · {moduleCount} {moduleCount === 1 ? 'module' : 'modules'}</>}
            </h2>
          </div>
          <span className="grow" />
          <button onClick={() => reload()} disabled={refreshing}>{refreshing ? 'reloading…' : 'Reload'}</button>
          <a href="#/onboard"><button className="primary">+ Add repo</button></a>
        </div>
      </div>

      {showErr && <div className="err">{showErr}</div>}

      {groups.length === 0 && (
        <div className="empty">
          <h3>No stores yet</h3>
          <p>Gather a repository into a knowledge graph — scan the layout, pick the modules, and the store answers what a grep-and-read chain would.</p>
          <a href="#/onboard"><button className="primary">Onboard a repo</button></a>
        </div>
      )}

      {groups.map((g) => {
        const producers = Object.entries(g.producers).sort((a, b) => b[1] - a[1])
        const pcolors = kindColorMap(producers.map(([p]) => p))
        // The store the repo-level buttons act on: its own store when it has
        // one, else the largest module (so a repo whose residual was deleted
        // is still operable).
        const primary = g.self || g.modules[0]
        const isOpen = !!expanded[g.root]
        const shown = isOpen ? g.modules : g.modules.slice(0, MODULE_PREVIEW)
        const hidden = g.modules.length - shown.length
        return (
          <div className="card" key={g.root}>
            <div className="card-title">
              <h3>{g.root}</h3>
              <span className={'badge ' + g.fresh}>{g.fresh}</span>
              {g.modules.length > 0 && (
                <span className="chip">{g.modules.length} modules</span>
              )}
              <span className="grow" />
              {primary && <>
                <a href={'#/viewer/' + encodeURIComponent(primary.key)}><button>Viewer</button></a>
                <a href={'#/query/' + encodeURIComponent(primary.key)}><button>Query</button></a>
                <button disabled={!g.sourcePath || busyKey !== ''} onClick={() => regather({ ...primary, source_path: g.sourcePath })}>
                  {busyKey === primary.key ? 're-gathering…' : 'Re-gather'}
                </button>
              </>}
            </div>

            <div className="stat-grid">
              <div className="stat"><b>{g.nodes.toLocaleString()}</b><span>nodes</span></div>
              <div className="stat"><b>{g.edges.toLocaleString()}</b><span>edges</span></div>
              {g.ageSeconds ? (
                <div className="stat"><b>{ago(g.ageSeconds)}</b><span>gathered ago</span></div>
              ) : null}
            </div>

            {g.summary && <div className="muted" style={{ marginTop: 12, fontSize: '.88rem' }}>{g.summary}</div>}

            <div className="row" style={{ marginTop: 14, gap: 6 }}>
              {g.sourcePath && <span className="chip">src <b>{g.sourcePath}</b></span>}
              {producers.map(([p, n]) => (
                <span className="chip" key={p} style={{ borderColor: pcolors.get(p), color: pcolors.get(p) }}>
                  {p} <b>{n}</b>
                </span>
              ))}
            </div>

            {g.modules.length > 0 && (
              <div className="modules" style={{ marginTop: 16 }}>
                {g.self && <ModuleRow s={g.self} label="(root files)" busyKey={busyKey} onRemove={remove} />}
                {shown.map((m) => (
                  <ModuleRow key={m.key} s={m} label={m.key.slice(g.root.length + 1)} busyKey={busyKey} onRemove={remove} />
                ))}
                {hidden > 0 && (
                  <button className="link" onClick={() => setExpanded((p) => ({ ...p, [g.root]: true }))}>
                    +{hidden} more {hidden === 1 ? 'module' : 'modules'}
                  </button>
                )}
                {isOpen && g.modules.length > MODULE_PREVIEW && (
                  <button className="link" onClick={() => setExpanded((p) => ({ ...p, [g.root]: false }))}>
                    show fewer
                  </button>
                )}
              </div>
            )}

            {/* A single-module repo keeps its own Remove; a monorepo removes
                per module (deleting a whole tree needs the CLI, on purpose). */}
            {g.modules.length === 0 && g.self && (
              <div className="row" style={{ marginTop: 14 }}>
                <span className="grow" />
                <button className="danger" onClick={() => remove(g.self!)}>Remove</button>
              </div>
            )}

            {g.all.some((m) => m.key === busyKey) && log && <pre className="stream">{log}</pre>}
          </div>
        )
      })}
    </div>
  )
}

function ModuleRow({ s, label, busyKey, onRemove }: {
  s: StoreInfo
  label: string
  busyKey: string
  onRemove: (s: StoreInfo) => void
}) {
  return (
    <div className="row modrow" style={{ gap: 8, padding: '6px 0', alignItems: 'center' }}>
      <span style={{ fontFamily: 'var(--mono, monospace)', fontSize: '.86rem' }}>{label}</span>
      <span className={'badge ' + s.fresh} style={{ transform: 'scale(.85)' }}>{s.fresh}</span>
      <span className="muted" style={{ fontSize: '.8rem' }}>{s.nodes.toLocaleString()} nodes</span>
      <span className="grow" />
      <a href={'#/viewer/' + encodeURIComponent(s.key)}><button>Viewer</button></a>
      <a href={'#/query/' + encodeURIComponent(s.key)}><button>Query</button></a>
      <button className="danger" disabled={busyKey !== ''} onClick={() => onRemove(s)}>Remove</button>
    </div>
  )
}

function ago(secs: number): string {
  if (secs < 3600) return Math.max(1, Math.round(secs / 60)) + 'm'
  if (secs < 86400) return Math.round(secs / 3600) + 'h'
  return Math.round(secs / 86400) + 'd'
}
