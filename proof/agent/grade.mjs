#!/usr/bin/env node
// Deterministic ANSWER-QUALITY grader — no model, no network, no judge.
//
// smoke.mjs checks what the ctx-optimize STORE prints. This checks what the
// AGENT ANSWERED, for every arm, by the identical rule — so a shell answer and
// a store answer are scored the same way and the score is reproducible.
//
// Per question, the questions file carries a `grade` block:
//   facts:  [{name, truth, any_of:[substr,...]}]  — one point each, hit if ANY
//           substring appears in the (lowercased) answer. `truth` is the
//           hand-verified file:line the fact came from; it is documentation,
//           never matched against.
//   forbid: [{name, any_of:[...]}]                — a false claim (e.g. "no
//           callers"). Each hit cancels one earned point.
// score = clamp(hits - forbidden, 0, facts.length) / facts.length
//
// Usage:
//   node grade.mjs --results DIR --name PREFIX --questions FILE [--json]
// Exit 0 always (this is a measurement, not a gate).

import fs from "node:fs";
import path from "node:path";

const args = parseArgs(process.argv.slice(2));
// --results accepts a comma-separated list of result dirs (repeat runs). The
// agent is not deterministic even at temperature 0 — tool-call order varies —
// so a single run is noise. Scores are averaged per question across runs.
const DIRS = String(args.results || die("--results required")).split(",").map((d) => path.resolve(d));
const NAME = args.name || die("--name required");
const QFILE = args.questions || die("--questions required");
const spec = JSON.parse(fs.readFileSync(QFILE, "utf8"));
const QS = new Map((spec.questions || []).map((q) => [q.id, q]));

const LABEL = { a: "shell", b: "ctx-optimize", c: "graphify" };
const ARMS = ["a", "b", "c"];

// ---- load records ----
const recs = [];
for (const DIR of DIRS) {
  for (const f of fs.readdirSync(DIR)) {
    const m = f.match(new RegExp(`^${NAME}-([abc])-(.+)\\.json$`));
    if (!m) continue;
    const [, arm, qid] = m;
    let j;
    try { j = JSON.parse(fs.readFileSync(path.join(DIR, f), "utf8")); }
    catch (e) { recs.push({ run: DIR, arm, qid, error: `unreadable record: ${e.message}` }); continue; }
    recs.push({ run: DIR, arm, qid, rec: j });
  }
}

// ---- grade ----
const graded = [];
for (const r of recs) {
  const q = QS.get(r.qid);
  if (!q || !q.grade) continue;
  if (r.error) { graded.push({ ...r, score: 0, hit: [], miss: [], bad: [], invalid: true }); continue; }
  const ans = String(r.rec.answer || "").toLowerCase();
  const hit = [], miss = [];
  for (const f of q.grade.facts) {
    (f.any_of.some((s) => ans.includes(s.toLowerCase())) ? hit : miss).push(f.name);
  }
  const bad = (q.grade.forbid || [])
    .filter((f) => f.any_of.some((s) => ans.includes(s.toLowerCase())))
    .map((f) => f.name);
  const n = q.grade.facts.length;
  const raw = Math.max(0, Math.min(n, hit.length - bad.length));
  graded.push({
    ...r, class: q.class, score: raw / n, n,
    hit, miss, bad,
    empty: ans.trim().length === 0,
    truncated: !!r.rec.truncated,
    toolErrors: r.rec.tool_errors || 0,
  });
}

if (args.json) {
  process.stdout.write(JSON.stringify(graded, null, 2) + "\n");
  process.exit(0);
}

// ---- report ----
const byArm = {};
for (const g of graded) {
  const a = (byArm[g.arm] ||= { score: 0, n: 0, wall: 0, tok: 0, cost: 0, steps: 0,
                                empty: 0, invalid: 0, falseClaims: 0, truncated: 0,
                                toolErrors: 0, byClass: {} });
  a.score += g.score; a.n++;
  if (g.empty) a.empty++;
  if (g.invalid) a.invalid++;
  if (g.truncated) a.truncated++;
  a.toolErrors += g.toolErrors || 0;
  a.falseClaims += g.bad.length;
  const c = (a.byClass[g.class] ||= { score: 0, n: 0 });
  c.score += g.score; c.n++;
  if (g.rec) {
    a.wall += g.rec.wall_s || 0;
    a.tok += g.rec.tokens?.total || 0;
    a.cost += g.rec.cost_usd || 0;
    a.steps += g.rec.steps || 0;
  }
}
const armsUsed = ARMS.filter((a) => byArm[a]);
const qids = [...new Set(graded.map((g) => g.qid))].sort();
const pctOf = (g) => `${Math.round(g.score * 100)}%`;

let out = `# Answer-quality benchmark — ${NAME}\n\n`;
out += `Deterministic grading: each question's answer is checked for hand-verified facts `;
out += `(function names / files, sourced by ripgrep against the checked-out tree). `;
out += `No LLM judge. Same rule for every arm. `;
out += `**${DIRS.length} run(s)** aggregated; per-question cells show the mean, with each run's score in parentheses when they disagree.\n\n`;

out += `## Per question\n\n| q | class | ${armsUsed.map((a) => LABEL[a]).join(" | ")} |\n`;
out += `|---|---|${armsUsed.map(() => "--:").join("|")}|\n`;
for (const qid of qids) {
  const cls = graded.find((g) => g.qid === qid)?.class || "";
  const cells = armsUsed.map((a) => {
    const gs = graded.filter((x) => x.qid === qid && x.arm === a);
    if (!gs.length) return "—";
    const mean = gs.reduce((s, g) => s + g.score, 0) / gs.length;
    let s = `${Math.round(mean * 100)}%`;
    if (gs.length > 1) {
      const each = gs.map((g) => Math.round(g.score * 100)).join("/");
      if (new Set(each.split("/")).size > 1) s += ` (${each})`;
    }
    if (gs.some((g) => g.bad.length)) s += " ⚠false";
    if (gs.some((g) => g.empty)) s += " ∅";
    return s;
  });
  out += `| ${qid} | ${cls} | ${cells.join(" | ")} |\n`;
}

out += `\n## Totals\n\n| metric | ${armsUsed.map((a) => LABEL[a]).join(" | ")} |\n`;
out += `|---|${armsUsed.map(() => "--:").join("|")}|\n`;
const row = (label, fn) => `| ${label} | ${armsUsed.map((a) => fn(byArm[a])).join(" | ")} |\n`;
out += row("**correctness**", (v) => `**${Math.round((v.score / v.n) * 100)}%**`);
const classes = [...new Set(graded.map((g) => g.class))].sort();
for (const c of classes) {
  out += row(`  · ${c} (${(byArm[armsUsed[0]].byClass[c]?.n ?? 0) / DIRS.length} q)`, (v) =>
    v.byClass[c] ? `${Math.round((v.byClass[c].score / v.byClass[c].n) * 100)}%` : "—");
}
out += row("false claims", (v) => `${v.falseClaims}`);
out += row("empty answers", (v) => `${v.empty}`);
out += row("hit step cap (no answer)", (v) => `${v.truncated}`);
out += row("tool errors", (v) => `${v.toolErrors}`);
out += row("failed runs", (v) => `${v.invalid}`);
const R = DIRS.length;
out += row(`wall s / run`, (v) => (v.wall / R).toFixed(1));
out += row(`tokens / run`, (v) => `${Math.round(v.tok / R)}`);
out += row(`cost / run`, (v) => `$${(v.cost / R).toFixed(4)}`);
out += row(`steps / run`, (v) => `${(v.steps / R).toFixed(1)}`);

out += `\n## Misses (what each arm failed to find)\n\n`;
for (const a of armsUsed) {
  const bad = graded.filter((g) => g.arm === a && (g.miss.length || g.bad.length));
  out += `**${LABEL[a]}**\n\n`;
  if (!bad.length) { out += `- (none)\n\n`; continue; }
  const seen = new Set();
  for (const g of bad.sort((x, y) => x.qid.localeCompare(y.qid))) {
    let line = `- \`${g.qid}\` missed: ${g.miss.join(", ") || "—"}`;
    if (g.bad.length) line += ` · **false claim: ${g.bad.join(", ")}**`;
    if (seen.has(line)) continue;
    seen.add(line);
    out += line + `\n`;
  }
  out += `\n`;
}
process.stdout.write(out);

function parseArgs(argv) {
  const o = {};
  for (let i = 0; i < argv.length; i++) {
    if (argv[i].startsWith("--")) {
      const k = argv[i].slice(2);
      const v = argv[i + 1] && !argv[i + 1].startsWith("--") ? argv[++i] : "true";
      o[k] = v;
    }
  }
  return o;
}
function die(m) { process.stderr.write(`grade: ${m}\n`); process.exit(2); }
