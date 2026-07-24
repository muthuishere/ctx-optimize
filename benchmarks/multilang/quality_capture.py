#!/usr/bin/env python3
"""Deterministic quality-capture pass: verbatim stdout dumps per tool per symbol,
no judging. Reuses the stores already built by bench_multilang.py (does NOT
re-gather -- assumes the previous pass's stores/.codegraph/.gitnexus/graphify-out
are still in place on disk).
"""
import json, os, subprocess, time

HOME = os.path.expanduser("~")
ARENA = os.path.join(HOME, "ctx-bench-arena", "multilang")
CORPORA_DIR = os.path.join(ARENA, "corpora")
STORES_DIR = os.path.join(ARENA, "stores")
QUALITY_DIR = os.path.join(ARENA, "quality")

CTX = "/Users/muthuishere/muthu/gitworkspace/ctx-optimize-bench-wt/bin/ctx-optimize"
CODEGRAPH = ["node", os.path.join(ARENA, "..", "tools", "codegraph", "dist", "bin", "codegraph.js")]
GITNEXUS = ["node", os.path.join(ARENA, "..", "tools", "gitnexus", "gitnexus", "dist", "cli", "index.js")]
GRAPHIFY = os.path.join(HOME, ".local", "bin", "graphify")

env_base = {k: v for k, v in os.environ.items()
            if not any(x in k for x in ("KEY", "TOKEN", "SECRET", "PASSWORD"))}

EXCLUDES = ["--exclude-dir=.git", "--exclude-dir=.codegraph", "--exclude-dir=.gitnexus",
            "--exclude-dir=graphify-out", "--exclude-dir=.ctxoptimize", "--exclude-dir=node_modules"]

CORPORA = {
    "c-linux": {"subdir": "block", "ast_lang": "c",
        "symbols": [
            ("blk_mq_alloc_request", "how does blk_mq_alloc_request allocate a request from a queue?"),
            ("blk_mq_free_request", "what does blk_mq_free_request do when releasing a request?"),
            ("blk_flush_integrity", "what does blk_flush_integrity do for integrity data?"),
        ]},
    "py-flask": {"subdir": "src", "ast_lang": "python",
        "symbols": [
            ("route", "how does Flask's route decorator register a view function?"),
            ("Flask", "what does the Flask class do as the application object?"),
            ("Blueprint", "how does Blueprint let you group routes for later registration?"),
        ]},
    "py-django": {"subdir": "django", "ast_lang": "python",
        "symbols": [
            ("QuerySet", "how does QuerySet build and lazily evaluate a database query?"),
            ("Model", "what does the Model base class provide for ORM models?"),
            ("Manager", "how does Manager attach QuerySet behavior to a model class?"),
        ]},
    "go-gin": {"subdir": "", "ast_lang": "go",
        "symbols": [
            ("Group", "how does RouterGroup.Group create a nested route group?"),
            ("New", "what does gin.New do when constructing an Engine?"),
            ("Run", "how does Engine.Run start the HTTP server?"),
        ]},
    "java-gson": {"subdir": "gson/src/main/java", "ast_lang": "java",
        "symbols": [
            ("Gson", "how does the Gson class serialize an object to JSON?"),
            ("GsonBuilder", "how does GsonBuilder configure and build a Gson instance?"),
            ("TypeAdapter", "what does the TypeAdapter abstract class define for custom (de)serialization?"),
        ]},
    "csharp-newtonsoft": {"subdir": "Src/Newtonsoft.Json", "ast_lang": "csharp",
        "symbols": [
            ("JsonSerializer", "how does JsonSerializer serialize an object graph to JSON?"),
            ("JsonTextReader", "how does JsonTextReader tokenize JSON text?"),
            ("JsonConvert", "what convenience methods does JsonConvert provide for serialization?"),
        ]},
    "ts-hono": {"subdir": "src", "ast_lang": "typescript",
        "symbols": [
            ("Hono", "how does the Hono class register routes and dispatch requests?"),
            ("HonoRequest", "what does the HonoRequest class wrap and expose?"),
            ("Context", "what does the Context class provide to a handler during a request?"),
        ]},
}


def run(cmd, env=None, cwd=None, timeout=60):
    try:
        p = subprocess.run(cmd, capture_output=True, text=True, env=env, cwd=cwd, timeout=timeout)
        return p.returncode, p.stdout, p.stderr
    except subprocess.TimeoutExpired as e:
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
    for sym, question in c["symbols"]:
        print(f"  symbol={sym}", flush=True)

        # ctx-optimize card
        env = dict(env_base, CTX_OPTIMIZE_STORE=ctx_store)
        rc, out, err = run([CTX, "card", sym], env=env, cwd=cdir)
        save(cid, "ctx-optimize-card", sym, [CTX, "card", sym], rc, out, err)

        # ctx-optimize query (NL question)
        rc, out, err = run([CTX, "query", question, "--budget", "2000"], env=env, cwd=cdir)
        save(cid, "ctx-optimize-query", sym, [CTX, "query", question], rc, out, err)

        # graphify query
        gout = os.path.join(cdir, "graphify-out", "graph.json")
        rc, out, err = run([GRAPHIFY, "query", question, "--budget", "2000", "--graph", gout], env=env_base, cwd=cdir)
        save(cid, "graphify", sym, [GRAPHIFY, "query", question], rc, out, err)

        # codegraph explore + query
        rc, out, err = run(CODEGRAPH + ["explore", sym], env=env_base, cwd=cdir, timeout=60)
        save(cid, "codegraph-explore", sym, CODEGRAPH + ["explore", sym], rc, out, err)
        rc, out, err = run(CODEGRAPH + ["query", sym], env=env_base, cwd=cdir, timeout=60)
        save(cid, "codegraph-query", sym, CODEGRAPH + ["query", sym], rc, out, err)

        # gitnexus context + query
        rc, out, err = run(GITNEXUS + ["context", sym], env=env_base, cwd=cdir, timeout=60)
        save(cid, "gitnexus-context", sym, GITNEXUS + ["context", sym], rc, out, err)
        rc, out, err = run(GITNEXUS + ["query", sym], env=env_base, cwd=cdir, timeout=60)
        save(cid, "gitnexus-query", sym, GITNEXUS + ["query", sym], rc, out, err)

        # ast-grep: definition-pattern search restricted to the symbol name via -p with literal, then grep the sym
        astlang = c["ast_lang"]
        # crude approach: run a broad definition pattern for the corpus and filter lines mentioning the symbol
        rc, out, err = run(["ast-grep", "run", "-p", sym, "-l", astlang, cdir], env=env_base, timeout=60)
        save(cid, "ast-grep", sym, ["ast-grep", "run", "-p", sym, "-l", astlang, cdir], rc, out, err)

        # grep baseline
        cmd = ["grep", "-rn"] + EXCLUDES + [sym, cdir]
        rc, out, err = run(cmd, env=env_base, timeout=60)
        save(cid, "grep-baseline", sym, cmd, rc, out[:20000], err)

print("\nDONE quality capture", flush=True)
