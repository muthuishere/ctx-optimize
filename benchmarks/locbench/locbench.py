#!/usr/bin/env python3
"""Loc-Bench — the first EXTERNAL yardstick this repo has.

WHY THIS EXISTS
---------------
Every quality number we publish today is self-authored, self-graded and
self-floored (docs/CRITIQUE.md item 7): we wrote the 20 questions, we wrote the
answer keys, and we set the floors. That is a fine regression net and worth
approximately nothing to a skeptic. Loc-Bench is somebody else's benchmark,
with somebody else's ground truth, and published baselines we did not choose.

  dataset  czlll/Loc-Bench_V1 (HuggingFace), 560 instances
  paper    LocAgent, ACL 2025 — arxiv.org/abs/2503.09089
  truth    edit_functions: ["path/to/file.py:func_name", ...] — the functions a
           real merged patch actually changed, so file AND function granularity
           come from the same field with no mapping layer to argue about
  pinning  every instance carries repo + base_commit (40-char SHA)

WHICH TIER WE ARE ENTERING, AND WHY IT IS THE HONEST ONE
--------------------------------------------------------
Loc-Bench's input is a natural-language GitHub issue. Answering it end-to-end
means reading prose and REASONING about which functions must change. That is
exactly what ctx-optimize refuses to do — the binary has no LLM, ever. Entering
the agentic tier would mean bolting a model on and reporting the model's score
as ours, which is the kind of thing this file exists to stop.

So we enter the tier we actually belong to: RETRIEVAL. The paper's own
retrieval baselines take the same issue text and return a ranked list, scored
with the same Acc@k against the same keys, with no LLM on either side. Those
are our peers:

    Loc-Bench, file-level Acc@5, retrieval-only (from the LocAgent paper)
      BM25 ................. 38.69%   <- lexical, our nearest relative
      Jina-Code-v2 ......... 43.43%
      Codesage-large-v2 .... 47.81%
      E5-base-v2 ........... 49.64%
      CodeRankEmbed ........ 52.55%   <- best retrieval baseline

    For contrast, NOT our tier:
      LocAgent (agentic, LLM) .... 84.59% file Acc@5

We are a lexical retriever. BM25 is the number that says whether our IDF +
prefix + trigram scoring is worth anything over the classic; CodeRankEmbed is
the number that says whether determinism costs us against embeddings. Beating
LocAgent is not on the table and claiming otherwise would be dishonest.

PROTOCOL
--------
* The whole `problem_statement` is the query, exactly as the BM25 baseline
  consumes it. Long issues are truncated at --query-chars (default 4000) and
  every truncation is COUNTED and REPORTED, because a silent cap would flatter
  us on the long tail.
* Hits are mapped to files by node `source`, deduped keeping first (best) rank.
  Acc@k = the fraction of instances where ANY ground-truth file appears in the
  top k. That is the paper's file-level metric.
* Function level uses the same ranked list against `file:func` keys, matching
  on the node's own label — we never reconstruct a function name from a file.
* Repos are pinned at base_commit with the same shallow+pinned fetch
  benchmarks/suite/setup.py uses, and `pinned: false` is recorded when a pin
  cannot be fetched. A floating checkout must never be reported as a pinned one.

USAGE
    python3 locbench.py --slice slices/small.json --json out.json
    python3 locbench.py --slice slices/small.json --k 1,3,5,10
"""

import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile
import time
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(os.path.dirname(HERE))
BIN = os.path.join(REPO, "bin", "ctx-optimize")

DATASET_ROWS = (
    "https://datasets-server.huggingface.co/rows"
    "?dataset=czlll%2FLoc-Bench_V1&config=default&split=test"
)


def sh(cmd, cwd=None, timeout=1800, env=None):
    e = dict(os.environ)
    if env:
        e.update(env)
    p = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True,
                       timeout=timeout, env=e)
    return p.returncode, (p.stdout or "") + (p.stderr or "")


# ------------------------------------------------------------------- dataset

def load_dataset(cache):
    """Fetch all 560 rows once and cache them. Stdlib only — no datasets lib."""
    if os.path.exists(cache):
        return json.load(open(cache))
    rows = []
    for off in range(0, 560, 100):
        u = f"{DATASET_ROWS}&offset={off}&length=100"
        d = json.loads(urllib.request.urlopen(u, timeout=120).read())
        rows += [r["row"] for r in d["rows"]]
    os.makedirs(os.path.dirname(cache), exist_ok=True)
    json.dump(rows, open(cache, "w"))
    return rows


def fetch_pinned(url, sha, dest):
    """Shallow clone at an exact SHA — benchmarks/suite/setup.py's contract.

    Returns (ok, resolved_sha, pinned). A floating checkout is reported as
    pinned=False and must never be published as a pinned number.
    """
    os.makedirs(dest, exist_ok=True)
    rc, _ = sh(["git", "init", "-q", dest])
    if rc != 0:
        return False, None, False
    sh(["git", "remote", "add", "origin", url], cwd=dest)
    rc, _ = sh(["git", "fetch", "--depth", "1", "origin", sha], cwd=dest, timeout=900)
    if rc == 0:
        sh(["git", "checkout", "-q", "FETCH_HEAD"], cwd=dest)
        _, got = sh(["git", "rev-parse", "HEAD"], cwd=dest)
        return True, got.strip(), True
    rc, _ = sh(["git", "fetch", "--depth", "1", "origin", "HEAD"], cwd=dest, timeout=900)
    if rc != 0:
        return False, None, False
    sh(["git", "checkout", "-q", "FETCH_HEAD"], cwd=dest)
    _, got = sh(["git", "rev-parse", "HEAD"], cwd=dest)
    return True, got.strip(), False


# ------------------------------------------------------------------ the tool

def gather(repo_dir, store):
    t = time.time()
    rc, log = sh([BIN, "add", repo_dir, "--path", repo_dir, "--store", store],
                 timeout=3600, env={"CTX_OPTIMIZE_STORE": store})
    return rc, time.time() - t, log


def ranked_files(repo_dir, store, question, limit, query_chars):
    """Ranked (file, label) list from one query. Returns (files, labels, truncated)."""
    q = " ".join(question.split())
    truncated = len(q) > query_chars
    q = q[:query_chars]
    rc, out = sh([BIN, "query", q, "--path", repo_dir, "--store", store,
                  "--json", "--limit", str(limit)],
                 timeout=600, env={"CTX_OPTIMIZE_STORE": store})
    if rc != 0:
        return [], [], truncated
    try:
        data = json.loads(out)
    except json.JSONDecodeError:
        return [], [], truncated
    files, labels, seen = [], [], set()
    for h in data.get("hits", []):
        n = h.get("node", {})
        src, lab = n.get("source", ""), n.get("label", "")
        if src and src not in seen:
            seen.add(src)
            files.append(src)
        labels.append((src, lab))
    return files, labels, truncated


# ------------------------------------------------------------------- grading

def acc_at_k(ranked, truth, k):
    """1 if ANY ground-truth item is in the top k — the paper's Acc@k.

    `ranked` items are either plain strings (file level) or sets of acceptable
    spellings for one rank position (function level).
    """
    t = set(truth)
    for item in ranked[:k]:
        if (item & t) if isinstance(item, set) else (item in t):
            return 1
    return 0


def grade(instance, files, labels, ks):
    truth_files = sorted({e.split(":", 1)[0] for e in instance["edit_functions"]})
    truth_funcs = set(instance["edit_functions"])

    # Function level: a hit counts when its FILE matches and its own label is
    # the function name. We never synthesise a name the node did not carry.
    #
    # Loc-Bench keys are QUALIFIED where the source is — `_writer.py:PdfWriter.
    # _insert_filtered_annotations` for a method, `connectivity.py:_build_n…`
    # for a free function. Our labels carry exactly the same qualification, so
    # both forms are offered and either may match. An earlier version of this
    # grader kept only the last dotted component, which turned exact
    # rank-3 hits into misses and under-scored us at 8% — a grader bug is as
    # capable of lying as a tool bug, in either direction.
    # ONE rank position per hit, holding both candidate spellings. Emitting two
    # entries per hit would silently make Acc@k more generous by pushing k
    # further down the list.
    ranked_funcs, seen = [], set()
    for src, lab in labels:
        if not src or not lab:
            continue
        forms = {f"{src}:{lab}", f"{src}:{lab.split('.')[-1]}"}
        key = f"{src}:{lab}"
        if key not in seen:
            seen.add(key)
            ranked_funcs.append(forms)

    return {
        "instance_id": instance["instance_id"],
        "repo": instance["repo"],
        "category": instance.get("category", ""),
        "truth_files": truth_files,
        "truth_funcs": sorted(truth_funcs),
        "file": {str(k): acc_at_k(files, truth_files, k) for k in ks},
        "func": {str(k): acc_at_k(ranked_funcs, truth_funcs, k) for k in ks},
        "top_files": files[:10],
        "repo_files": instance.get("_repo_files", 0),
    }


# ----------------------------------------------------------------- reporting

# Published Loc-Bench file-level Acc@5, retrieval tier (LocAgent, ACL 2025).
BASELINES = [
    ("BM25 (lexical)", 38.69),
    ("Jina-Code-v2", 43.43),
    ("Codesage-large-v2", 47.81),
    ("E5-base-v2", 49.64),
    ("CodeRankEmbed", 52.55),
]
AGENTIC = ("LocAgent (LLM, NOT our tier)", 84.59)


def report(results, ks, meta, out=sys.stdout):
    n = len(results)
    w = out.write
    w("\n" + "=" * 72 + "\n")
    w(f"Loc-Bench slice — {n} instances, {len(meta['repos'])} repos\n")
    w("=" * 72 + "\n\n")

    w("PINNING (a floating checkout is never reported as pinned)\n")
    for r, info in sorted(meta["repos"].items()):
        flag = "pinned" if info["pinned"] else "FLOATING — not pin-verified"
        w(f"  {r:32s} {info['sha'][:12]}  {flag}  gather {info['gather_s']:.1f}s\n")
    if meta["truncated"]:
        w(f"\n  {meta['truncated']}/{n} problem statements truncated at "
          f"{meta['query_chars']} chars — counted, not hidden\n")

    w("\nOUR SCORE (retrieval tier — no LLM, deterministic)\n")
    w(f"  {'granularity':14s}" + "".join(f"  Acc@{k:<5d}" for k in ks) + "\n")
    for lvl in ("file", "func"):
        row = f"  {lvl:14s}"
        for k in ks:
            v = 100.0 * sum(r[lvl][str(k)] for r in results) / max(1, n)
            row += f"  {v:6.2f}%  "
        w(row + "\n")

    ours5 = 100.0 * sum(r["file"]["5"] for r in results) / max(1, n)
    w("\nVERSUS PUBLISHED BASELINES (file Acc@5, same metric, same keys)\n")
    for name, val in BASELINES:
        mark = "  <-- us" if False else ""
        w(f"  {name:30s} {val:6.2f}%{mark}\n")
    w(f"  {'ctx-optimize query (this run)':30s} {ours5:6.2f}%  <-- us\n")
    w(f"\n  {AGENTIC[0]:30s} {AGENTIC[1]:6.2f}%\n")

    beat = [nm for nm, v in BASELINES if ours5 > v]
    lost = [nm for nm, v in BASELINES if ours5 <= v]
    w("\n  beat: " + (", ".join(beat) if beat else "nothing") + "\n")
    w("  lost to: " + (", ".join(lost) if lost else "nothing") + "\n")

    w("\n  CAVEAT: this is a SLICE, not the 560-instance benchmark. Published\n")
    w("  baselines are full-benchmark numbers. A slice score is indicative and\n")
    w("  is not comparable until the full set is run.\n")

    # SIZE SPLIT — printed always, because it is the single biggest threat to
    # validity. Picking a slice by clone convenience picks SMALL repos, and a
    # top-5 hit out of 45 files is a different problem from one out of 2,800.
    # The full benchmark is dominated by django/pandas/vllm-scale repos, so a
    # slice score that is not split by size is close to meaningless.
    big = [r for r in results if r.get("repo_files", 0) > 1000]
    small = [r for r in results if 0 < r.get("repo_files", 0) <= 1000]
    if big or small:
        w("\nSPLIT BY REPO SIZE (the slice's biggest bias)\n")
        for nm, grp in (("large repos  (>1000 files)", big),
                        ("small repos (<=1000 files)", small)):
            if not grp:
                w(f"  {nm:28s} n=0  — NOT SAMPLED, so this slice says nothing about them\n")
                continue
            g = len(grp)
            f5 = 100.0 * sum(x["file"]["5"] for x in grp) / g
            w(f"  {nm:28s} n={g:<3d} file Acc@5 {f5:6.2f}%\n")
        w("  The full benchmark is dominated by LARGE repos (django 35, yt-dlp 27,\n")
        w("  vllm 24, pandas 22). If the large-repo number is the weaker one, the\n")
        w("  headline above is an artifact of sampling and must not be published.\n")

    w("\nWORST INSTANCES (where we returned nothing useful)\n")
    misses = [r for r in results if not r["file"][str(max(ks))]]
    for r in misses[:8]:
        w(f"  {r['instance_id'][:44]:46s} truth={r['truth_files'][0][:38]}\n")
    if not misses:
        w("  none\n")
    w(f"  {len(misses)}/{n} instances miss at Acc@{max(ks)}\n")


# ---------------------------------------------------------------------- main

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--slice", required=True, help="JSON file listing instance_ids")
    ap.add_argument("--k", default="1,3,5,10")
    ap.add_argument("--limit", type=int, default=50, help="hits requested per query")
    ap.add_argument("--query-chars", type=int, default=4000)
    ap.add_argument("--arena", default=os.path.expanduser("~/ctx-locbench-arena"))
    ap.add_argument("--json", dest="out_json")
    a = ap.parse_args()

    ks = [int(x) for x in a.k.split(",")]
    if not os.path.exists(BIN):
        sys.exit(f"build the binary first: go build -o {BIN} ./cmd/ctx-optimize")

    spec = json.load(open(a.slice))
    rows = load_dataset(os.path.join(a.arena, "dataset.json"))
    by_id = {r["instance_id"]: r for r in rows}
    want = [by_id[i] for i in spec["instances"] if i in by_id]
    missing = [i for i in spec["instances"] if i not in by_id]
    if missing:
        print(f"WARNING: {len(missing)} instance_ids not in dataset: {missing[:3]}")

    meta = {"repos": {}, "truncated": 0, "query_chars": a.query_chars,
            "slice": os.path.basename(a.slice), "dataset": "czlll/Loc-Bench_V1"}
    results = []

    # Group by (repo, commit) so each checkout is gathered ONCE.
    groups = {}
    for inst in want:
        groups.setdefault((inst["repo"], inst["base_commit"]), []).append(inst)

    for (repo, sha), insts in groups.items():
        key = repo.replace("/", "__") + "@" + sha[:8]
        dest = os.path.join(a.arena, "repos", key)
        store = os.path.join(a.arena, "stores", key)
        print(f"[{key}] {len(insts)} instance(s)", flush=True)

        if not os.path.exists(os.path.join(dest, ".git")):
            ok, got, pinned = fetch_pinned(f"https://github.com/{repo}.git", sha, dest)
            if not ok:
                print(f"  clone FAILED — skipping (reported, not hidden)")
                meta["repos"][key] = {"sha": sha, "pinned": False,
                                      "gather_s": 0.0, "error": "clone failed"}
                continue
        else:
            _, got = sh(["git", "rev-parse", "HEAD"], cwd=dest)
            got, pinned = got.strip(), got.strip() == sha

        shutil.rmtree(store, ignore_errors=True)
        rc, secs, log = gather(dest, store)
        if rc != 0:
            print(f"  gather FAILED: {log[-300:]}")
            meta["repos"][key] = {"sha": got, "pinned": pinned,
                                  "gather_s": secs, "error": "gather failed"}
            continue
        meta["repos"][key] = {"sha": got, "pinned": pinned, "gather_s": secs}
        print(f"  gathered in {secs:.1f}s (pinned={pinned})", flush=True)

        nfiles = 0
        try:
            import glob as _g
            for nd in _g.glob(os.path.join(store, "*", "graph", "nodes.ndjson")):
                nfiles = sum(1 for l in open(nd) if '"kind":"file"' in l)
                break
        except OSError:
            pass
        meta["repos"][key]["files"] = nfiles

        for inst in insts:
            inst["_repo_files"] = nfiles
            files, labels, trunc = ranked_files(
                dest, store, inst["problem_statement"], a.limit, a.query_chars)
            if trunc:
                meta["truncated"] += 1
            results.append(grade(inst, files, labels, ks))

    report(results, ks, meta)
    if a.out_json:
        json.dump({"meta": meta, "ks": ks, "results": results},
                  open(a.out_json, "w"), indent=2)
        print(f"\nwrote {a.out_json}")


if __name__ == "__main__":
    main()
