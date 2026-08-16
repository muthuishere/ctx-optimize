import { describe, expect, it } from 'vitest'
import { parseViewerHash, viewerHash } from './screens/ViewerShell'

// The address is the product here: a viewer URL is only worth sending to
// somebody if it names the store, the projection AND the level. Before this,
// `view` and `root` lived in component state and only `root` was ever written
// back, so switching the view left the URL describing a page you were no
// longer on — and the link you copied opened something else.
describe('viewer address', () => {
  it('round-trips store, view and drilled root', () => {
    const a = { module: 'AI-company-master', view: 'house', root: 'src/aiteam/api' }
    expect(parseViewerHash(viewerHash(a).replace('#/viewer/', ''))).toMatchObject(a)
  })

  it('always writes the view, even when it is the default', () => {
    // A URL that omits it silently changes meaning the day the default changes.
    expect(viewerHash({ module: 'repo', view: 'graph', root: '' })).toContain('view=graph')
  })

  it('omits root at the top level rather than writing an empty one', () => {
    expect(viewerHash({ module: 'repo', view: 'flow', root: '' })).not.toContain('root=')
  })

  it('survives a store key or directory that needs escaping', () => {
    const a = { module: 'org/repo name', view: 'flow', root: 'src/a b/c+d' }
    const parsed = parseViewerHash(viewerHash(a).replace('#/viewer/', ''))
    expect(parsed.module).toBe('org/repo name')
    expect(parsed.root).toBe('src/a b/c+d')
  })

  it('defaults a bare module URL instead of rendering nothing', () => {
    const p = parseViewerHash('myrepo')
    expect(p.module).toBe('myrepo')
    expect(p.view).toBe('graph')
    expect(p.root).toBe('')
  })

  it('keeps the rest of the query readable by the viewer that owns it', () => {
    // the graph viewer's ?center= is address state too and must survive parsing
    const p = parseViewerHash('myrepo?view=graph&center=pkg%2Ff.go%3A%3AFn')
    expect(p.params.get('center')).toBe('pkg/f.go::Fn')
  })
})
