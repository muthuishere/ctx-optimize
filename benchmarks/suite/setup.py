#!/usr/bin/env python3
"""Build the competitor arena from `tools.json` — shallow, pinned, reproducible.

    python3 benchmarks/suite/setup.py [--arena ~/ctx-bench-arena] [--tools graphify codegraph]

Why this exists: the 2026-07-24 cross-tool run was produced by a scratch script
that was never committed, and its arena was built by hand. The numbers on the
compare page therefore could not be reproduced from the repo — only re-derived
from prose in RUN-NOTES.md. Everything needed to rebuild the field now lives
here.

Cloning: shallow AND pinned. `git clone --depth 1` alone gives whatever HEAD is
today, which silently re-versions the field between runs. So each tool is fetched
with `git fetch --depth 1 origin <sha>` (GitHub serves reachable SHAs), which
keeps the clone small while nailing the version. If the pin cannot be fetched —
force-push, moved org, deleted branch — setup falls back to the default branch
and RECORDS `pinned: false` plus the SHA it actually got. A floating version must
never be reported as a pinned one.

Output: <arena>/versions.json — resolved SHA, whether the pin held, build
success, and the entry point for every tool. That file is the provenance for any
number the runner publishes.
"""
import argparse, json, os, shutil, subprocess, sys

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(os.path.dirname(HERE))


def sh(cmd, cwd=None, timeout=1800):
    p = subprocess.run(cmd, cwd=cwd, capture_output=True, text=True, timeout=timeout)
    return p.returncode, (p.stdout or "") + (p.stderr or "")


def fetch_pinned(url, ref, dest):
    """Shallow clone at an exact SHA. Returns (ok, resolved_sha, pinned, log)."""
    os.makedirs(dest, exist_ok=True)
    rc, log = sh(["git", "init", "-q", dest])
    if rc != 0:
        return False, None, False, log
    sh(["git", "remote", "add", "origin", url], cwd=dest)

    if ref:
        rc, log = sh(["git", "fetch", "--depth", "1", "origin", ref], cwd=dest)
        if rc == 0:
            sh(["git", "checkout", "-q", "FETCH_HEAD"], cwd=dest)
            rc2, sha = sh(["git", "rev-parse", "HEAD"], cwd=dest)
            return True, sha.strip(), True, log

    # Pin unavailable (or none given) — take the default branch and say so.
    rc, log2 = sh(["git", "fetch", "--depth", "1", "origin", "HEAD"], cwd=dest)
    if rc != 0:
        return False, None, False, log2
    sh(["git", "checkout", "-q", "FETCH_HEAD"], cwd=dest)
    _, sha = sh(["git", "rev-parse", "HEAD"], cwd=dest)
    return True, sha.strip(), False, log2


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--arena", default=os.path.expanduser("~/ctx-bench-arena"))
    ap.add_argument("--tools", nargs="*", default=None)
    ap.add_argument("--force", action="store_true", help="re-clone tools already present")
    a = ap.parse_args()

    manifest = json.load(open(os.path.join(HERE, "tools.json")))
    tools_dir = os.path.join(a.arena, "tools")
    os.makedirs(tools_dir, exist_ok=True)
    versions = {}

    for t in manifest["tools"]:
        name = t["name"]
        if a.tools and name not in a.tools:
            continue
        if t.get("local"):
            versions[name] = {"local": True, "entry": os.path.join(REPO, t["entry"])}
            continue
        if not t.get("gatherable") and not t.get("repo"):
            # Field context only (SaaS, embeddings, packers) — nothing to build.
            versions[name] = {"skipped": t.get("gatherable_reason", "not benchmarkable")}
            print(f"-- {name}: {versions[name]['skipped']}")
            continue

        dest = os.path.join(tools_dir, name)
        rec = {"repo": t.get("repo"), "requested_ref": t.get("ref")}

        if os.path.isdir(os.path.join(dest, ".git")) and not a.force:
            _, sha = sh(["git", "rev-parse", "HEAD"], cwd=dest)
            rec.update({"resolved_sha": sha.strip(), "pinned": sha.strip() == (t.get("ref") or ""),
                        "reused_existing_clone": True})
            print(f"== {name}: reusing clone @ {sha.strip()[:12]}")
        else:
            if a.force:
                shutil.rmtree(dest, ignore_errors=True)
            ok, sha, pinned, log = fetch_pinned(t["repo"], t.get("ref"), dest)
            if not ok and t.get("repo_alt"):
                print(f"** {name}: {t['repo']} failed, trying repo_alt")
                shutil.rmtree(dest, ignore_errors=True)
                ok, sha, pinned, log = fetch_pinned(t["repo_alt"], t.get("ref"), dest)
                rec["used_repo_alt"] = True
            rec.update({"resolved_sha": sha, "pinned": pinned, "clone_ok": ok})
            if not ok:
                rec["error"] = log[-400:]
                versions[name] = rec
                print(f"!! {name}: clone FAILED — {log[-160:].strip()}")
                continue
            print(f"== {name}: {sha[:12]} pinned={pinned}")

        for step in t.get("build", []):
            rc, log = sh(step.split(), cwd=dest)
            rec.setdefault("build", []).append({"cmd": step, "ok": rc == 0,
                                                "log_tail": None if rc == 0 else log[-400:]})
            if rc != 0:
                print(f"!! {name}: build step {step!r} failed")
                break

        entry = os.path.join(dest, t["entry"]) if "/" in t.get("entry", "") else shutil.which(t.get("entry", ""))
        rec["entry"] = entry
        rec["entry_exists"] = bool(entry and os.path.exists(entry))
        if not rec["entry_exists"]:
            print(f"!! {name}: entry point missing ({t.get('entry')}) — it will be recorded as unavailable, not skipped")
        versions[name] = rec

    out = os.path.join(a.arena, "versions.json")
    with open(out, "w") as f:
        json.dump(versions, f, indent=1, sort_keys=True)
    print(f"\nwrote {out}")
    unpinned = [n for n, v in versions.items() if v.get("clone_ok") and not v.get("pinned")]
    if unpinned:
        print(f"WARNING: floating (not pinned) — {', '.join(unpinned)}. Any result using these is not version-reproducible.")


if __name__ == "__main__":
    main()
