#!/usr/bin/env python3
"""Honest multi-language cross-tool benchmark: ctx-optimize vs the field.

Every number here is [MEASURED] by this script on this machine. No LLM
judging happens here (that's a separate pass) -- this only does cold/warm
gather timing, footprint, and query latency, plus raw quality-capture dumps.

Corpora + tools live in an arena OUTSIDE the repo (~/ctx-bench-arena/multilang/).
Only this script + its JSON output + RUN-NOTES.md are committed to the repo.
"""
import json, os, shutil, statistics, subprocess, sys, time

HOME = os.path.expanduser("~")
ARENA = os.path.join(HOME, "ctx-bench-arena", "multilang")
CORPORA_DIR = os.path.join(ARENA, "corpora")
STORES_DIR = os.path.join(ARENA, "stores")
QUALITY_DIR = os.path.join(ARENA, "quality")
LOGS_DIR = os.path.join(ARENA, "logs")

CTX = "/Users/muthuishere/muthu/gitworkspace/ctx-optimize-bench-wt/bin/ctx-optimize"
CODEGRAPH = ["node", os.path.join(ARENA, "..", "tools", "codegraph", "dist", "bin", "codegraph.js")]
GITNEXUS = ["node", os.path.join(ARENA, "..", "tools", "gitnexus", "gitnexus", "dist", "cli", "index.js")]
GRAPHIFY = os.path.join(HOME, ".local", "bin", "graphify")
ASTGREP = "ast-grep"

CORPORA = [
    {"id": "c-linux", "lang": "c", "subdir": "block", "ast_pattern": "$RET $NAME($$$) { $$$ }", "ast_lang": "c",
     "query_q": "block device request queue handling", "codegraph_term": "queue"},
    {"id": "py-flask", "lang": "python", "subdir": "src", "ast_pattern": "def $NAME($$$):$$$BODY", "ast_lang": "python",
     "query_q": "route decorator handling", "codegraph_term": "route"},
    {"id": "py-django", "lang": "python", "subdir": "django", "ast_pattern": "def $NAME($$$):$$$BODY", "ast_lang": "python",
     "query_q": "queryset filter and evaluation", "codegraph_term": "queryset"},
    {"id": "go-gin", "lang": "go", "subdir": "", "ast_pattern": "func $NAME($$$) { $$$ }", "ast_lang": "go",
     "query_q": "router group handling", "codegraph_term": "router"},
    {"id": "java-gson", "lang": "java", "subdir": "gson/src/main/java", "ast_pattern": "public class $NAME {$$$BODY}", "ast_lang": "java",
     "query_q": "json serialization and deserialization", "codegraph_term": "serialize"},
    {"id": "csharp-newtonsoft", "lang": "csharp", "subdir": "Src/Newtonsoft.Json", "ast_pattern": "public class $NAME {$$$BODY}", "ast_lang": "csharp",
     "query_q": "json reader and writer implementation", "codegraph_term": "reader"},
    {"id": "ts-hono", "lang": "typescript", "subdir": "src", "ast_pattern": "function $NAME($$$) { $$$ }", "ast_lang": "typescript",
     "query_q": "middleware composition and routing", "codegraph_term": "middleware"},
]

env_base = {k: v for k, v in os.environ.items()
            if not any(x in k for x in ("KEY", "TOKEN", "SECRET", "PASSWORD"))}


def run(cmd, env=None, cwd=None, timeout=600):
    t0 = time.perf_counter()
    try:
        p = subprocess.run(cmd, capture_output=True, text=True, env=env, cwd=cwd, timeout=timeout)
        dt = time.perf_counter() - t0
        return dt, p.returncode, p.stdout, p.stderr
    except subprocess.TimeoutExpired as e:
        dt = time.perf_counter() - t0
        return dt, -1, (e.stdout or b"").decode(errors="replace") if isinstance(e.stdout, bytes) else (e.stdout or ""), "TIMEOUT after %ss" % timeout


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
        f.write(f"cmd: {cmd}\ndt: {dt}\nrc: {rc}\n--- stdout ---\n{stdout}\n--- stderr ---\n{stderr}\n")
    return fn


def get_version(cmd, args, cwd=None):
    dt, rc, out, err = run(cmd + args, env=env_base, cwd=cwd, timeout=60)
    return (out + err).strip().splitlines()[0] if (out + err).strip() else f"rc={rc}"


results = {"machine": None, "versions": {}, "corpora": {}}

# ---- machine stamp ----
def sysctl(key):
    dt, rc, out, err = run(["sysctl", "-n", key], env=env_base)
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


def gather_ctx(corpus_id, cdir, store):
    shutil.rmtree(store, ignore_errors=True)
    env = dict(env_base, CTX_OPTIMIZE_STORE=store)
    cold_times = []
    last = None
    for i in range(3):
        shutil.rmtree(store, ignore_errors=True)
        dt, rc, out, err = run([CTX, "add", cdir], env=env, timeout=900)
        log(corpus_id, "ctx-optimize", f"cold{i}", [CTX, "add", cdir], dt, rc, out, err)
        cold_times.append(dt)
        last = (rc, out, err)
    cold_s = min(cold_times)
    err_msg = None
    if last[0] != 0:
        err_msg = last[2][-500:]
    dt_warm, rc, out, err = run([CTX, "add", cdir], env=env, timeout=900)
    log(corpus_id, "ctx-optimize", "warm", [CTX, "add", cdir], dt_warm, rc, out, err)
    dt2, rc2, out2, err2 = run([CTX, "status", "--json"], env=env, cwd=cdir, timeout=60)
    st = {}
    try:
        st = json.loads(out2)
    except Exception:
        pass
    return {
        "cold_s": round(cold_s, 3), "warm_s": round(dt_warm, 3),
        "nodes": st.get("nodes"), "edges": st.get("edges"),
        "store_bytes": dir_size(store),
        "path_note": "add <path> (extraction + graph + prune + wiki regen; no LLM)",
        "runtime_deps": "none", "error": err_msg,
    }


def gather_codegraph(corpus_id, cdir):
    store = os.path.join(cdir, ".codegraph")
    shutil.rmtree(store, ignore_errors=True)
    env = dict(env_base, CODEGRAPH_TELEMETRY="0")
    cold_times = []
    last = None
    for i in range(3):
        shutil.rmtree(store, ignore_errors=True)
        dt, rc, out, err = run(CODEGRAPH + ["init", cdir], env=env, timeout=900)
        log(corpus_id, "codegraph", f"cold{i}", CODEGRAPH + ["init", cdir], dt, rc, out, err)
        cold_times.append(dt)
        last = (rc, out, err)
    cold_s = min(cold_times)
    err_msg = last[2][-500:] if last[0] != 0 else None
    dt_warm, rc, out, err = run(CODEGRAPH + ["sync", cdir], env=env, timeout=900)
    log(corpus_id, "codegraph", "warm", CODEGRAPH + ["sync", cdir], dt_warm, rc, out, err)
    dt2, rc2, out2, err2 = run(CODEGRAPH + ["status", cdir, "-j"], env=env, timeout=60)
    st = {}
    try:
        st = json.loads(out2)
    except Exception:
        pass
    return {
        "cold_s": round(cold_s, 3), "warm_s": round(dt_warm, 3),
        "nodes": st.get("nodeCount"), "edges": st.get("edgeCount"),
        "store_bytes": st.get("dbSizeBytes", dir_size(store)),
        "path_note": "init <path> cold, sync <path> warm (node-sqlite backend, no embeddings/LLM)",
        "runtime_deps": "none (local node-sqlite; telemetry disabled via CODEGRAPH_TELEMETRY=0)",
        "error": err_msg,
    }


def gather_gitnexus(corpus_id, cdir, reg_name):
    store = os.path.join(cdir, ".gitnexus")
    shutil.rmtree(store, ignore_errors=True)
    args = ["analyze", cdir, "--skip-git", "--index-only", "--name", reg_name]
    cold_times = []
    last = None
    for i in range(3):
        shutil.rmtree(store, ignore_errors=True)
        dt, rc, out, err = run(GITNEXUS + args, env=env_base, timeout=900)
        log(corpus_id, "gitnexus", f"cold{i}", GITNEXUS + args, dt, rc, out, err)
        cold_times.append(dt)
        last = (rc, out, err)
    cold_s = min(cold_times)
    err_msg = last[2][-500:] if last[0] != 0 else None
    dt_warm, rc, out, err = run(GITNEXUS + args, env=env_base, timeout=900)
    warm_note = "second analyze run (same args) -- see stdout for incremental vs forced-full-rebuild"
    forced_full = "forcing a full rebuild" in (out + err) or "schema" in (out + err).lower()
    log(corpus_id, "gitnexus", "warm", GITNEXUS + args, dt_warm, rc, out, err)
    combined = out + err
    nodes = edges = None
    import re
    m = re.search(r"([\d,]+)\s+nodes?\s*\|\s*([\d,]+)\s+edges?", combined)
    if m:
        nodes = int(m.group(1).replace(",", ""))
        edges = int(m.group(2).replace(",", ""))
    return {
        "cold_s": round(cold_s, 3), "warm_s": round(dt_warm, 3),
        "nodes": nodes, "edges": edges,
        "store_bytes": dir_size(store),
        "path_note": "analyze --skip-git --index-only (no --embeddings/--skills/--pdg; no LLM)",
        "runtime_deps": "none (local LadybugDB)",
        "warm_is_forced_full_rebuild": forced_full,
        "error": err_msg,
    }


def gather_graphify(corpus_id, cdir):
    gout = os.path.join(cdir, "graphify-out")
    shutil.rmtree(gout, ignore_errors=True)
    args = ["update", cdir, "--no-cluster"]
    cold_times = []
    last = None
    for i in range(3):
        shutil.rmtree(gout, ignore_errors=True)
        dt, rc, out, err = run([GRAPHIFY] + args, env=env_base, timeout=900)
        log(corpus_id, "graphify", f"cold{i}", [GRAPHIFY] + args, dt, rc, out, err)
        cold_times.append(dt)
        last = (rc, out, err)
    cold_s = min(cold_times)
    err_msg = last[2][-500:] if last[0] != 0 else None
    dt_warm, rc, out, err = run([GRAPHIFY] + args, env=env_base, timeout=900)
    log(corpus_id, "graphify", "warm", [GRAPHIFY] + args, dt_warm, rc, out, err)
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
        "cold_s": round(cold_s, 3), "warm_s": round(dt_warm, 3),
        "nodes": nodes, "edges": edges,
        "store_bytes": dir_size(gout) if os.path.isdir(gout) else None,
        "path_note": "update --no-cluster (extraction+graph only, no clustering/LLM)",
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
    print(f"  files={n_files}")

    try:
        store = os.path.join(STORES_DIR, f"ctx-{cid}")
        entry["ctx-optimize"] = gather_ctx(cid, cdir, store)
        print(f"  ctx-optimize cold={entry['ctx-optimize']['cold_s']}s warm={entry['ctx-optimize']['warm_s']}s nodes={entry['ctx-optimize']['nodes']} edges={entry['ctx-optimize']['edges']}")
    except Exception as e:
        entry["ctx-optimize"] = {"error": f"EXCEPTION: {e}"}
        print(f"  ctx-optimize FAILED: {e}")

    try:
        entry["codegraph"] = gather_codegraph(cid, cdir)
        print(f"  codegraph cold={entry['codegraph']['cold_s']}s warm={entry['codegraph']['warm_s']}s nodes={entry['codegraph']['nodes']} edges={entry['codegraph']['edges']}")
    except Exception as e:
        entry["codegraph"] = {"error": f"EXCEPTION: {e}"}
        print(f"  codegraph FAILED: {e}")

    try:
        entry["gitnexus"] = gather_gitnexus(cid, cdir, f"bench-multilang-{cid}")
        print(f"  gitnexus cold={entry['gitnexus']['cold_s']}s warm={entry['gitnexus']['warm_s']}s nodes={entry['gitnexus']['nodes']} edges={entry['gitnexus']['edges']}")
    except Exception as e:
        entry["gitnexus"] = {"error": f"EXCEPTION: {e}"}
        print(f"  gitnexus FAILED: {e}")

    try:
        entry["graphify"] = gather_graphify(cid, cdir)
        print(f"  graphify cold={entry['graphify']['cold_s']}s warm={entry['graphify']['warm_s']}s nodes={entry['graphify']['nodes']} edges={entry['graphify']['edges']}")
    except Exception as e:
        entry["graphify"] = {"error": f"EXCEPTION: {e}"}
        print(f"  graphify FAILED: {e}")

    # ---- query latency median of 5 ----
    q = c["query_q"]
    qres = {"question": q}
    ctx_store = os.path.join(STORES_DIR, f"ctx-{cid}")
    env = dict(env_base, CTX_OPTIMIZE_STORE=ctx_store)
    ts = []
    for i in range(5):
        dt, rc, out, err = run([CTX, "query", q, "--budget", "2000"], env=env, cwd=cdir, timeout=60)
        log(cid, "ctx-optimize", f"query{i}", [CTX, "query", q], dt, rc, out, err)
        ts.append(dt)
    qres["ctx-optimize_ms"] = round(statistics.median(ts) * 1000)

    ts = []
    for i in range(5):
        dt, rc, out, err = run(GITNEXUS + ["query", q], env=env_base, cwd=cdir, timeout=60)
        log(cid, "gitnexus", f"query{i}", GITNEXUS + ["query", q], dt, rc, out, err)
        ts.append(dt)
    qres["gitnexus_ms"] = round(statistics.median(ts) * 1000)

    term = c["codegraph_term"]
    ts = []
    for i in range(5):
        dt, rc, out, err = run(CODEGRAPH + ["query", term], env=env_base, cwd=cdir, timeout=60)
        log(cid, "codegraph", f"query{i}", CODEGRAPH + ["query", term], dt, rc, out, err)
        ts.append(dt)
    qres["codegraph_ms"] = round(statistics.median(ts) * 1000)
    qres["codegraph_query_term"] = term

    gout = os.path.join(cdir, "graphify-out", "graph.json")
    ts = []
    for i in range(5):
        dt, rc, out, err = run([GRAPHIFY, "query", q, "--budget", "2000", "--graph", gout], env=env_base, cwd=cdir, timeout=60)
        log(cid, "graphify", f"query{i}", [GRAPHIFY, "query", q], dt, rc, out, err)
        ts.append(dt)
    qres["graphify_ms"] = round(statistics.median(ts) * 1000)

    pattern = c["ast_pattern"]
    astlang = c["ast_lang"]
    ts = []
    ag_rc = None
    ag_out = ""
    for i in range(5):
        dt, rc, out, err = run([ASTGREP, "run", "-p", pattern, "-l", astlang, cdir], env=env_base, timeout=60)
        log(cid, "ast-grep", f"query{i}", [ASTGREP, "run", "-p", pattern, "-l", astlang, cdir], dt, rc, out, err)
        ts.append(dt)
        ag_rc = rc
        ag_out = out
    qres["ast-grep_ms"] = round(statistics.median(ts) * 1000)
    qres["ast-grep_pattern"] = pattern
    qres["ast-grep_lang"] = astlang
    qres["ast-grep_rc"] = ag_rc
    qres["ast-grep_nonempty"] = bool(ag_out.strip())
    print(f"  query: ctx={qres['ctx-optimize_ms']}ms gitnexus={qres['gitnexus_ms']}ms codegraph={qres['codegraph_ms']}ms graphify={qres['graphify_ms']}ms ast-grep={qres['ast-grep_ms']}ms (rc={ag_rc} nonempty={qres['ast-grep_nonempty']})")

    entry["query"] = qres
    results["corpora"][cid] = entry

    # checkpoint after each corpus in case of crash
    with open(os.path.join(ARENA, "results-multilang-partial.json"), "w") as f:
        json.dump(results, f, indent=2)

with open(os.path.join(ARENA, "results-multilang.json"), "w") as f:
    json.dump(results, f, indent=2)
print("\nWROTE results-multilang.json", flush=True)
