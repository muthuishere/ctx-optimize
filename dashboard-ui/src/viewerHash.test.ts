import { describe, expect, it } from 'vitest'
import { DEFAULT_VIEWER } from './viewers'
import { parseViewerHash, viewerHash } from './screens/ViewerShell'

// The address is the product here: a viewer URL is only worth sending to
// somebody if it names the store, the projection AND the level. Before this,
// `view` and `root` lived in component state and only `root` was ever written
// back, so switching the view left the URL describing a page you were no
// longer on — and the link you copied opened something else.
describe('viewer address', () => {
  it('round-trips store, view and drilled root', () => {
    const a = { module: 'AI-company-master', view: 'house', root: 'src/aiteam/api', grain: '' }
    expect(parseViewerHash(viewerHash(a).replace('#/viewer/', ''))).toMatchObject(a)
  })

  it('always writes the view, even when it is the default', () => {
    // A URL that omits it silently changes meaning the day the default changes.
    expect(viewerHash({ module: 'repo', view: 'graph', root: '', grain: '' })).toContain('view=graph')
  })

  it('omits root at the top level rather than writing an empty one', () => {
    expect(viewerHash({ module: 'repo', view: 'flow', root: '', grain: '' })).not.toContain('root=')
  })

  it('survives a store key or directory that needs escaping', () => {
    const a = { module: 'org/repo name', view: 'flow', root: 'src/a b/c+d', grain: '' }
    const parsed = parseViewerHash(viewerHash(a).replace('#/viewer/', ''))
    expect(parsed.module).toBe('org/repo name')
    expect(parsed.root).toBe('src/a b/c+d')
  })

  it('defaults a bare module URL instead of rendering nothing', () => {
    // CHANGED with the House removal: the default is FLOW, not graph. A bare
    // /viewer URL should open the view that answers the question people arrive
    // with — what is this codebase, what talks to what — rather than every node
    // at once. The assertion reads DEFAULT_VIEWER rather than a literal, so the
    // order in VIEWERS stays the single place the default is decided.
    const p = parseViewerHash('myrepo')
    expect(p.module).toBe('myrepo')
    expect(p.view).toBe(DEFAULT_VIEWER)
    expect(p.view).toBe('flow')
    expect(p.root).toBe('')
  })

  it('keeps the rest of the query readable by the viewer that owns it', () => {
    // the graph viewer's ?center= is address state too and must survive parsing
    const p = parseViewerHash('myrepo?view=graph&center=pkg%2Ff.go%3A%3AFn')
    expect(p.params.get('center')).toBe('pkg/f.go::Fn')
  })
})

// The grain is part of the address, but only when it is FORCED: a directory
// whose level can be inferred must not pin one, or a link shared today keeps
// showing files after the directory grows subdirectories.
describe('viewer address carries a forced grain', () => {
  it('round-trips a pinned grain', () => {
    const a = { module: 'linux', view: 'flow', root: 'drivers/base', grain: 'file' }
    expect(parseViewerHash(viewerHash(a).replace('#/viewer/', ''))).toMatchObject(a)
  })

  it('writes no grain when the level is inferred', () => {
    expect(viewerHash({ module: 'linux', view: 'flow', root: 'drivers/base', grain: '' }))
      .not.toContain('grain=')
  })
})
