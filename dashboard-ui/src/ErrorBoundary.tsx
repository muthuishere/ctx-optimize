import { Component, type ReactNode } from 'react'

// ErrorBoundary is the last line of defense for the whole UI: a single render
// throw — a graph node with an unexpected shape, a bad character in an id, a
// malformed field from a corrupt store — must NOT blank the entire page (the
// default React behavior is to unmount the whole tree). It catches the error,
// shows a readable fallback with a retry, and keeps the shell alive so the
// user can switch tabs and carry on. Keyed per-route in App so navigating away
// and back clears a crashed screen.
interface Props {
  children: ReactNode
  label?: string // what failed, for the fallback copy ("the viewer", "this screen")
}
interface State {
  error: Error | null
}

export default class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null }

  static getDerivedStateFromError(error: Error): State {
    return { error }
  }

  componentDidCatch(error: Error, info: unknown) {
    // Surface it in the console for anyone debugging; never rethrow.
    console.error('ctx-optimize dashboard caught a render error:', error, info)
  }

  render() {
    const { error } = this.state
    if (!error) return this.props.children
    const what = this.props.label || 'this view'
    return (
      <div className="errboundary">
        <div className="errboundary-card">
          <div className="errboundary-title">Couldn’t render {what}</div>
          <div className="errboundary-msg">
            Something in the data tripped up the renderer — the rest of the
            dashboard still works. Try again, or switch tabs.
          </div>
          <pre className="errboundary-detail">{String(error?.message || error)}</pre>
          <div className="errboundary-actions">
            <button onClick={() => this.setState({ error: null })}>Try again</button>
            <button onClick={() => window.location.reload()}>Reload</button>
          </div>
        </div>
      </div>
    )
  }
}
