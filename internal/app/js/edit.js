// Before editing: one compact plan per symbol (signature, callers, blast radius, tests to run).

import { settle } from "./safe";

/** Plan an edit to ONE symbol (exact label). Returns {symbol, signature, file, location, callers: [ids], calls: n, impactedFiles: [paths], tests: [ids], coChanged: [paths], confidence}. `callers` is a FLOOR for methods. Throws if the label is not found exactly. */
export async function planEdit(symbol) {
  const r = (await ops.ctx.change_plan({ args: [symbol] })).result;
  const files = new Set();
  for (const i of r.impacts || []) if (i.node.source) files.add(i.node.source);
  return {
    symbol: r.target.label,
    signature: r.signature,
    file: r.target.source,
    location: r.target.location,
    callers: r.called_by || [],
    calls: (r.calls || []).length,
    impactedFiles: [...files].sort(),
    tests: r.tests || [],
    coChanged: r.co_changes || [],
    confidence: r.confidence,
  };
}

/** Plan edits to SEVERAL symbols at once. Returns {plans: [planEdit result...], missing: [{name, error}], testsToRun: [unique test ids across all plans], filesTouched: [unique files across all plans, targets included]}. A misspelled symbol is reported under `missing`, never thrown. */
export async function planEdits(symbols) {
  const { ok, missing } = await settle(symbols, planEdit);
  const tests = new Set();
  const files = new Set();
  for (const p of ok) {
    for (const t of p.tests) tests.add(t);
    files.add(p.file);
    for (const f of p.impactedFiles) files.add(f);
  }
  return { plans: ok, missing, testsToRun: [...tests].sort(), filesTouched: [...files].sort() };
}
