import { api } from './api'
import { sanitizeScene } from './sanitize'
import type { Scene } from './types'

// One place that knows which endpoint answers for a given address, so the Flow
// and House views can never disagree about what a store name means.
//
// A bare repo name with no drill is asked at MODULE grain first: the cards are
// the repo's modules and the arrows are the manifest declarations that join
// them (ADR 22 D4). `/api/repo/scene` answers 404 for anything that is not a
// monorepo, and that 404 IS the answer — a single store falls straight through
// to its own directory-grain scene, with no guessing on the client.
export function fetchScene(module: string, root: string, grain: string): Promise<Scene> {
  const dirScene = () => {
    const q = new URLSearchParams({ module })
    if (root) q.set('root', root)
    if (grain) q.set('grain', grain)
    return api<Scene>(`/api/scene?${q}`).then(sanitizeScene)
  }
  if (root || grain || module.includes('/')) return dirScene()
  return api<Scene>(`/api/repo/scene?repo=${encodeURIComponent(module)}&cards=12`)
    .then(sanitizeScene)
    .catch(dirScene)
}
