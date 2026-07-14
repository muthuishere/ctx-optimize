// API client. Reads are plain fetches; mutations carry the per-process CSRF
// token (GET /api/token, loopback-only server-side) in X-Ctx-Token and are
// refused by the server for non-loopback peers regardless of this header.

export async function api<T>(path: string): Promise<T> {
  const r = await fetch(path)
  const j = await r.json()
  if (!r.ok) throw new Error(j.error || r.statusText)
  return j as T
}

let tok: string | null = null

async function token(): Promise<string> {
  if (!tok) tok = (await api<{ token: string }>('/api/token')).token
  return tok
}

export async function mutate<T>(method: string, path: string, body: unknown): Promise<T> {
  const r = await fetch(path, {
    method,
    headers: { 'X-Ctx-Token': await token(), 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  const j = await r.json()
  if (!r.ok) throw new Error(j.error || r.statusText)
  return j as T
}

// stream POSTs a body and feeds the chunked text/plain progress back as it
// arrives (onboarding, re-gather, remote sync). The final line is DONE or
// ERROR: … — surface it as-is.
export async function stream(
  path: string,
  body: unknown,
  onChunk: (text: string) => void,
): Promise<void> {
  const r = await fetch(path, {
    method: 'POST',
    headers: { 'X-Ctx-Token': await token(), 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  const ct = r.headers.get('content-type') || ''
  if (!r.ok && ct.includes('json')) {
    const j = await r.json()
    throw new Error(j.error || r.statusText)
  }
  if (!r.body) {
    onChunk(await r.text())
    return
  }
  const reader = r.body.getReader()
  const dec = new TextDecoder()
  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    onChunk(dec.decode(value, { stream: true }))
  }
}
