#!/usr/bin/env python3
"""Stress-test planning artifacts. No product code. Exit 0 only if all checks pass."""

from __future__ import annotations

import json
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
REPO = Path(__file__).resolve().parents[4]
fails: list[str] = []
warns: list[str] = []


def fail(msg: str) -> None:
    fails.append(msg)


def warn(msg: str) -> None:
    warns.append(msg)


def read(rel: str) -> str:
    p = ROOT / rel
    if not p.exists():
        fail(f"missing {rel}")
        return ""
    return p.read_text(encoding="utf-8")


def check_spec(rel: str, capability: str) -> None:
    text = read(rel)
    if not text:
        return
    if not text.startswith("## ADDED Requirements"):
        fail(f"{rel}: must start with ## ADDED Requirements")
    reqs = re.findall(r"^### Requirement: (.+)$", text, re.M)
    if not reqs:
        fail(f"{rel}: no requirements")
    for req in reqs:
        block = text.split(f"### Requirement: {req}", 1)[1]
        nxt = re.search(r"\n### Requirement:", block)
        if nxt:
            block = block[: nxt.start()]
        scenes = re.findall(r"^#### Scenario: (.+)$", block, re.M)
        if not scenes:
            fail(f"{rel}: requirement '{req}' has no #### Scenario")
        if re.search(r"^### Scenario:", block, re.M):
            fail(f"{rel}: '{req}' uses ### Scenario (must be ####)")
        for scene in scenes:
            sblock = block.split(f"#### Scenario: {scene}", 1)[1]
            n = re.search(r"\n#### Scenario:", sblock)
            if n:
                sblock = sblock[: n.start()]
            if "**WHEN**" not in sblock or "**THEN**" not in sblock:
                fail(f"{rel}: scenario '{scene}' missing WHEN/THEN")
    forbidden = ["recall handle", "local recall", "durable recall handle"]
    for phrase in forbidden:
        if phrase in text.lower() and "no command body" not in text.lower():
            warn(f"{rel}: mentions {phrase}")
    print(f"  spec {capability}: {len(reqs)} requirements, "
          f"{len(re.findall(r'^#### Scenario:', text, re.M))} scenarios")


def check_tasks() -> None:
    text = read("tasks.md")
    boxes = re.findall(r"^- \[[ x]\] (\d+\.\d+) ", text, re.M)
    if not boxes:
        fail("tasks.md: no checkbox tasks")
    headings = re.findall(r"^## (\d+)\. ", text, re.M)
    if headings != [str(i) for i in range(1, len(headings) + 1)]:
        fail(f"tasks.md: headings not contiguous: {headings}")
    for i, num in enumerate(boxes):
        pass
    dup = [n for n in boxes if boxes.count(n) > 1]
    if dup:
        fail(f"tasks.md: duplicate ids {sorted(set(dup))}")
    print(f"  tasks: {len(boxes)} checkboxes, {len(headings)} groups")


def check_design() -> None:
    text = read("design.md")
    for d in [f"### D{i} " for i in range(1, 10)]:
        if d not in text:
            fail(f"design.md missing {d.strip()}")
    for needle in ["direct argv", "non-interactive", "no durable raw recall",
                   "graph impact is an optional", "separate acceptance gate"]:
        if needle.lower() not in text.lower() and needle not in text:
            fail(f"design.md missing decision text: {needle}")
    if "Status: PROPOSED" not in text:
        fail("design.md: status is not PROPOSED")
    if "P1" not in text or "P2" not in text:
        fail("design.md: missing spike gates")
    print("  design: D1-D9 present")


def check_proposal() -> None:
    text = read("proposal.md")
    if "recall handle" in text.lower():
        fail("proposal.md still promises a recall handle")
    if "executes a command unchanged" in text:
        fail("proposal.md still claims shell-transparent unchanged execution")
    for cap in ("command-evidence-projection", "structured-transforms"):
        if cap not in text:
            fail(f"proposal.md missing capability {cap}")
        spec = ROOT / "specs" / cap / "spec.md"
        if not spec.exists():
            fail(f"spec folder missing for {cap}")
    if "14,596" not in text or "67.2%" not in text:
        warn("proposal.md census numbers drifted from 14596 / 67.2%")
    print("  proposal: capabilities match spec folders")


def check_spikes() -> None:
    p1 = read("spike-p1-evidence-projection.md")
    p2 = read("spike-p2-structured-transforms.md")
    for label, text in (("p1", p1), ("p2", p2)):
        for section in ("Question", "Acceptance", "Kill Criteria", "Deliverables"):
            if f"## {section}" not in text:
                fail(f"spike-{label} missing ## {section}")
    if "100% critical-block recall" not in p1:
        fail("p1 missing perfect critical-recall gate")
    if "head-plus-tail" not in p1:
        fail("p1 missing head-plus-tail baseline")
    if "Turing-complete" not in p2:
        fail("p2 missing eval kill criterion")
    if "MUST NOT be summed" not in p2:
        fail("p2 missing overlap warning")
    print("  spikes: required sections present")


def check_cross_doc() -> None:
    design = read("design.md")
    p1 = read("spike-p1-evidence-projection.md")
    if "durable raw recall" in design.lower() and "No durable recall" not in read(
        "specs/command-evidence-projection/spec.md"
    ):
        fail("spec does not pin no durable recall")
    if "JSON/NDJSON" in p1 and "json" not in read("proposal.md").lower():
        warn("p1 includes JSON/NDJSON family; proposal first-candidate list does not")
    print("  cross-doc: recall deferral consistent")


def check_openspec() -> None:
    r = subprocess.run(
        ["openspec", "validate", "compact-run-evidence-projection", "--strict"],
        cwd=str(REPO),
        capture_output=True,
        text=True,
    )
    if r.returncode != 0:
        fail(f"openspec validate failed: {r.stdout}{r.stderr}")
    else:
        print("  openspec validate --strict: ok")


def main() -> int:
    print("artifact stress test")
    check_proposal()
    check_design()
    check_spec("specs/command-evidence-projection/spec.md", "command-evidence-projection")
    check_spec("specs/structured-transforms/spec.md", "structured-transforms")
    check_tasks()
    check_spikes()
    check_cross_doc()
    check_openspec()
    for w in warns:
        print(f"WARN {w}")
    if fails:
        for f in fails:
            print(f"FAIL {f}")
        print(json.dumps({"ok": False, "fails": len(fails), "warns": len(warns)}))
        return 1
    print(json.dumps({"ok": True, "fails": 0, "warns": len(warns)}))
    return 0


if __name__ == "__main__":
    sys.exit(main())
