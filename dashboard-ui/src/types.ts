export interface Node {
  id: string
  label: string
  kind: string
  file_type?: string
  source?: string
  location?: string
  metadata?: Record<string, string>
}

export interface Edge {
  source: string
  target: string
  relation: string
}

export interface GraphResponse {
  nodes: Node[]
  edges: Edge[]
  total_nodes: number
  total_edges: number
  truncated: boolean
}

export interface Module {
  key: string
  root: string
  nodes: number
  edges: number
  summary?: string
}

export interface FreshnessReport {
  path: string
  state: string
  store_head: string
  current_head: string
  age_seconds: number
}

// Usage mirrors internal/usage.Summary — the served-counter roll-up (answers
// served + tokens/$ saved) the `status` verb prints. Only the fields the
// Overview needs are typed.
export interface Usage {
  total_served: number
  est_tokens_saved: number
  est_cost_saved_usd: number
}

export interface StoreInfo {
  key: string
  root: string
  nodes: number
  edges: number
  summary?: string
  fresh: string
  source_path?: string
  age_seconds?: number
  producers?: Record<string, number>
  freshness?: FreshnessReport[]
  usage?: Usage
}

export interface Neighbor {
  id: string
  relation: string
  dir: string
}

export interface Hit {
  node: Node
  score: number
  neighbors?: Neighbor[]
}

export interface QueryResult {
  query: string
  hits: Hit[]
}

export interface AuditLine {
  ts: string
  actor: string
  action: string
  target: string
  before_hash?: string
  after_hash?: string
}

export interface ConfigKV {
  key: string
  value: string
  source: string
}

// Pack covers all three axis shapes: grammar packs carry exts/wasm/config,
// route & manifest packs carry rules/file.
export interface Pack {
  name: string
  exts?: string[]
  wasm?: string
  config?: string
  rules?: number
  file?: string
}

export interface AdapterInfo {
  name: string
  run: string
  file: string
}

export interface Axis {
  axis: string
  kind: string
  note: string
  core?: string[]
  packs?: Pack[]
  adapters?: AdapterInfo[]
  error?: string
}

export interface Setup {
  store_root: string
  global: { file: string; config: Record<string, string> }
  project?: { path: string; file: string; config: Record<string, unknown> }
  effective: ConfigKV[]
  axes: Axis[]
  remote?: { url: string; from: string }
}

export interface ScanModule {
  path: string
  name?: string
  marker?: string
}

export interface ScanResult {
  modules: ScanModule[]
  clipped: boolean
  depth: number
}
