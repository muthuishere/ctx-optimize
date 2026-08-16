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

// StoreLinks mirrors internal/dashboard.StoreLinks — the two bases the Viewer
// joins into "open source" links for a selected node. github_base is present
// only when the repo's origin is a real GitHub remote.
export interface StoreLinks {
  repo_abs?: string
  github_base?: string
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
  links?: StoreLinks
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
  remote?: { push?: string; pull?: string; from: string }
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

// ---- the derived architecture scene (GET /api/scene, internal/scene) ----
// Mirrors internal/scene.Scene exactly. A Door carries a port's NAME and two
// flags; there is no value field on the wire, and there never may be.

export interface SceneCard {
  id: string
  label: string
  dir: string
  files: number
  decls: number
  in: number
  out: number
  ext_in: number
  ext_out: number
  layer: number
  row: number
  detail: string
  glyph: string
  hub: boolean
  children: number
  top: string
  enter_grain: string
  inner: number
}

export interface SceneCrumb {
  label: string
  root: string
  /** a different STORE to open — the repo above a module. Leaving a module is
      not a directory move, so it cannot travel as a `root`. */
  module?: string
}

export interface SceneQuestion {
  text: string
  command: string
}

export interface SceneLink {
  from: string
  to: string
  relation: string
  label: string
  weight: number
  /** names what the arrow stands for, where a count alone would mislead */
  detail?: string
}

export interface SceneDoor {
  label: string
  direction: string
  sensitive: boolean
  dynamic: boolean
}

export interface SceneWorld {
  id: string
  transport: string
  total: number
  provides: number
  consumes: number
  sensitive: number
  sample: SceneDoor[]
  truncated: boolean
}

export interface SceneStat {
  label: string
  text: string
}

export interface Scene {
  module: string
  title: string
  total_nodes: number
  total_edges: number
  subsystems_total: number
  subsystems_shown: number
  lifted_total: number
  lifted_shown: number
  cards: SceneCard[]
  links: SceneLink[]
  world: SceneWorld[]
  stats: SceneStat[]
  chips: string[]
  root: string
  level: string
  crumbs: SceneCrumb[]
  inside: SceneCrumb[]
  questions: SceneQuestion[]
  notes: string[]
  empty?: string
  /** the store to look at instead: a level with one card says nothing */
  redirect?: string
}
