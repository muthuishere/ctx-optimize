// Same-origin calls back into this workspace. A relative fetch path is not a
// host, so it lands in the SAME namespace as the routes the workspace
// provides — which is the only way scope-by-join can fire (ADR
// 2026-08-15-scope-join-broken). The rule that reads these lives in
// .ctxoptimize/boundaries.json beside this fixture.

// `/orders` is provided by api/main.go — a DIFFERENT module of this workspace.
// This is the internal boundary the gate exists to prove.
export async function listOrders() {
  return fetch("/orders");
}

// `/status` is provided by web/app.ts, in this same module.
export async function status() {
  return fetch("/status");
}

// Nothing in this workspace provides `/nowhere`. The join MISSES, and a miss
// must emit no scope at all — "external" would be a claim we cannot make,
// because a relative path we do not serve may still be served by a proxy.
export async function nowhere() {
  return fetch("/nowhere");
}
