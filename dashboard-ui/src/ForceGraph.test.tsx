import { describe, expect, it, beforeAll, vi } from 'vitest'
import { render, cleanup } from '@testing-library/react'
import ForceGraph from './ForceGraph'
import type { Edge, Node } from './types'

// jsdom has no 2D canvas. Stub getContext with a no-op recorder so the
// component's mount effect runs its REAL code path (resize → requestDraw →
// wake → loop) instead of bailing on a null context. We are testing that the
// effect executes without throwing, not what it paints.
beforeAll(() => {
  const noop = () => {}
  const ctx2d = new Proxy(
    {
      canvas: null,
      measureText: () => ({ width: 10 }),
      getImageData: () => ({ data: new Uint8ClampedArray(4) }),
      createLinearGradient: () => ({ addColorStop: noop }),
    } as Record<string, unknown>,
    // any 2D call the renderer makes resolves to a no-op function
    { get: (t, k) => (k in t ? t[k as string] : noop) },
  )
  // @ts-expect-error - test stub, jsdom lacks canvas
  HTMLCanvasElement.prototype.getContext = vi.fn(() => ctx2d)
  // jsdom gives every element a 0x0 rect; give the canvas a real size so the
  // layout/fit math runs on non-degenerate numbers.
  HTMLElement.prototype.getBoundingClientRect = vi.fn(
    () => ({ x: 0, y: 0, width: 800, height: 600, top: 0, left: 0, right: 800, bottom: 600, toJSON: noop }) as DOMRect,
  )
})

function graph(n: number): { nodes: Node[]; edges: Edge[] } {
  const nodes: Node[] = Array.from({ length: n }, (_, i) => ({
    id: `pkg/file${i}.go::Sym${i}`,
    label: `Sym${i}`,
    kind: i % 3 === 0 ? 'function' : i % 3 === 1 ? 'file' : 'route',
    source: `pkg/file${i}.go`,
  })) as Node[]
  const edges: Edge[] = nodes.slice(1).map((node, i) => ({
    source: nodes[i].id,
    target: node.id,
    relation: 'calls',
  })) as Edge[]
  return { nodes, edges }
}

describe('ForceGraph mount', () => {
  // REGRESSION (v0.3.6): the mount effect called resize() synchronously, which
  // ran requestDraw → wake → requestAnimationFrame(loop) while `loop` was still
  // a not-yet-initialized `const` 160 lines below. That threw
  // "Cannot access 'loop' before initialization" (minified: 'de') on EVERY
  // store and took the whole Viewer down through the error boundary. Mounting
  // the component is the only thing that reproduces it — this test fails
  // outright if `loop` ever goes back to a non-hoisted binding.
  it('mounts and runs its effect without throwing (no TDZ in the RAF loop)', () => {
    const { nodes, edges } = graph(12)
    expect(() =>
      render(
        <ForceGraph
          nodes={nodes}
          edges={edges}
          colors={new Map([['function', '#4ade80'], ['file', '#22d3ee'], ['route', '#a3e635']])}
          selectedId={null}
          onSelect={() => {}}
        />,
      ),
    ).not.toThrow()
    cleanup()
  })

  it('renders a canvas', () => {
    const { nodes, edges } = graph(5)
    const { container } = render(
      <ForceGraph nodes={nodes} edges={edges} colors={new Map()} selectedId={null} onSelect={() => {}} />,
    )
    expect(container.querySelector('canvas')).not.toBeNull()
    cleanup()
  })

  it('survives an empty graph', () => {
    expect(() =>
      render(<ForceGraph nodes={[]} edges={[]} colors={new Map()} selectedId={null} onSelect={() => {}} />),
    ).not.toThrow()
    cleanup()
  })

  it('drives the animation loop without throwing', async () => {
    const { nodes, edges } = graph(20)
    render(
      <ForceGraph nodes={nodes} edges={edges} colors={new Map()} selectedId={null} onSelect={() => {}} />,
    )
    // let real rAF frames run: the loop body (step/draw) executes here, so a
    // throw inside it surfaces as an unhandled error rather than passing.
    await new Promise((r) => setTimeout(r, 120))
    cleanup()
  })
})
