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

  const def = viewerById(addr.view)
  const Body = def.Component

  return (
    <div className="viewer-shell">
      <div className="vs-bar">
        <label className="vs-field">
          <span className="vs-lab">store</span>
          {/* Changing the store clears the drill: a directory from one repo
              names nothing in another, and carrying it over lands you on an
              "no directory ..." dead end that looks like a bug. */}
          <select value={addr.module} onChange={(e) => go({ module: e.target.value, root: '' })}>
            {mods.map((m) => (
              <option key={m.key} value={m.key}>{m.key} ({m.nodes})</option>
            ))}
          </select>
        </label>
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
          params={addr.params}
        />
      )}
    </div>
  )
}
