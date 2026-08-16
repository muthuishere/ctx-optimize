import type { ComponentType } from 'react'
import GraphViewer from './screens/GraphViewer'
import FlowViewer from './screens/FlowViewer'
import HouseViewer from './screens/HouseViewer'

// The VIEWER REGISTRY. Everything the switcher shows comes from this list, so a
// third and fourth viewer is one entry each — ViewerShell renders the dropdown
// from VIEWERS and never names a viewer itself.
//
// Contract for a viewer component: it is handed the selected store `module`,
// the drilled `root`, and `onRoot` to move between levels — and owns the whole
// stage from there. The shell owns the header strip AND the address: a viewer
// never touches history, so there is one implementation of "where am I".
export interface ViewerProps {
  module: string
  /** the drilled directory ("" = the whole repo). Comes from the URL. */
  root: string
  /** the forced grain ("" = infer from root) */
  grain: string
  /**
   * navigate to a level. Writes the URL; the shell owns the address. `grain`
   * is passed only when the target cannot be inferred from `root` alone — the
   * card standing for a directory's OWN files, whose root is the directory it
   * is already showing.
   */
  onRoot: (root: string, grain?: string) => void
  /** the rest of the query, read-only — e.g. the graph viewer's ?center=. */
  params: URLSearchParams
}

export interface ViewerDef {
  id: string
  label: string
  blurb: string
  Component: ComponentType<ViewerProps>
}

export const VIEWERS: ViewerDef[] = [
  {
    id: 'graph',
    label: 'Graph — force-directed',
    blurb: 'every node, expanded on click',
    Component: GraphViewer,
  },
  {
    id: 'flow',
    label: 'Flow — derived architecture',
    blurb: 'directories, lifted edges, the outer world',
    Component: FlowViewer,
  },
  {
    id: 'house',
    label: 'House — the codebase as a building',
    blurb: 'floors are dependency depth, doors are the boundary',
    Component: HouseViewer,
  },
]

export const DEFAULT_VIEWER = VIEWERS[0].id

export function viewerById(id: string): ViewerDef {
  return VIEWERS.find((v) => v.id === id) || VIEWERS[0]
}
