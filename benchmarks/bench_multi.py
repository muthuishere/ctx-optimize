#!/usr/bin/env python3
"""Cross-tool head-to-head: ctx-optimize vs graphify / CodeGraph / GitNexus / ast-grep.

This is the harness behind `openspec/changes/2026-07-24-cross-tool-benchmark/`.
The 2026-07-24 run used a scratch copy of this logic that was never committed,
so the numbers could not be reproduced without re-deriving the method from
RUN-NOTES.md. It lives in the repo now. Do not run it from a scratch copy again.

Method (unchanged from RUN-NOTES.md 2026-07-24, so runs stay comparable):

- Every tool gets a FRESH copy of each corpus; each tool's store is wiped before
  each cold run, so no tool's artifacts leak into another's timing or bytes.
- Each tool is timed on its OWN fastest deterministic path — no LLM, no
  clustering, no embeddings. Named per tool in TOOLS below and echoed into the
  results JSON, because a benchmark that hides the command is not evidence.
- cold = nothing indexed. warm = immediate second run on an unchanged tree.
- query latency = median of N runs of one fixed question at a fixed budget.

Scale caveat this run exists to fix: every corpus in the 2026-07-24 run was
<=754 files. `--corpora linux` adds an 84,300-file tree, which is the regime
nobody has measured for ANY tool in this table.

Honesty rules enforced in code, not in prose:
- A tool that exceeds its timeout is recorded as {"error": "TIMEOUT after Ns"},
  never silently dropped and never reported as a win for anyone else.
- Reduced repetition on huge corpora is recorded per row in `runs`, so a
  best-of-1 number can never be read as a best-of-3 number.
"""
import argparse, json, os, shutil, statistics, subprocess, sys, time

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ARENA = os.path.expanduser("~/ctx-bench-arena")

CTX = os.path.join(REPO, "bin", "ctx-optimize")
GRAPHIFY = shutil.which("graphify") or os.path.expanduser("~/.local/bin/graphify")
CODEGRAPH = os.path.join(ARENA, "tools/codegraph/dist/bin/codegraph.js")
GITNEXUS = os.path.join(ARENA, "tools/gitnexus/gitnexus/dist/cli/index.js")
ASTGREP = shutil.which("ast-grep")

# corpus -> (path, files, query, ast-grep pattern, ast-grep lang)
CORPORA = {
    "corpus-flask": (f"{ARENA}/corpora/corpus-flask", "route decorator handling", "def $NAME($$$):", "python"),
    "corpus-gin": (f"{ARENA}/corpora/corpus-gin", "router group middleware", "func $NAME($$$) {$$$}", "go"),
    "corpus-ctx-src": (f"{ARENA}/corpora/corpus-ctx-src", "store merge producer", "func $NAME($$$) {$$$}", "go"),
    "corpus-graphify-src": (f"{ARENA}/corpora/corpus-graphify-src", "how does clustering work", "def $NAME($$$):", "python"),
    "linux": (os.path.expanduser("~/ctx-golden-corpora/linux"), "block device queue", "int $NAME($$$) {$$$}", "c"),
}


def run(cmd, timeout, env=None, cwd=None):
    """Wall-clock a command. Returns (seconds, proc_or_None); None means timeout."""
    full = dict(os.environ)
    full.update(env or {})
    t0 = time.perf_counter()
    try:
        p = subprocess.run(cmd, capture_output=True, text=True, env=full, cwd=cwd, timeout=timeout)
    except subprocess.TimeoutExpired:
        return time.perf_counter() - t0, None
    return time.perf_counter() - t0, p


def dir_size(path):
    total = 0
    for root, _, files in os.walk(path):
        for f in files:
            try:
                total += os.path.getsize(os.path.join(root, f))
            except OSError:
                pass
    return total


def wipe(*paths):
    for p in paths:
        shutil.rmtree(p, ignore_errors=True)


def count_files(path):
    n = 0
    for root, dirs, files in os.walk(path):
        dirs[:] = [d for d in dirs if d not in (".git", "node_modules")]
        n += len(files)
    return n


# Each tool: how to wipe, how to gather, how to query, where its store lives.
# `path_note` is published verbatim — it is the claim being made.
def tool_ctx(corpus_path, store_root):
    key = os.path.basename(corpus_path)
    return {
        "name": "ctx-optimize",
        "store": os.path.join(store_root, key),
        "wipe": [os.path.join(store_root, key)],
        "cold": [CTX, "add", corpus_path, "--path", corpus_path],
        "warm": [CTX, "add", corpus_path, "--path", corpus_path],
        "env": {"CTX_OPTIMIZE_STORE": store_root},
        "query": lambda q: [CTX, "query", q, "--path", corpus_path, "--budget", "2000"],
        # NOTE: since ADR 2026-07-27-wiki-off-by-default this no longer includes
        # wiki generation. The 2026-07-24 run's ctx-optimize column DID include
        # it, so that column is not comparable to this one. Stated, not hidden.
        "path_note": "add <path> (extraction + graph + prune; wiki off by default since v0.12; no LLM)",
        "deps": "none",
    }


def tool_graphify(corpus_path, _store_root):
    out = os.path.join(corpus_path, "graphify-out")
    return {
        "name": "graphify",
        "store": out,
        "wipe": [out],
        "cold": [GRAPHIFY, "update", corpus_path, "--no-cluster"],
        "warm": [GRAPHIFY, "update", corpus_path, "--no-cluster"],
        # graphify refuses to READ a graph.json over 512MB — on linux it builds a
        # 1.2GB graph and then errors out on every query, in 0.06s. Timing that
        # error path would have published graphify as 60x faster than everyone.
        # The cap is raised here so the tool gets its best honest number; the
        # out-of-box failure is reported separately, not buried.
        "env": {"GRAPHIFY_MAX_GRAPH_BYTES": "8GB"},
        "query": lambda q: [GRAPHIFY, "query", q, "--graph", os.path.join(out, "graph.json"), "--budget", "2000"],
        "path_note": "update <path> --no-cluster (extraction+graph only, no clustering/LLM; its fastest path)",
        "deps": "none",
    }


def tool_codegraph(corpus_path, _store_root):
    out = os.path.join(corpus_path, ".codegraph")
    return {
        "name": "codegraph",
        "store": out,
        "wipe": [out],
        "cold": ["node", CODEGRAPH, "init", corpus_path],
        "warm": ["node", CODEGRAPH, "sync", corpus_path],
        "env": {"CODEGRAPH_TELEMETRY": "0"},
        # Must run FROM the corpus dir: codegraph resolves its store from cwd and
        # errors "not initialized" otherwise. A first pass recorded that error as
        # a missing query number, which would have read as "codegraph has no
        # query" — it has one, the harness was calling it wrong.
        "cwd": corpus_path,
        # FULL phrase, not q.split()[0]. The 2026-07-24 harness passed only the
        # first word, so CodeGraph answered "block" while every other tool
        # answered "block device queue" — and the resulting number FLATTERED
        # them (536ms single-word vs 880ms on the real question). A benchmark
        # that asks two tools different questions is not a benchmark.
        "query": lambda q: ["node", CODEGRAPH, "query", q],
        "path_note": "init <path> cold / sync <path> warm (local node-sqlite, no embeddings/LLM)",
        "deps": "none (local node-sqlite; telemetry disabled in the timed path)",
    }


def tool_gitnexus(corpus_path, _store_root):
    out = os.path.join(corpus_path, ".gitnexus")
    return {
        "name": "gitnexus",
        "store": out,
        "wipe": [out],
        "cold": ["node", GITNEXUS, "analyze", corpus_path, "--skip-git", "--index-only"],
        "warm": ["node", GITNEXUS, "analyze", corpus_path, "--skip-git", "--index-only"],
        "env": {},
        # GitNexus keeps a GLOBAL registry keyed `bench-<dirname>`, so with more
        # than one corpus indexed a bare `query` aborts with "Multiple
        # repositories indexed". --repo disambiguates. Same class of harness bug
        # as codegraph's cwd: the tool works, the call was wrong.
        "cwd": corpus_path,
        "registry_name": "bench-" + os.path.basename(corpus_path),
        "query": lambda q: ["node", GITNEXUS, "query", q, "--repo", "bench-" + os.path.basename(corpus_path)],
        "path_note": "analyze --skip-git --index-only (no embeddings/skills/PDG; no LLM)",
        "deps": "none (local LadybugDB)",
    }


BUILDERS = {
    "ctx-optimize": tool_ctx,
    "graphify": tool_graphify,
    "codegraph": tool_codegraph,
    "gitnexus": tool_gitnexus,
}


def bench_tool(spec, runs, timeout, qruns, question):
    entry = {"path_note": spec["path_note"], "runtime_deps": spec["deps"], "error": None, "runs": runs}
    colds = []
    for _ in range(runs):
        wipe(*spec["wipe"])
        dt, p = run(spec["cold"], timeout, env=spec["env"], cwd=spec.get("cwd"))
        if p is None:
            entry["error"] = f"TIMEOUT after {timeout}s (cold gather)"
            entry["cold_s"] = None
            return entry
        if p.returncode != 0:
            entry["error"] = f"exit {p.returncode}: {(p.stderr or p.stdout)[-300:].strip()}"
            entry["cold_s"] = None
            return entry
        colds.append(dt)
    entry["cold_s"] = round(min(colds), 3)

    dt, p = run(spec["warm"], timeout, env=spec["env"], cwd=spec.get("cwd"))
    entry["warm_s"] = round(dt, 3) if p is not None and p.returncode == 0 else None
    entry["store_bytes"] = dir_size(spec["store"])

    qs = []
    for _ in range(qruns):
        dt, p = run(spec["query"](question), timeout, env=spec["env"], cwd=spec.get("cwd"))
        if p is None:
            entry["query_ms"] = None
            entry["query_error"] = f"TIMEOUT after {timeout}s"
            return entry
        if p.returncode != 0:
            entry["query_ms"] = None
            entry["query_error"] = f"exit {p.returncode}: {(p.stderr or p.stdout)[-200:].strip()}"
            return entry
        qs.append(dt * 1000)
    entry["query_ms"] = round(statistics.median(qs))
    entry["query_runs"] = qruns
    return entry


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--corpora", nargs="+", default=list(CORPORA))
    ap.add_argument("--tools", nargs="+", default=list(BUILDERS))
    ap.add_argument("--runs", type=int, default=3, help="cold-gather repetitions (best-of)")
    ap.add_argument("--qruns", type=int, default=5, help="query repetitions (median)")
    ap.add_argument("--timeout", type=int, default=1800, help="per-command seconds")
    ap.add_argument("--out", default=os.path.join(REPO, "benchmarks", "results-multi-scale.json"))
    a = ap.parse_args()

    store_root = os.path.join(ARENA, "stores")
    results = {"machine": "Apple M5 Pro, 18 cores, 48 GB", "versions": {}, "corpora": {}}
    _, p = run([CTX, "version"], 60)
    results["versions"]["ctx-optimize"] = p.stdout.strip() if p else "?"

    for corpus in a.corpora:
        cpath, question, pat, lang = CORPORA[corpus]
        if not os.path.isdir(cpath):
            print(f"SKIP {corpus}: {cpath} missing", file=sys.stderr)
            continue
        nfiles = count_files(cpath)
        print(f"\n=== {corpus} ({nfiles} files) — query: {question!r}", flush=True)
        entry = {"files": nfiles, "question": question}
        for tname in a.tools:
            spec = BUILDERS[tname](cpath, store_root)
            print(f"  {tname} …", end="", flush=True)
            r = bench_tool(spec, a.runs, a.timeout, a.qruns, question)
            entry[tname] = r
            print(f" cold={r.get('cold_s')}s query={r.get('query_ms')}ms {r.get('error') or ''}", flush=True)
        # grep-class controls. These are what an agent falls back to when a tool
        # is too slow, so "we beat graphify" is only half the question — the
        # other half is whether we beat the thing the agent would do instead.
        # NOT apples-to-apples and must never be published as if it were: rg
        # returns matching LINES, we return resolved symbols with callers. The
        # point is the latency budget, not the answer.
        for gname, gcmd in (("ripgrep", "rg"), ("grep", "grep")):
            gbin = shutil.which(gcmd)
            if not gbin:
                continue
            term = question.split()[0]
            cmd = [gbin, "-n", term, cpath] if gcmd == "rg" else [gbin, "-rn", term, cpath]
            dts = []
            for _ in range(a.qruns):
                dt, p = run(cmd, a.timeout)
                if p is None:
                    break
                dts.append(dt * 1000)
            entry[gname] = {
                "query_ms": round(statistics.median(dts)) if dts else None,
                "term": term,
                "path_note": f"{gcmd} literal scan, no index — returns matching lines, NOT resolved symbols",
            }
            print(f"  {gname} query={entry[gname]['query_ms']}ms", flush=True)
        if ASTGREP:
            dts = []
            for _ in range(a.qruns):
                dt, p = run([ASTGREP, "run", "-p", pat, "-l", lang, cpath], a.timeout)
                if p is None:
                    break
                dts.append(dt * 1000)
            entry["ast-grep"] = {
                "query_ms": round(statistics.median(dts)) if dts else None,
                "pattern": pat, "lang": lang,
                "path_note": "no index — scans the tree on every query",
            }
            print(f"  ast-grep query={entry['ast-grep']['query_ms']}ms", flush=True)
        results["corpora"][corpus] = entry
        with open(a.out, "w") as f:
            json.dump(results, f, indent=1, sort_keys=True)

    print(f"\nwrote {a.out}")


if __name__ == "__main__":
    main()
