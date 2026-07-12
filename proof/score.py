#!/usr/bin/env python3
"""Extract comparable metrics from proof-matrix raw runs.

Reads the per-run JSON files run.py produced and prints one row per run:
tokens (fresh = input + cache_creation + output; cache reads listed
separately), turns/calls where the harness reports them, wall seconds, and
the answer text (to answers/ for judging).

Token semantics differ per harness — extraction is per-harness, comparisons
are only ever A-vs-B within one harness.
"""
import json
import pathlib
import sys


def claude_metrics(rec):
    d = json.loads(rec["stdout"])
    u = d.get("usage", {})
    fresh = (u.get("input_tokens", 0) + u.get("cache_creation_input_tokens", 0)
             + u.get("output_tokens", 0))
    return {"fresh_tokens": fresh,
            "cache_read_tokens": u.get("cache_read_input_tokens", 0),
            "turns": d.get("num_turns"),
            "cost_usd": d.get("total_cost_usd"),
            "answer": d.get("result", "")}


def codex_metrics(rec):
    last_usage, answer, calls = None, "", 0
    for line in rec["stdout"].splitlines():
        try:
            ev = json.loads(line)
        except json.JSONDecodeError:
            continue
        t = ev.get("type", "")
        if t in ("item.completed",):
            item = ev.get("item", {})
            if item.get("item_type") == "command_execution" or item.get("type") == "command_execution":
                calls += 1
            if item.get("item_type") == "agent_message" or item.get("type") == "agent_message":
                answer = item.get("text", answer)
        if "token" in t or "usage" in json.dumps(ev)[:200].lower():
            info = ev.get("usage") or ev.get("info", {}).get("total_token_usage") or ev.get("info", {}).get("token_usage")
            if info:
                last_usage = info
    if not answer:
        answer = rec.get("aux", "")
    m = {"turns": calls or None, "answer": answer}
    if last_usage:
        cached = last_usage.get("cached_input_tokens", 0)
        m["fresh_tokens"] = (last_usage.get("input_tokens", 0) - cached
                             + last_usage.get("output_tokens", 0))
        m["cache_read_tokens"] = cached
    return m


def devin_metrics(rec):
    # devin -p --export writes an ATIF trace; final_metrics carries totals
    m = {"answer": rec["stdout"].strip()}
    aux = rec.get("aux", "")
    try:
        fm = json.loads(aux).get("final_metrics", {})
    except (json.JSONDecodeError, TypeError):
        # aux may be a truncated tail of a large export; final_metrics is the
        # last key, so recover it textually
        import re
        mt = re.search(r'"final_metrics"\s*:\s*(\{[^}]*\})', aux)
        fm = json.loads(mt.group(1)) if mt else {}
    if fm:
        cached = fm.get("total_cached_tokens", 0)
        m["fresh_tokens"] = (fm.get("total_prompt_tokens", 0) - cached
                             + fm.get("total_completion_tokens", 0))
        m["cache_read_tokens"] = cached
        m["turns"] = fm.get("total_steps")
    return m


EXTRACT = {"claude": claude_metrics, "codex": codex_metrics, "devin": devin_metrics}


def main():
    out_dir = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else "results")
    answers = out_dir / "answers"
    answers.mkdir(exist_ok=True)
    rows = []
    for p in sorted(out_dir.glob("*-*-q*.json")):
        rec = json.loads(p.read_text())
        try:
            m = EXTRACT[rec["harness"].partition("@")[0]](rec)
        except Exception as e:
            m = {"error": f"{type(e).__name__}: {e}", "answer": ""}
        row = {"run": p.stem, "harness": rec["harness"], "arm": rec["arm"],
               "qid": rec["qid"], "wall_s": rec["wall_s"], "exit": rec["exit"],
               **{k: v for k, v in m.items() if k != "answer"}}
        rows.append(row)
        (answers / f"{p.stem}.md").write_text(m.get("answer", ""))
    print(json.dumps(rows, indent=1))
    (out_dir / "metrics.json").write_text(json.dumps(rows, indent=1))


if __name__ == "__main__":
    main()
