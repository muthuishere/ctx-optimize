#!/usr/bin/env python3
"""Constructed adversarial fixtures. Oracle is authored with the fixture, not by the projector."""

from __future__ import annotations

import hashlib
import json
from typing import Any

CANARY = "FAKESECRET_a1b2c3d4e5f6g7h8i9j0"
FAMILIES = ("logs", "tests", "search", "git", "json")


def _oracle(oid: str, marker: str, criticality: str, facets: list[str], template: str) -> dict[str, Any]:
    return {
        "id": oid,
        "marker": marker,
        "criticality": criticality,
        "facets": facets,
        "template": template,
    }


def _fx(
    fid: str,
    family: str,
    argv: list[str],
    exit_code: int,
    stdout: str,
    stderr: str,
    oracles: list[dict[str, Any]],
    diagnosis: str,
    next_action: str,
    session: str,
    repo: str,
) -> dict[str, Any]:
    raw = (stdout + "\n" + stderr).encode("utf-8")
    return {
        "id": fid,
        "family": family,
        "origin": "constructed",
        "argv": argv,
        "exit": exit_code,
        "stdout": stdout,
        "stderr": stderr,
        "canaries": [CANARY],
        "oracle_blocks": oracles,
        "expected_diagnosis": diagnosis,
        "expected_next_action": next_action,
        "session": session,
        "repo": repo,
        "template_cluster": hashlib.sha256((oracles[0]["template"] if oracles else fid).encode()).hexdigest()[:12],
        "bytes": len(raw),
        "graph_snapshot_hash": "none",
    }


def _noise(n: int, prefix: str) -> str:
    lines = []
    for i in range(n):
        lines.append(
            f"2026-09-13T07:08:{i % 60:02d}.{i:03d}Z pid={1000 + i} INFO {prefix} "
            f"id=aaaaaaaa-bbbb-cccc-dddd-{i:012d} ok=true"
        )
        if i == n // 2:
            lines.append(f"DEBUG leftover token {CANARY} ignored")
    return "\n".join(lines)


def logs_fixtures() -> list[dict[str, Any]]:
    out: list[dict[str, Any]] = []
    noise = _noise(80, "accepted request")
    fatal = (
        "2026-09-13T07:09:00.001Z pid=9999 FATAL worker crashed file=internal/worker.go line=88\n"
        "goroutine 1 [running]:\n"
        "main.runWorker(0x1)\n"
        "\tinternal/worker.go:88 +0x123\n"
        "main.main()\n"
        "\tcmd/app/main.go:12 +0x45"
    )
    out.append(
        _fx(
            "logs-late-fatal",
            "logs",
            ["app"],
            1,
            noise,
            fatal,
            [
                _oracle("c1", "FATAL worker crashed", "critical", ["cause", "location", "summary"], "fatal worker crashed file line"),
                _oracle("c2", "internal/worker.go:88", "critical", ["location"], "stack worker.go"),
                _oracle("n1", "accepted request", "noise", [], "info accepted request"),
            ],
            "worker crashed at worker.go:88",
            "open internal/worker.go:88",
            "s1",
            "svc-a",
        )
    )
    repeated = "\n".join(
        f"2026-09-13T07:10:{i:02d}Z pid={2000 + i} WARN retry timeout id={i:08x}-cafe-babe-dead-beefdeadbeef"
        for i in range(30)
    )
    out.append(
        _fx(
            "logs-repeated-template",
            "logs",
            ["app"],
            0,
            repeated + "\n2026-09-13T07:11:00Z pid=1 INFO done retries=30",
            "",
            [
                _oracle("s1", "INFO done retries=30", "relevant", ["summary"], "info done retries"),
                _oracle("w1", "WARN retry timeout", "supporting", ["cause"], "warn retry timeout"),
            ],
            "30 retry timeouts then success",
            "inspect timeout budget",
            "s1",
            "svc-a",
        )
    )
    stack = (
        "Traceback (most recent call last):\n"
        '  File "job.py", line 41, in run\n'
        "    parse(blob)\n"
        '  File "job.py", line 18, in parse\n'
        "    raise ValueError('bad record')\n"
        "ValueError: bad record"
    )
    out.append(
        _fx(
            "logs-multiline-stack",
            "logs",
            ["python", "job.py"],
            1,
            "starting job\n" + _noise(20, "tick"),
            stack,
            [
                _oracle("c1", "ValueError: bad record", "critical", ["cause", "summary"], "valueerror bad record"),
                _oracle("c2", 'File "job.py", line 18', "critical", ["location"], "traceback job.py"),
            ],
            "parse raised ValueError at job.py:18",
            "fix job.py:18",
            "s2",
            "svc-b",
        )
    )
    interleaved = "\n".join(
        [
            "svc=api INFO listening :8080",
            "svc=worker INFO dequeue id=1",
            "svc=api ERROR 500 /invoice/42 db timeout",
            "svc=worker INFO dequeue id=2",
            "svc=api INFO 200 /health",
        ]
    )
    out.append(
        _fx(
            "logs-interleaved-services",
            "logs",
            ["compose", "logs"],
            0,
            interleaved,
            "",
            [
                _oracle("c1", "ERROR 500 /invoice/42", "critical", ["cause", "affected target"], "error 500 invoice"),
                _oracle("n1", "200 /health", "noise", [], "info health"),
            ],
            "invoice 42 failed with db timeout",
            "inspect invoice handler and db",
            "s3",
            "svc-c",
        )
    )
    ansi = "\rDownloading 10%\rDownloading 40%\rDownloading 90%\rDownloading 100%\nOK\n"
    out.append(
        _fx(
            "logs-ansi-progress",
            "logs",
            ["curl"],
            0,
            ansi,
            "",
            [_oracle("s1", "Downloading 100%", "supporting", ["summary"], "progress download"), _oracle("s2", "OK", "relevant", ["summary"], "ok")],
            "download completed",
            "continue",
            "s3",
            "svc-c",
        )
    )
    out.append(
        _fx(
            "logs-pass-vs-fail-collision",
            "logs",
            ["app"],
            1,
            "check user=42 result=PASS latency=3ms\ncheck user=42 result=FAIL latency=4ms expected=active actual=deleted\n",
            "",
            [
                _oracle("c1", "result=FAIL", "critical", ["cause", "summary"], "check result fail"),
                _oracle("p1", "result=PASS", "supporting", ["summary"], "check result pass"),
            ],
            "same user later failed expected active actual deleted",
            "inspect deletion path",
            "s4",
            "svc-d",
        )
    )
    return out


def tests_fixtures() -> list[dict[str, Any]]:
    out: list[dict[str, Any]] = []
    out.append(
        _fx(
            "tests-success",
            "tests",
            ["go", "test", "./..."],
            0,
            "ok  \tgithub.com/acme/app\t0.123s\nPASS\n",
            "",
            [_oracle("s1", "PASS", "relevant", ["summary"], "pass summary")],
            "all tests passed",
            "continue",
            "s5",
            "app",
        )
    )
    compile_err = (
        "internal/bill.go:22:6: undefined: TaxRate\n"
        "FAIL github.com/acme/app [build failed]\n"
    )
    out.append(
        _fx(
            "tests-compiler-error",
            "tests",
            ["go", "test", "./internal"],
            1,
            compile_err,
            "",
            [
                _oracle("c1", "undefined: TaxRate", "critical", ["cause", "location"], "undefined TaxRate"),
                _oracle("c2", "build failed", "critical", ["summary"], "build failed"),
            ],
            "TaxRate undefined at bill.go:22",
            "define TaxRate in bill.go",
            "s5",
            "app",
        )
    )
    one = (
        "=== RUN   TestInvoiceTotal\n"
        "    invoice_test.go:40: got 11 want 10\n"
        "--- FAIL: TestInvoiceTotal (0.00s)\n"
        "FAIL\n"
    )
    out.append(
        _fx(
            "tests-one-failure",
            "tests",
            ["go", "test"],
            1,
            _noise(15, "ok package") + "\n" + one,
            "",
            [
                _oracle("c1", "FAIL: TestInvoiceTotal", "critical", ["affected target", "summary"], "fail TestInvoiceTotal"),
                _oracle("c2", "got 11 want 10", "critical", ["cause"], "got want mismatch"),
            ],
            "TestInvoiceTotal expected 10 got 11",
            "fix invoice total",
            "s6",
            "app",
        )
    )
    many = "".join(
        f"--- FAIL: TestCase{i} (0.01s)\n    case_test.go:{10+i}: boom {i}\n" for i in range(8)
    ) + "FAIL\n"
    out.append(
        _fx(
            "tests-many-failures",
            "tests",
            ["go", "test"],
            1,
            many,
            "",
            [_oracle("c1", "FAIL: TestCase0", "critical", ["affected target"], "fail TestCase"), _oracle("s1", "FAIL", "critical", ["summary"], "fail summary")],
            "multiple TestCase failures",
            "inspect shared fixture",
            "s6",
            "app",
        )
    )
    retry = "--- FAIL: TestFlaky (0.10s)\n    flaky_test.go:9: attempt 1 timeout\n--- PASS: TestFlaky (0.20s)\nPASS\n"
    out.append(
        _fx(
            "tests-retry-then-pass",
            "tests",
            ["pytest", "-q"],
            0,
            retry,
            "",
            [
                _oracle("s1", "PASS", "relevant", ["summary"], "pass"),
                _oracle("w1", "attempt 1 timeout", "supporting", ["cause"], "timeout"),
            ],
            "flaky test passed after retry",
            "consider quarantining TestFlaky",
            "s7",
            "app",
        )
    )
    nested = (
        "Error Trace:   handler_test.go:70\n"
        "Error:       Received unexpected error:\n"
        "             query failed\n"
        "             caused by: connection refused 127.0.0.1:5432\n"
        "--- FAIL: TestHandler (0.02s)\nFAIL\n"
    )
    out.append(
        _fx(
            "tests-nested-cause",
            "tests",
            ["go", "test"],
            1,
            nested,
            "",
            [
                _oracle("c1", "connection refused 127.0.0.1:5432", "critical", ["cause", "location"], "connection refused"),
                _oracle("c2", "FAIL: TestHandler", "critical", ["affected target"], "fail TestHandler"),
            ],
            "handler test failed because postgres refused connection",
            "start postgres on 5432",
            "s7",
            "app",
        )
    )
    warn_late = "WARNING deprecated api used\n" + _noise(25, "ok") + "\nFAIL github.com/acme/app 0.40s\n"
    out.append(
        _fx(
            "tests-warning-late-fail",
            "tests",
            ["go", "test"],
            1,
            warn_late,
            "",
            [
                _oracle("c1", "FAIL github.com/acme/app", "critical", ["summary"], "fail package"),
                _oracle("w1", "WARNING deprecated", "supporting", [], "warning deprecated"),
            ],
            "package failed after warnings",
            "scroll to FAIL line",
            "s8",
            "app",
        )
    )
    return out


def search_fixtures() -> list[dict[str, Any]]:
    out: list[dict[str, Any]] = []
    hunks = "internal/bill.go:22: func TaxRate() {}\ninternal/bill.go:40: return TaxRate()\ninternal/pay.go:8: tax := TaxRate()\n"
    out.append(
        _fx(
            "search-hunks",
            "search",
            ["rg", "TaxRate"],
            0,
            hunks,
            "",
            [_oracle("r1", "internal/bill.go:22", "relevant", ["location"], "bill TaxRate"), _oracle("r2", "internal/pay.go:8", "relevant", ["location"], "pay TaxRate")],
            "TaxRate defined and used",
            "read bill.go:22",
            "s9",
            "app",
        )
    )
    dup = "foo.go:1: TODO fix\nfoo.go:1: TODO fix\nbar.go:9: TODO fix\n"
    out.append(
        _fx(
            "search-duplicates",
            "search",
            ["rg", "TODO"],
            0,
            dup,
            "",
            [_oracle("r1", "bar.go:9", "relevant", ["location"], "todo bar")],
            "duplicate TODO matches",
            "dedupe then inspect bar.go",
            "s9",
            "app",
        )
    )
    out.append(
        _fx(
            "search-no-matches",
            "search",
            ["rg", "DoesNotExistToken"],
            1,
            "",
            "No matches found\n",
            [_oracle("c1", "No matches found", "critical", ["no-result", "summary"], "no matches")],
            "pattern absent",
            "try another symbol",
            "s10",
            "app",
        )
    )
    long_line = "big.go:1: " + ("A" * 8000) + " NEEDLE_TOKEN " + ("B" * 200) + "\n"
    out.append(
        _fx(
            "search-long-line",
            "search",
            ["rg", "NEEDLE_TOKEN"],
            0,
            long_line,
            "",
            [_oracle("c1", "NEEDLE_TOKEN", "critical", ["location"], "needle")],
            "needle sits inside a huge line",
            "do not prefix-truncate the line",
            "s10",
            "app",
        )
    )
    out.append(
        _fx(
            "search-binary-notice",
            "search",
            ["rg", "foo"],
            0,
            "",
            "binary file matches: dist/app.bin\n",
            [_oracle("r1", "binary file matches", "relevant", ["summary"], "binary match")],
            "match is in a binary",
            "skip binary or use strings",
            "s11",
            "app",
        )
    )
    out.append(
        _fx(
            "search-truncated",
            "search",
            ["rg", "x"],
            0,
            "a.go:1: x\n[truncated]\n",
            "",
            [_oracle("c1", "[truncated]", "critical", ["no-result", "summary"], "truncated")],
            "results truncated",
            "re-run with tighter path",
            "s11",
            "app",
        )
    )
    return out


def git_fixtures() -> list[dict[str, Any]]:
    out: list[dict[str, Any]] = []
    out.append(
        _fx(
            "git-status-dirty",
            "git",
            ["git", "status", "--porcelain"],
            0,
            " M internal/bill.go\n?? scratch.tmp\n",
            "",
            [_oracle("r1", "internal/bill.go", "relevant", ["affected target"], "modified bill"), _oracle("r2", "scratch.tmp", "supporting", [], "untracked")],
            "dirty tree with bill.go modified",
            "review bill.go diff",
            "s12",
            "app",
        )
    )
    diff = (
        "diff --git a/internal/bill.go b/internal/bill.go\n"
        "--- a/internal/bill.go\n"
        "+++ b/internal/bill.go\n"
        "@@ -20,3 +20,4 @@\n"
        " func Total() int {\n"
        "-        return 10\n"
        "+        return 11\n"
        " }\n"
    )
    out.append(
        _fx(
            "git-diff",
            "git",
            ["git", "diff"],
            0,
            diff,
            "",
            [_oracle("c1", "return 11", "critical", ["cause"], "total changed 11"), _oracle("c2", "internal/bill.go", "relevant", ["location"], "bill diff")],
            "Total changed 10 to 11",
            "confirm intended tax change",
            "s12",
            "app",
        )
    )
    log = "commit abcdef0123456789deadbeefcafebabe01234567\nAuthor: Dev\n    fix invoice\n"
    out.append(
        _fx(
            "git-log",
            "git",
            ["git", "log", "-1"],
            0,
            log,
            "",
            [_oracle("r1", "fix invoice", "relevant", ["summary"], "commit message")],
            "latest commit fixes invoice",
            "inspect that commit",
            "s13",
            "app",
        )
    )
    out.append(
        _fx(
            "git-rename",
            "git",
            ["git", "status"],
            0,
            "R  old/pay.go -> new/pay.go\n",
            "",
            [_oracle("r1", "old/pay.go -> new/pay.go", "relevant", ["affected target"], "rename pay")],
            "pay.go renamed",
            "update imports",
            "s13",
            "app",
        )
    )
    conflict = (
        "<<<<<<< HEAD\nreturn 10\n=======\nreturn 11\n>>>>>>> feature\n"
        "CONFLICT (content): Merge conflict in internal/bill.go\n"
    )
    out.append(
        _fx(
            "git-conflict",
            "git",
            ["git", "merge", "feature"],
            1,
            conflict,
            "",
            [
                _oracle("c1", "CONFLICT (content): Merge conflict in internal/bill.go", "critical", ["cause", "location"], "merge conflict bill"),
                _oracle("c2", "<<<<<<< HEAD", "critical", ["cause"], "conflict markers"),
            ],
            "merge conflict in bill.go",
            "resolve bill.go markers",
            "s14",
            "app",
        )
    )
    out.append(
        _fx(
            "git-binary-diff",
            "git",
            ["git", "diff"],
            0,
            "Binary files a/logo.png and b/logo.png differ\n",
            "",
            [_oracle("r1", "Binary files", "relevant", ["summary"], "binary differ")],
            "binary asset changed",
            "inspect image, not patch",
            "s14",
            "app",
        )
    )
    out.append(
        _fx(
            "git-detached",
            "git",
            ["git", "status"],
            0,
            "HEAD detached at abcdef0\n",
            "",
            [_oracle("c1", "HEAD detached", "critical", ["summary"], "detached head")],
            "detached HEAD",
            "checkout a branch",
            "s15",
            "app",
        )
    )
    out.append(
        _fx(
            "git-failing-hook",
            "git",
            ["git", "commit", "-m", "x"],
            1,
            "",
            "pre-commit: gofmt failed on internal/bill.go\nhook declined\n",
            [
                _oracle("c1", "gofmt failed on internal/bill.go", "critical", ["cause", "location"], "gofmt failed"),
                _oracle("c2", "hook declined", "critical", ["summary"], "hook declined"),
            ],
            "pre-commit gofmt failed",
            "run gofmt on bill.go",
            "s15",
            "app",
        )
    )
    return out


def json_fixtures() -> list[dict[str, Any]]:
    out: list[dict[str, Any]] = []
    out.append(
        _fx(
            "json-array",
            "json",
            ["cat", "items.json"],
            0,
            json.dumps([{"id": 1}, {"id": 2}]),
            "",
            [_oracle("r1", '"id": 1', "relevant", ["summary"], "array ids")],
            "two item ids",
            "project ids",
            "s16",
            "api",
        )
    )
    out.append(
        _fx(
            "json-scalar",
            "json",
            ["echo"],
            0,
            "42\n",
            "",
            [_oracle("r1", "42", "relevant", ["summary"], "scalar")],
            "scalar 42",
            "use as count",
            "s16",
            "api",
        )
    )
    out.append(
        _fx(
            "json-malformed",
            "json",
            ["jq", "."],
            1,
            "",
            "parse error: Invalid numeric literal at line 2, column 0\n",
            [_oracle("c1", "parse error", "critical", ["cause", "summary"], "json parse error")],
            "malformed json",
            "fix input at line 2",
            "s17",
            "api",
        )
    )
    nd = '{"ok":true}\n{"ok":false,"error":"not found"}\n{"ok":true}\n'
    out.append(
        _fx(
            "json-ndjson-hetero",
            "json",
            ["cat", "events.ndjson"],
            0,
            nd,
            "",
            [_oracle("c1", '"error":"not found"', "critical", ["cause"], "ndjson error not found")],
            "one ndjson record failed",
            "inspect the false record",
            "s17",
            "api",
        )
    )
    truncated = '{"items":[{"id":1},{"id":'
    out.append(
        _fx(
            "json-truncated",
            "json",
            ["curl"],
            1,
            truncated,
            "",
            [_oracle("c1", '{"items":[{"id":1},{"id":', "critical", ["cause", "summary"], "truncated json")],
            "body truncated mid-object",
            "retry the request",
            "s18",
            "api",
        )
    )
    huge = json.dumps({"noise": ["x" * 50] * 40, "error": "invoice 99 missing tax"})
    out.append(
        _fx(
            "json-huge-semantic-failure-exit0",
            "json",
            ["api-get"],
            0,
            huge,
            "",
            [_oracle("c1", "invoice 99 missing tax", "critical", ["cause", "affected target"], "invoice missing tax")],
            "API returned semantic error with exit 0",
            "do not trust exit code; fix invoice 99",
            "s18",
            "api",
        )
    )
    repeated = "\n".join(json.dumps({"ts": f"2026-09-13T07:{i:02d}:00Z", "msg": "heartbeat", "uuid": f"{i:08x}-aaaa-bbbb-cccc-ddddeeeeffff"}) for i in range(20))
    repeated += "\n" + json.dumps({"ts": "2026-09-13T07:59:00Z", "msg": "fatal", "error": "disk full"})
    out.append(
        _fx(
            "json-repeated-then-fatal",
            "json",
            ["tail", "app.ndjson"],
            0,
            repeated,
            "",
            [_oracle("c1", "disk full", "critical", ["cause", "summary"], "fatal disk full")],
            "heartbeats then disk full",
            "free disk",
            "s19",
            "api",
        )
    )
    return out


def all_fixtures() -> list[dict[str, Any]]:
    fx = logs_fixtures() + tests_fixtures() + search_fixtures() + git_fixtures() + json_fixtures()
    from more_fixtures import extras
    fx.extend(extras())
    ids = [f["id"] for f in fx]
    if len(ids) != len(set(ids)):
        raise RuntimeError("duplicate fixture ids")
    return fx


def split(fixtures: list[dict[str, Any]]) -> dict[str, list[str]]:
    """Split by session+repo+id hash so near-duplicates stay together."""
    buckets = {"train": [], "dev": [], "test": []}
    for f in fixtures:
        key = f"{f['session']}|{f['repo']}|{f['template_cluster']}"
        h = int(hashlib.sha256(key.encode()).hexdigest()[:8], 16) % 100
        if h < 50:
            buckets["train"].append(f["id"])
        elif h < 75:
            buckets["dev"].append(f["id"])
        else:
            buckets["test"].append(f["id"])
    return buckets
