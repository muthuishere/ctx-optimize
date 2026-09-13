#!/usr/bin/env python3
"""Run constructed P1 ablations. Partial spike: no historical corpus, no agent benchmark."""

from __future__ import annotations

import hashlib
import json
import sys
from collections import defaultdict
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))

from eval import aggregate, score_case
from generate import all_fixtures, split
from project import project, token_count

METHODS = [
    "prefix",
    "headtail",
    "source_order",
    "exact_dedupe",
    "drain",
    "logram",
    "idf",
    "idf_mmr",
    "submodular",
    "source_order+mand",
    "exact_dedupe+mand",
    "idf+mand",
    "idf_mmr+mand",
    "submodular+mand",
]
BUDGETS = [256, 512, 1024]


def determinism(fx: dict, method: str, budget: int, n: int = 20) -> bool:
    first = None
    for _ in range(n):
        r = project(fx, method, budget)
        payload = json.dumps({"e": r["emitted"], "d": r["disclosure"]}, sort_keys=True)
        if first is None:
            first = payload
        elif payload != first:
            return False
    return True


def gates(summary: dict, method: str) -> dict[str, bool]:
    fam_ok = all(v["critical_recall"] >= 0.85 for v in summary["families"].values()) if method.endswith("+mand") or method in ("idf", "submodular") else True
    return {
        "perfect_critical": summary["critical_recall"] == 1.0,
        "weighted_90": summary["weighted_recall"] >= 0.90,
        "family_85": all(v["critical_recall"] >= 0.85 for v in summary["families"].values()),
        "reduction_50": summary["median_reduction"] >= 0.50,
        "no_canary": summary["canary_leaks"] == 0,
        "no_collision": summary["collisions"] == 0,
        "conserved": summary["conservation_fail"] == 0,
        "fam_ok": fam_ok,
    }


def main() -> int:
    fixtures = all_fixtures()
    buckets = split(fixtures)
    by_id = {f["id"]: f for f in fixtures}
    test_ids = buckets["test"] or [f["id"] for f in fixtures]
    # Use all constructed fixtures for this partial run; split is recorded.
    eval_set = fixtures
    late = by_id["logs-late-fatal"]
    results = {
        "n_fixtures": len(fixtures),
        "by_family": {fam: sum(1 for f in fixtures if f["family"] == fam) for fam in ("logs", "tests", "search", "git", "json")},
        "split": {k: len(v) for k, v in buckets.items()},
        "test_ids": test_ids,
        "methods": {},
        "contracts": {},
        "late_fatal": {},
        "verdict": {},
    }
    owns: dict[str, set] = defaultdict(set)
    for name, ids in buckets.items():
        for i in ids:
            owns[by_id[i]["template_cluster"]].add(name)
    results["split_cluster_leaks"] = sum(1 for s in owns.values() if len(s) > 1)
    for method in METHODS:
        rows = []
        for fx in eval_set:
            r = project(fx, method, 512)
            rows.append(score_case(fx, r))
        summary = aggregate(rows)
        summary["gates"] = gates(summary, method)
        results["methods"][method] = summary
        lf = score_case(late, project(late, method, 256))
        results["late_fatal"][method] = {
            "critical_recall": lf["critical_recall"],
            "reduction": lf["reduction"],
        }
    results["percent_budgets"] = {}
    for method in ("prefix", "headtail", "idf+mand", "submodular+mand"):
        rows10, rows25 = [], []
        for fx in eval_set:
            b10 = max(8, token_count(fx["stdout"] + fx["stderr"]) // 10)
            b25 = max(8, token_count(fx["stdout"] + fx["stderr"]) // 4)
            rows10.append(score_case(fx, project(fx, method, b10)))
            rows25.append(score_case(fx, project(fx, method, b25)))
        results["percent_budgets"][method] = {
            "p10": {
                "critical_recall": aggregate(rows10)["critical_recall"],
                "weighted_recall": aggregate(rows10)["weighted_recall"],
                "median_reduction": aggregate(rows10)["median_reduction"],
                "false_mandatory_omissions": sum(1 for x in rows10 if x["critical_total"] and x["critical_hit"] < x["critical_total"]),
            },
            "p25": {
                "critical_recall": aggregate(rows25)["critical_recall"],
                "weighted_recall": aggregate(rows25)["weighted_recall"],
                "median_reduction": aggregate(rows25)["median_reduction"],
                "false_mandatory_omissions": sum(1 for x in rows25 if x["critical_total"] and x["critical_hit"] < x["critical_total"]),
            },
        }
        if method == "idf+mand":
            results["p10_idf_mand_misses"] = [
                x["id"] for x in rows10 if x["critical_total"] and x["critical_hit"] < x["critical_total"]
            ]

    fx0 = fixtures[0]
    results["contracts"]["determinism_20"] = determinism(fx0, "idf+mand", 512, 20)
    results["contracts"]["determinism_100"] = determinism(late, "submodular+mand", 512, 100)
    r = project(late, "idf+mand", 512)
    results["contracts"]["conservation"] = (
        r["disclosure"]["source_blocks"]
        == r["disclosure"]["emitted_blocks"] + r["disclosure"]["omitted_blocks"]
    )
    results["contracts"]["canary_absent_from_disclosure"] = (
        "FAKESECRET_a1b2c3d4e5f6g7h8i9j0" not in json.dumps(r["disclosure"])
    )
    tiny = project(late, "idf+mand", 8)
    results["contracts"]["mandatory_over_budget"] = bool(
        tiny["disclosure"]["budget_overflow"]
        and "FATAL worker crashed" in tiny["emitted"]
    )
    passfail = by_id["logs-pass-vs-fail-collision"]
    drain_c = project(passfail, "drain", 512)["collision"]
    results["contracts"]["drain_pass_fail_collision"] = drain_c

    mand = results["methods"]["idf+mand"]
    base = results["methods"]["headtail"]
    idf = results["methods"]["idf"]
    mmr = results["methods"]["idf_mmr"]
    results["verdict"] = {
        "prefix_loses_late_fatal": results["late_fatal"]["prefix"]["critical_recall"] < 1.0,
        "mandatory_keeps_late_fatal": results["late_fatal"]["idf+mand"]["critical_recall"] == 1.0,
        "headtail_critical": base["critical_recall"],
        "idf_mand_critical": mand["critical_recall"],
        "idf_mand_weighted": mand["weighted_recall"],
        "mmr_gain_over_idf": mmr["weighted_recall"] - idf["weighted_recall"],
        "submodular_gain_over_idf": results["methods"]["submodular"]["weighted_recall"] - idf["weighted_recall"],
        "accept_idf_mand": mand["gates"]["perfect_critical"] and mand["gates"]["no_canary"],
        "kill_prefix": results["late_fatal"]["prefix"]["critical_recall"] < 1.0,
        "historical_corpus": False,
        "agent_benchmark": False,
        "graph_impact": False,
        "two_annotator_alpha": False,
    }
    raw = json.dumps(results, sort_keys=True, indent=2)
    results["hash"] = hashlib.sha256(raw.encode()).hexdigest()
    out = Path(__file__).resolve().parents[1] / "results-p1.json"
    out.write_text(json.dumps(results, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps({
        "wrote": str(out),
        "n": len(fixtures),
        "late_fatal_prefix": results["late_fatal"]["prefix"]["critical_recall"],
        "late_fatal_idf_mand": results["late_fatal"]["idf+mand"]["critical_recall"],
        "idf_mand_critical": mand["critical_recall"],
        "idf_mand_weighted": round(mand["weighted_recall"], 3),
        "headtail_critical": round(base["critical_recall"], 3),
        "mmr_gain": round(results["verdict"]["mmr_gain_over_idf"], 4),
        "contracts": results["contracts"],
    }, indent=2))
    return 0


if __name__ == "__main__":
    sys.exit(main())
