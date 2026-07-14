import { useState } from 'react'
import { mutate, stream } from '../api'
import type { ScanResult } from '../types'

// Onboard: path → scan preview (editable module checkboxes) → confirm →
// live progress stream → repo lands in the store. Server-side this is the
// SAME code path as `ctx-optimize scan` / `init --scan --yes` / `add .`.
export default function Onboard() {
  const [path, setPath] = useState('')
  const [name, setName] = useState('')
  const [scan, setScan] = useState<ScanResult | null>(null)
  const [checked, setChecked] = useState<Record<string, boolean>>({})
  const [log, setLog] = useState('')
  const [phase, setPhase] = useState<'input' | 'preview' | 'running' | 'done'>('input')
  const [err, setErr] = useState('')

  const doScan = async () => {
    setErr('')
    try {
      const res = await mutate<ScanResult>('POST', '/api/onboard', { path })
      setScan(res)
      const c: Record<string, boolean> = {}
      for (const m of res.modules || []) c[m.path] = true
      setChecked(c)
      const base = path.replace(/[\\/]+$/, '').split(/[\\/]/).pop() || ''
      setName(base)
      setPhase('preview')
    } catch (e: any) {
      setErr(String(e.message || e))
    }
  }

  const confirm = async () => {
    setErr('')
    setLog('')
    setPhase('running')
    const modules = Object.entries(checked).filter(([, v]) => v).map(([k]) => k)
    try {
      await stream('/api/onboard/confirm', { path, name, modules }, (t) => setLog((p) => p + t))
      setPhase('done')
    } catch (e: any) {
      setErr(String(e.message || e))
      setPhase('preview')
    }
  }

  return (
    <div style={{ maxWidth: 760 }}>
      <h2 className="screen">Onboard a repo</h2>
      <div className="card">
        <div className="row">
          <input
            type="text" className="grow" placeholder="/absolute/path/to/repo"
            value={path} onChange={(e) => setPath(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && path && doScan()}
            disabled={phase === 'running'}
          />
          <button className="primary" onClick={doScan} disabled={!path || phase === 'running'}>scan</button>
        </div>
        <div className="k" style={{ marginTop: 6, fontSize: 11.5 }}>
          scan is read-only: it previews the module layout before anything is written
        </div>
      </div>

      {err && <div className="err">{err}</div>}

      {scan && phase !== 'input' && (
        <div className="card">
          <h3>scan result (depth {scan.depth})</h3>
          {(scan.modules || []).length === 0 ? (
            <div className="k">single-module repo — one store, no fan-out</div>
          ) : (
            <div>
              <div className="k" style={{ marginBottom: 6 }}>
                {scan.modules.length} modules found — untick any to leave out (the list is yours to edit later in .ctxoptimize/config.json)
              </div>
              {scan.modules.map((m) => (
                <label key={m.path} style={{ display: 'flex', gap: 8, padding: '2px 0', cursor: 'pointer' }}>
                  <input
                    type="checkbox" checked={!!checked[m.path]}
                    onChange={(e) => setChecked({ ...checked, [m.path]: e.target.checked })}
                    disabled={phase === 'running'}
                  />
                  <span>{m.path}</span>
                  <span className="k">({m.marker})</span>
                </label>
              ))}
              {scan.clipped && <div className="k">note: markers exist past the depth bound — deeper modules may exist</div>}
            </div>
          )}
          <div className="row" style={{ marginTop: 10 }}>
            <span className="k">store name</span>
            <input type="text" value={name} onChange={(e) => setName(e.target.value)} disabled={phase === 'running'} />
            <span className="grow" />
            <button className="primary" onClick={confirm} disabled={phase === 'running'}>
              {phase === 'running' ? 'gathering…' : 'confirm + gather'}
            </button>
          </div>
        </div>
      )}

      {log && <pre className="stream">{log}</pre>}

      {phase === 'done' && (
        <div className="card">
          onboarded ✓ — <a href={'#/viewer/' + encodeURIComponent(name)}>open the viewer</a> ·{' '}
          <a href="#/repos">back to repos</a>
        </div>
      )}
    </div>
  )
}
