#!/usr/bin/env python3
"""Coordinator-directed fix, applied to the big-repo pass:

1. VERB PARITY for the 0-change warm cell: ctx-optimize warm uses `up` (its
   sync-equivalent, no-ops when fresh vs git HEAD) instead of `add` again.
   CodeGraph/gitnexus/graphify already used their sync-equivalent verbs.
2. A REAL incremental cell: `resync_1file_s` -- commit a 1-line comment edit
   to one real source file, then time each tool's sync verb. IMPORTANT,
   measured finding: ctx-optimize's freshness check is git-HEAD-based, not
   working-tree-based -- an UNCOMMITTED edit is invisible to `up` (confirmed
   by hand: 0.047s, "up to date with git HEAD", change not detected). So the
   edit is committed (git commit) before timing, exactly like a real repo
   change would be, then the corpus is git-reset back to its pinned SHA
   afterward so the corpus stays byte-identical to the manifest ref.

Reads/patches results-multilang-big-partial.json / results-multilang-big.json
in place (adds "warm_verb_fix" block per corpus, does not touch the original
cold/warm numbers so the raw record stays intact).
"""
import json, os, shutil, subprocess, time

HOME = os.path.expanduser("~")
ARENA = os.path.join(HOME, "ctx-bench-arena", "multilang")
CORPORA_DIR = os.path.join(ARENA, "corpora")
STORES_DIR = os.path.join(ARENA, "stores")
LOGS_DIR = os.path.join(ARENA, "logs-big")

CTX = "/Users/muthuishere/muthu/gitworkspace/ctx-optimize-bench-wt/bin/ctx-optimize"
CODEGRAPH = ["node", os.path.join(ARENA, "..", "tools", "codegraph", "dist", "bin", "codegraph.js")]
GITNEXUS = ["node", os.path.join(ARENA, "..", "tools", "gitnexus", "gitnexus", "dist", "cli", "index.js")]
GRAPHIFY = os.path.join(HOME, ".local", "bin", "graphify")

env_base = {k: v for k, v in os.environ.items()
            if not any(x in k for x in ("KEY", "TOKEN", "SECRET", "PASSWORD"))}

RESULTS_JSON = os.path.join(ARENA, "results-multilang-big-partial.json")
FINAL_JSON = os.path.join(ARENA, "results-multilang-big.json")

# id -> (clone_root, subdir, pinned_sha, touch_file_relative_to_subdir, gitnexus_reg_name)
CORPORA = {
    "c-postgres": {"subdir": "src", "sha": "05ffe9398b758bbb8d30cc76e9bbc638dab2d477",
                   "touch": "backend/access/heap/heapam.c"},
    "py-django": {"subdir": "django", "sha": "2719a7f8c161233f45d34b624a9df9392c86cc1b",
                  "touch": "db/models/query.py"},
    "go-kubernetes": {"subdir": "pkg", "sha": "39683505b630ff2121012f3c5b16215a1449d5ed",
                       "touch": "scheduler/scheduler.go"},
    "java-spring": {"subdir": "", "sha": "5356a1b1ac983c1e121d809fb54c3cb43f640f9b",
                     "touch": "spring-beans/src/main/java/org/springframework/beans/factory/support/DefaultListableBeanFactory.java"},
    "csharp-efcore": {"subdir": "src", "sha": "6a2be34d045329d9eff9536ec824226696d53e00",
                       "touch": "EFCore/DbContext.cs"},
    "ts-typescript": {"subdir": "src", "sha": "f0e992167440686f948965e5441a918b34251886",
                       "touch": "compiler/checker.ts"},
}
COMMENT_BY_EXT = {
    ".c": "// bench-touch\n", ".py": "# bench-touch\n", ".go": "// bench-touch\n",
    ".java": "// bench-touch\n", ".cs": "// bench-touch\n", ".ts": "// bench-touch\n",
}


def run(cmd, env=None, cwd=None, timeout=600):
    t0 = time.perf_counter()
    try:
        p = subprocess.run(cmd, capture_output=True, text=True, env=env, cwd=cwd, timeout=timeout)
        dt = time.perf_counter() - t0
        return dt, p.returncode, p.stdout, p.stderr, False
    except subprocess.TimeoutExpired:
        dt = time.perf_counter() - t0
        return dt, -1, "", f"TIMEOUT after {timeout}s", True


def log(cid, tool, label, cmd, dt, rc, out, err):
    os.makedirs(LOGS_DIR, exist_ok=True)
    idx = log._counter = getattr(log, "_counter", 0) + 1
    fn = os.path.join(LOGS_DIR, f"warmfix-{idx:04d}-{cid}-{tool}-{label}.log")
    with open(fn, "w") as f:
        f.write(f"cmd: {cmd}\ndt: {dt}\nrc: {rc}\n--- stdout (tail 3000) ---\n{out[-3000:]}\n--- stderr (tail 3000) ---\n{err[-3000:]}\n")


def load_results():
    path = RESULTS_JSON if os.path.exists(RESULTS_JSON) else FINAL_JSON
    with open(path) as f:
        return json.load(f), path


def save_results(data, path):
    with open(path, "w") as f:
        json.dump(data, f, indent=2)


results = json.load(open(FINAL_JSON)); results_path = FINAL_JSON

for cid, c in CORPORA.items():
    if cid not in results.get("corpora", {}):
        print(f"skip {cid}: not present yet in results (main pass hasn't reached it)", flush=True)
        continue
    entry = results["corpora"][cid]
    if entry.get("ctx-optimize", {}).get("cold_timeout"):
        print(f"skip {cid}: ctx-optimize cold gather timed out, no store to warm/resync", flush=True)
        continue

    clone_root = os.path.join(CORPORA_DIR, cid)
    cdir = os.path.join(clone_root, c["subdir"]) if c["subdir"] else clone_root
    ctx_store = os.path.join(STORES_DIR, f"ctx-{cid}")
    print(f"\n=== warm-fix + resync: {cid} dir={cdir} ===", flush=True)

    fix = {}

    # ---- 1. ctx-optimize 0-change warm via `up` ----
    env = dict(env_base, CTX_OPTIMIZE_STORE=ctx_store)
    # PRIME (untimed): reconcile store_head vs current git HEAD before timing.
    # Any prior manual/exploratory `up`/commit/reset cycle on this corpus can
    # leave store_head pointing at a SHA the corpus no longer has (we hit this
    # for real on c-postgres during script development), which would make the
    # "0-change" cell measure a full re-gather instead of a true no-op. Prime
    # once, untimed, so the timed call below is a genuine fresh-vs-HEAD no-op.
    _dt, _rc, _out, _err, _to = run([CTX, "up"], env=env, cwd=cdir, timeout=600)
    log(cid, "ctx-optimize", "warm-up-PRIME-untimed", [CTX, "up"], _dt, _rc, _out, _err)
    dt, rc, out, err, to = run([CTX, "up"], env=env, cwd=cdir, timeout=600)
    log(cid, "ctx-optimize", "warm-up", [CTX, "up"], dt, rc, out, err)
    fix["ctx-optimize_warm_up_s"] = None if to else round(dt, 3)
    fix["ctx-optimize_warm_up_note"] = (out + err)[-300:]
    print(f"  ctx-optimize `up` (0-change) = {fix['ctx-optimize_warm_up_s']}s : {fix['ctx-optimize_warm_up_note'].strip()[:120]}", flush=True)

    # ---- 2. 1-file-edit resync ----
    touch_path = os.path.join(cdir, c["touch"])
    if not os.path.exists(touch_path):
        print(f"  SKIP resync: touch file not found: {touch_path}", flush=True)
        fix["resync_1file_error"] = f"touch file not found: {touch_path}"
        results["corpora"][cid]["warm_verb_fix"] = fix
        save_results(results, results_path)
        continue

    ext = os.path.splitext(touch_path)[1]
    comment = COMMENT_BY_EXT.get(ext, "// bench-touch\n")
    with open(touch_path, "a") as f:
        f.write(comment)

    commit_dt, rc, out, err, to = run(
        ["git", "-c", "user.email=bench@bench", "-c", "user.name=bench", "commit", "-am", "bench: 1-file resync touch"],
        env=env_base, cwd=clone_root, timeout=60)
    if rc != 0:
        print(f"  SKIP resync: git commit failed rc={rc} {err[-200:]}", flush=True)
        fix["resync_1file_error"] = f"git commit failed: {err[-300:]}"
        # best effort revert
        run(["git", "checkout", "--", c["touch"]], env=env_base, cwd=cdir, timeout=30)
        results["corpora"][cid]["warm_verb_fix"] = fix
        save_results(results, results_path)
        continue

    resync = {}

    dt, rc, out, err, to = run([CTX, "up"], env=env, cwd=cdir, timeout=600)
    log(cid, "ctx-optimize", "resync1", [CTX, "up"], dt, rc, out, err)
    resync["ctx-optimize_s"] = None if to else round(dt, 3)
    resync["ctx-optimize_note"] = "up verb; store re-extracts fully on any staleness (no incremental path today)"
    print(f"  resync ctx-optimize `up` after 1-commit-edit = {resync['ctx-optimize_s']}s", flush=True)

    cg_env = dict(env_base, CODEGRAPH_TELEMETRY="0")
    dt, rc, out, err, to = run(CODEGRAPH + ["sync", cdir], env=cg_env, timeout=600)
    log(cid, "codegraph", "resync1", CODEGRAPH + ["sync", cdir], dt, rc, out, err)
    resync["codegraph_s"] = None if to else round(dt, 3)
    print(f"  resync codegraph `sync` after 1-commit-edit = {resync['codegraph_s']}s", flush=True)

    args = ["analyze", cdir, "--skip-git", "--index-only", "--name", f"bench-big-{cid}"]
    dt, rc, out, err, to = run(GITNEXUS + args, env=env_base, timeout=600)
    log(cid, "gitnexus", "resync1", GITNEXUS + args, dt, rc, out, err)
    resync["gitnexus_s"] = None if to else round(dt, 3)
    print(f"  resync gitnexus `analyze` after 1-commit-edit = {resync['gitnexus_s']}s", flush=True)

    args = ["update", cdir, "--no-cluster"]
    dt, rc, out, err, to = run([GRAPHIFY] + args, env=env_base, timeout=600)
    log(cid, "graphify", "resync1", [GRAPHIFY] + args, dt, rc, out, err)
    resync["graphify_s"] = None if to else round(dt, 3)
    print(f"  resync graphify `update` after 1-commit-edit = {resync['graphify_s']}s", flush=True)

    fix["resync_1file_s"] = resync

    # ---- revert corpus to pinned SHA ----
    run(["git", "reset", "--hard", c["sha"]], env=env_base, cwd=clone_root, timeout=60)
    print(f"  reverted {cid} to pinned SHA {c['sha'][:10]}", flush=True)

    results["corpora"][cid]["warm_verb_fix"] = fix
    save_results(results, results_path)

print("\nDONE warm_resync_fix.py", flush=True)
