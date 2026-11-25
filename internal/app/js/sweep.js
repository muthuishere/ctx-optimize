// Literal and regex sweeps of the source tree (what agents otherwise do with repeated grep), capped and grouped.

/** Sweep the source tree for ONE regex (Go RE2). `opts` = {ext: ".go,.ts", path: "dir/", maxFiles: 15, maxLines: 3}. Returns {pattern, count, fileCount, files: [{file, lines: [{line, text}]}], truncated}. Files are sorted by match count; text is trimmed to 160 chars. */
export async function sweep(pattern, opts = {}) {
  const args = [pattern];
  if (opts.ext) args.push("--ext", opts.ext);
  if (opts.path) args.push("--path", opts.path);
  const hits = await ops.ctx.search({ args });
  const byFile = {};
  for (const h of hits) {
    byFile[h.file] = byFile[h.file] || [];
    byFile[h.file].push({ line: h.line, text: String(h.text).trim().slice(0, 160) });
  }
  const maxFiles = opts.maxFiles || 15;
  const maxLines = opts.maxLines || 3;
  const files = Object.keys(byFile)
    .map((file) => ({ file, n: byFile[file].length, lines: byFile[file] }))
    .sort((a, b) => b.n - a.n || (a.file < b.file ? -1 : 1));
  return {
    pattern,
    count: hits.length,
    fileCount: files.length,
    files: files.slice(0, maxFiles).map((f) => ({ file: f.file, lines: f.lines.slice(0, maxLines) })),
    truncated: files.length > maxFiles,
  };
}

/** Sweep SEVERAL regexes in parallel with the same `opts` and return {results: {pattern: sweep result}}. A bad regex is reported as {error} for that pattern only. Use it instead of repeating grep. */
export async function sweepMany(patterns, opts = {}) {
  const rows = await Promise.all(patterns.map(async (p) => {
    try {
      return [p, await sweep(p, opts)];
    } catch (e) {
      return [p, { error: String(e.message).split("\n")[0] }];
    }
  }));
  const results = {};
  for (const r of rows) results[r[0]] = r[1];
  return { results };
}
