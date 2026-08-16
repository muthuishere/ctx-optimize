#!/usr/bin/env python3
"""SPIKE: how do the modules of a monorepo actually connect to each other?

    python3 scripts/spikes/monorepo-links.py [--root ~/ctxoptimize] [--json]

ADR 22 started from one repo and one mechanism (an HTTP call from a UI to an
API). The owner's correction was that real monorepos wire modules together
several different ways — a package dependency, HTTP, a websocket, a spawned
process — and the design must not be tuned to whichever one happened to be in
front of us. This measures which mechanisms actually occur, across every
multi-module repo in the local store, before anything is built.

WHAT IT COUNTS, and why each is a fair test:

  dependency  a module declares a sibling's package name in its manifest.
              The strongest possible signal — it is a DECLARATION, not an
              inference — and the one the store is closest to being able to
              join, because dependency node ids are already global
              (`dep:npm/@mastra/core`).
  http / ws   a module's consumed identifier matches a sibling's provided
              route, after both sides normalise ${x} / :x / * to one wildcard.
  process     a module spawns a binary a sibling produces.
  shares      two modules touch the SAME external port. Real, but it means
              "both call OpenAI", never "A calls B" — counted separately so it
              can never be mistaken for a call.

THE PROVIDER SIDE IS READ FROM THE SOURCE, NOT THE STORE. That is the point of
the spike: the store records what a module CONSUMES and not what it IS (a
package.json is stored as a config node without its `name`). So identity is
read from the manifest on disk to answer "if we recorded it, what would join?"
A repo whose sources are gone is skipped and said so, never counted as zero.
"""
import argparse, json, os, sys, glob, collections, re

def load_ndjson(path):
    try:
        with open(path) as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                try:
                    yield json.loads(line)
                except Exception:
                    continue
    except OSError:
        return


def module_identity(src):
    """What this module calls itself: npm name, go module path, crate, dist name."""
    out = set()
    if not src or not os.path.isdir(src):
        return out
    pj = os.path.join(src, "package.json")
    if os.path.exists(pj):
        try:
            n = json.load(open(pj)).get("name")
            if n:
                out.add(n)
        except Exception:
            pass
    gm = os.path.join(src, "go.mod")
    if os.path.exists(gm):
        try:
            for line in open(gm):
                if line.startswith("module "):
                    out.add(line.split(None, 1)[1].strip())
                    break
        except Exception:
            pass
    ct = os.path.join(src, "Cargo.toml")
    if os.path.exists(ct):
        try:
            m = re.search(r'^\s*name\s*=\s*"([^"]+)"', open(ct).read(), re.M)
            if m:
                out.add(m.group(1))
        except Exception:
            pass
    for py in ("pyproject.toml", "setup.py"):
        p = os.path.join(src, py)
        if os.path.exists(p):
            try:
                m = re.search(r'^\s*name\s*=\s*["\']([^"\']+)["\']', open(p).read(), re.M)
                if m:
                    out.add(m.group(1))
            except Exception:
                pass
    return out


# ${x}, :x and * all mean "one segment I cannot resolve"
_PARAM = re.compile(r"\$\{[^}]*\}|:[A-Za-z_][A-Za-z0-9_]*|\*")

def norm_route(p):
    p = _PARAM.sub("*", p or "")
    p = re.sub(r"/+", "/", p).rstrip("/")
    return p.lower()


def store_identity(store):
    """What the STORE says this module is — ADR 22 D0's `publishes` edges.

    Preferred over reading the manifest on disk: it is the thing being built,
    and a repo whose sources are gone can still be measured once its store
    carries identity. Falls back to the source only when the store has none,
    which is how a store gathered before D0 is still counted.
    """
    out = set()
    for e in load_ndjson(os.path.join(store, "graph", "edges.ndjson")):
        if e.get("relation") != "publishes":
            continue
        md = e.get("metadata") or {}
        if md.get("vendored") == "true":
            continue  # a vendored upstream is not a sibling product
        t = e.get("target") or ""
        if t.startswith("dep:"):
            out.add(t.split("/", 1)[1] if "/" in t else t)
    return out


def scan_module(nodes_path):
    d = {"deps": set(), "provides": collections.defaultdict(set),
         "consumes": collections.defaultdict(set), "ports": set()}
    for n in load_ndjson(nodes_path):
        k = n.get("kind")
        if k == "dependency":
            lab = n.get("label")
            if lab:
                d["deps"].add(lab)
            continue
        if k != "port":
            continue
        m = n.get("metadata") or {}
        t, dirn = m.get("transport"), m.get("direction")
        ident = m.get("identifier") or n.get("label") or ""
        d["ports"].add(n.get("id"))
        if dirn == "provides":
            d["provides"][t].add(ident)
        elif dirn == "consumes":
            d["consumes"][t].add(ident)
    return d


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", default=os.path.expanduser("~/ctxoptimize"))
    ap.add_argument("--json", action="store_true")
    a = ap.parse_args()

    repos = collections.defaultdict(dict)
    for p in glob.glob(os.path.join(a.root, "*", "**", "graph", "nodes.ndjson"), recursive=True):
        store = os.path.dirname(os.path.dirname(p))
        rel = os.path.relpath(store, a.root)
        repos[rel.split(os.sep)[0]][rel] = store

    report = []
    for repo, mods in sorted(repos.items()):
        if len(mods) < 2:
            continue
        info, ident, missing_src = {}, {}, 0
        for mod, store in mods.items():
            info[mod] = scan_module(os.path.join(store, "graph", "nodes.ndjson"))
            src = None
            try:
                s = json.load(open(os.path.join(store, "source.json")))
                s = s if isinstance(s, list) else s.get("sources", [s])
                src = s[0].get("path") if s else None
            except Exception:
                pass
            ident[mod] = store_identity(store) or module_identity(src)
            if not ident[mod]:
                missing_src += 1

        names = sorted(mods)
        links = collections.Counter()
        detail = collections.defaultdict(list)
        for i, A in enumerate(names):
            for B in names:
                if A == B:
                    continue
                # dependency: A declares B's own package name
                hit = info[A]["deps"] & ident[B]
                if hit:
                    links["dependency"] += 1
                    detail["dependency"].append(f"{A} -> {B} ({sorted(hit)[0]})")
                # http / ws: A consumes a route B provides
                for t in ("network.http", "network.ws"):
                    pb = {norm_route(x) for x in info[B]["provides"].get(t, set()) if x.startswith("/")}
                    ca = {norm_route(x) for x in info[A]["consumes"].get(t, set()) if x.startswith("/")}
                    m = pb & ca
                    if m:
                        links[t] += 1
                        detail[t].append(f"{A} -> {B} ({len(m)} routes)")
                # process: A spawns a binary named like B
                pa = info[A]["consumes"].get("process.exec", set())
                bins = {os.path.basename(x) for x in pa}
                if bins & {os.path.basename(x) for x in ident[B]} or bins & {B.split("/")[-1]}:
                    links["process.exec"] += 1
                    detail["process.exec"].append(f"{A} -> {B}")
            for B in names[i + 1:]:
                sh = info[A]["ports"] & info[B]["ports"]
                if sh:
                    links["shares"] += 1

        report.append({"repo": repo, "modules": len(mods),
                       "modules_without_source": missing_src,
                       "links": dict(links),
                       "examples": {k: v[:3] for k, v in detail.items()}})

    if a.json:
        print(json.dumps(report, indent=1))
        return

    print(f"{'repo':26} {'mods':>4} {'no-src':>6} {'dep':>5} {'http':>5} {'ws':>4} {'proc':>5} {'shares':>7}")
    print("-" * 72)
    for r in report:
        l = r["links"]
        print(f"{r['repo'][:26]:26} {r['modules']:>4} {r['modules_without_source']:>6} "
              f"{l.get('dependency',0):>5} {l.get('network.http',0):>5} {l.get('network.ws',0):>4} "
              f"{l.get('process.exec',0):>5} {l.get('shares',0):>7}")
    print()
    tot = collections.Counter()
    for r in report:
        for k, v in r["links"].items():
            tot[k] += v
    print("TOTAL directed module->module links, by mechanism:", dict(tot))
    print(f"multi-module repos scanned: {len(report)}")
    nosrc = sum(1 for r in report if r["modules_without_source"] == r["modules"])
    print(f"repos with no identity at all (store lacks `publishes` AND sources gone; counted as 0): {nosrc}")
    print()
    for r in report:
        if not r["examples"]:
            continue
        print(f"== {r['repo']}")
        for k, ex in r["examples"].items():
            for e in ex:
                print(f"   {k:14} {e}")


if __name__ == "__main__":
    main()
