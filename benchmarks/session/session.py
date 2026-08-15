#!/usr/bin/env python3
"""Multi-pass SESSION benchmark: what an agent actually costs over an hour.

WHY THIS EXISTS
---------------
Every code-tool benchmark in the field — ours included, until now — measures ONE
PASS: time to build an index. That is the metric ripgrep, Zoekt and GNU Global
are built to win, and measuring only that flatters grep-class tools and punishes
graph-class ones for work that gets amortized away in practice.

Real agent work is MULTI-PASS. You build once, ask a dozen questions, edit a few
files, re-index, ask more questions, and repeat for hours. Under that model the
cold build is a rounding error and two things dominate:

  1. incremental re-index cost   (paid after EVERY edit)
  2. answer quality per query    (paid on EVERY question)

So this harness models a session, not an index build:

  PHASE 0  cold build          wall, index bytes, peak RSS
  PHASE 1  queries (cold)      latency + graded answers
  PHASE 2  scripted edit       deterministic, reverted afterwards
  PHASE 3  incremental update  wall  <- the metric that separates tools
  PHASE 4  queries (warm)      latency + graded answers + STALENESS probes
           ...rounds 2..R repeat 2-4, and we report cumulative session cost.

THE STALENESS PROBE is the sharpest discriminator here and is missing from every
benchmark we surveyed. A tool that re-indexes in 0ms by doing nothing is not
fast, it is wrong — but a build-time-only benchmark scores it as the winner.
Each round renames a symbol, which yields two probes in opposite directions:

    old name must VANISH from answers   (catches a stale index serving dead facts)
    new name must APPEAR in answers     (catches an index that never updated)

Both are exact-match on a symbol that provably did not / did not not exist
before the edit, so grading needs no LLM and no judgement call.

FAIRNESS RULES, because a benchmark you design and win is worth nothing
----------------------------------------------------------------------
* Questions carry a CLASS. `locate` questions ("where is X defined") are
  answerable by grep, and grep should and does win some of them. `relate`
  questions ("who calls X", "what breaks if I change X") are not answerable by a
  line-matching tool at all. Reporting a single blended score would hide both
  facts, so the table separates them.
* A tool that cannot answer a class is marked `n/a` and EXCLUDED from that
  score. It is never given a 0 — scoring a trigram indexer 0/10 on "who calls
  this" is a rigged comparison, not a finding.
* Speed rows and quality rows are separate. Zoekt/gtags/rg can honestly share a
  build+incremental row while sharing no quality row.
* Corpora are pinned by commit, edits are scripted, and both are recorded in the
  results JSON so a skeptic can reproduce or attack the exact run.
* Where we lose, the table says so in the same font as where we win.

USAGE
    python3 session.py --session sessions/newtonsoft.json            # all tools
    python3 session.py --session sessions/newtonsoft.json --tool ctx-optimize
    python3 session.py --session sessions/newtonsoft.json --rounds 3 --json out.json
"""

import argparse
import json
import os
import platform
import re
import shutil
import statistics
import subprocess
import sys
import tempfile
import time

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.abspath(os.path.join(HERE, "..", ".."))


# ---------------------------------------------------------------- measurement


def run(cmd, cwd=None, env=None, timeout=3600):
    """Run a command, returning (seconds, peak_rss_bytes|None, CompletedProcess).

    Peak RSS comes from /usr/bin/time -l (darwin) or -v (GNU); if neither is
    parseable we report None rather than a guess, because a fabricated memory
    number is worse than a missing one.
    """
    full, rss_file = cmd, None
    timer = "/usr/bin/time"
    if os.path.exists(timer):
        rss_file = tempfile.NamedTemporaryFile(delete=False, suffix=".time")
        rss_file.close()
        flag = "-l" if platform.system() == "Darwin" else "-v"
        full = [timer, flag] + cmd

    t0 = time.perf_counter()
    try:
        p = subprocess.run(
            full, capture_output=True, text=True, cwd=cwd, env=env, timeout=timeout
        )
    except subprocess.TimeoutExpired:
        return timeout, None, None
    dt = time.perf_counter() - t0

    rss = None
    if rss_file:
        try:
            # /usr/bin/time writes to stderr, which subprocess captured for us.
            for line in (p.stderr or "").splitlines():
                m = re.search(r"(\d+)\s+maximum resident set size", line)
                if m:
                    rss = int(m.group(1))  # darwin: bytes
                    break
                m = re.search(r"Maximum resident set size[^:]*:\s*(\d+)", line)
                if m:
                    rss = int(m.group(1)) * 1024  # GNU: kbytes
                    break
        finally:
            os.unlink(rss_file.name)
    return dt, rss, p


def dir_bytes(path):
    total = 0
    for root, _, files in os.walk(path):
        for f in files:
            try:
                total += os.path.getsize(os.path.join(root, f))
            except OSError:
                pass
    return total


def human_bytes(n):
    if n is None:
        return "—"
    for unit in ("B", "KB", "MB", "GB"):
        if n < 1024:
            return f"{n:.0f}{unit}"
        n /= 1024
    return f"{n:.1f}TB"


# ---------------------------------------------------------------------- edits
#
# Edits are applied to the corpus IN PLACE and reverted from an in-memory
# backup in a finally block. The corpus is a pinned checkout we do not own, so
# leaving it dirty is a bug, not an inconvenience.


class EditSession:
    def __init__(self, corpus):
        self.corpus = corpus
        self.backups = {}  # abspath -> original bytes

    def _backup(self, rel):
        p = os.path.join(self.corpus, rel)
        if p not in self.backups:
            with open(p, "rb") as fh:
                self.backups[p] = fh.read()
        return p

    def apply(self, edit):
        """Apply one edit. Returns a dict describing what changed, for the log.

        Every op VERIFIES its own effect and raises on surprise. A scripted edit
        that silently no-ops would make the staleness probes vacuously pass.
        """
        op = edit["op"]
        if op == "rename_symbol":
            p = self._backup(edit["file"])
            src = self.backups[p].decode("utf-8", "surrogateescape")
            pat = re.compile(r"\b%s\b" % re.escape(edit["old"]))
            n = len(pat.findall(src))
            if n != edit["expect_occurrences"]:
                raise RuntimeError(
                    f"{edit['file']}: expected {edit['expect_occurrences']} "
                    f"occurrences of {edit['old']}, found {n} — corpus drifted "
                    f"from the pin, refusing to run a non-reproducible edit"
                )
            out = pat.sub(edit["new"], src)
            with open(p, "w", encoding="utf-8", errors="surrogateescape") as fh:
                fh.write(out)
            return {"op": op, "file": edit["file"], "occurrences": n}

        if op == "append_symbol":
            p = self._backup(edit["file"])
            with open(p, "a", encoding="utf-8", errors="surrogateescape") as fh:
                fh.write("\n" + edit["text"] + "\n")
            return {"op": op, "file": edit["file"], "symbol": edit.get("symbol")}

        if op == "touch":
            p = os.path.join(self.corpus, edit["file"])
            os.utime(p, None)  # mtime only, content identical
            return {"op": op, "file": edit["file"]}

        raise ValueError(f"unknown edit op: {op}")

    def revert(self):
        for p, data in self.backups.items():
            with open(p, "wb") as fh:
                fh.write(data)
        self.backups.clear()


# --------------------------------------------------------------------- adapter


class Tool:
    """A benchmarked tool, defined entirely by data in tools.json.

    Command templates use {corpus}, {index}, {query}. A tool with no `build`
    (ripgrep) is an on-the-fly searcher: its build and incremental cost are 0
    by definition, which is a real and important advantage this benchmark is
    designed to show rather than hide.
    """

    def __init__(self, spec):
        self.spec = spec
        self.name = spec["name"]
        self.classes = set(spec.get("answers_classes", []))
        self.index = None

    def _fmt(self, argv, **kw):
        out = [a.format(**kw) for a in argv]
        # Queries run with cwd=corpus because search tools search the working
        # directory. A RELATIVE tool path (bin/ctx-optimize) would then resolve
        # against the corpus and fail silently, scoring 0 — which reads as "the
        # tool cannot answer" instead of "the harness is broken". Absolutize
        # against the repo so the two are never confused again.
        if out and not os.path.isabs(out[0]):
            cand = os.path.join(REPO, out[0])
            if os.path.exists(cand):
                out[0] = cand
        return out

    def _env(self, index):
        return dict(os.environ, **{k: v.format(index=index) for k, v in
                                   self.spec.get("env", {}).items()})

    def _steps(self, key):
        """A build/incremental may be one command or several (init, then add)."""
        argv = self.spec.get({"build": "session_build"}.get(key, key))
        if not argv:
            return []
        return argv if isinstance(argv[0], list) else [argv]

    def _run_steps(self, key, corpus, index, what):
        """Run all steps, summing wall time and taking the max RSS.

        Timing the whole sequence is the honest number: a tool that needs a
        separate init step is genuinely paying for it.
        """
        total, peak = 0.0, None
        for argv in self._steps(key):
            dt, rss, p = run(self._fmt(argv, corpus=corpus, index=index),
                             env=self._env(index))
            total += dt
            if rss and (peak is None or rss > peak):
                peak = rss
            if p is None or p.returncode != 0:
                raise RuntimeError(f"{self.name} {what} failed: "
                                   f"{(p.stderr or '')[-400:] if p else 'timeout'}")
        return total, peak

    def build(self, corpus, index):
        self.index = index
        if not self._steps("build"):
            return {"seconds": 0.0, "rss": None, "index_bytes": 0, "indexless": True}
        secs, rss = self._run_steps("build", corpus, index, "build")
        return {"seconds": secs, "rss": rss, "index_bytes": dir_bytes(index),
                "indexless": False}

    def update(self, corpus, index):
        if not self._steps("incremental"):
            return {"seconds": 0.0, "indexless": True}
        secs, _ = self._run_steps("incremental", corpus, index, "incremental")
        return {"seconds": secs, "indexless": False}

    def ask(self, question, corpus, index):
        env = dict(os.environ, **{k: v.format(index=index) for k, v in
                                  self.spec.get("env", {}).items()})
        argv = self.spec["session_query"]
        # A tool may declare a different command per question class (e.g. we use
        # `query` to locate and `affected` to relate).
        #
        # `query_as` lets ONE class use more than one command without inventing
        # a class to carry it: "which env vars does this read" and "which of
        # them are credentials" are both boundary/config, but the second needs
        # the sensitive filter. The class (what is being measured) stays
        # separate from the command (how this tool answers it).
        per_class = self.spec.get("query_by_class", {})
        argv = per_class.get(question.get("query_as", question["class"]), argv)
        # Search tools get the LITERAL symbol, not the natural-language terms —
        # deliberately the most generous input for them. Handing ripgrep
        # "where are requests hashed" and scoring it 0 would be a rigged test;
        # handing it the exact name it needs is the honest hard case for us.
        field = self.spec.get("query_field", "q")
        q = question.get(field) or question["q"]
        dt, _, p = run(self._fmt(argv, corpus=corpus, index=index, query=q),
                       cwd=corpus, env=env, timeout=120)
        if p is None:
            return dt, "", "timeout"
        out = (p.stdout or "") + (p.stderr or "")
        # rg exits 1 on "no matches", which is a legitimate answer; anything
        # else is a broken invocation and must not be graded as a wrong answer.
        broken = None
        if p.returncode not in (0, 1):
            broken = f"exit {p.returncode}: {(p.stderr or '')[:160]}"
        return dt, out, broken


# --------------------------------------------------------------------- grading
#
# Grading is exact substring match against `expect_any` / `expect_all` /
# `expect_absent`, on the tool's own stdout. No LLM, no rubric, no judgement.
# The cost of that rigour is that a question must name something textually
# unambiguous, which is why the question sets are small and hand-checked.
#
# ADR 13 D2 holds the new boundary classes to exactly this bar: an env var at a
# real file:line, a host in a real literal, a route confirmable — every
# expectation below was verified with `ctx-optimize search` (the same
# independent sweep `boundaries verify` uses) before it entered a question set.


def _wordish(ch):
    return ch.isalnum() or ch == "_"


def _found(needle, output):
    """Word-boundary match, but only at edges that ARE word characters.

    A bare `\\b` is wrong for expectations that start or end with punctuation.
    `\\b/healthz\\b` requires a WORD character immediately before the slash, so
    it never matched `port:network.http:</healthz` — the route was right there
    and the grader scored it 0. Measured: api-surface read 0/1 on a route that
    `nodes` was printing.

    A grader lies in both directions, so this is deliberately the narrow fix:
    identifiers still get real boundaries (`Merge` must not match `Merged`),
    while `/healthz` and `ws://x` fall back to substring at the punctuation end.
    """
    left = r"(?<!\w)" if _wordish(needle[0]) else ""
    right = r"(?!\w)" if _wordish(needle[-1]) else ""
    return re.search(left + re.escape(needle) + right, output) is not None


def grade(question, output):
    """Return (scored: bool, correct: bool, detail: str)."""
    if "expect_absent" in question:
        absent = question["expect_absent"]
        hit = _found(absent, output)
        return True, not hit, ("stale: still reports %s" % absent) if hit else "ok"
    # expect_all: EVERY term must appear. This is what makes `transport-shape`
    # gradeable without asking a schema question (ADR 13 D5) — we ask "how does
    # X reach Y", and require the ANSWER to carry both the peer AND the
    # transport. An expect_any there would pass on the host alone and prove
    # nothing about whether we know it is HTTP.
    if "expect_all" in question:
        missing = [w for w in question["expect_all"] if not _found(w, output)]
        if missing:
            return True, False, "missing: " + ", ".join(missing)
        return True, True, "all of: " + ", ".join(question["expect_all"])
    for want in question["expect_any"]:
        if _found(want, output):
            return True, True, want
    return True, False, "no expected symbol in output"


# ------------------------------------------------------------------ the session


def run_session(tool, session, corpus, rounds, keep_index=None):
    qs = session["questions"]
    result = {"tool": tool.name, "rounds": [], "answers_classes": sorted(tool.classes)}
    index = keep_index or tempfile.mkdtemp(prefix=f"sessidx-{tool.name}-")
    edits = EditSession(corpus)

    def ask_all(tag, extra=()):
        rows = []
        for q in list(qs) + list(extra):
            if q["class"] not in tool.classes:
                rows.append({"id": q["id"], "class": q["class"], "scored": False,
                             "reason": "tool cannot answer this class"})
                continue
            dt, out, broken = tool.ask(q, corpus, index)
            if broken:
                rows.append({"id": q["id"], "class": q["class"], "scored": False,
                             "seconds": dt, "reason": "HARNESS/TOOL ERROR: " + broken})
                continue
            scored, ok, detail = grade(q, out)
            rows.append({"id": q["id"], "class": q["class"], "scored": scored,
                         "correct": ok, "seconds": dt, "detail": detail})
        return {"phase": tag, "queries": rows}

    try:
        # PHASE 0 — cold build
        b = tool.build(corpus, index)
        result["build"] = b

        # PHASE 1 — cold queries
        result["cold_queries"] = ask_all("cold")

        for r in range(1, rounds + 1):
            spec = session["rounds"][(r - 1) % len(session["rounds"])]
            applied = [edits.apply(e) for e in spec["edits"]]

            # PHASE 3 — incremental update
            up = tool.update(corpus, index)

            # PHASE 4 — warm queries + staleness probes
            warm = ask_all(f"warm-{r}", spec.get("probes", []))

            # AUDIT.md (2026-07-24) caught GitNexus "warm" runs that were
            # actually full rebuilds on 2 of 4 corpora. Flag it in the data
            # rather than trusting a tool's own words about its warm path.
            cold = result["build"]["seconds"]
            if cold and up["seconds"] >= 0.9 * cold:
                up["suspected_full_rebuild"] = True
                up["note"] = (f"incremental {up['seconds']:.2f}s is >=90% of the "
                              f"cold build {cold:.2f}s — treat as a rebuild")
            result["rounds"].append({"round": r, "edits": applied,
                                     "incremental": up, **warm})
            edits.revert()
            # Put the index back in sync with the reverted tree so the next
            # round starts from the same state the cold build produced.
            tool.update(corpus, index)
    finally:
        edits.revert()
        if not keep_index:
            shutil.rmtree(index, ignore_errors=True)
    return result


# --------------------------------------------------------------------- reporting


def score(rows, cls):
    got = [r for r in rows if r["class"] == cls and r.get("scored")]
    if not got:
        return None, 0
    return sum(1 for r in got if r.get("correct")), len(got)


def report(session, results, out=sys.stdout):
    w = out.write
    corpus = session["corpus"]
    w(f"\nSESSION BENCHMARK — {corpus['name']} @ {corpus['ref']}\n")
    w(f"{session['description']}\n")
    w(f"machine: {platform.platform()} / {os.cpu_count()} cpu\n\n")

    w("SPEED — every tool shares these rows honestly\n")
    w(f"{'tool':<16}{'cold build':>12}{'index':>10}{'peak RSS':>11}"
      f"{'incremental':>14}{'session total':>15}\n")
    for r in results:
        b = r["build"]
        inc = [x["incremental"]["seconds"] for x in r["rounds"]]
        inc_med = statistics.median(inc) if inc else 0.0
        qtime = sum(q.get("seconds", 0.0) for ph in [r["cold_queries"]] + r["rounds"]
                    for q in ph["queries"])
        total = b["seconds"] + sum(inc) + qtime
        build_s = "indexless" if b.get("indexless") else f"{b['seconds']:.2f}s"
        w(f"{r['tool']:<16}{build_s:>12}{human_bytes(b['index_bytes']):>10}"
          f"{human_bytes(b['rss']):>11}{inc_med:>13.2f}s{total:>14.2f}s\n")

    # Classes are discovered from the session, never hardcoded — ADR 13 adds
    # boundary/egress, boundary/config, boundary/process, api-surface,
    # transport-shape and doc-to-code, and a hardcoded locate/relate table
    # would silently drop every one of them.
    classes = []
    for q in session["questions"] + [p for r in session["rounds"]
                                     for p in r.get("probes", [])]:
        if q["class"] not in classes:
            classes.append(q["class"])

    w("\nQUALITY — per class, NEVER blended. A tool that cannot answer a class\n")
    w("is n/a and EXCLUDED, never scored 0 (ADR 13 D1).\n\n")
    w(f"{'class':<20}")
    for r in results:
        w(f"{r['tool']:>18}")
    w("\n")
    for cls in classes:
        w(f"{cls:<20}")
        for r in results:
            cold = score(r["cold_queries"]["queries"], cls)
            warm_rows = [q for ph in r["rounds"] for q in ph["queries"]]
            wm = score(warm_rows, cls)
            if cold[0] is None and wm[0] is None:
                cell = "n/a"
            else:
                parts = []
                if cold[0] is not None:
                    parts.append(f"{cold[0]}/{cold[1]}")
                if wm[0] is not None:
                    parts.append(f"{wm[0]}/{wm[1]}w")
                cell = " ".join(parts)
            w(f"{cell:>18}")
        w("\n")

    # Breadth is a CAPABILITY STATEMENT, not a score (ADR 13 D1). We do not
    # average our unique classes into a number we then win; we say plainly who
    # answers what and let the reader draw the conclusion.
    w("\nCAPABILITY MATRIX — which classes each tool answers AT ALL\n")
    w(f"{'class':<20}")
    for r in results:
        w(f"{r['tool']:>18}")
    w("\n")
    for cls in classes:
        w(f"{cls:<20}")
        for r in results:
            w(f"{('yes' if cls in r.get('answers_classes', []) else '—'):>18}")
        w("\n")

    w("\nSTALENESS DETAIL — did the tool answer the NEW truth after the edit?\n")
    for r in results:
        bad = [q for ph in r["rounds"] for q in ph["queries"]
               if q["class"] == "staleness" and q.get("scored") and not q.get("correct")]
        if not bad:
            any_scored = any(q["class"] == "staleness" and q.get("scored")
                             for ph in r["rounds"] for q in ph["queries"])
            w(f"  {r['tool']:<16}{'all probes fresh' if any_scored else 'n/a'}\n")
        else:
            for q in bad:
                w(f"  {r['tool']:<16}{q['id']}: {q['detail']}\n")
    w("\n")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--session", required=True)
    ap.add_argument("--tools",
                    default=os.path.join(HERE, "..", "suite", "tools.json"),
                    help="THE field manifest. One file, shared with the "
                         "one-pass runner — never fork it.")
    ap.add_argument("--tool", action="append", help="restrict to these tool names")
    ap.add_argument("--rounds", type=int, default=2)
    ap.add_argument("--json", help="write machine-readable results here")
    a = ap.parse_args()

    session = json.load(open(a.session))
    specs = json.load(open(a.tools))["tools"]
    # A manifest entry without session_query is listed for completeness (SaaS,
    # cloud-indexed, not yet adapted) — the field is documented there with the
    # reason. Skipping silently here would hide the gap, so say it.
    runnable = [s for s in specs if s.get("session_query")]
    skipped = [s["name"] for s in specs if not s.get("session_query")]
    if a.tool:
        runnable = [s for s in runnable if s["name"] in a.tool]
    specs = runnable

    corpus = os.path.expanduser(session["corpus"]["path"])
    if not os.path.isdir(corpus):
        sys.exit(f"corpus not found: {corpus}")

    results = []
    for spec in specs:
        try:
            results.append(run_session(Tool(spec), session, corpus, a.rounds))
        except Exception as e:  # a broken adapter must not lose the other tools
            print(f"!! {spec['name']}: {e}", file=sys.stderr)
            results.append({"tool": spec["name"], "error": str(e),
                            "build": {"seconds": 0, "index_bytes": None, "rss": None},
                            "cold_queries": {"queries": []}, "rounds": []})

    report(session, results)
    if a.json:
        json.dump({"session": session, "machine": platform.platform(),
                   "results": results}, open(a.json, "w"), indent=2)
        print(f"wrote {a.json}")


if __name__ == "__main__":
    main()
