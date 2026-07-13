#!/usr/bin/env node
// Deterministic smoke checker — the FREE half of the benchmark.
//
// For every question in a questions file that carries a `smoke` spec, run the
// ctx-optimize argv against the already-built store (no model, no network) and
// assert every `expect` substring appears in the output. `cwd` (repo-relative)
// runs the command from a module subdir — proving scope resolution and
// escalation, not just lookup.
//
// Usage: node smoke.mjs --repo /path/to/clone --bin /path/to/ctx-optimize \
//                       --questions questions-monorepo.json
// Exit 0 = all pass; 1 = any fail. Prints one PASS/FAIL line per check.

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

const args = parseArgs(process.argv.slice(2));
const REPO = path.resolve(args.repo || die("--repo required"));
const BIN = args.bin || "ctx-optimize";
const QFILE = args.questions || die("--questions required");

const spec = JSON.parse(fs.readFileSync(QFILE, "utf8"));
const checks = (spec.questions || []).filter((q) => q.smoke);
if (checks.length === 0) die(`no smoke specs in ${QFILE}`);

let failed = 0;
for (const q of checks) {
  const cwd = q.smoke.cwd ? path.join(REPO, q.smoke.cwd) : REPO;
  let out;
  try {
    out = execFileSync(BIN, q.smoke.argv, {
      cwd, encoding: "utf8", timeout: 60000, maxBuffer: 32 * 1024 * 1024,
      stdio: ["ignore", "pipe", "pipe"],
    });
  } catch (e) {
    out = `${e.stdout || ""}\n${e.stderr || e.message || e}`;
  }
  const missing = q.smoke.expect.filter((s) => !out.includes(s));
  const where = q.smoke.cwd ? ` (from ${q.smoke.cwd})` : "";
  if (missing.length === 0) {
    console.log(`PASS  ${q.id}  ${q.smoke.argv.join(" ")}${where}`);
  } else {
    failed++;
    console.log(`FAIL  ${q.id}  ${q.smoke.argv.join(" ")}${where}`);
    console.log(`      missing: ${missing.join(" · ")}`);
    console.log(`      got: ${out.slice(0, 400).replace(/\n/g, "\n      ")}`);
  }
}
console.log(`\n${checks.length - failed}/${checks.length} smoke checks passed`);
process.exit(failed ? 1 : 0);

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
function die(m) { process.stderr.write(`smoke: ${m}\n`); process.exit(2); }
