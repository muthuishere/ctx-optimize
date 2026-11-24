// Orient in an unfamiliar repo: the hubs worth reading first, and how two symbols connect.

/** The `top` most-connected symbols of THIS repo (default 10; imported standard-library/external modules are left out) as {label, kind, file, in, out}. `in` = how many things depend on it. Start reading here. */
export async function orient(top = 10) {
  const h = await ops.ctx.hubs({ args: ["--top", String(top * 4)] });
  return h.hubs.filter((x) => x.node.kind !== "module").slice(0, top).map((x) => ({
    label: x.node.label,
    kind: x.node.kind,
    file: x.node.source,
    in: x.in,
    out: x.out,
  }));
}

/** How `a` and `b` (exact labels) connect: one line per hop, "from -relation-> to", or an empty list when no path exists. Throws if either label is not found exactly. */
export async function connect(a, b) {
  const p = await ops.ctx.path({ args: [a, b] });
  return (p.steps || []).map((s) => (s.dir === "<-" ? s.to + " -" + s.relation + "-> " + s.from : s.from + " -" + s.relation + "-> " + s.to));
}
