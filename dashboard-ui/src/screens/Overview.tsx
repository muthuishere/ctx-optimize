import { useMemo } from 'react'
import { kindColorMap } from '../App'
import { useStores } from '../stores'
import { groupByRepo } from '../grouping'

// Overview — the landing page. Global roll-up (stores, nodes, edges, fresh vs
// stale, producer breakdown) summed client-side from /api/stores, plus a
// compact per-repo grid linking into Viewer/Query. Cached, so it paints
// instantly on re-entry.
// compact renders a token count as a short human number (12.3K, 4.5M).
function compact(n: number): string {
  if (n >= 1e6) return (n / 1e6).toFixed(1).replace(/\.0$/, '') + 'M'
  if (n >= 1e3) return (n / 1e3).toFixed(1).replace(/\.0$/, '') + 'K'
  return n.toLocaleString()
}

export default function Overview() {
  const { stores, err, refreshing, reload } = useStores()

  const roll = useMemo(() => {
    const s = stores || []
    let nodes = 0, edges = 0, fresh = 0, stale = 0
    let served = 0, saved = 0, usd = 0
    const producers: Record<string, number> = {}
    for (const st of s) {
      nodes += st.nodes
      edges += st.edges
      if (st.fresh === 'fresh') fresh++
      else if (st.fresh === 'stale') stale++
      // Served-counter roll-up: each store contributes 0 when it has no
      // metrics yet, so the sum is always safe.
      served += st.usage?.total_served || 0
      saved += st.usage?.est_tokens_saved || 0
      usd += st.usage?.est_cost_saved_usd || 0
      for (const [p, n] of Object.entries(st.producers || {})) {
        producers[p] = (producers[p] || 0) + n
      }
    }
    // Repos vs modules: a monorepo's module stores are parts of ONE product,
    // so the headline counts repos and reports modules separately (ADR
    // 2026-07-19-dashboard-repo-grouping).
    const groups = groupByRepo(s)
    const modules = groups.reduce((n, g) => n + g.modules.length, 0)
    return { count: groups.length, modules, nodes, edges, fresh, stale, served, saved, usd, producers }
  }, [stores])

  if (!stores) return err
    ? <div className="screenwrap"><div className="err">{err}</div></div>
    : <div className="screenwrap"><div className="k">loading…</div></div>

  const producers = Object.entries(roll.producers).sort((a, b) => b[1] - a[1])
  const pcolors = kindColorMap(producers.map(([p]) => p))

  return (
    <div className="screenwrap">
      <div className="head">
        <div className="row">
          <div>
            <div className="kicker">overview</div>
            <h2 className="screen">Everything gathered, at a glance</h2>
          </div>
          <span className="grow" />
          <button onClick={() => reload()} disabled={refreshing}>{refreshing ? 'reloading…' : 'Reload'}</button>
          <a href="#/onboard"><button className="primary">+ Add repo</button></a>
        </div>
      </div>

      {roll.count === 0 ? (
        <div className="empty">
          <h3>No stores yet</h3>
          <p>Gather a repository into a knowledge graph — scan the layout, pick the modules, and the store answers what a grep-and-read chain would.</p>
          <a href="#/onboard"><button className="primary">Onboard a repo</button></a>
        </div>
      ) : (
        <>
          <div className="card">
            <div className="stat-grid">
              <div className="stat"><b>{roll.count.toLocaleString()}</b><span>{roll.count === 1 ? 'repo' : 'repos'}</span></div>
              {roll.modules > 0 && (
                <div className="stat"><b>{roll.modules.toLocaleString()}</b><span>modules</span></div>
              )}
              <div className="stat"><b>{roll.nodes.toLocaleString()}</b><span>nodes</span></div>
              <div className="stat"><b>{roll.edges.toLocaleString()}</b><span>edges</span></div>
              <div className="stat"><b>{roll.fresh.toLocaleString()}</b><span>fresh</span></div>
              <div className="stat"><b>{roll.stale.toLocaleString()}</b><span>stale</span></div>
            </div>
            {roll.served > 0 && (
              <div className="saved-banner" title="each answered read replaces the grep-and-read chain it stood in for; ~$3 / M input tokens">
                <span className="saved-usd">~${roll.usd < 100 ? roll.usd.toFixed(2) : Math.round(roll.usd).toLocaleString()} saved</span>
                <span className="saved-dot">·</span>
                <span>{roll.served.toLocaleString()} answers served</span>
                <span className="saved-dot">·</span>
                <span>{compact(roll.saved)} tokens saved</span>
              </div>
            )}
            {producers.length > 0 && (
              <div className="row" style={{ marginTop: 16, gap: 6 }}>
                {producers.map(([p, n]) => (
                  <span className="chip" key={p} style={{ borderColor: pcolors.get(p), color: pcolors.get(p) }}>
                    {p} <b>{n.toLocaleString()}</b>
                  </span>
                ))}
              </div>
            )}
          </div>

          <div className="grid-cards">
            {(stores || []).slice().sort((a, b) => b.nodes - a.nodes).map((s) => (
              <div className="card ov-repo" key={s.key}>
                <div className="card-title">
                  <h3>{s.key}</h3>
                  <span className={'badge ' + s.fresh}>{s.fresh}</span>
                </div>
                <div className="stat-grid">
                  <div className="stat"><b>{s.nodes.toLocaleString()}</b><span>nodes</span></div>
                  <div className="stat"><b>{s.edges.toLocaleString()}</b><span>edges</span></div>
                </div>
                {s.summary && <div className="muted" style={{ marginTop: 10, fontSize: '.85rem' }}>{s.summary}</div>}
                <div className="row" style={{ marginTop: 14 }}>
                  <a href={'#/viewer/' + encodeURIComponent(s.key)}><button>Viewer</button></a>
                  <a href={'#/query/' + encodeURIComponent(s.key)}><button>Query</button></a>
                  <span className="grow" />
                  <a href="#/repos" className="k" style={{ fontSize: '.8rem' }}>manage →</a>
                </div>
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  )
}
