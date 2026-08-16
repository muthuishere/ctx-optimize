// Pure, defensive normalizers for graph data coming off the API. The dashboard
// must survive a malformed store / buggy adapter: one bad node is cleaned or
// dropped ALONE, never allowed to throw and blank the viewer. Kept dependency-
// free and side-effect-free so it is trivially unit-testable.
import type {
  Edge,
  Node,
  Scene,
  SceneCard,
  SceneDoor,
  SceneLink,
  SceneStat,
  SceneCrumb,
  SceneQuestion,
  SceneWorld,
} from './types'

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

// sanitizeNode makes ONE node safe to render, or drops it. Fields are coerced
// to the shapes the renderer expects (string label/kind/source/location, object
// metadata); only a node with no usable id is unrenderable and returns null.
export function sanitizeNode(n: any): Node | null {
  if (!n || n.id == null) return null
  const id = String(n.id)
  return {
    id,
    label: typeof n.label === 'string' ? n.label : String(n.label ?? id),
    kind: typeof n.kind === 'string' ? n.kind : String(n.kind ?? ''),
    file_type: typeof n.file_type === 'string' ? n.file_type : undefined,
    source: typeof n.source === 'string' ? n.source : undefined,
    location: typeof n.location === 'string' ? n.location : undefined,
    metadata: n.metadata && typeof n.metadata === 'object' ? n.metadata : undefined,
  }
}

// sanitizeEdge is the edge equivalent: endpoints and relation coerced to
// strings, or the edge is dropped (a dangling edge just won't draw — no crash).
export function sanitizeEdge(e: any): Edge | null {
  if (!e || e.source == null || e.target == null) return null
  return {
    source: String(e.source),
    target: String(e.target),
    relation: typeof e.relation === 'string' ? e.relation : '',
  }
}

// sanitizeScene guarantees every array field on a Scene IS an array. The server
// now normalises this too, but the dashboard is served by whatever binary is
// running — a store gathered by an older `serve` returned `"chips": null`, and
// `for (const s of scene.chips)` threw and blanked a view that had seven cards
// ready to draw. One absent field must never cost the whole screen, so the
// client refuses to trust the wire even when the wire is correct.
export function sanitizeScene(s: any): Scene {
  const arr = <T,>(v: any): T[] => (Array.isArray(v) ? v : [])
  return {
    ...(s || {}),
    module: String(s?.module ?? ''),
    title: String(s?.title ?? ''),
    total_nodes: Number(s?.total_nodes ?? 0),
    total_edges: Number(s?.total_edges ?? 0),
    cards: arr<SceneCard>(s?.cards),
    links: arr<SceneLink>(s?.links),
    world: arr<SceneWorld>(s?.world).map((w: any) => ({ ...w, sample: arr<SceneDoor>(w?.sample) })),
    stats: arr<SceneStat>(s?.stats),
    chips: arr<string>(s?.chips),
    notes: arr<string>(s?.notes),
    root: String(s?.root ?? ''),
    level: String(s?.level ?? 'directory'),
    // A scene with no crumbs is a level you cannot leave, so the trail back to
    // the whole repo is synthesised rather than trusted.
    questions: arr<SceneQuestion>(s?.questions),
    inside: arr<SceneCrumb>(s?.inside),
    crumbs: arr<SceneCrumb>(s?.crumbs).length
      ? arr<SceneCrumb>(s?.crumbs)
      : [{ label: String(s?.title ?? 'repo'), root: '' }],
  } as Scene
}
