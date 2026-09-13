#!/usr/bin/env python3
"""Throwaway evidence projector. Emits original bytes; canonicalizes only for rank."""

from __future__ import annotations

import math
import re
from dataclasses import dataclass, field
from typing import Any

TOKEN_RE = re.compile(r"[A-Za-z0-9_./:-]+")
ISO = re.compile(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z?")
UUID = re.compile(r"\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b", re.I)
HEX = re.compile(r"\b[0-9a-f]{8,}\b", re.I)
PID = re.compile(r"\bpid=\d+\b", re.I)
DURATION = re.compile(r"\b\d+(?:\.\d+)?(?:s|ms|ns)\b")
MANDATORY_RE = re.compile(
    r"(fatal|panic|traceback|undefined:|build failed|--- fail:|\bfail(?:ed)?\b|error |"
    r"no matches found|\[truncated\]|conflict|hook declined|head detached|"
    r"<<<<<<|======|>>>>>>|parse error|disk full|connection refused|"
    r"missing tax|got \d+ want|gofmt|assert |expected:|received:|"
    r"data race|build constraints|importerror|cannot import)",
    re.I,
)
STACK_CONT = re.compile(r"^(goroutine |\s+at |\s+File |Traceback|\t)")


def tokenize(s: str) -> list[str]:
    return [m.group(0).lower() for m in TOKEN_RE.finditer(s) if len(m.group(0)) > 1]


def token_count(s: str) -> int:
    return max(1, (len(s.encode("utf-8")) + 3) // 4)


def canonicalize(s: str) -> str:
    s = ISO.sub("<TS>", s)
    s = UUID.sub("<UUID>", s)
    s = PID.sub("pid=<PID>", s)
    s = DURATION.sub("<DUR>", s)
    s = HEX.sub("<HEX>", s)
    return s


@dataclass
class Block:
    id: str
    stream: str
    start: int
    end: int
    text: str
    family: str
    index: int
    template: str = ""
    mandatory: bool = False
    facets: list[str] = field(default_factory=list)

    def tokens(self) -> list[str]:
        return tokenize(self.template or canonicalize(self.text))


def _lines(blob: str, stream: str) -> list[tuple[int, int, str]]:
    out = []
    pos = 0
    raw = blob
    if raw and not raw.endswith("\n"):
        parts = raw.split("\n")
    else:
        parts = raw.split("\n")
        if parts and parts[-1] == "":
            parts = parts[:-1]
    for line in parts:
        start = pos
        end = pos + len(line.encode("utf-8"))
        out.append((start, end, line))
        pos = end + 1
    return out


def segment(fx: dict[str, Any]) -> list[Block]:
    blocks: list[Block] = []
    idx = 0
    for stream, blob in (("stdout", fx["stdout"]), ("stderr", fx["stderr"])):
        if blob == "":
            continue
        family = fx["family"]
        lines = _lines(blob, stream)
        buf: list[tuple[int, int, str]] = []

        def flush() -> None:
            nonlocal idx, buf
            if not buf:
                return
            text = "\n".join(t[2] for t in buf)
            b = Block(
                id=f"{stream}-{idx}",
                stream=stream,
                start=buf[0][0],
                end=buf[-1][1],
                text=text,
                family=family,
                index=idx,
            )
            b.template = canonicalize(text)
            b.mandatory = bool(MANDATORY_RE.search(text)) or (
                fx["exit"] != 0 and "FAIL" in text
            )
            if re.search(r"file=.+\.go|:\d+|Traceback|undefined:", text, re.I):
                b.facets.append("location")
            if MANDATORY_RE.search(text):
                b.facets.append("cause")
            if re.search(r"PASS|FAIL|done|error|conflict|detached", text, re.I):
                b.facets.append("summary")
            blocks.append(b)
            idx += 1
            buf = []

        for start, end, line in lines:
            cont = bool(STACK_CONT.match(line)) or (
                family == "tests" and buf and line.startswith("    ")
            )
            hunk = family == "git" and buf and (
                line.startswith("+") or line.startswith("-") or line.startswith(" ")
            )
            if buf and not cont and not hunk:
                flush()
            buf.append((start, end, line))
            if family == "json" and line.startswith("{") and line.endswith("}"):
                flush()
        flush()
    for i, b in enumerate(blocks):
        if i == 0:
            continue
        first = b.text.split("\n", 1)[0]
        if blocks[i - 1].mandatory and (
            STACK_CONT.match(first)
            or first.startswith("main.")
            or first.startswith("Error Trace")
        ):
            b.mandatory = True
            if "location" not in b.facets:
                b.facets.append("location")
    if not blocks:
        blocks.append(
            Block(id="empty-0", stream="stdout", start=0, end=0, text="", family=fx["family"], index=0)
        )
    return blocks


def _pack(blocks: list[Block], budget: int, chosen: list[Block]) -> list[Block]:
    used = sum(token_count(b.text) for b in chosen)
    out = list(chosen)
    seen = {b.id for b in chosen}
    for b in blocks:
        if b.id in seen:
            continue
        cost = token_count(b.text)
        if used + cost > budget and out:
            continue
        out.append(b)
        seen.add(b.id)
        used += cost
    out.sort(key=lambda b: (0 if b.stream == "stdout" else 1, b.start, b.index))
    return out


def prefix(fx: dict[str, Any], budget: int) -> list[Block]:
    blob = fx["stdout"] + ("\n" + fx["stderr"] if fx["stderr"] else "")
    raw = blob.encode("utf-8")
    keep = min(len(raw), budget * 4)
    text = raw[:keep].decode("utf-8", "replace")
    return [Block(id="prefix", stream="stdout", start=0, end=keep, text=text, family=fx["family"], index=0)]


def head_tail(fx: dict[str, Any], budget: int) -> list[Block]:
    blob = fx["stdout"] + ("\n" + fx["stderr"] if fx["stderr"] else "")
    raw = blob.encode("utf-8")
    keep = min(len(raw), budget * 4)
    head_n = keep // 2
    tail_n = keep - head_n
    text = (raw[:head_n] + b"\n...\n" + raw[-tail_n:] if len(raw) > keep else raw).decode("utf-8", "replace")
    return [Block(id="headtail", stream="stdout", start=0, end=len(raw), text=text, family=fx["family"], index=0)]


def source_order(blocks: list[Block], budget: int, mandatory: bool) -> list[Block]:
    chosen = [b for b in blocks if b.mandatory] if mandatory else []
    return _pack(blocks, budget, chosen)


def exact_dedupe(blocks: list[Block], budget: int, mandatory: bool) -> list[Block]:
    seen: set[str] = set()
    uniq: list[Block] = []
    for b in blocks:
        if b.text in seen and not b.mandatory:
            continue
        seen.add(b.text)
        uniq.append(b)
    return source_order(uniq, budget, mandatory)


def drain_template(text: str) -> str:
    toks = canonicalize(text).split()
    out = []
    for t in toks:
        out.append("<*>" if re.search(r"\d", t) else t)
    return " ".join(out)


def logram_template(text: str, rare: set[str]) -> str:
    toks = canonicalize(text).split()
    grams = [" ".join(toks[i : i + 2]) for i in range(len(toks) - 1)]
    out = []
    for i, t in enumerate(toks):
        g = grams[i] if i < len(grams) else ""
        out.append("<*>" if g in rare and re.search(r"\d", t) else t)
    return " ".join(out)


def _idf_scores(blocks: list[Block]) -> list[int]:
    df: dict[str, int] = {}
    token_sets = []
    for b in blocks:
        ts = set(b.tokens())
        token_sets.append(ts)
        for t in ts:
            df[t] = df.get(t, 0) + 1
    n = max(1, len(blocks))
    scores = []
    for ts in token_sets:
        s = 0
        for t in sorted(ts):
            s += int(1000 * math.log((n + 1) / (1 + df[t])))
        scores.append(s)
    return scores


def rank_idf(blocks: list[Block], budget: int, mandatory: bool, mmr: bool) -> list[Block]:
    scores = _idf_scores(blocks)
    chosen: list[Block] = []
    if mandatory:
        chosen = [b for b in blocks if b.mandatory]
    selected_idx = {b.index for b in chosen}
    remaining = [i for i, b in enumerate(blocks) if b.index not in selected_idx]
    token_sets = [set(b.tokens()) for b in blocks]
    while remaining:
        best_i = None
        best_key = None
        for i in remaining:
            rel = scores[i]
            if mmr and chosen:
                max_sim = 0
                for c in chosen:
                    a, d = token_sets[i], set(c.tokens())
                    inter = len(a & d)
                    union = len(a | d) or 1
                    sim = (1000 * inter) // union
                    if sim > max_sim:
                        max_sim = sim
                rel = (700 * rel - 300 * max_sim) // 1000
            key = (rel, -blocks[i].index)
            if best_key is None or key > best_key:
                best_key, best_i = key, i
        assert best_i is not None
        trial = chosen + [blocks[best_i]]
        if sum(token_count(b.text) for b in trial) > budget and chosen:
            remaining = [i for i in remaining if i != best_i]
            continue
        chosen.append(blocks[best_i])
        remaining = [i for i in remaining if i != best_i]
    chosen.sort(key=lambda b: (0 if b.stream == "stdout" else 1, b.start, b.index))
    return chosen


def submodular(blocks: list[Block], budget: int, mandatory: bool) -> list[Block]:
    chosen: list[Block] = [b for b in blocks if b.mandatory] if mandatory else []
    covered = set()
    templates = set()
    for b in chosen:
        covered.update(b.facets)
        templates.add(b.template)
    remaining = [b for b in blocks if b.id not in {c.id for c in chosen}]
    scores = _idf_scores(blocks)
    by_id = {b.id: scores[i] for i, b in enumerate(blocks)}
    while remaining:
        best = None
        best_key = None
        for b in remaining:
            gain = 5000 * len(set(b.facets) - covered) + (1000 if b.template not in templates else 0) + by_id[b.id]
            key = (gain, -b.index)
            if best_key is None or key > best_key:
                best_key, best = key, b
        assert best is not None
        trial = chosen + [best]
        if sum(token_count(b.text) for b in trial) > budget and chosen:
            remaining = [b for b in remaining if b.id != best.id]
            continue
        chosen.append(best)
        covered.update(best.facets)
        templates.add(best.template)
        remaining = [b for b in remaining if b.id != best.id]
    chosen.sort(key=lambda b: (0 if b.stream == "stdout" else 1, b.start, b.index))
    return chosen


def apply_templates(blocks: list[Block], kind: str) -> list[Block]:
    if kind == "drain":
        for b in blocks:
            b.template = drain_template(b.text)
        return blocks
    if kind == "logram":
        df: dict[str, int] = {}
        grams_per = []
        for b in blocks:
            toks = canonicalize(b.text).split()
            grams = [" ".join(toks[i : i + 2]) for i in range(max(0, len(toks) - 1))]
            grams_per.append(grams)
            for g in set(grams):
                df[g] = df.get(g, 0) + 1
        rare = {g for g, c in df.items() if c == 1}
        for b in blocks:
            b.template = logram_template(b.text, rare)
        return blocks
    return blocks


def collision_outcome(blocks: list[Block]) -> bool:
    """True if a template merges PASS and FAIL (canonicalizer kill)."""
    groups: dict[str, set[str]] = {}
    for b in blocks:
        flags = set()
        if re.search(r"result=PASS|\bPASS\b", b.text) and not re.search(r"FAIL", b.text):
            flags.add("pass")
        if re.search(r"result=FAIL|\bFAIL\b|FATAL", b.text):
            flags.add("fail")
        if flags:
            groups.setdefault(b.template, set()).update(flags)
    return any(len(v) > 1 for v in groups.values())


def project(fx: dict[str, Any], method: str, budget: int) -> dict[str, Any]:
    blocks = segment(fx)
    mandatory = method.endswith("+mand")
    base = method.replace("+mand", "")
    selected: list[Block]
    if base == "prefix":
        selected = prefix(fx, budget)
        omitted = []
        source_n = 1
    elif base == "headtail":
        selected = head_tail(fx, budget)
        omitted = []
        source_n = 1
    else:
        if base in ("drain", "logram"):
            blocks = apply_templates(blocks, base)
            selected = exact_dedupe(blocks, budget, mandatory)
        elif base == "source_order":
            selected = source_order(blocks, budget, mandatory)
        elif base == "exact_dedupe":
            selected = exact_dedupe(blocks, budget, mandatory)
        elif base == "idf":
            selected = rank_idf(blocks, budget, mandatory, mmr=False)
        elif base == "idf_mmr":
            selected = rank_idf(blocks, budget, mandatory, mmr=True)
        elif base == "submodular":
            selected = submodular(blocks, budget, mandatory)
        else:
            raise ValueError(method)
        sel_ids = {b.id for b in selected}
        omitted = [b for b in blocks if b.id not in sel_ids]
        source_n = len(blocks)
    emitted = render(fx, selected)
    overflow = mandatory and any(b.mandatory for b in selected) and sum(token_count(b.text) for b in selected) > budget
    disclosure = {
        "source_bytes": len((fx["stdout"] + fx["stderr"]).encode("utf-8")),
        "emitted_bytes": len(emitted.encode("utf-8")),
        "source_blocks": source_n,
        "emitted_blocks": len(selected),
        "omitted_blocks": len(omitted) if base not in ("prefix", "headtail") else max(0, source_n - 1),
        "family": fx["family"],
        "confidence": "constructed-prototype",
        "budget_overflow": bool(overflow),
        "fallback": None,
        "omitted_ids": [b.id for b in omitted] if base not in ("prefix", "headtail") else [],
        "omitted_templates": [b.template[:80] for b in omitted] if base not in ("prefix", "headtail") else [],
        "method": method,
        "budget": budget,
    }
    return {
        "emitted": emitted,
        "disclosure": disclosure,
        "selected": selected,
        "blocks": blocks,
        "collision": collision_outcome(blocks) if base in ("drain", "logram") else False,
    }


def render(fx: dict[str, Any], selected: list[Block]) -> str:
    stdout_parts = [b.text for b in selected if b.stream == "stdout"]
    stderr_parts = [b.text for b in selected if b.stream == "stderr"]
    # prefix/headtail already mixed
    if selected and selected[0].id in ("prefix", "headtail"):
        return selected[0].text
    out = []
    if stdout_parts:
        out.append("\n".join(stdout_parts))
    if stderr_parts:
        out.append("\n".join(stderr_parts))
    return "\n".join(out)
