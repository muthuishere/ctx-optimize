#!/usr/bin/env python3
"""Honest head-to-head: ctx-optimize vs graphify, deterministic paths only.

Methodology notes (these go on the site verbatim):
- graphify timed with `update <path> --no-cluster` = its pure extraction+graph
  build, NO clustering, NO LLM. This is graphify's FASTEST honest path.
- ctx-optimize timed with `add <path>` which does MORE work: extraction +
  graph + prune + full markdown wiki regeneration. If ctx-optimize still wins,
  the comparison is conservative in graphify's favor.
- cold = no prior output; warm = immediate second run on unchanged tree.
- query timing = median of 5 runs, same question, budget 2000, both tools.
"""
import json, os, shutil, statistics, subprocess, sys, time

S = os.path.dirname(os.path.abspath(__file__))
CTX = "/Users/muthuishere/muthu/gitworkspace/ctx-optimize/bin/ctx-optimize"
GRAPHIFY = os.path.expanduser("~/.local/bin/graphify")

CORPORA = ["corpus-flask", "corpus-gin", "corpus-ctx-src", "corpus-graphify-src"]
QUERY_CORPUS = "corpus-graphify-src"
QUERIES = ["how does clustering work", "remote push pull hooks", "community labeling"]

def run(cmd, env=None, cwd=None):
    t0 = time.perf_counter()
    p = subprocess.run(cmd, capture_output=True, text=True, env=env, cwd=cwd)
    dt = time.perf_counter() - t0
    return dt, p

def dir_size(path):
    total = 0
    for root, _, files in os.walk(path):
        for f in files:
            try: total += os.path.getsize(os.path.join(root, f))
            except OSError: pass
    return total

results = {"machine": "Apple M5 Pro, 18 cores, 48 GB", "corpora": {}}
dt, p = run([CTX, "version"]); results["ctx_version"] = p.stdout.strip()
dt, p = run([GRAPHIFY, "--version"]); results["graphify_version"] = (p.stdout + p.stderr).strip()

env_base = {k: v for k, v in os.environ.items()
            if not any(x in k for x in ("KEY", "TOKEN", "SECRET", "PASSWORD"))}

for corpus in CORPORA:
    cdir = os.path.join(S, corpus)
    entry = {}
    n_files = sum(len(fs) for _, _, fs in os.walk(cdir))
    entry["files"] = n_files

    # ---- ctx-optimize ----
    store = os.path.join(S, "ctxstore-" + corpus)
    shutil.rmtree(store, ignore_errors=True)
    env = dict(env_base, CTX_OPTIMIZE_STORE=store)
    dt_cold, p = run([CTX, "add", cdir], env=env)
    if p.returncode != 0:
        entry["ctx_error"] = p.stderr[-500:]
    dt_warm, _ = run([CTX, "add", cdir], env=env)
    dt2, p = run([CTX, "status", "--path", cdir, "--json"], env=env)
    st = json.loads(p.stdout) if p.returncode == 0 else {}
    entry["ctx"] = {
        "cold_s": round(dt_cold, 2), "warm_s": round(dt_warm, 2),
        "nodes": st.get("nodes"), "edges": st.get("edges"),
        "store_bytes": dir_size(store),
        "note": "add includes prune + full wiki regeneration",
    }

    # ---- graphify ----
    gout = os.path.join(cdir, "graphify-out")
    shutil.rmtree(gout, ignore_errors=True)
    dt_cold, p = run([GRAPHIFY, "update", cdir, "--no-cluster"], env=env_base)
    gerr = None
    if p.returncode != 0:
        gerr = (p.stderr or p.stdout)[-500:]
    dt_warm, _ = run([GRAPHIFY, "update", cdir, "--no-cluster"], env=env_base)
    g_nodes = g_edges = None
    gpath = os.path.join(gout, "graph.json")
    if os.path.exists(gpath):
        with open(gpath) as f:
            g = json.load(f)
        g_nodes = len(g.get("nodes", []))
        g_edges = len(g.get("edges", g.get("links", [])))
    entry["graphify"] = {
        "cold_s": round(dt_cold, 2), "warm_s": round(dt_warm, 2),
        "nodes": g_nodes, "edges": g_edges,
        "out_bytes": dir_size(gout) if os.path.isdir(gout) else None,
        "error": gerr,
        "note": "update --no-cluster = extraction only, no clustering/LLM (its fastest path)",
    }
    results["corpora"][corpus] = entry
    print(f"{corpus}: ctx cold {entry['ctx']['cold_s']}s / graphify cold {entry['graphify']['cold_s']}s", flush=True)

# ---- query latency (median of 5) ----
qdir = os.path.join(S, QUERY_CORPUS)
qstore = os.path.join(S, "ctxstore-" + QUERY_CORPUS)
env = dict(env_base, CTX_OPTIMIZE_STORE=qstore)
qres = []
for q in QUERIES:
    ts = []
    for _ in range(5):
        dt, p = run([CTX, "query", q, "--path", qdir, "--budget", "2000"], env=env)
        ts.append(dt)
    ctx_ms = statistics.median(ts) * 1000
    ts = []
    for _ in range(5):
        dt, p = run([GRAPHIFY, "query", q, "--budget", "2000",
                     "--graph", os.path.join(qdir, "graphify-out", "graph.json")], env=env_base, cwd=qdir)
        ts.append(dt)
    g_ms = statistics.median(ts) * 1000
    qres.append({"q": q, "ctx_ms": round(ctx_ms), "graphify_ms": round(g_ms)})
    print(f"query {q!r}: ctx {ctx_ms:.0f}ms / graphify {g_ms:.0f}ms", flush=True)
results["query_latency"] = {"corpus": QUERY_CORPUS, "median_of": 5, "runs": qres}

with open(os.path.join(S, "results.json"), "w") as f:
    json.dump(results, f, indent=2)
print("WROTE results.json", flush=True)
