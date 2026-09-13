#!/usr/bin/env python3
from __future__ import annotations

import json
from collections import defaultdict
from typing import Any

from project import token_count

WEIGHT = {"critical": 5, "relevant": 2, "supporting": 1, "noise": 0}


def covered(emitted: str, marker: str) -> bool:
    return marker in emitted


def score_case(fx: dict[str, Any], result: dict[str, Any]) -> dict[str, Any]:
    emitted = result["emitted"]
    crit = [o for o in fx["oracle_blocks"] if o["criticality"] == "critical"]
    crit_hit = sum(1 for o in crit if covered(emitted, o["marker"]))
    w_num = w_den = 0
    facet_need: set[str] = set()
    facet_got: set[str] = set()
    for o in fx["oracle_blocks"]:
        w = WEIGHT.get(o["criticality"], 0)
        w_den += w
        hit = covered(emitted, o["marker"])
        if hit:
            w_num += w
            facet_got.update(o["facets"])
        facet_need.update(o["facets"])
    disc = result["disclosure"]
    conserved = True
    if result["selected"] and result["selected"][0].id not in ("prefix", "headtail"):
        conserved = disc["source_blocks"] == disc["emitted_blocks"] + disc["omitted_blocks"]
    canary_in_disc = "FAKESECRET_a1b2c3d4e5f6g7h8i9j0" in json.dumps(disc)
    return {
        "id": fx["id"],
        "family": fx["family"],
        "critical_total": len(crit),
        "critical_hit": crit_hit,
        "critical_recall": 1.0 if not crit else crit_hit / len(crit),
        "weighted_num": w_num,
        "weighted_den": w_den or 1,
        "facet_recall": 1.0 if not facet_need else len(facet_got) / len(facet_need),
        "reduction": 1.0 - (disc["emitted_bytes"] / max(1, disc["source_bytes"])),
        "conserved": conserved,
        "canary_in_disclosure": canary_in_disc,
        "collision": result["collision"],
        "overflow": disc["budget_overflow"],
        "tokens_out": token_count(emitted),
    }


def aggregate(rows: list[dict[str, Any]]) -> dict[str, Any]:
    by_fam: dict[str, list] = defaultdict(list)
    for r in rows:
        by_fam[r["family"]].append(r)
    def avg(xs, key):
        return sum(x[key] for x in xs) / max(1, len(xs))
    families = {}
    for fam, xs in sorted(by_fam.items()):
        families[fam] = {
            "n": len(xs),
            "critical_recall": avg(xs, "critical_recall"),
            "weighted_recall": sum(x["weighted_num"] for x in xs) / max(1, sum(x["weighted_den"] for x in xs)),
            "facet_recall": avg(xs, "facet_recall"),
            "median_reduction": sorted(x["reduction"] for x in xs)[len(xs) // 2],
            "false_mandatory_omissions": sum(1 for x in xs if x["critical_total"] and x["critical_hit"] < x["critical_total"]),
        }
    return {
        "n": len(rows),
        "critical_recall": avg(rows, "critical_recall"),
        "weighted_recall": sum(x["weighted_num"] for x in rows) / max(1, sum(x["weighted_den"] for x in rows)),
        "facet_recall": avg(rows, "facet_recall"),
        "median_reduction": sorted(x["reduction"] for x in rows)[len(rows) // 2] if rows else 0,
        "conservation_fail": sum(1 for x in rows if not x["conserved"]),
        "canary_leaks": sum(1 for x in rows if x["canary_in_disclosure"]),
        "collisions": sum(1 for x in rows if x["collision"]),
        "families": families,
    }
