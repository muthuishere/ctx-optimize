#!/usr/bin/env node
// Aggregate the per-run JSON records into a markdown table + the three
// headline deltas (time, tokens, cost). Arm a = shell, arm b = store.
//   node summarize.mjs <results-dir> <name> <model>
import fs from "node:fs";
import path from "node:path";

const [dir, name, model] = process.argv.slice(2);
const files = fs.readdirSync(dir).filter((f) => f.startsWith(`${name}-`) && f.endsWith(".json"));
const rec = {};
for (const f of files) {
  const m = f.match(new RegExp(`^${name}-([ab])-(.+)\\.json$`));
  if (!m) continue;
  const [, arm, qid] = m;
  try {
    const j = JSON.parse(fs.readFileSync(path.join(dir, f), "utf8"));
    (rec[qid] ||= {})[arm] = j;
  } catch { /* skip partial */ }
}

const qids = Object.keys(rec).sort();
const rows = [];
const tot = { a: { wall: 0, tok: 0, cost: 0, steps: 0 }, b: { wall: 0, tok: 0, cost: 0, steps: 0 } };
for (const qid of qids) {
  const a = rec[qid].a, b = rec[qid].b;
  if (!a || !b) continue;
  for (const [arm, r] of [["a", a], ["b", b]]) {
    tot[arm].wall += r.wall_s; tot[arm].tok += r.tokens.total;
    tot[arm].cost += r.cost_usd; tot[arm].steps += r.steps;
  }
  rows.push({ qid, a, b });
}

const pct = (a, b) => (a === 0 ? "—" : `${b <= a ? "−" : "+"}${Math.abs(Math.round(((b - a) / a) * 100))}%`);
const usd = (n) => `$${n.toFixed(4)}`;

let out = "";
out += `# Headless benchmark — ${name}\n\n`;
out += `Model: \`${model}\` · via OpenRouter · arm **a** = shell-only, arm **b** = ctx-optimize store first.\n`;
out += `Tokens and cost are OpenRouter's own accounting (\`usage.include=true\`), not estimates.\n\n`;

out += `| question | arm | wall s | tokens | cost | steps |\n|---|---|--:|--:|--:|--:|\n`;
for (const { qid, a, b } of rows) {
  out += `| ${qid} | a shell | ${a.wall_s} | ${a.tokens.total} | ${usd(a.cost_usd)} | ${a.steps} |\n`;
  out += `| ${qid} | b store | ${b.wall_s} | ${b.tokens.total} | ${usd(b.cost_usd)} | ${b.steps} |\n`;
}
out += `\n`;

out += `## Totals (${rows.length} questions)\n\n`;
out += `| | arm a (shell) | arm b (store) | delta |\n|---|--:|--:|--:|\n`;
out += `| wall time | ${tot.a.wall.toFixed(1)}s | ${tot.b.wall.toFixed(1)}s | **${pct(tot.a.wall, tot.b.wall)}** |\n`;
out += `| tokens | ${tot.a.tok} | ${tot.b.tok} | **${pct(tot.a.tok, tot.b.tok)}** |\n`;
out += `| cost | ${usd(tot.a.cost)} | ${usd(tot.b.cost)} | **${pct(tot.a.cost, tot.b.cost)}** |\n`;
out += `| steps | ${tot.a.steps} | ${tot.b.steps} | **${pct(tot.a.steps, tot.b.steps)}** |\n`;

process.stdout.write(out);
