// Find symbols by words and inspect them in one step: query, then a card for each hit.

/** Find the best `limit` symbols for 2-4 search terms and return one compact record each: {label, kind, file, location, signature, calledBy}. `calledBy` is a FLOOR when the symbol is a method (unattributed callers are not counted). Hits whose card cannot be resolved exactly are skipped. */
export async function findCards(terms, limit = 5) {
  const q = await ops.ctx.query({ args: [terms, "--budget", "1500"] });
  const hits = q.hits.slice(0, limit);
  const cards = await Promise.all(hits.map(async (h) => {
    try {
      const c = await ops.ctx.card({ args: [h.node.label] });
      return {
        label: c.node.label,
        kind: c.node.kind,
        file: c.node.source,
        location: c.node.location,
        signature: c.signature,
        calledBy: (c.called_by || []).length,
      };
    } catch (e) {
      return null;
    }
  }));
  return cards.filter((c) => c !== null);
}
