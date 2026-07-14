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

  if (err) return <div className="err">{err}</div>
  if (!lines) return <div className="k">loading…</div>

  return (
    <div style={{ maxWidth: 980 }}>
      <h2 className="screen">Changes — audit.ndjson (append-only, every mutation door)</h2>
      {lines.length === 0 && (
        <div className="card k">
          no changes recorded yet — dashboard mutations and `config` sets land here
        </div>
      )}
      {lines.length > 0 && (
        <table className="list">
          <thead>
            <tr><th>when</th><th>actor</th><th>action</th><th>target</th><th>hash</th></tr>
          </thead>
          <tbody>
            {[...lines].reverse().map((l, i) => (
              <tr key={i}>
                <td className="k" style={{ whiteSpace: 'nowrap' }}>{l.ts}</td>
                <td><span className="chip"><b>{l.actor}</b></span></td>
                <td>{l.action}</td>
                <td className="k">{l.target}</td>
                <td className="k" style={{ whiteSpace: 'nowrap' }}>
                  {l.before_hash || l.after_hash
                    ? `${(l.before_hash || '∅').slice(0, 7)}→${(l.after_hash || '∅').slice(0, 7)}`
                    : ''}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  )
}
