// The default first move for "how does X work / where is X / what do I touch": ONE program does the whole investigation.

import { findCards } from "./find";
import { planEdit } from "./edit";
import { sweep } from "./sweep";

/** Investigate a topic in one call. `q` = {terms: "2-4 words", pattern: optional regex for a literal sweep, ext: ".go", path: "dir/", limit: 5}. Returns {symbols: [compact cards], plan: planEdit of the best symbol (or null), sweep: sweep result (or null)}. Add `pattern` for strings the graph does not index (config values, error text, comments). Read the cited file:line ranges next; do not grep again. */
export async function investigate(q) {
  const limit = q.limit || 5;
  const symbols = await findCards(q.terms, limit);
  let plan = null;
  if (symbols.length > 0) {
    try {
      plan = await planEdit(symbols[0].label);
    } catch (e) {
      plan = { error: String(e.message).split("\n")[0] };
    }
  }
  const sw = q.pattern ? await sweep(q.pattern, { ext: q.ext, path: q.path }) : null;
  return { symbols, plan, sweep: sw };
}
