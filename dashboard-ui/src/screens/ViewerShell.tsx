import { useEffect, useMemo, useState } from 'react'
import { api } from '../api'
import { safeDecode } from '../sanitize'
import type { Module } from '../types'
import { DEFAULT_VIEWER, VIEWERS, viewerById } from '../viewers'

// ViewerShell owns THE ADDRESS. Everything that decides what you are looking
// at — which store, which projection, which directory you drilled into — lives
// in the hash and nowhere else:
//
//     #/viewer/<module>?view=<id>&root=<dir>
//
// It used to be split three ways: the module came from the route, `view` and
// `root` lived in component state, and only `root` was ever written back. So
// switching the store or the view silently left the URL describing a page you
// were no longer on, and a link you copied took someone somewhere else. Both
// viewers also kept their own copy of `root` plus their own history plumbing,
// which is two implementations of one fact.
//
// Now the URL is the single source of truth and this component holds no state
// for any of it: every control writes the hash, and the app re-renders from it.
// That is what makes a viewer URL worth sending to somebody — it names the
// store, the projection AND the level, so `?view=house&root=src/aiteam/api`
// opens exactly what the sender was looking at.
export interface ViewerAddress {
  module: string
  view: string
  root: string
  /** forces the level: "" infers it from root, "file"/"decl" pin it */
  grain: string
}

export function parseViewerHash(raw: string): ViewerAddress & { params: URLSearchParams } {
  const qi = raw.indexOf('?')
  const module = safeDecode(qi < 0 ? raw : raw.slice(0, qi))
  const params = new URLSearchParams(qi < 0 ? '' : raw.slice(qi + 1))
  return {
    module,
    view: params.get('view') || DEFAULT_VIEWER,
    root: params.get('root') || '',
    grain: params.get('grain') || '',
    params,
  }
}

export function viewerHash(a: ViewerAddress): string {
  const q = new URLSearchParams()
  // `view` is always written, even when it is the default: a URL that omits it
  // silently changes meaning the day the default changes.
  q.set('view', a.view)
  if (a.root) q.set('root', a.root)
  // grain only when it is FORCED. A directory that can be inferred must not
  // carry one, or a shared link pins a level that the code may since have
  // grown past.
  if (a.grain) q.set('grain', a.grain)
  return '#/viewer/' + encodeURIComponent(a.module) + '?' + q.toString()
}

export default function ViewerShell({ initialModule: rawArg }: { initialModule: string }) {
  const addr = useMemo(() => parseViewerHash(rawArg), [rawArg])
  const [mods, setMods] = useState<Module[]>([])
  const [err, setErr] = useState('')

  // Assigning location.hash pushes a history entry AND fires hashchange, which
  // is what App already listens to — so Back walks out of a drill, out of a
  // view switch and out of a store switch, with no second history mechanism.
  const go = (next: Partial<ViewerAddress>) => {
    window.location.hash = viewerHash({ ...addr, ...next })
  }

  useEffect(() => {
    api<Module[]>('/api/modules')
      .then((m) => {
        setMods(m)
        if (!addr.module && m.length > 0) {
          window.location.replace('#' + viewerHash({ ...addr, module: m[0].key }).slice(1))
        }
      })
      .catch((e) => setErr(String(e.message || e)))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  // Modules are grouped under their REPO. The flat list put an 18-module
  // monorepo's parts among genuinely separate products — `reqsume`,
  // `reqsume/apps/api`, `reqsume/apps/ui`, `spinx`, `stock-core/apps/cli` …  —
  // as peers, which is the same failure ADR 2026-07-19 fixed on the overview
  // and the viewer never picked up. The repo is the product; its modules are
  // parts of it, and this is how you find the ui, the api and the worker.
  const groups = useMemo(() => {
    const by = new Map<string, Module[]>()
    for (const m of mods) {
      const root = m.root || m.key
      const list = by.get(root) || []
      list.push(m)
      by.set(root, list)
    }
    return [...by.entries()]
      .map(([root, list]) => ({
        root,
        list: list.slice().sort((a, b) => {
          if (a.key === root) return -1 // the repo's own store first
          if (b.key === root) return 1
          return b.nodes - a.nodes
        }),
      }))
      .sort((a, b) => a.root.localeCompare(b.root))
  }, [mods])

  // a module's short name inside its repo: `reqsume/apps/api` reads as `apps/api`
  const shortName = (key: string, root: string) =>
    key === root || !root ? key : key.slice(root.length + 1)

  // Which repo the current address belongs to. The select's value has to be a
  // REPO even when the address names one of its modules, or picking a store
  // after drilling into a module shows an empty select.
  const repoOf = (key: string) => {
    const hit = mods.find((m) => m.key === key)
    if (hit) return hit.root || hit.key
    const i = key.indexOf('/')
    return i > 0 ? key.slice(0, i) : key
  }
  const currentRepo = addr.module ? repoOf(addr.module) : ''
  const inModule = !!addr.module && addr.module !== currentRepo
  const repoModules = useMemo(
    () => (groups.find((g) => g.root === currentRepo)?.list || []).filter((m) => m.key !== currentRepo),
    [groups, currentRepo],
  )

  const def = viewerById(addr.view)
  const Body = def.Component

  return (
    <div className="viewer-shell">
      <div className="vs-bar">
        <label className="vs-field">
          <span className="vs-lab">repo</span>
          {/* The list is REPOS, not modules (ADR 22 D3). A monorepo's forty
              packages listed beside genuinely separate products made the
              chooser the place you got lost; the repo is the product, and its
              modules are cards on the scene you land on. Changing it clears the
              drill: a directory from one repo names nothing in another. */}
          <select value={currentRepo} onChange={(e) => go({ module: e.target.value, root: '', grain: '' })}>
            {groups.map((g) => (
              <option key={g.root} value={g.root}>
                {g.root}
                {g.list.length > 1 ? ` · ${g.list.length} modules` : ''}
                {' '}({g.list.reduce((n, m) => n + m.nodes, 0).toLocaleString()})
              </option>
            ))}
          </select>
        </label>
        {/* The repo scene draws its top modules and says so, which leaves the
            rest with no way in once the store list stopped naming them. This is
            that way in: every module of the current repo, always. Reachability
            is not something a ranked sample gets to decide. */}
        {repoModules.length > 1 && (
          <label className="vs-field">
            <span className="vs-lab">module</span>
            <select
              value={inModule ? addr.module : currentRepo}
              onChange={(e) => go({ module: e.target.value, root: '', grain: '' })}
            >
              <option value={currentRepo}>← all {repoModules.length} modules</option>
              {repoModules.map((m) => (
                <option key={m.key} value={m.key}>
                  {shortName(m.key, currentRepo)} ({m.nodes.toLocaleString()})
                </option>
              ))}
            </select>
          </label>
        )}
        <label className="vs-field">
          <span className="vs-lab">view</span>
          {/* The drill SURVIVES a view switch: the level is a fact about the
              code, the projection is only how it is drawn. */}
          <select value={def.id} onChange={(e) => go({ view: e.target.value })}>
            {VIEWERS.map((v) => (
              <option key={v.id} value={v.id}>{v.label}</option>
            ))}
          </select>
        </label>
        <span className="vs-blurb">{def.blurb}</span>
        {addr.root && <span className="vs-root">{addr.root}{addr.grain ? ` · ${addr.grain}s` : ''}</span>}
        {err && <span className="err">{err}</span>}
      </div>
      {/* key on view+module: switching either gives the viewer a clean mount
          rather than making every viewer defend against a swapped store. */}
      {addr.module && (
        <Body
          key={def.id + ' ' + addr.module}
          module={addr.module}
          root={addr.root}
          grain={addr.grain}
          onRoot={(r: string, grain = '') => go({ root: r, grain })}
          onModule={(key: string) => go({ module: key, root: '', grain: '' })}
          params={addr.params}
        />
      )}
    </div>
  )
}
