// Blast radius across several symbols: which files break, merged and deduplicated.

import { settle } from "./safe";

/** Reverse impact of every symbol in `symbols` (exact labels) to `depth` hops (default 2). Returns {files: [{file, dependsOn: [symbols]}] sorted by how many symbols reach the file, missing: [{name, error}]}. A method's reach is a FLOOR. */
export async function blastRadius(symbols, depth = 2) {
  const { ok, missing } = await settle(symbols, async (s) => {
    const a = await ops.ctx.affected({ args: [s, "--depth", String(depth)] });
    return { symbol: s, files: [...new Set(a.affected.map((x) => x.node.source).filter((f) => f))] };
  });
  const byFile = {};
  for (const r of ok) {
    for (const f of r.files) {
      byFile[f] = byFile[f] || [];
      byFile[f].push(r.symbol);
    }
  }
  const files = Object.keys(byFile)
    .map((file) => ({ file, dependsOn: byFile[file] }))
    .sort((a, b) => b.dependsOn.length - a.dependsOn.length || (a.file < b.file ? -1 : 1));
  return { files, missing };
}
