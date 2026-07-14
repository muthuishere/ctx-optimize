import { useEffect, useState } from 'react'
import Overview from './screens/Overview'
import Repos from './screens/Repos'
import Onboard from './screens/Onboard'
import Query from './screens/Query'
import Viewer from './screens/Viewer'
import Settings from './screens/Settings'
import Changes from './screens/Changes'

// Hash routing keeps the app a single embedded file server: only "/" is ever
// requested. Routes: #/overview #/repos #/onboard #/query/<module>
// #/viewer/<module> #/settings #/changes. The logo lands on #/overview.
function useHash(): string {
  const [hash, setHash] = useState(window.location.hash || '#/overview')
  useEffect(() => {
    const on = () => setHash(window.location.hash || '#/overview')
    window.addEventListener('hashchange', on)
    return () => window.removeEventListener('hashchange', on)
  }, [])
  return hash
}

const TABS: [string, string][] = [
  ['repos', 'Repos'],
  ['onboard', 'Onboard'],
  ['query', 'Query'],
  ['viewer', 'Viewer'],
  ['settings', 'Settings'],
  ['changes', 'Changes'],
]

export default function App() {
  const hash = useHash()
  // Route = first segment; arg = raw remainder (screens decode it — module
  // keys and node ids may contain any character once URI-encoded).
  const h = hash.replace(/^#\//, '')
  const i = h.indexOf('/')
  const route = (i < 0 ? h : h.slice(0, i)) || 'overview'
  const arg = i < 0 ? '' : h.slice(i + 1)

  return (
    <div className="app">
      <header className="top">
        <a href="#/overview" className="logo" title="Overview">
          <h1>ctx-<em>optimize</em></h1>
        </a>
        <span className="sub">gather once · answer from the store</span>
        <nav className="tabs">
          {TABS.map(([r, label]) => (
            <a key={r} href={'#/' + r} className={route === r ? 'active' : ''}>
              {label}
            </a>
          ))}
        </nav>
      </header>
      <main className={'body' + (route === 'viewer' ? ' nopad' : '')}>
        {(route === 'overview' || route === '') && <Overview />}
        {route === 'repos' && <Repos />}
        {route === 'onboard' && <Onboard />}
        {route === 'query' && <Query initialModule={arg} />}
        {route === 'viewer' && <Viewer initialModule={arg} />}
        {route === 'settings' && <Settings />}
        {route === 'changes' && <Changes />}
      </main>
    </div>
  )
}

// Stable per-kind colors, shared by Query, Viewer and Overview. The v0.3
// "special" kinds get distinct vivid hues so routes/deps/k8s/tasks/images
// read at a glance; everything else draws from a calm green-forward spread.
export const SPECIAL_COLORS: Record<string, string> = {
  route: '#a3e635', // lime
  dependency: '#fbbf24', // amber
  resource: '#38bdf8', // blue (k8s / infra)
  task: '#2dd4bf', // teal
  image: '#c084fc', // violet
  config: '#fb923c', // orange
}
export const SPECIAL_KINDS = Object.keys(SPECIAL_COLORS)

const PALETTE = ['#4ade80', '#f472b6', '#22d3ee', '#facc15', '#fb7185',
  '#34d399', '#93c5fd', '#fda4af', '#5eead4', '#e879f9']

export function kindColorMap(kinds: string[]): Map<string, string> {
  const m = new Map<string, string>()
  let i = 0
  Array.from(new Set(kinds)).sort().forEach((k) => {
    if (SPECIAL_COLORS[k]) {
      m.set(k, SPECIAL_COLORS[k])
      return
    }
    m.set(k, PALETTE[i % PALETTE.length])
    i++
  })
  return m
}
