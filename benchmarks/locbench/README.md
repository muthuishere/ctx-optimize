# Loc-Bench — the first external yardstick

Every quality number this repo publishes today is self-authored, self-graded
and self-floored (`docs/CRITIQUE.md` item 7). This harness scores us on
somebody else's benchmark, with somebody else's ground truth, against baselines
we did not choose.

- **dataset** [`czlll/Loc-Bench_V1`](https://huggingface.co/datasets/czlll/Loc-Bench_V1) — 560 instances
- **paper** LocAgent, ACL 2025 — [arxiv.org/abs/2503.09089](https://arxiv.org/abs/2503.09089) (Apache-2.0)
- **truth** `edit_functions: ["path/file.py:Class.method", …]` — the functions a
  real merged patch changed. File and function granularity from one field, so
  there is no mapping layer for a skeptic to attack.
- **pinning** every instance carries `repo` + `base_commit` (40-char SHA)

```sh
go build -o bin/ctx-optimize ./cmd/ctx-optimize
python3 benchmarks/locbench/locbench.py --slice benchmarks/locbench/slices/small.json
```

Stdlib only. The dataset is fetched once over the HuggingFace rows API and
cached at `~/ctx-locbench-arena/dataset.json`; repos are shallow-cloned at their
pinned SHA using the same contract as `benchmarks/suite/setup.py`, and a
checkout that could not be pinned is reported `FLOATING`, never as pinned.

## Which tier we enter, and why

Loc-Bench's input is a natural-language GitHub issue. Answering it end-to-end
means reasoning about which functions must change — exactly what this binary
refuses to do. Entering the agentic tier would mean bolting an LLM on and
reporting the model's score as ours.

So we enter the **retrieval** tier, where the paper's own baselines take the
same issue text and return a ranked list, scored with the same Acc@k against the
same keys, with no LLM on either side:

| Loc-Bench, file Acc@5, retrieval only | |
|---|---|
| BM25 (lexical — our nearest relative) | 38.69% |
| Jina-Code-v2 | 43.43% |
| Codesage-large-v2 | 47.81% |
| E5-base-v2 | 49.64% |
| CodeRankEmbed (best retrieval) | 52.55% |
| *LocAgent (agentic, LLM — **not** our tier)* | *84.59%* |

Beating LocAgent is not on the table, and claiming otherwise would be dishonest.

## Result on the 12-instance slice — read the split, not the headline

```
file  Acc@1 58.33%   Acc@3 66.67%   Acc@5 66.67%   Acc@10 75.00%
func  Acc@1 16.67%   Acc@3 25.00%   Acc@5 33.33%   Acc@10 41.67%

SPLIT BY REPO SIZE
  large repos  (>1000 files)   n=3   file Acc@5  33.33%
  small repos (<=1000 files)   n=9   file Acc@5  77.78%
```

**The 66.67% headline is an artifact of sampling and must not be published.**
The slice was chosen for clone-and-gather feasibility, which selects *small*
repos — median 154 files, with yfinance at 34. Picking the right file from 45
in a top-5 is a different problem from picking it from 2,800, and the full
benchmark is dominated by large repos (django 35 instances, yt-dlp 27, vllm 24,
pandas 22).

On the one large repo in the slice we score **33.33%, below BM25's 38.69%**.
n=3 is far too small to conclude from, but it points the opposite way to the
headline, which is precisely why the size split prints on every run rather than
living in a footnote.

**What this slice actually establishes:** the harness works end to end, the
protocol is fair, and our real standing is unknown until the full set runs.

## Two grader bugs found while building this

Recorded because a grader can lie in either direction:

1. **Under-scoring us.** The first function-level grader kept only the last
   dotted component of a label, so `PdfWriter._insert_filtered_annotations` —
   an exact rank-3 hit — did not match the truth key
   `pypdf/_writer.py:PdfWriter._insert_filtered_annotations`. Function Acc@5
   read 8.33% instead of 33.33%.
2. **Nearly over-scoring us.** The fix initially emitted *two* ranked entries
   per hit (qualified and bare), which silently pushes `k` further down the
   list and inflates Acc@k. Corrected to one rank position per hit holding both
   acceptable spellings.

## Running the full benchmark

The blocker is disk and wall-clock, not method: 560 instances across ~90 repos,
each needing a shallow clone at its own commit plus a gather. Instances
grouped by `(repo, base_commit)` already share one checkout — the slice's 12
instances needed 12 distinct checkouts because Loc-Bench pins a different commit
per issue, so expect close to one clone per instance.

Order-of-magnitude from the slice: gathers ran 0.3–1.8s, and the dominant cost
is cloning. Budget disk for ~560 shallow checkouts of repos up to django's size,
and run it on a quiet machine — `benchmarks/session/session.py`'s warning
applies here too, since a loaded box swung an identical A/B by 12 points earlier
in this project.

Worth adding before the full run:

- **more `k` values and per-category scores** — Loc-Bench labels each instance
  bug report / feature request / performance / security, and those are likely
  to behave very differently for a lexical retriever;
- **a competitor column.** The harness scores one tool today. `graphify`,
  `codegraph` and `gitnexus` are already built in `~/ctx-bench-arena/tools/`
  and could take the same ranked-list protocol, which would put every tool on
  an external yardstick instead of ours.
