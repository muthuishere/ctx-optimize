import { useState } from 'react'
import { mutate, stream } from '../api'
import type { ScanResult } from '../types'

// Onboard: path → scan preview (editable module checkboxes) → confirm →
// live progress stream → repo lands in the store. Server-side this is the
// SAME code path as `ctx-optimize scan` / `init --scan --yes` / `add .`.
type Phase = 'input' | 'preview' | 'running' | 'done'

export default function Onboard() {
  const [path, setPath] = useState('')
  const [name, setName] = useState('')
  const [scan, setScan] = useState<ScanResult | null>(null)
  const [checked, setChecked] = useState<Record<string, boolean>>({})
  const [log, setLog] = useState('')
  const [phase, setPhase] = useState<Phase>('input')
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

  const stepState = (s: Phase): 'done' | 'active' | '' => {
    const order: Phase[] = ['input', 'preview', 'running', 'done']
    const cur = order.indexOf(phase)
    const idx = order.indexOf(s)
    if (idx < cur) return 'done'
    if (idx === cur) return 'active'
    return ''
  }

  return (
    <div className="screenwrap narrow">
      <div className="head">
        <div className="kicker">onboard</div>
        <h2 className="screen">Gather a repo into the store</h2>
        <p className="screen-sub">Scan the layout, pick the modules, confirm. Same path as <code>init --scan --yes</code> then <code>add .</code></p>
      </div>

      <div className="stepper">
        <div className={'step ' + stepState('input')}><span className="dot">1</span> Path</div>
        <span className="step-line" />
        <div className={'step ' + stepState('preview')}><span className="dot">2</span> Scan</div>
        <span className="step-line" />
        <div className={'step ' + (phase === 'done' ? 'done' : phase === 'running' ? 'active' : '')}><span className="dot">3</span> Gather</div>
      </div>

      <div className="card">
        <div className="row">
          <input
            type="text" className="grow" placeholder="/absolute/path/to/repo"
            value={path} onChange={(e) => setPath(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && path && doScan()}
            disabled={phase === 'running'}
          />
          <button className="primary" onClick={doScan} disabled={!path || phase === 'running'}>Scan</button>
        </div>
        <div className="k" style={{ marginTop: 8, fontSize: '.8rem' }}>
          Scan is read-only — it previews the module layout before anything is written.
        </div>
      </div>

      {err && <div className="err">{err}</div>}

      {scan && phase !== 'input' && (
        <div className="card">
          <div className="card-title">
            <h3>Scan result</h3>
            <span className="chip">depth <b>{scan.depth}</b></span>
          </div>
          {(scan.modules || []).length === 0 ? (
            <div className="muted">Single-module repo — one store, no fan-out.</div>
          ) : (
            <div>
              <div className="muted" style={{ marginBottom: 8, fontSize: '.88rem' }}>
                {scan.modules.length} modules found — untick any to leave out. The list stays yours to edit later in <code>.ctxoptimize/config.json</code>.
              </div>
              <div className="checklist">
                {scan.modules.map((m) => (
                  <label key={m.path}>
                    <input
                      type="checkbox" checked={!!checked[m.path]}
                      onChange={(e) => setChecked({ ...checked, [m.path]: e.target.checked })}
                      disabled={phase === 'running'}
                    />
                    <span className="mpath">{m.path}</span>
                    <span className="chip">{m.marker}</span>
                  </label>
                ))}
              </div>
              {scan.clipped && <div className="k" style={{ marginTop: 8, fontSize: '.82rem' }}>Note: markers exist past the depth bound — deeper modules may exist.</div>}
            </div>
          )}
          <hr className="divider" />
          <div className="row">
            <span className="k">Store name</span>
            <input type="text" value={name} onChange={(e) => setName(e.target.value)} disabled={phase === 'running'} />
            <span className="grow" />
            <button className="primary" onClick={confirm} disabled={phase === 'running'}>
              {phase === 'running' ? 'Gathering…' : 'Confirm + gather'}
            </button>
          </div>
        </div>
      )}

      {log && <pre className="stream">{log}</pre>}

      {phase === 'done' && (
        <div className="card" style={{ borderColor: 'var(--accent-dim)' }}>
          <div className="card-title">
            <span className="badge fresh">onboarded</span>
            <span className="muted">store <b style={{ color: 'var(--text)' }}>{name}</b> is ready</span>
          </div>
          <div className="row">
            <a href={'#/viewer/' + encodeURIComponent(name)}><button className="primary">Open the viewer</button></a>
            <a href={'#/query/' + encodeURIComponent(name)}><button>Query it</button></a>
            <a href="#/repos"><button>Back to repos</button></a>
          </div>
        </div>
      )}
    </div>
  )
}
