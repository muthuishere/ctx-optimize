import { useEffect, useState } from 'react'
import Repos from './screens/Repos'
import Onboard from './screens/Onboard'
import Query from './screens/Query'
import Viewer from './screens/Viewer'
import Settings from './screens/Settings'
import Changes from './screens/Changes'

// Hash routing keeps the app a single embedded file server: only "/" is ever
// requested. Routes: #/repos #/onboard #/query/<module> #/viewer/<module>
// #/settings #/changes
function useHash(): string {
  const [hash, setHash] = useState(window.location.hash || '#/repos')
  useEffect(() => {
    const on = () => setHash(window.location.hash || '#/repos')
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
  const route = (i < 0 ? h : h.slice(0, i)) || 'repos'
  const arg = i < 0 ? '' : h.slice(i + 1)

  return (
    <div className="app">
      <header className="top">
        <h1>ctx-optimize</h1>
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

// Stable per-kind colors, shared by Query and Viewer.
const PALETTE = ['#38bdf8', '#f472b6', '#a3e635', '#fbbf24', '#a78bfa', '#34d399',
  '#fb7185', '#22d3ee', '#fb923c', '#e879f9', '#facc15', '#4ade80',
  '#93c5fd', '#fda4af', '#5eead4', '#c4b5fd']

export function kindColorMap(kinds: string[]): Map<string, string> {
  const m = new Map<string, string>()
  Array.from(new Set(kinds)).sort().forEach((k, i) => m.set(k, PALETTE[i % PALETTE.length]))
  return m
}
