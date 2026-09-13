#!/usr/bin/env python3
"""Closed read-only transform prototype. No eval, imports, subprocess, or writes."""

from __future__ import annotations

import json
import re
from typing import Any

OPS = {
    "parse_json",
    "parse_ndjson",
    "project",
    "pluck",
    "filter_eq",
    "filter_exists",
    "sort",
    "unique",
    "group_count",
    "regex_extract",
    "assert_eq",
    "assert_min",
}


class Reject(Exception):
    pass


class Fail(Exception):
    def __init__(self, msg: str, code: int = 1):
        super().__init__(msg)
        self.code = code


def _get(obj: Any, path: list[str]) -> Any:
    cur = obj
    for p in path:
        if isinstance(cur, dict):
            if p not in cur:
                return _Missing
            cur = cur[p]
        else:
            return _Missing
    return cur


class _MissingType:
    pass


_Missing = _MissingType()


def validate(ops: list[dict[str, Any]]) -> None:
    if not ops:
        raise Reject("empty transform")
    for op in ops:
        name = op.get("op")
        if name not in OPS:
            raise Reject(f"unsupported op {name!r}")
        extra = set(op) - {"op", "path", "key", "value", "pattern", "group", "min"}
        if extra:
            raise Reject(f"unknown fields {sorted(extra)}")


def run(ops: list[dict[str, Any]], stdin: str) -> tuple[int, str, str]:
    validate(ops)
    data: Any = stdin
    consumed = False
    try:
        for op in ops:
            name = op["op"]
            if name == "parse_json":
                consumed = True
                data = json.loads(stdin)
            elif name == "parse_ndjson":
                consumed = True
                rows = []
                for i, line in enumerate(stdin.splitlines()):
                    if not line.strip():
                        continue
                    try:
                        rows.append(json.loads(line))
                    except json.JSONDecodeError as e:
                        raise Fail(f"malformed ndjson record {i}: {e}")
                data = rows
            elif name == "project":
                data = _get(data, list(op["path"]))
                if data is _Missing:
                    raise Fail("path missing")
            elif name == "pluck":
                if not isinstance(data, list):
                    raise Fail("pluck requires array")
                key = op["key"]
                data = [_get(x, [key]) if isinstance(x, dict) else _Missing for x in data]
                data = [x for x in data if x is not _Missing]
            elif name == "filter_eq":
                key, value = op["key"], op["value"]
                if not isinstance(data, list):
                    raise Fail("filter requires array")
                out = []
                for row in data:
                    got = _get(row, [key]) if isinstance(row, dict) else _Missing
                    if got is _Missing:
                        continue
                    if got == value:
                        out.append(row)
                data = out
            elif name == "filter_exists":
                key = op["key"]
                if not isinstance(data, list):
                    raise Fail("filter requires array")
                data = [row for row in data if isinstance(row, dict) and key in row]
            elif name == "sort":
                key = op.get("key")
                if isinstance(data, list):
                    if key:
                        data = sorted(data, key=lambda r: json.dumps(_get(r, [key]), sort_keys=True))
                    else:
                        data = sorted(data, key=lambda r: json.dumps(r, sort_keys=True))
            elif name == "unique":
                if not isinstance(data, list):
                    raise Fail("unique requires array")
                seen = set()
                out = []
                for x in data:
                    k = json.dumps(x, sort_keys=True)
                    if k in seen:
                        continue
                    seen.add(k)
                    out.append(x)
                data = out
            elif name == "group_count":
                key = op["key"]
                if not isinstance(data, list):
                    raise Fail("group_count requires array")
                counts: dict[str, int] = {}
                for row in data:
                    got = _get(row, [key]) if isinstance(row, dict) else _Missing
                    k = json.dumps(got, sort_keys=True)
                    counts[k] = counts.get(k, 0) + 1
                data = [{"key": json.loads(k), "count": c} for k, c in sorted(counts.items())]
            elif name == "regex_extract":
                pat = re.compile(op["pattern"])
                grp = int(op.get("group", 0))
                found = [m.group(grp) for m in pat.finditer(stdin if not consumed or isinstance(data, str) else json.dumps(data))]
                data = found
            elif name == "assert_eq":
                if data != op["value"]:
                    raise Fail(f"assert_eq failed: got {data!r} want {op['value']!r}")
            elif name == "assert_min":
                n = len(data) if isinstance(data, list) else 0
                if n < int(op["min"]):
                    raise Fail(f"assert_min failed: got {n} want >= {op['min']}")
        out = json.dumps(data, sort_keys=True) + "\n"
        return 0, out, ""
    except Fail as e:
        return e.code, "", str(e) + "\n"
