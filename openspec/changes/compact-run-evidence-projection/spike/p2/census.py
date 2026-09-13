#!/usr/bin/env python3
"""Aggregate-only AST census of local Claude transcripts. Never prints program text."""

from __future__ import annotations

import ast
import hashlib
import json
import os
import re
import sys
from collections import Counter
from pathlib import Path

ROOT = Path.home() / ".claude" / "projects"
PY_CMD = re.compile(r"python3?(?:\s+-c\s+(['\"])(?P<c>.*?)\1|(?P<rest>[^\n]*))", re.S)
FENCE = re.compile(r"```python\n(.*?)```", re.S)


def classify(src: str) -> set[str]:
    tags = set()
    low = src.lower()
    if any(x in low for x in ("json.loads", "json.load", "json.dumps", "ndjson")):
        tags.add("json")
    if "open(" in src and any(m in src for m in ("'w'", '"w"', "'a'", '"a"')):
        tags.add("writes")
    if any(x in low for x in ("re.", "regex", "split(", "replace(")):
        tags.add("text")
    if any(x in low for x in ("assert ", "assert(", "sys.exit")):
        tags.add("verification")
    if any(x in low for x in ("sum(", "counter", "groupby", "collections.")):
        tags.add("aggregation")
    if any(x in low for x in ("os.path", "pathlib", "listdir", "glob")):
        tags.add("filesystem")
    if "sqlite" in low:
        tags.add("sqlite")
    return tags or {"other"}


def norm_hash(src: str) -> str:
    try:
        dump = ast.dump(ast.parse(src), annotate_fields=True)
    except SyntaxError:
        dump = src.strip()
    return hashlib.sha256(dump.encode("utf-8", "replace")).hexdigest()


def extract(text: str) -> list[str]:
    out = []
    for m in FENCE.finditer(text):
        out.append(m.group(1))
    for m in PY_CMD.finditer(text):
        if m.group("c"):
            out.append(m.group("c"))
    return out


def main() -> int:
    if not ROOT.exists():
        print(json.dumps({"ok": False, "reason": "no ~/.claude/projects"}))
        return 2
    occ = 0
    hashes: Counter[str] = Counter()
    cats: Counter[str] = Counter()
    files = 0
    # Bound work: newest 400 jsonl files
    found = []
    for p in ROOT.rglob("*.jsonl"):
        try:
            found.append((p.stat().st_mtime, p))
        except OSError:
            continue
    jsonl = [p for _, p in sorted(found, reverse=True)[:400]]
    for path in jsonl:
        files += 1
        try:
            raw = path.read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue
        for src in extract(raw):
            if len(src) < 8:
                continue
            occ += 1
            hashes[norm_hash(src)] += 1
            for t in classify(src):
                cats[t] += 1
    unique = len(hashes)
    print(json.dumps({
        "ok": True,
        "transcript_files_scanned": files,
        "programs": occ,
        "unique_ast": unique,
        "categories_overlap": dict(cats),
        "note": "newest 400 jsonl only; program text never printed; not the original 30-day 14596 census",
        "ast_method": "ast.dump(ast.parse(src), annotate_fields=True) then sha256",
    }, sort_keys=True))
    return 0


if __name__ == "__main__":
    sys.exit(main())
