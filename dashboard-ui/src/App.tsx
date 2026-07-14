import { useEffect, useState } from 'react'
import ErrorBoundary from './ErrorBoundary'
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
        {/* One boundary per route: a screen that throws shows a fallback
            instead of blanking the whole app, and the shell/nav stays live so
            switching tabs recovers. Keyed by route+arg so navigating resets it. */}
        <ErrorBoundary key={route + '/' + arg} label={'the ' + route + ' view'}>
          {(route === 'overview' || route === '') && <Overview />}
          {route === 'repos' && <Repos />}
          {route === 'onboard' && <Onboard />}
          {route === 'query' && <Query initialModule={arg} />}
          {route === 'viewer' && <Viewer initialModule={arg} />}
          {route === 'settings' && <Settings />}
          {route === 'changes' && <Changes />}
        </ErrorBoundary>
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

// safeDecode never throws: decodeURIComponent dies on a malformed %-escape (a
// stray '%' in a route path or symbol id), and screens call it in render — an
// unguarded throw there blanks the whole page. On failure keep the raw string.
export function safeDecode(s: string): string {
  try {
    return decodeURIComponent(s)
  } catch {
    return s
  }
}

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

// Producer accents — the PROVENANCE axis, distinct from kind. The tier-1
// built-ins get fixed, meaning-carrying hues (code = the green core, docs =
// teal, manifests = amber, git-history = violet); every adapter (postgres,
// kafka, …) draws a stable per-name hue from a cooler spread so it reads as
// "brought in from outside" next to the green core.
export const PRODUCER_COLORS: Record<string, string> = {
  code: '#4ade80', // green — the core
  markdown: '#5eead4', // teal — docs
  manifests: '#fbbf24', // amber — config/manifests
  'git-history': '#a78bfa', // violet — history
}
export const KNOWN_PRODUCERS = Object.keys(PRODUCER_COLORS)

const PRODUCER_PALETTE = ['#38bdf8', '#f472b6', '#fb923c', '#22d3ee',
  '#c084fc', '#f87171', '#a3e635', '#e879f9', '#93c5fd', '#fda4af']

export function producerColorMap(producers: string[]): Map<string, string> {
  const m = new Map<string, string>()
  let i = 0
  Array.from(new Set(producers)).sort().forEach((p) => {
    if (PRODUCER_COLORS[p]) {
      m.set(p, PRODUCER_COLORS[p])
      return
    }
    m.set(p, PRODUCER_PALETTE[i % PRODUCER_PALETTE.length])
    i++
  })
  return m
}
