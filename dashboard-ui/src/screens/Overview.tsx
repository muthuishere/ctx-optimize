import { useMemo } from 'react'
import { kindColorMap } from '../App'
import { useStores } from '../stores'

// Overview — the landing page. Global roll-up (stores, nodes, edges, fresh vs
// stale, producer breakdown) summed client-side from /api/stores, plus a
// compact per-repo grid linking into Viewer/Query. Cached, so it paints
// instantly on re-entry.
export default function Overview() {
  const { stores, err, refreshing, reload } = useStores()

  const roll = useMemo(() => {
    const s = stores || []
    let nodes = 0, edges = 0, fresh = 0, stale = 0
    const producers: Record<string, number> = {}
    for (const st of s) {
      nodes += st.nodes
      edges += st.edges
      if (st.fresh === 'fresh') fresh++
      else if (st.fresh === 'stale') stale++
      for (const [p, n] of Object.entries(st.producers || {})) {
        producers[p] = (producers[p] || 0) + n
      }
    }
    return { count: s.length, nodes, edges, fresh, stale, producers }
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
              <div className="stat"><b>{roll.count.toLocaleString()}</b><span>stores</span></div>
              <div className="stat"><b>{roll.nodes.toLocaleString()}</b><span>nodes</span></div>
              <div className="stat"><b>{roll.edges.toLocaleString()}</b><span>edges</span></div>
              <div className="stat"><b>{roll.fresh.toLocaleString()}</b><span>fresh</span></div>
              <div className="stat"><b>{roll.stale.toLocaleString()}</b><span>stale</span></div>
            </div>
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
