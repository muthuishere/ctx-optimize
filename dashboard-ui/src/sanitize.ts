// Pure, defensive normalizers for graph data coming off the API. The dashboard
// must survive a malformed store / buggy adapter: one bad node is cleaned or
// dropped ALONE, never allowed to throw and blank the viewer. Kept dependency-
// free and side-effect-free so it is trivially unit-testable.
import type { Edge, Node } from './types'

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
