import { useCallback, useEffect, useState } from 'react'
import { api, mutate, stream } from '../api'
import type { Setup, StoreInfo } from '../types'

// Settings — every config key (both levels, which level set it, edit
// inline), packs per axis with their file paths, adapters, remote (with
// push/pull triggers). The FILE stays the source of truth: every card names
// the file it renders.
const VALUES: Record<string, string[]> = {
  instructions: ['ALL', 'CLAUDE', 'AGENTS', 'NONE'],
  skills: ['ALL', 'CLAUDE', 'AGENTS'],
  hooks: ['ALL', 'CLAUDE', 'AGENTS', 'NONE'],
}

export default function Settings() {
  const [setup, setSetup] = useState<Setup | null>(null)
  const [stores, setStores] = useState<StoreInfo[]>([])
  const [repoPath, setRepoPath] = useState('')
  const [err, setErr] = useState('')
  const [syncLog, setSyncLog] = useState('')
  const [busy, setBusy] = useState('')

  const reload = useCallback((path: string) => {
    const q = path ? '?path=' + encodeURIComponent(path) : ''
    api<Setup>('/api/setup' + q).then(setSetup).catch((e) => setErr(String(e.message || e)))
  }, [])

  useEffect(() => {
    api<StoreInfo[]>('/api/stores').then((s) => {
      setStores(s.filter((x) => x.source_path))
    }).catch(() => {})
    reload('')
  }, [reload])

  const setKey = async (level: string, key: string, value: string) => {
    setErr('')
    try {
      await mutate('PUT', '/api/config', { level, path: repoPath, key, value })
      reload(repoPath)
    } catch (e: any) {
      setErr(String(e.message || e))
    }
  }

  const sync = async (verb: string) => {
    if (!repoPath) return
    setBusy(verb)
    setSyncLog('')
    try {
      await stream('/api/remote/' + verb, { path: repoPath }, (t) => setSyncLog((p) => p + t))
    } catch (e: any) {
      setSyncLog((p) => p + '\nERROR: ' + (e.message || e))
    }
    setBusy('')
  }

  if (!setup) return err ? <div className="err">{err}</div> : <div className="k">loading…</div>

  return (
    <div style={{ maxWidth: 860 }}>
      <h2 className="screen">Settings</h2>

      <div className="card">
        <div className="row">
          <span className="k">project scope</span>
          <select value={repoPath} onChange={(e) => { setRepoPath(e.target.value); reload(e.target.value) }}>
            <option value="">(global only)</option>
            {stores.map((s) => (
              <option key={s.key} value={s.source_path}>{s.key} — {s.source_path}</option>
            ))}
          </select>
        </div>
      </div>

      {err && <div className="err">{err}</div>}

      <div className="card">
        <h3>config keys</h3>
        <table className="list">
          <thead>
            <tr><th>key</th><th>effective</th><th>set by</th><th>set global</th>{repoPath && <th>set project</th>}</tr>
          </thead>
          <tbody>
            {setup.effective.map((kv) => (
              <tr key={kv.key}>
                <td>{kv.key}</td>
                <td><b>{kv.value}</b></td>
                <td className="k">{kv.source}</td>
                <td>
                  <select value={kv.source === 'global' ? kv.value : ''}
                    onChange={(e) => e.target.value && setKey('global', kv.key, e.target.value)}>
                    <option value="">…</option>
                    {VALUES[kv.key].map((v) => <option key={v} value={v}>{v}</option>)}
                  </select>
                </td>
                {repoPath && (
                  <td>
                    <select value={kv.source === 'project' ? kv.value : ''}
                      onChange={(e) => e.target.value && setKey('project', kv.key, e.target.value)}>
                      <option value="">…</option>
                      {VALUES[kv.key].map((v) => <option key={v} value={v}>{v}</option>)}
                    </select>
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
        <div className="k" style={{ marginTop: 8, fontSize: 11.5 }}>
          global: {setup.global.file}
          {setup.project ? <> · project: {setup.project.file} (committable)</> : null}
        </div>
      </div>

      {setup.axes.map((a) => (
        <div className="card" key={a.axis}>
          <h3>{a.axis} <span className="chip">{a.kind}</span></h3>
          <div className="k" style={{ fontSize: 12, marginBottom: 6 }}>{a.note}</div>
          {a.error && <div className="err">{a.error}</div>}
          {(a.packs || []).map((p) => (
            <div className="row" key={p.name} style={{ padding: '3px 0' }}>
              <span className="chip"><b>{p.name}</b> {p.exts.join(' ')}</span>
              <span className="k" style={{ fontSize: 11.5 }}>{p.wasm}</span>
            </div>
          ))}
          {a.kind === 'packs' && (a.packs || []).length === 0 && !a.error && (
            <div className="k">no packs discovered</div>
          )}
          {(a.adapters || []).map((ad, i) => (
            <div className="row" key={i} style={{ padding: '3px 0' }}>
              <span className="chip"><b>{ad.name}</b></span>
              <span style={{ fontSize: 12 }}>{ad.run}</span>
              <span className="k" style={{ fontSize: 11.5 }}>{ad.file}</span>
            </div>
          ))}
          {a.axis === 'adapters' && !repoPath && (
            <div className="k">pick a project scope above to list its adapters</div>
          )}
        </div>
      ))}

      <div className="card">
        <h3>remote</h3>
        {setup.remote ? (
          <div className="row">
            <span className="chip"><b>{setup.remote.url}</b></span>
            <span className="k">from {setup.remote.from}</span>
            <span className="grow" />
            <button disabled={!!busy} onClick={() => sync('push')}>{busy === 'push' ? 'pushing…' : 'push'}</button>
            <button disabled={!!busy} onClick={() => sync('pull')}>{busy === 'pull' ? 'pulling…' : 'pull'}</button>
          </div>
        ) : (
          <div className="k">
            {repoPath
              ? 'no remote configured — run `ctx-optimize remote init <url>` in the repo'
              : 'pick a project scope above to see its remote'}
          </div>
        )}
        {syncLog && <pre className="stream">{syncLog}</pre>}
      </div>
    </div>
  )
}
