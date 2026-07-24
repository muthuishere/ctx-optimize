#!/usr/bin/env node
// Headless benchmark agent — OpenRouter, no Anthropic weekly limit involved.
//
// One question, one model, run against the same cloned repo under one of three
// arms:
//   arm a — shell only (grep/rg/find/sed/cat) — the agent everyone has
//   arm b — ctx-optimize store first, shell only to fill gaps
//   arm c — graphify store first, shell only to fill gaps  (the competitor)
// and report, per arm: wall seconds, prompt/completion tokens, dollar cost
// (OpenRouter's own accounting), and tool-call count (steps). Token and cost
// numbers come from OpenRouter's `usage` block with `usage.include=true`, so
// they are the provider's bill, not our estimate.
//
// Usage:
//   OPENROUTER_API_KEY=... node agent.mjs \
//     --repo /path/to/clone --bin /path/to/ctx-optimize \
//     --arm a|b|c --q "question text" [--model openai/gpt-4o-mini] [--max-steps 12]
//     [--graphify-bin graphify]
//
// Prints one JSON record to stdout. The key is read from the environment only
// (never logged). Deterministic-ish: temperature 0.

import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

const args = parseArgs(process.argv.slice(2));
const KEY = process.env.OPENROUTER_API_KEY;
if (!KEY) { die("OPENROUTER_API_KEY not set in environment"); }
const REPO = path.resolve(args.repo || die("--repo required"));
const BIN = args.bin || "ctx-optimize";
const GBIN = args["graphify-bin"] || "graphify";
const ARM = args.arm || "b";
const Q = args.q || die("--q required");
const MODEL = args.model || "openai/gpt-4o-mini";
const MAX_STEPS = parseInt(args["max-steps"] || "12", 10);
const OUT_CAP = 8000; // truncate any single tool output fed back to the model

const SYS_A =
  "You answer questions about the code in the current repository. You may use " +
  "ONLY the run_shell tool (grep, rg, find, sed, cat, head, ls). Find the " +
  "answer, then reply concisely with the concrete function/type names and " +
  "file:line citations. Do not modify anything. Stop as soon as you can answer.";

const SYS_B =
  "You answer questions about the code in the current repository. A ctx-optimize " +
  "knowledge store for this repo is ALREADY BUILT. Prefer it over reading files:\n" +
  "  query \"<terms>\"  -> ranked symbol hits with signatures + neighbors\n" +
  "  card <symbol>    -> signature, doc, callers, callees (no file read)\n" +
  "  affected <symbol>-> what depends on it (blast radius)\n" +
  "  path <a> <b>     -> how two symbols connect\n" +
  "  explain <symbol> -> relationships around a symbol\n" +
  "Call the ctx_optimize tool FIRST. Use run_shell only to fill a specific gap " +
  "the store leaves. Reply concisely with function/type names and file:line " +
  "citations. Do not modify anything. Stop as soon as you can answer.";

const SYS_C =
  "You answer questions about the code in the current repository. A graphify " +
  "knowledge graph for this repo is ALREADY BUILT (graphify-out/). Prefer it " +
  "over reading files:\n" +
  "  query \"<question>\" -> graph traversal of nodes related to the question\n" +
  "  explain <node>     -> a node and its neighbors\n" +
  "  affected <node>    -> nodes impacted by it\n" +
  "  path <a> <b>       -> how two nodes connect\n" +
  "Call the graphify tool FIRST. Use run_shell only to fill a specific gap the " +
  "graph leaves. Reply concisely with function/type names and file:line " +
  "citations. Do not modify anything. Stop as soon as you can answer.";

const TOOLS_A = [shellTool()];
const TOOLS_B = [ctxTool(), shellTool()];
const TOOLS_C = [graphifyTool(), shellTool()];
const ARM_CFG = {
  a: { sys: SYS_A, tools: TOOLS_A },
  b: { sys: SYS_B, tools: TOOLS_B },
  c: { sys: SYS_C, tools: TOOLS_C },
};

function shellTool() {
  return {
    type: "function",
    function: {
      name: "run_shell",
      description:
        "Run a read-only shell command in the repository root to search and " +
        "read files (grep, rg, find, sed, cat, head, ls, wc). No writes.",
      parameters: {
        type: "object",
        properties: { command: { type: "string", description: "the shell command" } },
        required: ["command"],
      },
    },
  };
}
function ctxTool() {
  return {
    type: "function",
    function: {
      name: "ctx_optimize",
      description:
        "Query the prebuilt ctx-optimize knowledge store for this repo. Pass " +
        "the sub-command and its arguments, e.g. [\"query\",\"refund flow\"], " +
        "[\"card\",\"ChargeService\"], [\"affected\",\"User\"], " +
        "[\"path\",\"A\",\"B\"], [\"explain\",\"X\"]. Returns ranked, cited results.",
      parameters: {
        type: "object",
        properties: {
          argv: {
            type: "array",
            items: { type: "string" },
            description: "sub-command + args, without the leading 'ctx-optimize'",
          },
        },
        required: ["argv"],
      },
    },
  };
}

function graphifyTool() {
  return {
    type: "function",
    function: {
      name: "graphify",
      description:
        "Query the prebuilt graphify knowledge graph for this repo. Pass the " +
        "sub-command and its arguments, e.g. [\"query\",\"refund flow\"], " +
        "[\"explain\",\"ChargeService\"], [\"affected\",\"User\"], " +
        "[\"path\",\"A\",\"B\"]. Returns related graph nodes and edges.",
      parameters: {
        type: "object",
        properties: {
          argv: {
            type: "array",
            items: { type: "string" },
            description: "sub-command + args, without the leading 'graphify'",
          },
        },
        required: ["argv"],
      },
    },
  };
}

function runShell(command) {
  try {
    return execFileSync("/bin/sh", ["-c", command], {
      cwd: REPO, encoding: "utf8", timeout: 30000, maxBuffer: 32 * 1024 * 1024,
      stdio: ["ignore", "pipe", "pipe"],
    });
  } catch (e) {
    return `[shell error] ${e.stderr || e.message || e}`.slice(0, OUT_CAP);
  }
}
function runCtx(argv) {
  const safe = Array.isArray(argv) ? argv.map(String) : [String(argv)];
  // always ask for JSON when the sub-command supports it
  const sub = safe[0] || "";
  if (["query", "ask", "card", "explain", "affected", "path", "hubs", "status"].includes(sub)
      && !safe.includes("--json")) safe.push("--json");
  try {
    return execFileSync(BIN, safe, {
      cwd: REPO, encoding: "utf8", timeout: 30000, maxBuffer: 32 * 1024 * 1024,
      stdio: ["ignore", "pipe", "pipe"],
    });
  } catch (e) {
    return `[ctx-optimize error] ${e.stderr || e.message || e}`.slice(0, OUT_CAP);
  }
}
function runGraphify(argv) {
  const safe = Array.isArray(argv) ? argv.map(String) : [String(argv)];
  try {
    return execFileSync(GBIN, safe, {
      cwd: REPO, encoding: "utf8", timeout: 30000, maxBuffer: 32 * 1024 * 1024,
      stdio: ["ignore", "pipe", "pipe"],
    });
  } catch (e) {
    return `[graphify error] ${e.stderr || e.message || e}`.slice(0, OUT_CAP);
  }
}

async function chat(messages, tools) {
  const res = await fetch("https://openrouter.ai/api/v1/chat/completions", {
    method: "POST",
    headers: {
      Authorization: `Bearer ${KEY}`,
      "Content-Type": "application/json",
      "HTTP-Referer": "https://github.com/muthuishere/ctx-optimize",
      "X-Title": "ctx-optimize benchmark",
    },
    body: JSON.stringify({
      model: MODEL,
      temperature: 0,
      messages,
      tools,
      tool_choice: "auto",
      usage: { include: true }, // <- OpenRouter returns real $ cost in usage
    }),
  });
  if (!res.ok) {
    const t = await res.text();
    throw new Error(`OpenRouter ${res.status}: ${t.slice(0, 500)}`);
  }
  return res.json();
}

async function main() {
  const cfg = ARM_CFG[ARM] || die(`unknown arm ${ARM}`);
  const messages = [
    { role: "system", content: cfg.sys },
    { role: "user", content: `Question: ${Q}` },
  ];

  const usage = { prompt: 0, completion: 0, total: 0, cost: 0 };
  const calls = { run_shell: 0, ctx_optimize: 0, graphify: 0 };
  let answer = "";
  const t0 = Date.now();

  for (let step = 0; step < MAX_STEPS; step++) {
    const resp = await chat(messages, cfg.tools);
    const u = resp.usage || {};
    usage.prompt += u.prompt_tokens || 0;
    usage.completion += u.completion_tokens || 0;
    usage.total += u.total_tokens || 0;
    usage.cost += u.cost || 0;

    const msg = resp.choices?.[0]?.message;
    if (!msg) throw new Error("no message in response");
    messages.push(msg);

    const toolCalls = msg.tool_calls || [];
    if (toolCalls.length === 0) { answer = msg.content || ""; break; }

    for (const tc of toolCalls) {
      const name = tc.function?.name;
      let out = "";
      try {
        const a = JSON.parse(tc.function?.arguments || "{}");
        if (name === "run_shell") { calls.run_shell++; out = runShell(a.command || ""); }
        else if (name === "ctx_optimize") { calls.ctx_optimize++; out = runCtx(a.argv || []); }
        else if (name === "graphify") { calls.graphify++; out = runGraphify(a.argv || []); }
        else out = `[unknown tool ${name}]`;
      } catch (e) {
        out = `[tool arg parse error] ${e.message}`;
      }
      messages.push({
        role: "tool", tool_call_id: tc.id, name,
        content: String(out).slice(0, OUT_CAP),
      });
    }
  }

  const wall = (Date.now() - t0) / 1000;
  const record = {
    model: MODEL, arm: ARM, question: Q,
    wall_s: round(wall, 2),
    steps: calls.run_shell + calls.ctx_optimize + calls.graphify,
    tool_calls: calls,
    tokens: { prompt: usage.prompt, completion: usage.completion, total: usage.total },
    cost_usd: round(usage.cost, 6),
    answer: answer.trim(),
  };
  process.stdout.write(JSON.stringify(record, null, 2) + "\n");
}

// ---- helpers ----
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
function round(n, d) { const f = 10 ** d; return Math.round(n * f) / f; }
function die(m) { process.stderr.write(`agent: ${m}\n`); process.exit(2); }

main().catch((e) => { process.stderr.write(`agent: ${e.stack || e}\n`); process.exit(1); });
