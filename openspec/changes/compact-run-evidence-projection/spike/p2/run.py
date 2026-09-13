#!/usr/bin/env python3
"""Replay constructed Python vs closed transforms. Independent of P1."""

from __future__ import annotations

import ast
import hashlib
import json
import subprocess
import sys
import tempfile
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent))
from transform import Reject, run, validate

PROGRAMS = [
    {
        "id": "json-pluck-ids",
        "py": "import json,sys; d=json.load(sys.stdin); print(json.dumps([x['id'] for x in d['items']], separators=(',', ': ')))",
        "stdin": '{"items":[{"id":1},{"id":2}]}',
        "ops": [{"op": "parse_json"}, {"op": "project", "path": ["items"]}, {"op": "pluck", "key": "id"}],
        "replaceable": True,
        "category": "json",
    },
    {
        "id": "json-filter-eq",
        "py": "import json,sys; d=json.load(sys.stdin); print(json.dumps([x for x in d if x.get('ok') is True], separators=(',', ':')))",
        "stdin": '[{"ok":true,"n":1},{"ok":false,"n":2},{"ok":true,"n":3}]',
        "ops": [{"op": "parse_json"}, {"op": "filter_eq", "key": "ok", "value": True}],
        "replaceable": True,
        "category": "json",
    },
    {
        "id": "json-null-vs-missing",
        "py": "import json,sys; rows=json.load(sys.stdin); print(json.dumps([r for r in rows if 'a' in r], separators=(',', ':')))",
        "stdin": '[{"a":null},{"b":1},{"a":2}]',
        "ops": [{"op": "parse_json"}, {"op": "filter_exists", "key": "a"}],
        "replaceable": True,
        "category": "json",
        "note": "null is present; missing is not",
    },
    {
        "id": "ndjson-malformed",
        "py": "import json,sys\nfor i,line in enumerate(sys.stdin):\n    json.loads(line)\n",
        "stdin": '{"ok":true}\n{bad\n{"ok":false}\n',
        "ops": [{"op": "parse_ndjson"}],
        "replaceable": True,
        "category": "json",
        "expect_nonzero": True,
    },
    {
        "id": "sort-unique",
        "py": "import json,sys; xs=json.load(sys.stdin); print(json.dumps(sorted(set(xs))))",
        "stdin": "[3,1,2,1,3]",
        "ops": [{"op": "parse_json"}, {"op": "unique"}, {"op": "sort"}],
        "replaceable": True,
        "category": "aggregation",
    },
    {
        "id": "group-count",
        "py": "import json,sys,collections; rows=json.load(sys.stdin); c=collections.Counter(r['k'] for r in rows); print(json.dumps([{'count':c[k],'key':k} for k in sorted(c)]))",
        "stdin": '[{"k":"a"},{"k":"b"},{"k":"a"}]',
        "ops": [{"op": "parse_json"}, {"op": "group_count", "key": "k"}],
        "replaceable": True,
        "category": "aggregation",
    },
    {
        "id": "regex-extract",
        "py": "import json,re,sys; print(json.dumps(re.findall(r'id=(\\d+)', sys.stdin.read())))",
        "stdin": "id=10 foo id=22\n",
        "ops": [{"op": "regex_extract", "pattern": r"id=(\d+)", "group": 1}],
        "replaceable": True,
        "category": "text",
    },
    {
        "id": "assert-min",
        "py": "import json,sys; xs=json.load(sys.stdin)\nassert len(xs)>=1\nprint(json.dumps(xs, separators=(',', ':')))",
        "stdin": "[]",
        "ops": [{"op": "parse_json"}, {"op": "assert_min", "min": 1}],
        "replaceable": True,
        "category": "verification",
        "expect_nonzero": True,
    },
    {
        "id": "eval-forbidden",
        "py": "eval(sys.stdin.read())",
        "stdin": "1+1",
        "ops": [{"op": "eval", "code": "1+1"}],
        "replaceable": False,
        "category": "general",
    },
    {
        "id": "write-forbidden",
        "py": "open('/tmp/x','w').write('x')",
        "stdin": "",
        "ops": [{"op": "write", "path": "/tmp/x"}],
        "replaceable": False,
        "category": "writes",
    },
    {
        "id": "import-os-forbidden",
        "py": "import os; print(os.listdir('.'))",
        "stdin": "",
        "ops": None,
        "replaceable": False,
        "category": "filesystem",
    },
    {
        "id": "json-unicode-pluck",
        "py": "import json,sys; d=json.load(sys.stdin); print(json.dumps(d['msg']))",
        "stdin": '{"msg":"naïve"}',
        "ops": [{"op": "parse_json"}, {"op": "project", "path": ["msg"]}],
        "replaceable": True,
        "category": "json",
    },
    {
        "id": "empty-group",
        "py": "import json,sys; rows=json.load(sys.stdin); print(json.dumps([]))",
        "stdin": "[]",
        "ops": [{"op": "parse_json"}, {"op": "group_count", "key": "k"}],
        "replaceable": True,
        "category": "aggregation",
    },
]


def py_run(code: str, stdin: str) -> tuple[int, str, str]:
    with tempfile.NamedTemporaryFile("w", suffix=".py", delete=False) as f:
        f.write(code)
        path = f.name
    r = subprocess.run(
        [sys.executable, path],
        input=stdin.encode(),
        capture_output=True,
        timeout=5,
        cwd=tempfile.gettempdir(),
    )
    Path(path).unlink(missing_ok=True)
    return r.returncode, r.stdout.decode(), r.stderr.decode()


def ast_hash(code: str) -> str:
    try:
        tree = ast.parse(code)
        dump = ast.dump(tree, annotate_fields=True)
    except SyntaxError:
        dump = code
    return hashlib.sha256(dump.encode()).hexdigest()[:16]


def jsonish_equal(a: str, b: str) -> bool:
    a, b = a.strip(), b.strip()
    try:
        return json.loads(a) == json.loads(b)
    except Exception:
        return a == b


def main() -> int:
    rows = []
    supported_ok = 0
    supported_n = 0
    false_supported = 0
    rejected_unsupported = 0
    for p in PROGRAMS:
        rec = {"id": p["id"], "category": p["category"], "ast": ast_hash(p["py"]), "replaceable": p["replaceable"]}
        if not p["replaceable"]:
            try:
                if p.get("ops") is None:
                    raise Reject("no mapping")
                validate(p["ops"])
                rec["status"] = "FALSE_SUPPORTED"
                false_supported += 1
            except Reject:
                rec["status"] = "rejected"
                rejected_unsupported += 1
            rows.append(rec)
            continue
        supported_n += 1
        code, out, err = run(p["ops"], p["stdin"])
        py_code, py_out, py_err = py_run(p["py"], p["stdin"])
        nonzero = bool(p.get("expect_nonzero"))
        if nonzero:
            match = (code != 0) and (py_code != 0)
        else:
            match = code == 0 and py_code == 0 and jsonish_equal(out, py_out)
        rec["transform_exit"] = code
        rec["python_exit"] = py_code
        rec["match"] = match
        rec["status"] = "ok" if match else "mismatch"
        if match:
            supported_ok += 1
        rows.append(rec)

    unique = len({r["ast"] for r in rows})
    replaceable = [r for r in rows if r["replaceable"]]
    results = {
        "n": len(PROGRAMS),
        "unique_ast": unique,
        "supported_n": supported_n,
        "supported_ok": supported_ok,
        "equivalence": supported_ok / max(1, supported_n),
        "false_supported": false_supported,
        "rejected_unsupported": rejected_unsupported,
        "occurrence_weighted_replaceable": sum(1 for r in rows if r["replaceable"]) / len(rows),
        "rows": rows,
        "historical_census_replayed": False,
        "write_gate": "not-run",
        "agent_authoring": False,
    }
    results["verdict"] = {
        "read_only_prototype_equivalent": supported_ok == supported_n and false_supported == 0,
        "eval_rejected": any(r["id"] == "eval-forbidden" and r["status"] == "rejected" for r in rows),
        "write_rejected": any(r["id"] == "write-forbidden" and r["status"] == "rejected" for r in rows),
        "null_vs_missing": next(r["status"] for r in rows if r["id"] == "json-null-vs-missing"),
        "malformed_ndjson_fails": next(r["match"] for r in rows if r["id"] == "ndjson-malformed"),
        "coverage_gate_met": False,
        "reason": "constructed N=11; 50% occurrence-weighted gate needs the 14596 census replay",
    }
    outp = Path(__file__).resolve().parents[1] / "results-p2.json"
    outp.write_text(json.dumps(results, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    print(json.dumps({
        "wrote": str(outp),
        "equivalence": results["equivalence"],
        "false_supported": false_supported,
        "rejected_unsupported": rejected_unsupported,
        "verdict": results["verdict"],
    }, indent=2))
    return 0 if supported_ok == supported_n and false_supported == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
