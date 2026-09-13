#!/usr/bin/env python3
"""Additional unique adversarial fixtures. Imported by generate.all_fixtures."""

from __future__ import annotations

from generate import CANARY, _fx, _noise, _oracle


def extra_logs():
    out = []
    java = (
        _noise(40, "tomcat")
        + "\n"
        + "java.lang.NullPointerException: invoice\n"
        + "\tat com.acme.Bill.total(Bill.java:88)\n"
        + "\tat com.acme.BillServlet.doGet(BillServlet.java:12)\n"
    )
    out.append(_fx("logs-java-npe", "logs", ["java", "-jar", "app.jar"], 1, java, "", [
        _oracle("c1", "NullPointerException: invoice", "critical", ["cause"], "npe invoice"),
        _oracle("c2", "Bill.java:88", "critical", ["location"], "bill.java"),
    ], "NPE in Bill.total", "open Bill.java:88", "s20", "jvm"))
    nginx = _noise(30, "access") + "\n2026-09-13T08:00:00Z ERROR 502 upstream timed out uri=/invoice/7\n"
    out.append(_fx("logs-nginx-late-502", "logs", ["nginx"], 0, nginx, "", [
        _oracle("c1", "502 upstream timed out", "critical", ["cause", "affected target"], "502 timeout"),
    ], "invoice 7 upstream timeout", "check upstream", "s20", "edge"))
    zap = "\n".join(
        '{"level":"info","msg":"ok","ts":"%02d"}' % i for i in range(25)
    ) + '\n{"level":"error","msg":"queue full","ts":"99"}\n'
    out.append(_fx("logs-json-zap", "logs", ["app"], 0, zap, "", [
        _oracle("c1", "queue full", "critical", ["cause"], "queue full"),
    ], "queue full", "scale workers", "s21", "svc-e"))
    syslog = "Sep 13 07:00:00 box kernel: oom-kill of pid 4411 (worker)\n"
    out.append(_fx("logs-oom", "logs", ["dmesg"], 0, syslog, "", [
        _oracle("c1", "oom-kill of pid 4411", "critical", ["cause"], "oom kill"),
    ], "worker OOM", "raise memory", "s21", "svc-e"))
    rust = "thread 'main' panicked at src/bill.rs:9:5:\nmissing tax\nnote: run with RUST_BACKTRACE=1\n"
    out.append(_fx("logs-rust-panic", "logs", ["./app"], 101, "", rust, [
        _oracle("c1", "panicked at src/bill.rs:9:5", "critical", ["cause", "location"], "rust panic"),
        _oracle("c2", "missing tax", "critical", ["cause"], "missing tax"),
    ], "panic missing tax", "fix bill.rs:9", "s22", "svc-f"))
    mixed = "INFO start\nWARN slow query 1200ms\nERROR deadlock detected tx=9\nINFO done\n"
    out.append(_fx("logs-deadlock", "logs", ["app"], 0, mixed, "", [
        _oracle("c1", "deadlock detected tx=9", "critical", ["cause"], "deadlock"),
    ], "deadlock tx 9", "inspect locking", "s22", "svc-f"))
    cr = "progress 1%\rprogress 50%\rprogress 99%\nERROR checksum mismatch file=blob.bin\n"
    out.append(_fx("logs-cr-then-error", "logs", ["dl"], 1, cr, "", [
        _oracle("c1", "checksum mismatch file=blob.bin", "critical", ["cause", "affected target"], "checksum"),
    ], "download checksum failed", "re-fetch blob.bin", "s23", "svc-g"))
    two_fatals = "FATAL disk full path=/var/lib/app\nFATAL cannot rotate log\n"
    out.append(_fx("logs-two-fatals", "logs", ["app"], 1, two_fatals, "", [
        _oracle("c1", "disk full path=/var/lib/app", "critical", ["cause"], "disk full"),
        _oracle("c2", "cannot rotate log", "critical", ["cause"], "rotate"),
    ], "disk full and rotate failed", "free disk", "s23", "svc-g"))
    ansi_err = "\x1b[31mERROR\x1b[0m connection refused 10.0.0.8:5432\n"
    out.append(_fx("logs-ansi-error", "logs", ["app"], 1, ansi_err, "", [
        _oracle("c1", "connection refused 10.0.0.8:5432", "critical", ["cause"], "conn refused"),
    ], "db refused", "start postgres", "s24", "svc-h"))
    retry_then_fatal = "\n".join(f"WARN retry {i}" for i in range(15)) + "\nFATAL giving up after 15 retries\n"
    out.append(_fx("logs-retry-exhausted", "logs", ["app"], 1, retry_then_fatal, "", [
        _oracle("c1", "giving up after 15 retries", "critical", ["summary"], "retries exhausted"),
    ], "retries exhausted", "inspect dependency", "s24", "svc-h"))
    health = "ok\nok\nok\nFAIL healthcheck /ready returned 503\n"
    out.append(_fx("logs-health-503", "logs", ["health"], 1, health, "", [
        _oracle("c1", "/ready returned 503", "critical", ["summary"], "ready 503"),
    ], "readiness 503", "inspect /ready", "s25", "svc-i"))
    permission = "INFO writing /etc/app/config.json\nERROR permission denied /etc/app/config.json\n"
    out.append(_fx("logs-eperm", "logs", ["app"], 1, permission, "", [
        _oracle("c1", "permission denied /etc/app/config.json", "critical", ["cause", "location"], "eperm"),
    ], "cannot write config", "fix permissions", "s25", "svc-i"))
    timeout = _noise(20, "poll") + "\nERROR context deadline exceeded calling billing.internal\n"
    out.append(_fx("logs-deadline", "logs", ["app"], 1, timeout, "", [
        _oracle("c1", "deadline exceeded calling billing.internal", "critical", ["cause", "affected target"], "deadline"),
    ], "billing deadline", "raise timeout", "s26", "svc-j"))
    sig = "INFO running\nFATAL signal: killed\n"
    out.append(_fx("logs-killed", "logs", ["app"], 137, "INFO running\n", "FATAL signal: killed\n", [
        _oracle("c1", "signal: killed", "critical", ["cause"], "killed"),
    ], "process killed", "check OOM/kill", "s26", "svc-j"))
    return out


def extra_tests():
    out = []
    out.append(_fx("tests-cargo-fail", "tests", ["cargo", "test"], 101,
        "test bill::total ... FAILED\nthread 'bill::total' panicked at tests/bill.rs:4: missing tax\n", "", [
        _oracle("c1", "bill::total ... FAILED", "critical", ["affected target"], "cargo fail"),
        _oracle("c2", "missing tax", "critical", ["cause"], "missing tax"),
    ], "cargo test bill::total failed", "fix tests/bill.rs:4", "s27", "rust"))
    out.append(_fx("tests-pytest-assert", "tests", ["pytest", "-q"], 1,
        "F\nassert 11 == 10\nFAILED tests/test_bill.py::test_total\n", "", [
        _oracle("c1", "FAILED tests/test_bill.py::test_total", "critical", ["affected target"], "pytest fail"),
        _oracle("c2", "assert 11 == 10", "critical", ["cause"], "assert mismatch"),
    ], "pytest total 11!=10", "fix test_bill.py", "s27", "py"))
    out.append(_fx("tests-tsc", "tests", ["tsc", "--noEmit"], 1,
        "src/bill.ts(22,7): error TS2304: Cannot find name 'TaxRate'.\n", "", [
        _oracle("c1", "Cannot find name 'TaxRate'", "critical", ["cause", "location"], "ts2304"),
    ], "TaxRate missing in bill.ts", "define TaxRate", "s28", "js"))
    out.append(_fx("tests-eslint", "tests", ["eslint", "."], 1,
        "internal/bill.js:10: error  no-undef  TaxRate is not defined\n", "", [
        _oracle("c1", "TaxRate is not defined", "critical", ["cause", "location"], "eslint undef"),
    ], "eslint no-undef TaxRate", "define or import", "s28", "js"))
    out.append(_fx("tests-maven", "tests", ["mvn", "test"], 1,
        "Tests run: 8, Failures: 1, Errors: 0\n[ERROR] testTotal(com.acme.BillTest)  expected: 10 but was: 11\n", "", [
        _oracle("c1", "expected: 10 but was: 11", "critical", ["cause"], "junit mismatch"),
        _oracle("c2", "Failures: 1", "critical", ["summary"], "maven fail"),
    ], "BillTest expected 10 was 11", "fix Bill.total", "s29", "jvm"))
    out.append(_fx("tests-npm", "tests", ["npm", "test"], 1,
        "FAIL src/bill.test.js\nExpected: 10\nReceived: 11\nTest Suites: 1 failed, 1 total\n", "", [
        _oracle("c1", "FAIL src/bill.test.js", "critical", ["affected target"], "jest fail"),
        _oracle("c2", "Received: 11", "critical", ["cause"], "received 11"),
    ], "jest received 11", "fix bill.js", "s29", "js"))
    out.append(_fx("tests-collection-error", "tests", ["pytest"], 2,
        "ERROR collecting tests/test_bill.py\nImportError: cannot import name TaxRate\n", "", [
        _oracle("c1", "ERROR collecting tests/test_bill.py", "critical", ["summary"], "collect error"),
        _oracle("c2", "cannot import name TaxRate", "critical", ["cause"], "import error"),
    ], "collection import error", "export TaxRate", "s30", "py"))
    out.append(_fx("tests-skip-then-fail", "tests", ["go", "test"], 1,
        "--- SKIP: TestSlow (0.00s)\n--- FAIL: TestBill (0.00s)\n    bill_test.go:3: missing tax\nFAIL\n", "", [
        _oracle("c1", "FAIL: TestBill", "critical", ["affected target"], "fail TestBill"),
        _oracle("c2", "missing tax", "critical", ["cause"], "missing tax"),
    ], "TestBill missing tax", "add tax", "s30", "app"))
    out.append(_fx("tests-timeout", "tests", ["go", "test", "-timeout", "1s"], 1,
        "panic: test timed out after 1s\nFAIL\n", "", [
        _oracle("c1", "test timed out after 1s", "critical", ["cause"], "timeout"),
    ], "test timeout", "find hang", "s31", "app"))
    out.append(_fx("tests-race", "tests", ["go", "test", "-race"], 1,
        "WARNING: DATA RACE\nRead at bill.go:22\nFAIL\n", "", [
        _oracle("c1", "DATA RACE", "critical", ["cause"], "race"),
        _oracle("c2", "Read at bill.go:22", "critical", ["location"], "race loc"),
    ], "data race bill.go:22", "synchronize", "s31", "app"))
    out.append(_fx("tests-coverage-ok", "tests", ["go", "test", "-cover"], 0,
        "ok  \tgithub.com/acme/app\tcoverage: 81.0% of statements\nPASS\n", "", [
        _oracle("s1", "PASS", "relevant", ["summary"], "pass"),
    ], "pass with coverage", "continue", "s32", "app"))
    out.append(_fx("tests-build-tag-fail", "tests", ["go", "test", "-tags", "integration"], 1,
        "package github.com/acme/app: build constraints exclude all Go files\nFAIL\n", "", [
        _oracle("c1", "build constraints exclude all Go files", "critical", ["cause"], "build tags"),
    ], "build tags excluded files", "fix tags", "s32", "app"))
    out.append(_fx("tests-snapshot", "tests", ["jest"], 1,
        "Snapshot name: Bill\n- 10\n+ 11\nFAIL src/bill.test.js\n", "", [
        _oracle("c1", "FAIL src/bill.test.js", "critical", ["affected target"], "snapshot fail"),
        _oracle("c2", "+ 11", "critical", ["cause"], "snapshot 11"),
    ], "snapshot 10 vs 11", "review snapshot", "s33", "js"))
    return out


def extra_search():
    out = []
    out.append(_fx("search-git-grep", "search", ["git", "grep", "-n", "TaxRate"], 0,
        "internal/bill.go:22:func TaxRate() int {\n", "", [
        _oracle("r1", "internal/bill.go:22", "relevant", ["location"], "git grep"),
    ], "TaxRate in bill.go", "read bill.go:22", "s34", "app"))
    out.append(_fx("search-count-zero", "search", ["rg", "-c", "Nope"], 1, "", "No matches found\n", [
        _oracle("c1", "No matches found", "critical", ["no-result"], "no matches"),
    ], "no matches", "change pattern", "s34", "app"))
    out.append(_fx("search-context", "search", ["rg", "-C", "2", "TaxRate"], 0,
        "internal/bill.go-20-\ninternal/bill.go-21-\ninternal/bill.go:22:func TaxRate() int {\ninternal/bill.go-23-    return 10\n", "", [
        _oracle("r1", "func TaxRate() int", "relevant", ["location"], "hunk"),
    ], "TaxRate definition hunk", "read surrounding", "s35", "app"))
    out.append(_fx("search-binary-and-text", "search", ["rg", "foo"], 0,
        "a.go:1: foo\n", "binary file matches: dist/app.bin\n", [
        _oracle("r1", "a.go:1: foo", "relevant", ["location"], "text hit"),
        _oracle("r2", "binary file matches", "supporting", ["summary"], "binary"),
    ], "text and binary hits", "prefer a.go", "s35", "app"))
    out.append(_fx("search-truncated-mid-hunk", "search", ["rg", "x"], 0,
        "a.go:1: x\nb.go:2: x\n[truncated 4000 matches]\n", "", [
        _oracle("c1", "[truncated 4000 matches]", "critical", ["no-result", "summary"], "truncated"),
    ], "results truncated", "narrow path", "s36", "app"))
    out.append(_fx("search-unicode", "search", ["rg", " naïve"], 0,
        "docs/a.md:3: naïve approach\n", "", [
        _oracle("r1", "naïve approach", "relevant", ["location"], "unicode"),
    ], "unicode match", "read docs/a.md", "s36", "app"))
    out.append(_fx("search-path-filter-empty", "search", ["rg", "TaxRate", "vendor/"], 1, "", "No matches found\n", [
        _oracle("c1", "No matches found", "critical", ["no-result"], "no matches"),
    ], "none under vendor", "search without path", "s37", "app"))
    out.append(_fx("search-multiline", "search", ["rg", "-U", "func TaxRate"], 0,
        "internal/bill.go:22:func TaxRate() int {\ninternal/bill.go:23:    return 10\ninternal/bill.go:24:}\n", "", [
        _oracle("r1", "func TaxRate() int", "relevant", ["location"], "multiline"),
    ], "multiline function", "read bill.go", "s37", "app"))
    out.append(_fx("search-duplicate-files", "search", ["rg", "TODO"], 0,
        "a.go:1: TODO\na.go:2: TODO\na.go:3: TODO\nb.go:9: TODO critical leak\n", "", [
        _oracle("c1", "TODO critical leak", "critical", ["cause", "location"], "todo leak"),
    ], "real TODO in b.go", "open b.go:9", "s38", "app"))
    out.append(_fx("search-windows-path", "search", ["rg", "Main"], 0,
        "src\\Main.go:1: func Main()\n", "", [
        _oracle("r1", "src\\Main.go:1", "relevant", ["location"], "windows path"),
    ], "windows path hit", "open Main.go", "s38", "app"))
    out.append(_fx("search-error-on-stderr", "search", ["rg", "["], 2, "", "regex parse error:\n    [\n", [
        _oracle("c1", "regex parse error", "critical", ["cause"], "bad regex"),
    ], "invalid regex", "escape pattern", "s39", "app"))
    out.append(_fx("search-long-path", "search", ["rg", "x"], 0,
        "very/" * 40 + "deep.go:1: x NEEDLE_HERE\n", "", [
        _oracle("c1", "NEEDLE_HERE", "critical", ["location"], "deep needle"),
    ], "needle in deep path", "do not truncate path", "s40", "app"))
    out.append(_fx("search-globs-no-hit", "search", ["rg", "TaxRate", "-g", "*.md"], 1, "", "No matches found\n", [
        _oracle("c1", "No matches found", "critical", ["no-result"], "no matches"),
    ], "no md hits", "drop glob", "s40b", "app"))
    out.append(_fx("search-jsonl-rg", "search", ["rg", "--json", "TaxRate"], 0,
        '{"type":"match","data":{"path":{"text":"bill.go"},"line_number":22}}\n', "", [
        _oracle("r1", '"line_number":22', "relevant", ["location"], "rg json"),
    ], "json match line 22", "open bill.go:22", "s40", "app"))
    return out


def extra_git():
    out = []
    out.append(_fx("git-rebase-conflict", "git", ["git", "rebase", "main"], 1,
        "CONFLICT (content): Merge conflict in internal/bill.go\nRebasing (2/5)\n", "", [
        _oracle("c1", "CONFLICT (content): Merge conflict in internal/bill.go", "critical", ["cause", "location"], "rebase conflict"),
    ], "rebase conflict bill.go", "resolve then rebase --continue", "s41", "app"))
    out.append(_fx("git-cherry-pick-fail", "git", ["git", "cherry-pick", "abc"], 1,
        "error: cherry-pick is not possible because you have unmerged files\n", "", [
        _oracle("c1", "cherry-pick is not possible", "critical", ["cause"], "cherry pick"),
    ], "unmerged files block cherry-pick", "finish merge", "s41", "app"))
    out.append(_fx("git-stash-pop", "git", ["git", "stash", "pop"], 1,
        "CONFLICT (content): Merge conflict in internal/pay.go\nThe stash entry is kept\n", "", [
        _oracle("c1", "Merge conflict in internal/pay.go", "critical", ["location"], "stash conflict"),
    ], "stash pop conflict pay.go", "resolve pay.go", "s42", "app"))
    out.append(_fx("git-bisect-bad", "git", ["git", "bisect", "bad"], 0,
        "Bisecting: 3 revisions left\nabcdef0 is the first bad commit\n", "", [
        _oracle("r1", "first bad commit", "relevant", ["summary"], "bisect"),
    ], "first bad abcdef0", "inspect commit", "s42", "app"))
    out.append(_fx("git-submodule", "git", ["git", "status"], 0,
        " m vendor/lib\n", "", [
        _oracle("r1", "vendor/lib", "relevant", ["affected target"], "submodule dirty"),
    ], "dirty submodule", "update submodule", "s43", "app"))
    out.append(_fx("git-push-rejected", "git", ["git", "push"], 1,
        "", "error: failed to push some refs to origin\nhint: Updates were rejected because the remote contains work\n", [
        _oracle("c1", "failed to push some refs", "critical", ["summary"], "push rejected"),
    ], "non-fast-forward push", "pull --rebase", "s43", "app"))
    out.append(_fx("git-index-lock", "git", ["git", "commit"], 1,
        "", "fatal: Unable to create .git/index.lock: File exists.\n", [
        _oracle("c1", "index.lock: File exists", "critical", ["cause"], "index lock"),
    ], "index lock", "remove stale lock if safe", "s44", "app"))
    out.append(_fx("git-log-oneline", "git", ["git", "log", "--oneline", "-3"], 0,
        "abc111 fix invoice\ndef222 wip\n333aaa init\n", "", [
        _oracle("r1", "fix invoice", "relevant", ["summary"], "log"),
    ], "recent fix invoice", "inspect abc111", "s45", "app"))
    out.append(_fx("git-restore-denied", "git", ["git", "checkout", "bill.go"], 1,
        "", "error: pathspec 'bill.go' did not match any file(s) known to git\n", [
        _oracle("c1", "did not match any file", "critical", ["cause"], "pathspec"),
    ], "path unknown", "check filename", "s45", "app"))
    out.append(_fx("git-hook-lint", "git", ["git", "commit"], 1,
        "", "pre-commit: eslint failed\nhook declined\n", [
        _oracle("c1", "eslint failed", "critical", ["cause"], "eslint hook"),
        _oracle("c2", "hook declined", "critical", ["summary"], "hook"),
    ], "eslint hook failed", "fix lint", "s46", "app"))
    out.append(_fx("git-branch-detached-note", "git", ["git", "status"], 0,
        "HEAD detached at 1a2b3c4\nnothing to commit, working tree clean\n", "", [
        _oracle("c1", "HEAD detached at 1a2b3c4", "critical", ["summary"], "detached"),
    ], "detached HEAD", "checkout branch", "s46b", "app"))
    out.append(_fx("git-merge-abort-needed", "git", ["git", "merge", "feat"], 1,
        "Automatic merge failed; fix conflicts and then commit the result.\nCONFLICT (content): Merge conflict in README.md\n", "", [
        _oracle("c1", "Merge conflict in README.md", "critical", ["location"], "readme conflict"),
    ], "readme conflict", "resolve README.md", "s46", "app"))
    return out


def extra_json():
    out = []
    out.append(_fx("json-graphql-errors", "json", ["curl"], 0,
        '{"data":null,"errors":[{"message":"invoice 99 missing tax"}]}', "", [
        _oracle("c1", "invoice 99 missing tax", "critical", ["cause"], "gql error"),
    ], "graphql semantic error exit 0", "fix invoice 99", "s47", "api"))
    out.append(_fx("json-http-500", "json", ["curl"], 0,
        '{"status":500,"error":"connection refused 127.0.0.1:5432"}', "", [
        _oracle("c1", "connection refused 127.0.0.1:5432", "critical", ["cause"], "db refused"),
    ], "api 500 db down", "start postgres", "s47", "api"))
    out.append(_fx("json-empty-array", "json", ["cat"], 0, "[]\n", "", [
        _oracle("r1", "[]", "relevant", ["no-result"], "empty"),
    ], "empty array", "nothing to project", "s48", "api"))
    out.append(_fx("json-duplicate-keys", "json", ["cat"], 0, '{"a":1,"a":2}\n', "", [
        _oracle("r1", '"a":2', "relevant", ["summary"], "dup key"),
    ], "duplicate key last wins visually", "do not assume first key", "s48", "api"))
    out.append(_fx("json-ndjson-truncated", "json", ["tail"], 1, '{"ok":true}\n{"ok":false,"err":"disk full"\n', "", [
        _oracle("c1", "disk full", "critical", ["cause"], "truncated disk full"),
    ], "truncated ndjson with disk full", "retry read", "s49", "api"))
    out.append(_fx("json-openapi-error", "json", ["curl"], 0,
        '{"error":{"code":"invalid_tax","message":"TaxRate undefined"}}', "", [
        _oracle("c1", "TaxRate undefined", "critical", ["cause"], "invalid_tax"),
    ], "openapi invalid_tax", "define TaxRate", "s49", "api"))
    out.append(_fx("json-prometheus", "json", ["curl"], 0,
        "http_requests_total 12\nup 0\n", "", [
        _oracle("c1", "up 0", "critical", ["summary"], "prom down"),
    ], "target down", "inspect scrape", "s50", "api"))
    out.append(_fx("json-es-hits", "json", ["curl"], 0,
        '{"hits":{"total":{"value":0},"hits":[]}}', "", [
        _oracle("r1", '"value":0', "relevant", ["no-result"], "zero hits"),
    ], "zero search hits", "widen query", "s50", "api"))
    out.append(_fx("json-k8s-notfound", "json", ["kubectl", "get"], 1,
        '{"kind":"Status","status":"Failure","reason":"NotFound","message":"deployments.apps bill not found"}', "", [
        _oracle("c1", "bill not found", "critical", ["affected target"], "not found"),
    ], "deployment missing", "apply manifest", "s51", "k8s"))
    out.append(_fx("json-unicode", "json", ["cat"], 0, '{"msg":"naïve failure: missing tax"}', "", [
        _oracle("c1", "missing tax", "critical", ["cause"], "unicode missing tax"),
    ], "unicode message missing tax", "fix tax", "s51", "api"))
    out.append(_fx("json-nested-error", "json", ["cat"], 0,
        '{"ok":true,"result":{"errors":[{"path":"total","error":"got 11 want 10"}]}}', "", [
        _oracle("c1", "got 11 want 10", "critical", ["cause"], "nested mismatch"),
    ], "nested semantic fail exit 0", "fix total", "s52", "api"))
    out.append(_fx("json-huge-then-error", "json", ["cat"], 0,
        '{"noise":[' + ",".join(['"x"'] * 80) + '],"error":"fatal worker crashed"}', "", [
        _oracle("c1", "fatal worker crashed", "critical", ["cause"], "json fatal"),
    ], "error after huge noise array", "do not prefix-truncate", "s52", "api"))
    out.append(_fx("json-null-error", "json", ["cat"], 0, '{"error":null,"data":{"n":1}}', "", [
        _oracle("r1", '"error":null', "relevant", ["summary"], "null error"),
    ], "error field null means success", "use data", "s53", "api"))
    return out


def extras():
    return extra_logs() + extra_tests() + extra_search() + extra_git() + extra_json()
