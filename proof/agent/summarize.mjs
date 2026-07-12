#!/usr/bin/env node
// Aggregate the per-run JSON records into a markdown table + headline deltas.
//   arm a = shell   ·   arm b = ctx-optimize   ·   arm c = graphify
// Deltas for b and c are measured against arm a (the shell baseline everyone
// has). arm c only appears if graphify records are present.
//   node summarize.mjs <results-dir> <name> <model>
import fs from "node:fs";
import path from "node:path";

const [dir, name, model] = process.argv.slice(2);
const ARMS = ["a", "b", "c"];
const LABEL = { a: "shell", b: "ctx-optimize", c: "graphify" };
const files = fs.readdirSync(dir).filter((f) => f.startsWith(`${name}-`) && f.endsWith(".json"));
const rec = {};
for (const f of files) {
  const m = f.match(new RegExp(`^${name}-([abc])-(.+)\\.json$`));
  if (!m) continue;
  const [, arm, qid] = m;
  try {
    const j = JSON.parse(fs.readFileSync(path.join(dir, f), "utf8"));
    (rec[qid] ||= {})[arm] = j;
  } catch { /* skip partial */ }
}

const qids = Object.keys(rec).sort();
const blank = () => ({ wall: 0, tok: 0, cost: 0, steps: 0, n: 0 });
const tot = { a: blank(), b: blank(), c: blank() };
const rows = [];
for (const qid of qids) {
  if (!rec[qid].a || !rec[qid].b) continue; // need at least the a/b pair
  rows.push({ qid, ...rec[qid] });
  for (const arm of ARMS) {
    const r = rec[qid][arm];
    if (!r) continue;
    tot[arm].wall += r.wall_s; tot[arm].tok += r.tokens.total;
    tot[arm].cost += r.cost_usd; tot[arm].steps += r.steps; tot[arm].n++;
  }
}
const haveC = tot.c.n > 0;
const armsUsed = haveC ? ARMS : ["a", "b"];

const pct = (base, v) => (base === 0 ? "—" : `${v <= base ? "−" : "+"}${Math.abs(Math.round(((v - base) / base) * 100))}%`);
const usd = (n) => `$${n.toFixed(4)}`;

let out = "";
out += `# Headless benchmark — ${name}\n\n`;
out += `Model: \`${model}\` · via OpenRouter · same model, same questions, one freshly-cloned repo.\n`;
out += `Arms: **a** shell-only · **b** ctx-optimize store first · ${haveC ? "**c** graphify store first" : "_(graphify not installed — arm c skipped)_"}.\n`;
out += `Tokens and cost are OpenRouter's own accounting (\`usage.include=true\`), not estimates. Deltas are vs arm a.\n\n`;

out += `| question | arm | wall s | tokens | cost | steps |\n|---|---|--:|--:|--:|--:|\n`;
for (const row of rows) {
  for (const arm of armsUsed) {
    const r = row[arm];
    if (!r) continue;
    out += `| ${row.qid} | ${arm} ${LABEL[arm]} | ${r.wall_s} | ${r.tokens.total} | ${usd(r.cost_usd)} | ${r.steps} |\n`;
  }
}
out += `\n`;

out += `## Totals (${rows.length} questions)\n\n`;
if (haveC) {
  out += `| metric | a shell | b ctx-optimize | c graphify | b vs a | c vs a |\n|---|--:|--:|--:|--:|--:|\n`;
  const line = (m, fmt) =>
    `| ${m.label} | ${fmt(tot.a[m.k])} | ${fmt(tot.b[m.k])} | ${fmt(tot.c[m.k])} | **${pct(tot.a[m.k], tot.b[m.k])}** | **${pct(tot.a[m.k], tot.c[m.k])}** |\n`;
  out += line({ label: "wall time", k: "wall" }, (v) => `${v.toFixed(1)}s`);
  out += line({ label: "tokens", k: "tok" }, (v) => `${v}`);
  out += line({ label: "cost", k: "cost" }, usd);
  out += line({ label: "steps", k: "steps" }, (v) => `${v}`);
} else {
  out += `| metric | a shell | b ctx-optimize | b vs a |\n|---|--:|--:|--:|\n`;
  const line = (label, k, fmt) =>
    `| ${label} | ${fmt(tot.a[k])} | ${fmt(tot.b[k])} | **${pct(tot.a[k], tot.b[k])}** |\n`;
  out += line("wall time", "wall", (v) => `${v.toFixed(1)}s`);
  out += line("tokens", "tok", (v) => `${v}`);
  out += line("cost", "cost", usd);
  out += line("steps", "steps", (v) => `${v}`);
}

process.stdout.write(out);
