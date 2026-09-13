#!/usr/bin/env python3
"""Scratch execute-once / argv contract tests. Not product code."""

from __future__ import annotations

import os
import subprocess
import sys
import tempfile
from pathlib import Path


def run_argv(argv: list[str], stdin: bytes = b"", env: dict | None = None) -> subprocess.CompletedProcess:
    return subprocess.run(
        argv,
        input=stdin,
        capture_output=True,
        env=env or os.environ.copy(),
        cwd=tempfile.gettempdir(),
        timeout=10,
    )


def main() -> int:
    fails = []
    marker = Path(tempfile.gettempdir()) / "ctxopt-spike-once.txt"
    marker.unlink(missing_ok=True)

    # spaces and metacharacters are one argv slot
    p = run_argv([sys.executable, "-c", "import sys; print(sys.argv[1])", "a $HOME; b"])
    if p.stdout.decode().strip() != "a $HOME; b":
        fails.append(f"metachar argv expanded: {p.stdout!r}")

    # execute-once: write marker, then pretend projector failed
    p = run_argv([sys.executable, "-c", f"open({str(marker)!r},'w').write('1')"])
    if p.returncode != 0 or not marker.exists() or marker.read_text() != "1":
        fails.append("side effect missing")
    # projector failure must not rerun
    if marker.read_text() != "1":
        fails.append("reran command")
    marker.unlink(missing_ok=True)

    # stdin preserved
    p = run_argv([sys.executable, "-c", "import sys; sys.stdout.write(sys.stdin.read())"], stdin=b"hello\n")
    if p.stdout != b"hello\n":
        fails.append(f"stdin mismatch {p.stdout!r}")

    # separate streams
    p = run_argv([sys.executable, "-c", "import sys; sys.stdout.write('O'); sys.stderr.write('E')"])
    if p.stdout != b"O" or p.stderr != b"E":
        fails.append(f"streams {p.stdout!r} {p.stderr!r}")

    # exit status preserved
    p = run_argv([sys.executable, "-c", "raise SystemExit(7)"])
    if p.returncode != 7:
        fails.append(f"exit {p.returncode}")

    # command-not-found
    try:
        p = run_argv(["ctx-optimize-no-such-binary-xyz"])
        if p.returncode == 0:
            fails.append("missing binary succeeded")
    except FileNotFoundError:
        pass

    print({"ok": not fails, "fails": fails})
    return 1 if fails else 0


if __name__ == "__main__":
    raise SystemExit(main())
