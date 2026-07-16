import { useCallback, useEffect, useState } from 'react'
import { api, mutate, stream } from '../api'
import type { Setup, StoreInfo } from '../types'

// Settings — every config key (both levels, which level set it, edit inline),
// packs per axis (core recognizers + discovered packs with their file paths),
// an inline "add pack" form for routes/manifests, adapters, and remote (with
// push/pull triggers). The FILE stays the source of truth: every card names
// the file it renders.
const VALUES: Record<string, string[]> = {
  instructions: ['ALL', 'CLAUDE', 'AGENTS', 'NONE'],
  skills: ['ALL', 'CLAUDE', 'AGENTS'],
  hooks: ['ALL', 'CLAUDE', 'AGENTS', 'NONE'],
}

// Axes that accept a new pack from the dashboard (routes add / manifests add).
const ADDABLE = new Set(['routes', 'manifests'])

export default function Settings() {
  const [setup, setSetup] = useState<Setup | null>(null)
  const [stores, setStores] = useState<StoreInfo[]>([])
  const [repoPath, setRepoPath] = useState('')
  const [err, setErr] = useState('')
  const [syncLog, setSyncLog] = useState('')
  const [busy, setBusy] = useState('')
  const [packSrc, setPackSrc] = useState<Record<string, string>>({})
  const [packScope, setPackScope] = useState<Record<string, string>>({})
  const [packBusy, setPackBusy] = useState('')

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

  const addPack = async (axis: string) => {
    const source = (packSrc[axis] || '').trim()
    if (!source) return
    const scope = packScope[axis] || (repoPath ? 'project' : 'global')
    setErr('')
    setPackBusy(axis)
    try {
      await mutate('POST', '/api/pack', { axis, scope, path: repoPath, source })
      setPackSrc((p) => ({ ...p, [axis]: '' }))
      reload(repoPath)
    } catch (e: any) {
      setErr(String(e.message || e))
    }
    setPackBusy('')
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

  if (!setup) return err ? <div className="screenwrap"><div className="err">{err}</div></div> : <div className="screenwrap"><div className="k">loading…</div></div>

  return (
    <div className="screenwrap narrow">
      <div className="head">
        <div className="kicker">settings</div>
        <h2 className="screen">Config, packs & remote</h2>
        <p className="screen-sub">The file stays the source of truth — every card names the file it renders. Edits are audited.</p>
      </div>

      <div className="card">
        <div className="row">
          <span className="k">Project scope</span>
          <select className="grow" value={repoPath} onChange={(e) => { setRepoPath(e.target.value); reload(e.target.value) }}>
            <option value="">(global only)</option>
            {stores.map((s) => (
              <option key={s.key} value={s.source_path}>{s.key} — {s.source_path}</option>
            ))}
          </select>
        </div>
        {!repoPath && stores.length > 0 && (
          <div className="k" style={{ marginTop: 8, fontSize: '.82rem' }}>
            Pick a repo to see its project config, packs, adapters and remote. Global config + machine packs show below either way.
          </div>
        )}
      </div>

      {err && <div className="err">{err}</div>}

      <div className="card">
        <h3>Config keys</h3>
        <div className="tablewrap"><table className="list">
          <thead>
            <tr><th>key</th><th>effective</th><th>set by</th><th>set global</th>{repoPath && <th>set project</th>}</tr>
          </thead>
          <tbody>
            {setup.effective.map((kv) => (
              <tr key={kv.key}>
                <td className="mono">{kv.key}</td>
                <td><b style={{ color: 'var(--text)' }}>{kv.value}</b></td>
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
        </table></div>
        <div className="k mono" style={{ marginTop: 12, fontSize: '.78rem' }}>
          global: {setup.global.file}
          {setup.project ? <> · project: {setup.project.file} (committable)</> : null}
        </div>
      </div>

      {setup.axes.map((a) => (
        <div className="card" key={a.axis}>
          <div className="card-title">
            <h3>{a.axis}</h3>
            <span className="chip">{a.kind}</span>
          </div>
          <div className="muted" style={{ fontSize: '.85rem', marginBottom: 10 }}>{a.note}</div>
          {a.error && <div className="err">{a.error}</div>}

          {a.core && a.core.length > 0 && (
            <div className="row" style={{ gap: 6, marginBottom: 10 }}>
              <span className="k" style={{ fontSize: '.76rem' }}>core</span>
              {a.core.map((c) => <span className="chip" key={c}>{c}</span>)}
            </div>
          )}

          {(a.packs || []).map((p) => (
            <div className="row" key={p.name} style={{ padding: '4px 0' }}>
              <span className="chip"><b>{p.name}</b>{p.exts ? ' ' + p.exts.join(' ') : ''}{typeof p.rules === 'number' ? ` · ${p.rules} rule${p.rules === 1 ? '' : 's'}` : ''}</span>
              <span className="k mono" style={{ fontSize: '.76rem' }}>{p.wasm || p.file}</span>
            </div>
          ))}
          {a.kind === 'packs' && (a.packs || []).length === 0 && !a.error && (
            <div className="k">no packs discovered — {ADDABLE.has(a.axis) ? 'core recognizers still apply; add one below' : 'core recognizers still apply'}</div>
          )}

          {ADDABLE.has(a.axis) && (
            <div className="row" style={{ gap: 6, marginTop: 12, alignItems: 'stretch' }}>
              <input type="text" className="grow" placeholder="pack name, or github/json URL"
                value={packSrc[a.axis] || ''}
                onChange={(e) => setPackSrc((p) => ({ ...p, [a.axis]: e.target.value }))}
                onKeyDown={(e) => e.key === 'Enter' && addPack(a.axis)} />
              <select value={packScope[a.axis] || (repoPath ? 'project' : 'global')}
                onChange={(e) => setPackScope((p) => ({ ...p, [a.axis]: e.target.value }))}>
                <option value="project" disabled={!repoPath}>project</option>
                <option value="global">global</option>
              </select>
              <button className="primary" disabled={packBusy === a.axis || !(packSrc[a.axis] || '').trim()}
                onClick={() => addPack(a.axis)}>
                {packBusy === a.axis ? 'adding…' : 'Add pack'}
              </button>
            </div>
          )}

          {(a.adapters || []).map((ad, i) => (
            <div className="row" key={i} style={{ padding: '4px 0' }}>
              <span className="chip"><b>{ad.name}</b></span>
              <span className="mono" style={{ fontSize: '.82rem' }}>{ad.run}</span>
              <span className="k mono" style={{ fontSize: '.76rem' }}>{ad.file}</span>
            </div>
          ))}
          {a.axis === 'adapters' && !repoPath && (
            <div className="k">pick a project scope above to list its adapters</div>
          )}
          {a.axis === 'adapters' && repoPath && (a.adapters || []).length === 0 && (
            <div className="k">no adapters — drop a .js/.py/.sh into the repo's .ctxoptimize/adapters/</div>
          )}
        </div>
      ))}

      <div className="card">
        <h3>Remote</h3>
        {setup.remote ? (
          <div className="row">
            {setup.remote.push && <span className="chip">push: <b className="mono">{setup.remote.push}</b></span>}
            {setup.remote.pull && <span className="chip">pull: <b className="mono">{setup.remote.pull}</b></span>}
            <span className="k">from {setup.remote.from}</span>
            <span className="grow" />
            <button disabled={!!busy || !setup.remote.push} onClick={() => sync('push')}>{busy === 'push' ? 'pushing…' : 'Push'}</button>
            <button disabled={!!busy || !setup.remote.pull} onClick={() => sync('pull')}>{busy === 'pull' ? 'pulling…' : 'Pull'}</button>
          </div>
        ) : (
          <div className="k">
            {repoPath
              ? 'No remote — declare {"remote": {"push": "<cmd>", "pull": "<cmd>"}} in .ctxoptimize/config.json (samples: push.js.sample / pull.js.sample, written by init).'
              : 'Pick a project scope above to see its remote.'}
          </div>
        )}
        {syncLog && <pre className="stream">{syncLog}</pre>}
      </div>
    </div>
  )
}
