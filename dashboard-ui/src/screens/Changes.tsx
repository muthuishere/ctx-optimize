import { useEffect, useState } from 'react'
import { api } from '../api'
import type { AuditLine } from '../types'

// Changes — the audit feed: every mutation from every door (dashboard AND
// CLI), newest first. Same data as `ctx-optimize log`.
export default function Changes() {
  const [lines, setLines] = useState<AuditLine[] | null>(null)
  const [err, setErr] = useState('')

  useEffect(() => {
    api<AuditLine[]>('/api/audit').then(setLines).catch((e) => setErr(String(e.message || e)))
  }, [])

  if (err) return <div className="screenwrap"><div className="err">{err}</div></div>
  if (!lines) return <div className="screenwrap"><div className="k">loading…</div></div>

  const rows = [...lines].reverse()

  return (
    <div className="screenwrap narrow">
      <div className="head">
        <div className="kicker">changes</div>
        <h2 className="screen">Audit feed</h2>
        <p className="screen-sub">Every mutation from every door — dashboard and CLI — appended to <code>audit.ndjson</code>. Read it with <code>ctx-optimize log</code>.</p>
      </div>

      {rows.length === 0 && (
        <div className="empty">
          <h3>No changes yet</h3>
          <p>Dashboard mutations and <code>config</code> sets land here, newest first.</p>
        </div>
      )}

      {rows.length > 0 && (
        <div className="timeline">
          {rows.map((l, i) => {
            const hash = l.before_hash || l.after_hash
              ? <>{(l.before_hash || '∅').slice(0, 7)} → <b>{(l.after_hash || '∅').slice(0, 7)}</b></>
              : null
            return (
              <div className={'tl-item' + (l.actor === 'cli' ? ' cli' : '')} key={i}>
                <div className="tl-head">
                  <span className={'badge ' + (l.actor === 'dashboard' ? 'fresh' : 'unknown')}>{l.actor}</span>
                  <span className="tl-action">{l.action}</span>
                  <span className="tl-when">{when(l.ts)}</span>
                </div>
                {l.target && <div className="tl-target">{l.target}</div>}
                {hash && <div className="tl-hash">{hash}</div>}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}

function when(ts: string): string {
  const t = Date.parse(ts)
  if (isNaN(t)) return ts
  const secs = (Date.now() - t) / 1000
  if (secs < 60) return 'just now'
  if (secs < 3600) return Math.round(secs / 60) + 'm ago'
  if (secs < 86400) return Math.round(secs / 3600) + 'h ago'
  if (secs < 86400 * 30) return Math.round(secs / 86400) + 'd ago'
  return new Date(t).toLocaleDateString()
}
