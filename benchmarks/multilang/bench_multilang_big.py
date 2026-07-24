#!/usr/bin/env python3
"""BIG-repo multi-language cross-tool benchmark: does ctx-optimize's speed +
footprint edge HOLD AT SCALE? Owner-directed pivot from small bounded subdirs
to full-scale flagship repos (postgres/django/kubernetes/spring/efcore/typescript).

Methodology deviation from the small-corpus pass (stated honestly): cold
gather is a SINGLE run (not best-of-3) capped at CELL_TIMEOUT_S (~10 min)
per (tool,corpus) cell -- doing 3 cold runs of a 10-minute tool on a 10k-file
repo would not fit in a session. A tool that exceeds the cap or crashes gets
a recorded TIMEOUT/FAIL cell with elapsed time -- that IS a scale finding,
never hidden.
"""
import json, os, re, shutil, statistics, subprocess, time

HOME = os.path.expanduser("~")
ARENA = os.path.join(HOME, "ctx-bench-arena", "multilang")
CORPORA_DIR = os.path.join(ARENA, "corpora")
STORES_DIR = os.path.join(ARENA, "stores")
LOGS_DIR = os.path.join(ARENA, "logs-big")

CTX = "/Users/muthuishere/muthu/gitworkspace/ctx-optimize-bench-wt/bin/ctx-optimize"
CODEGRAPH = ["node", os.path.join(ARENA, "..", "tools", "codegraph", "dist", "bin", "codegraph.js")]
GITNEXUS = ["node", os.path.join(ARENA, "..", "tools", "gitnexus", "gitnexus", "dist", "cli", "index.js")]
GRAPHIFY = os.path.join(HOME, ".local", "bin", "graphify")
ASTGREP = "ast-grep"

CELL_TIMEOUT_S = 600  # ~10 min hard cap per gather run, owner-directed

CORPORA = [
    {"id": "c-postgres", "lang": "c", "subdir": "src", "ast_pattern": "$RET $NAME($$$) { $$$ }", "ast_lang": "c",
     "query_q": "heap tuple insertion and buffer management", "codegraph_term": "heap"},
    {"id": "py-django", "lang": "python", "subdir": "django", "ast_pattern": "def $NAME($$$):$$$BODY", "ast_lang": "python",
     "query_q": "queryset filter and evaluation", "codegraph_term": "queryset"},
    {"id": "go-kubernetes", "lang": "go", "subdir": "pkg", "ast_pattern": "func $NAME($$$) { $$$ }", "ast_lang": "go",
     "query_q": "pod scheduling and controller reconciliation", "codegraph_term": "scheduler"},
    {"id": "java-spring", "lang": "java", "subdir": "", "ast_pattern": "public class $NAME {$$$BODY}", "ast_lang": "java",
     "query_q": "bean definition and dependency injection", "codegraph_term": "bean"},
    {"id": "csharp-efcore", "lang": "csharp", "subdir": "src", "ast_pattern": "public class $NAME {$$$BODY}", "ast_lang": "csharp",
     "query_q": "change tracking and entity state management", "codegraph_term": "tracker"},
    {"id": "ts-typescript", "lang": "typescript", "subdir": "src", "ast_pattern": "function $NAME($$$) { $$$ }", "ast_lang": "typescript",
     "query_q": "type checker and symbol resolution", "codegraph_term": "checker"},
]

env_base = {k: v for k, v in os.environ.items()
            if not any(x in k for x in ("KEY", "TOKEN", "SECRET", "PASSWORD"))}


def run(cmd, env=None, cwd=None, timeout=CELL_TIMEOUT_S):
    t0 = time.perf_counter()
    try:
        p = subprocess.run(cmd, capture_output=True, text=True, env=env, cwd=cwd, timeout=timeout)
        dt = time.perf_counter() - t0
        return dt, p.returncode, p.stdout, p.stderr, False
    except subprocess.TimeoutExpired:
        dt = time.perf_counter() - t0
        return dt, -1, "", f"TIMEOUT after {timeout}s (killed)", True


def dir_size(path):
    total = 0
    if not os.path.isdir(path):
        return 0
    for root, _, files in os.walk(path):
        for f in files:
            try:
                total += os.path.getsize(os.path.join(root, f))
            except OSError:
                pass
    return total


def log(corpus_id, tool, label, cmd, dt, rc, stdout, stderr):
    os.makedirs(LOGS_DIR, exist_ok=True)
    idx = log._counter = getattr(log, "_counter", 0) + 1
    fn = os.path.join(LOGS_DIR, f"{idx:04d}-{corpus_id}-{tool}-{label}.log")
    with open(fn, "w") as f:
        f.write(f"cmd: {cmd}\ndt: {dt}\nrc: {rc}\n--- stdout (tail 4000) ---\n{stdout[-4000:]}\n--- stderr (tail 4000) ---\n{stderr[-4000:]}\n")
    return fn


def get_version(cmd, args, cwd=None):
    dt, rc, out, err, to = run(cmd + args, env=env_base, cwd=cwd, timeout=60)
    return (out + err).strip().splitlines()[0] if (out + err).strip() else f"rc={rc}"


results = {"machine": None, "versions": {}, "cell_timeout_s": CELL_TIMEOUT_S,
           "methodology_note": "BIG-repo pass: single cold run (not best-of-3), 1 warm run, each capped at cell_timeout_s. See RUN-NOTES.md.",
           "corpora": {}}


def sysctl(key):
    dt, rc, out, err, to = run(["sysctl", "-n", key], env=env_base, timeout=30)
    return out.strip()


brand = sysctl("machdep.cpu.brand_string")
ncpu = sysctl("hw.ncpu")
mem_gb = round(int(sysctl("hw.memsize")) / (1024**3))
results["machine"] = f"{brand}, {ncpu} cores, {mem_gb} GB"

results["versions"]["ctx-optimize"] = get_version([CTX], ["version"])
results["versions"]["codegraph"] = get_version(CODEGRAPH, ["--version"])
results["versions"]["gitnexus"] = get_version(GITNEXUS, ["--version"])
results["versions"]["graphify"] = get_version([GRAPHIFY], ["--version"])
results["versions"]["ast-grep"] = get_version([ASTGREP], ["--version"])
print("VERSIONS:", json.dumps(results["versions"], indent=2), flush=True)


def cell(corpus_id, tool, label, cmd, env=None, cwd=None, timeout=CELL_TIMEOUT_S):
    dt, rc, out, err, timed_out = run(cmd, env=env, cwd=cwd, timeout=timeout)
    log(corpus_id, tool, label, cmd, dt, rc, out, err)
    return dt, rc, out, err, timed_out


def gather_ctx(cid, cdir, store):
    shutil.rmtree(store, ignore_errors=True)
    env = dict(env_base, CTX_OPTIMIZE_STORE=store)
    dt_cold, rc, out, err, to = cell(cid, "ctx-optimize", "cold", [CTX, "add", cdir], env=env)
    if to:
        return {"cold_s": None, "cold_timeout": True, "cold_elapsed_s": round(dt_cold, 1), "error": "TIMEOUT", "warm_s": None, "nodes": None, "edges": None, "store_bytes": dir_size(store)}
    err_msg = out[-500:] + err[-500:] if rc != 0 else None
    dt_warm, rc2, out2, err2, to2 = cell(cid, "ctx-optimize", "warm", [CTX, "add", cdir], env=env)
    dt3, rc3, out3, err3, to3 = cell(cid, "ctx-optimize", "status", [CTX, "status", "--json"], env=env, cwd=cdir)
    st = {}
    try:
        st = json.loads(out3)
    except Exception:
        pass
    return {
        "cold_s": round(dt_cold, 3), "cold_timeout": False,
        "warm_s": None if to2 else round(dt_warm, 3), "warm_timeout": to2,
        "nodes": st.get("nodes"), "edges": st.get("edges"),
        "store_bytes": dir_size(store),
        "path_note": "add <path> (extraction + graph + prune + wiki regen; no LLM); SINGLE cold run (see methodology_note)",
        "runtime_deps": "none", "error": err_msg,
    }


def gather_codegraph(cid, cdir):
    store = os.path.join(cdir, ".codegraph")
    shutil.rmtree(store, ignore_errors=True)
    env = dict(env_base, CODEGRAPH_TELEMETRY="0")
    dt_cold, rc, out, err, to = cell(cid, "codegraph", "cold", CODEGRAPH + ["init", cdir], env=env)
    if to:
        return {"cold_s": None, "cold_timeout": True, "cold_elapsed_s": round(dt_cold, 1), "error": "TIMEOUT", "warm_s": None, "nodes": None, "edges": None, "store_bytes": dir_size(store)}
    err_msg = (out[-500:] + err[-500:]) if rc != 0 else None
    dt_warm, rc2, out2, err2, to2 = cell(cid, "codegraph", "warm", CODEGRAPH + ["sync", cdir], env=env)
    dt3, rc3, out3, err3, to3 = cell(cid, "codegraph", "status", CODEGRAPH + ["status", cdir, "-j"], env=env, timeout=60)
    st = {}
    try:
        st = json.loads(out3)
    except Exception:
        pass
    return {
        "cold_s": round(dt_cold, 3), "cold_timeout": False,
        "warm_s": None if to2 else round(dt_warm, 3), "warm_timeout": to2,
        "nodes": st.get("nodeCount"), "edges": st.get("edgeCount"),
        "store_bytes": st.get("dbSizeBytes", dir_size(store)),
        "path_note": "init <path> cold, sync <path> warm (node-sqlite, no embeddings/LLM); SINGLE cold run",
        "runtime_deps": "none (local node-sqlite; telemetry disabled)",
        "error": err_msg,
    }


def gather_gitnexus(cid, cdir, reg_name):
    store = os.path.join(cdir, ".gitnexus")
    shutil.rmtree(store, ignore_errors=True)
    args = ["analyze", cdir, "--skip-git", "--index-only", "--name", reg_name]
    dt_cold, rc, out, err, to = cell(cid, "gitnexus", "cold", GITNEXUS + args)
    if to:
        return {"cold_s": None, "cold_timeout": True, "cold_elapsed_s": round(dt_cold, 1), "error": "TIMEOUT", "warm_s": None, "nodes": None, "edges": None, "store_bytes": dir_size(store)}
    err_msg = (out[-500:] + err[-500:]) if rc != 0 else None
    dt_warm, rc2, out2, err2, to2 = cell(cid, "gitnexus", "warm", GITNEXUS + args)
    combined = out + err
    nodes = edges = None
    m = re.search(r"([\d,]+)\s+nodes?\s*\|\s*([\d,]+)\s+edges?", combined)
    if m:
        nodes = int(m.group(1).replace(",", ""))
        edges = int(m.group(2).replace(",", ""))
    forced_full = "forcing a full rebuild" in combined
    return {
        "cold_s": round(dt_cold, 3), "cold_timeout": False,
        "warm_s": None if to2 else round(dt_warm, 3), "warm_timeout": to2,
        "nodes": nodes, "edges": edges,
        "store_bytes": dir_size(store),
        "path_note": "analyze --skip-git --index-only (no --embeddings/--skills/--pdg; no LLM); SINGLE cold run",
        "runtime_deps": "none (local LadybugDB)",
        "warm_is_forced_full_rebuild": forced_full,
        "error": err_msg,
    }


def gather_graphify(cid, cdir):
    gout = os.path.join(cdir, "graphify-out")
    shutil.rmtree(gout, ignore_errors=True)
    args = ["update", cdir, "--no-cluster"]
    dt_cold, rc, out, err, to = cell(cid, "graphify", "cold", [GRAPHIFY] + args)
    if to:
        return {"cold_s": None, "cold_timeout": True, "cold_elapsed_s": round(dt_cold, 1), "error": "TIMEOUT", "warm_s": None, "nodes": None, "edges": None, "store_bytes": dir_size(gout) if os.path.isdir(gout) else None}
    err_msg = (out[-500:] + err[-500:]) if rc != 0 else None
    dt_warm, rc2, out2, err2, to2 = cell(cid, "graphify", "warm", [GRAPHIFY] + args)
    nodes = edges = None
    gpath = os.path.join(gout, "graph.json")
    if os.path.exists(gpath):
        try:
            with open(gpath) as f:
                g = json.load(f)
            nodes = len(g.get("nodes", []))
            edges = len(g.get("edges", g.get("links", [])))
        except Exception:
            pass
    return {
        "cold_s": round(dt_cold, 3), "cold_timeout": False,
        "warm_s": None if to2 else round(dt_warm, 3), "warm_timeout": to2,
        "nodes": nodes, "edges": edges,
        "store_bytes": dir_size(gout) if os.path.isdir(gout) else None,
        "path_note": "update --no-cluster (extraction+graph only, no clustering/LLM); SINGLE cold run",
        "runtime_deps": "none", "error": err_msg,
    }


for c in CORPORA:
    cid = c["id"]
    cdir = os.path.join(CORPORA_DIR, cid, c["subdir"]) if c["subdir"] else os.path.join(CORPORA_DIR, cid)
    print(f"\n=== {cid} ({c['lang']}) dir={cdir} ===", flush=True)
    entry = {"lang": c["lang"], "dir": cdir}
    n_files = sum(len(fs) for r, _, fs in os.walk(cdir) if "/.git" not in r and "/.codegraph" not in r
                  and "/.gitnexus" not in r and "/graphify-out" not in r and "/.ctxoptimize" not in r
                  and "ctxstore" not in r)
    entry["files"] = n_files
    print(f"  files={n_files}", flush=True)

    store = os.path.join(STORES_DIR, f"ctx-{cid}")
    for name, fn, args in [
        ("ctx-optimize", gather_ctx, (cid, cdir, store)),
        ("codegraph", gather_codegraph, (cid, cdir)),
        ("gitnexus", gather_gitnexus, (cid, cdir, f"bench-big-{cid}")),
        ("graphify", gather_graphify, (cid, cdir)),
    ]:
        t0 = time.time()
        try:
            entry[name] = fn(*args)
            r = entry[name]
            if r.get("cold_timeout"):
                print(f"  {name}: COLD TIMEOUT after {r.get('cold_elapsed_s')}s (cap={CELL_TIMEOUT_S}s)", flush=True)
            else:
                print(f"  {name} cold={r['cold_s']}s warm={r.get('warm_s')}s nodes={r.get('nodes')} edges={r.get('edges')} store_bytes={r.get('store_bytes')}", flush=True)
        except Exception as e:
            entry[name] = {"error": f"EXCEPTION: {e}"}
            print(f"  {name} FAILED (exception): {e}", flush=True)
        with open(os.path.join(ARENA, "results-multilang-big-partial.json"), "w") as f:
            json.dump(results, f, indent=2)

    # ---- query latency: median of 5, 60s timeout per run (query should be fast even on big repos for a working tool) ----
    q = c["query_q"]
    qres = {"question": q}
    env = dict(env_base, CTX_OPTIMIZE_STORE=store)

    def median_query(tool_label, cmd_fn, cwd=None, env=None):
        ts = []
        rc_last = None
        out_last = ""
        any_timeout = False
        for i in range(5):
            cmd = cmd_fn()
            dt, rc, out, err, to = run(cmd, env=env, cwd=cwd, timeout=60)
            log(cid, tool_label, f"query{i}", cmd, dt, rc, out, err)
            ts.append(dt)
            rc_last, out_last = rc, out
            any_timeout = any_timeout or to
        return round(statistics.median(ts) * 1000), rc_last, out_last, any_timeout

    if not entry.get("ctx-optimize", {}).get("cold_timeout"):
        ms, rc, out, to = median_query("ctx-optimize", lambda: [CTX, "query", q, "--budget", "2000"], cwd=cdir, env=env)
        qres["ctx-optimize_ms"] = ms
        qres["ctx-optimize_query_timeout"] = to
    else:
        qres["ctx-optimize_ms"] = None
        qres["ctx-optimize_query_note"] = "skipped: cold gather timed out, no store to query"

    if not entry.get("gitnexus", {}).get("cold_timeout"):
        ms, rc, out, to = median_query("gitnexus", lambda: GITNEXUS + ["query", q], cwd=cdir)
        qres["gitnexus_ms"] = ms
        qres["gitnexus_query_timeout"] = to
    else:
        qres["gitnexus_ms"] = None
        qres["gitnexus_query_note"] = "skipped: cold gather timed out"

    term = c["codegraph_term"]
    if not entry.get("codegraph", {}).get("cold_timeout"):
        ms, rc, out, to = median_query("codegraph", lambda: CODEGRAPH + ["query", term])
        qres["codegraph_ms"] = ms
        qres["codegraph_query_term"] = term
        qres["codegraph_query_timeout"] = to
    else:
        qres["codegraph_ms"] = None
        qres["codegraph_query_note"] = "skipped: cold gather timed out"

    gout = os.path.join(cdir, "graphify-out", "graph.json")
    if not entry.get("graphify", {}).get("cold_timeout"):
        ms, rc, out, to = median_query("graphify", lambda: [GRAPHIFY, "query", q, "--budget", "2000", "--graph", gout], cwd=cdir)
        qres["graphify_ms"] = ms
        qres["graphify_query_timeout"] = to
    else:
        qres["graphify_ms"] = None
        qres["graphify_query_note"] = "skipped: cold gather timed out"

    pattern = c["ast_pattern"]
    astlang = c["ast_lang"]
    ts = []
    ag_rc = None
    ag_out = ""
    for i in range(5):
        dt, rc, out, err, to = run([ASTGREP, "run", "-p", pattern, "-l", astlang, cdir], env=env_base, timeout=120)
        log(cid, "ast-grep", f"query{i}", [ASTGREP, "run", "-p", pattern, "-l", astlang, cdir], dt, rc, out, err)
        ts.append(dt)
        ag_rc = rc
        ag_out = out
    qres["ast-grep_ms"] = round(statistics.median(ts) * 1000)
    qres["ast-grep_pattern"] = pattern
    qres["ast-grep_lang"] = astlang
    qres["ast-grep_rc"] = ag_rc
    qres["ast-grep_nonempty"] = bool(ag_out.strip())
    print(f"  query: ctx={qres.get('ctx-optimize_ms')}ms gitnexus={qres.get('gitnexus_ms')}ms codegraph={qres.get('codegraph_ms')}ms graphify={qres.get('graphify_ms')}ms ast-grep={qres['ast-grep_ms']}ms (rc={ag_rc} nonempty={qres['ast-grep_nonempty']})", flush=True)

    entry["query"] = qres
    results["corpora"][cid] = entry
    with open(os.path.join(ARENA, "results-multilang-big-partial.json"), "w") as f:
        json.dump(results, f, indent=2)

with open(os.path.join(ARENA, "results-multilang-big.json"), "w") as f:
    json.dump(results, f, indent=2)
print("\nWROTE results-multilang-big.json", flush=True)
