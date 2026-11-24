// Helpers shared by the ctx modules: run one lookup per name without letting a
// single miss reject the whole fan-out.

/** Run `fn(name)` for every name in parallel. Returns {ok: [...results], missing: [{name, error}]}; a name the store cannot resolve exactly lands in `missing` instead of throwing. */
export async function settle(names, fn) {
  const rows = await Promise.all(names.map(async (name) => {
    try {
      return { name, value: await fn(name) };
    } catch (e) {
      return { name, error: String(e.message).split("\n")[0] };
    }
  }));
  return {
    ok: rows.filter((r) => !r.error).map((r) => r.value),
    missing: rows.filter((r) => r.error).map((r) => ({ name: r.name, error: r.error })),
  };
}
