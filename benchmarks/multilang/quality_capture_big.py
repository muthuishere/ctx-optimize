#!/usr/bin/env python3
"""Deterministic quality-capture pass for the BIG-repo corpora: verbatim stdout
dumps per tool per symbol, no judging. Reuses stores built by
bench_multilang_big.py (assumes .codegraph/.gitnexus/graphify-out/ctx stores
are still on disk from that pass -- run this right after, before cleanup).
"""
import os, subprocess

HOME = os.path.expanduser("~")
ARENA = os.path.join(HOME, "ctx-bench-arena", "multilang")
CORPORA_DIR = os.path.join(ARENA, "corpora")
STORES_DIR = os.path.join(ARENA, "stores")
QUALITY_DIR = os.path.join(ARENA, "quality-big")

CTX = "/Users/muthuishere/muthu/gitworkspace/ctx-optimize-bench-wt/bin/ctx-optimize"
CODEGRAPH = ["node", os.path.join(ARENA, "..", "tools", "codegraph", "dist", "bin", "codegraph.js")]
GITNEXUS = ["node", os.path.join(ARENA, "..", "tools", "gitnexus", "gitnexus", "dist", "cli", "index.js")]
GRAPHIFY = os.path.join(HOME, ".local", "bin", "graphify")

env_base = {k: v for k, v in os.environ.items()
            if not any(x in k for x in ("KEY", "TOKEN", "SECRET", "PASSWORD"))}

EXCLUDES = ["--exclude-dir=.git", "--exclude-dir=.codegraph", "--exclude-dir=.gitnexus",
            "--exclude-dir=graphify-out", "--exclude-dir=.ctxoptimize", "--exclude-dir=node_modules"]

CORPORA = {
    "c-postgres": {"subdir": "src", "ast_lang": "c", "skip_gather_check": True,
        "symbols": [
            ("heap_insert", "how does heap_insert write a new tuple into a heap relation?"),
            ("heap_update", "how does heap_update modify an existing tuple?"),
            ("ReadBuffer", "how does ReadBuffer fetch a disk block into a shared buffer?"),
        ]},
    "py-django": {"subdir": "django", "ast_lang": "python",
        "symbols": [
            ("QuerySet", "how does QuerySet build and lazily evaluate a database query?"),
            ("Model", "what does the Model base class provide for ORM models?"),
            ("Manager", "how does Manager attach QuerySet behavior to a model class?"),
        ]},
    "go-kubernetes": {"subdir": "pkg", "ast_lang": "go",
        "symbols": [
            ("New", "what does scheduler.New construct when building the Scheduler?"),
            ("Run", "how does Scheduler.Run start the scheduling loop?"),
            ("applyDefaultHandlers", "what does applyDefaultHandlers wire up on the Scheduler?"),
        ]},
    "java-spring": {"subdir": "", "ast_lang": "java",
        "symbols": [
            ("DefaultListableBeanFactory", "how does DefaultListableBeanFactory register and resolve bean definitions?"),
            ("AnnotationConfigApplicationContext", "how does AnnotationConfigApplicationContext bootstrap a Spring context from annotated classes?"),
        ]},
    "csharp-efcore": {"subdir": "src", "ast_lang": "csharp",
        "symbols": [
            ("ChangeTracker", "how does ChangeTracker track entity state changes?"),
            ("DbContext", "what does the DbContext class provide as the EF Core session/unit of work?"),
        ]},
    "ts-typescript": {"subdir": "src", "ast_lang": "typescript",
        "symbols": [
            ("createTypeChecker", "how does createTypeChecker build the TypeScript type checker?"),
        ]},
}


def run(cmd, env=None, cwd=None, timeout=90):
    try:
        p = subprocess.run(cmd, capture_output=True, text=True, env=env, cwd=cwd, timeout=timeout)
        return p.returncode, p.stdout, p.stderr
    except subprocess.TimeoutExpired:
        return -1, "", f"TIMEOUT after {timeout}s"


def save(corpus_id, tool, sym, cmd, rc, out, err):
    d = os.path.join(QUALITY_DIR, corpus_id)
    os.makedirs(d, exist_ok=True)
    fn = os.path.join(d, f"{tool}__{sym}.txt")
    with open(fn, "w") as f:
        f.write(f"# cmd: {' '.join(cmd) if isinstance(cmd, list) else cmd}\n# rc: {rc}\n\n--- STDOUT ---\n{out}\n--- STDERR ---\n{err}\n")
    return fn


for cid, c in CORPORA.items():
    cdir = os.path.join(CORPORA_DIR, cid, c["subdir"]) if c["subdir"] else os.path.join(CORPORA_DIR, cid)
    print(f"\n=== {cid} ===", flush=True)
    ctx_store = os.path.join(STORES_DIR, f"ctx-{cid}")
    have_ctx_store = os.path.isdir(ctx_store)
    have_codegraph = os.path.isdir(os.path.join(cdir, ".codegraph"))
    have_gitnexus = os.path.isdir(os.path.join(cdir, ".gitnexus"))
    have_graphify = os.path.exists(os.path.join(cdir, "graphify-out", "graph.json"))
    print(f"  stores present: ctx={have_ctx_store} codegraph={have_codegraph} gitnexus={have_gitnexus} graphify={have_graphify}", flush=True)

    for sym, question in c["symbols"]:
        print(f"  symbol={sym}", flush=True)
        env = dict(env_base, CTX_OPTIMIZE_STORE=ctx_store)

        if have_ctx_store:
            rc, out, err = run([CTX, "card", sym], env=env, cwd=cdir)
            save(cid, "ctx-optimize-card", sym, [CTX, "card", sym], rc, out, err)
            rc, out, err = run([CTX, "query", question, "--budget", "2000"], env=env, cwd=cdir)
            save(cid, "ctx-optimize-query", sym, [CTX, "query", question], rc, out, err)
        else:
            save(cid, "ctx-optimize-card", sym, "SKIPPED", None, "", "no store: cold gather timed out or failed")
            save(cid, "ctx-optimize-query", sym, "SKIPPED", None, "", "no store: cold gather timed out or failed")

        if have_graphify:
            gout = os.path.join(cdir, "graphify-out", "graph.json")
            rc, out, err = run([GRAPHIFY, "query", question, "--budget", "2000", "--graph", gout], env=env_base, cwd=cdir)
            save(cid, "graphify", sym, [GRAPHIFY, "query", question], rc, out, err)
        else:
            save(cid, "graphify", sym, "SKIPPED", None, "", "no store: cold gather timed out or failed")

        if have_codegraph:
            rc, out, err = run(CODEGRAPH + ["explore", sym], env=env_base, cwd=cdir, timeout=90)
            save(cid, "codegraph-explore", sym, CODEGRAPH + ["explore", sym], rc, out, err)
            rc, out, err = run(CODEGRAPH + ["query", sym], env=env_base, cwd=cdir, timeout=90)
            save(cid, "codegraph-query", sym, CODEGRAPH + ["query", sym], rc, out, err)
        else:
            save(cid, "codegraph-explore", sym, "SKIPPED", None, "", "no store: cold gather timed out or failed")
            save(cid, "codegraph-query", sym, "SKIPPED", None, "", "no store: cold gather timed out or failed")

        if have_gitnexus:
            rc, out, err = run(GITNEXUS + ["context", sym], env=env_base, cwd=cdir, timeout=90)
            save(cid, "gitnexus-context", sym, GITNEXUS + ["context", sym], rc, out, err)
            rc, out, err = run(GITNEXUS + ["query", sym], env=env_base, cwd=cdir, timeout=90)
            save(cid, "gitnexus-query", sym, GITNEXUS + ["query", sym], rc, out, err)
        else:
            save(cid, "gitnexus-context", sym, "SKIPPED", None, "", "no store: cold gather timed out or failed")
            save(cid, "gitnexus-query", sym, "SKIPPED", None, "", "no store: cold gather timed out or failed")

        astlang = c["ast_lang"]
        rc, out, err = run(["ast-grep", "run", "-p", sym, "-l", astlang, cdir], env=env_base, timeout=90)
        save(cid, "ast-grep", sym, ["ast-grep", "run", "-p", sym, "-l", astlang, cdir], rc, out, err)

        cmd = ["grep", "-rn"] + EXCLUDES + [sym, cdir]
        rc, out, err = run(cmd, env=env_base, timeout=90)
        save(cid, "grep-baseline", sym, cmd, rc, out[:20000], err)

print("\nDONE big-repo quality capture", flush=True)
