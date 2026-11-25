// Check citations before a human acts on them.

/** Verify several claims at once (node id, exact label, or "file:L10-L20"). Returns {ok: bool, claims: [{claim, holds, reason}]}. A claim that fails is reported, never thrown. */
export async function verifyClaims(claims) {
  const rows = await Promise.all(claims.map(async (c) => {
    try {
      await ops.ctx.verify({ args: [c] });
      return { claim: c, holds: true, reason: "" };
    } catch (e) {
      return { claim: c, holds: false, reason: String(e.message).split("\n")[0] };
    }
  }));
  return { ok: rows.every((r) => r.holds), claims: rows };
}
