#!/usr/bin/env python3
"""Proof-matrix runner — the pre-committed composite test (docs/CRITIQUE.md).

For each harness in {claude, codex, devin}, run every question twice on the
same corpus:

  arm A — grep/read only (the baseline agent everyone already has)
  arm B — ctx-optimize store first (query / card / explain / path / affected
          / wiki), file reads only to fill gaps

and capture each harness's OWN usage reporting plus wall time. Token numbers
are only ever compared WITHIN a harness (A vs B ratio) — cross-harness token
accounting is not comparable (CRITIQUE risk #5).

Usage:
  run.py --corpus DIR --out DIR [--harness claude,codex,devin] [--arm a,b]
         [--q q1,q2,...] [--questions questions.json]

One JSON file per run lands in --out: <harness>-<arm>-<qid>.json with the
answer, raw usage payload, wall seconds, and exit code. Scoring is a separate
step (score.py) so runs are resumable — existing result files are skipped.
"""
import argparse
import json
import pathlib
import subprocess
import sys
import time

ARM_A_RULES = (
    "You are answering a question about the codebase in the current directory. "
    "Use ONLY shell search and file reading (grep/rg/find/sed/cat or your file "
    "tools). Do NOT use ctx-optimize, any prebuilt index, knowledge store, or "
    "wiki — the repo source is your only resource."
)

ARM_B_RULES = (
    "You are answering a question about the codebase in the current directory. "
    "A ctx-optimize knowledge store for this repo is already built. Use it FIRST "
    "and prefer it over opening files:\n"
    '  ctx-optimize query "<search terms>"   # ranked symbol hits with signatures\n'
    "  ctx-optimize card <symbol>            # signature + doc + callers/callees, no file read\n"
    "  ctx-optimize explain <symbol>         # relationships around a symbol\n"
    "  ctx-optimize path <sym-a> <sym-b>     # how two symbols connect\n"
    "  ctx-optimize affected <symbol>        # impact: what depends on it\n"
    "  plus wiki pages under ~/ctxoptimize/linux/wiki/ (start at index.md).\n"
    "Run these from the current directory. Only open source files if the store "
    "leaves a specific gap the answer still needs."
)

COMMON = (
    "Answer concisely with function names and file:line citations. "
    "Do not modify anything.\n\nQuestion: {q}"
)

TIMEOUT = 900


def build_cmd(harness, prompt, aux_path):
    # harness may carry a model variant: "claude@haiku", "codex@gpt-5-mini"
    base, _, model = harness.partition("@")
    if base == "claude":
        cmd = ["claude", "-p", prompt, "--output-format", "json",
               "--dangerously-skip-permissions"]
        if model:
            cmd += ["--model", model]
        return cmd
    if base == "codex":
        cmd = ["codex", "exec", "--json", "--skip-git-repo-check",
               "-s", "danger-full-access",
               "-o", str(aux_path)]
        if model:
            cmd += ["-m", model]
        return cmd + [prompt]
    if base == "devin":
        cmd = ["devin", "-p", prompt, "--permission-mode", "dangerous",
               "--export", str(aux_path)]
        if model:
            cmd += ["--model", model]
        return cmd
    raise ValueError(harness)


def run_one(harness, arm, q, corpus, out_dir):
    result_path = out_dir / f"{harness}-{arm}-{q['id']}.json"
    if result_path.exists():
        print(f"  skip {result_path.name} (exists)")
        return
    rules = ARM_A_RULES if arm == "a" else ARM_B_RULES
    prompt = rules + "\n\n" + COMMON.format(q=q["prompt"])
    aux_path = out_dir / f"{harness}-{arm}-{q['id']}.aux"
    cmd = build_cmd(harness, prompt, aux_path)
    t0 = time.time()
    try:
        proc = subprocess.run(cmd, cwd=corpus, capture_output=True, text=True,
                              timeout=TIMEOUT)
        wall = time.time() - t0
        rec = {"harness": harness, "arm": arm, "qid": q["id"],
               "wall_s": round(wall, 1), "exit": proc.returncode,
               "stdout": proc.stdout, "stderr": proc.stderr[-4000:]}
    except subprocess.TimeoutExpired:
        rec = {"harness": harness, "arm": arm, "qid": q["id"],
               "wall_s": TIMEOUT, "exit": -1, "stdout": "", "stderr": "TIMEOUT"}
    if aux_path.exists():
        rec["aux"] = aux_path.read_text()[-200000:]
        aux_path.unlink()
    result_path.write_text(json.dumps(rec, indent=1))
    print(f"  done {result_path.name}  wall={rec['wall_s']}s exit={rec['exit']}")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--corpus", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--harness", default="claude")
    ap.add_argument("--arm", default="a,b")
    ap.add_argument("--q", default="all")
    ap.add_argument("--questions",
                    default=str(pathlib.Path(__file__).parent / "questions.json"))
    args = ap.parse_args()

    questions = json.loads(pathlib.Path(args.questions).read_text())["questions"]
    if args.q != "all":
        wanted = set(args.q.split(","))
        questions = [q for q in questions if q["id"] in wanted]
    out_dir = pathlib.Path(args.out)
    out_dir.mkdir(parents=True, exist_ok=True)

    for harness in args.harness.split(","):
        for q in questions:
            for arm in args.arm.split(","):
                print(f"[{harness} {arm} {q['id']}]", flush=True)
                run_one(harness, arm, q, args.corpus, out_dir)


if __name__ == "__main__":
    main()
