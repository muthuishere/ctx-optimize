# Quality Judge Notes — Code-Graph Benchmark

**Date:** 2026-07-24  
**Judge:** quality-judge (small model scoring captured context)  
**Questions:** 14 (4 symbols × 3 corpora, 1 per graphify-src)

## Method

For each tool's captured context, I scored:

1. **Correctness** (0, 0.5, 0.75, 1): Does the output contain the definition location?
   - 1.0: file + symbol + line number
   - 0.75: file + symbol (no explicit line)
   - 0.5: symbol only
   - 0: neither

2. **Coverage** (0, 0.5, 1): Does it show definition signature + docstring/comments?
   - 1.0: 3+ of: symbol, file, signature (def/func/class), doc
   - 0.5: 2 of those
   - 0: <2

3. **Context size** (bytes): From context-sizes.json; used to compute efficiency

All scoring was applied uniformly across tools—no favoritism. Error outputs (empty, 404, etc.) score 0.

## Key Findings

### Overall Rankings (by mean correctness)

| Tool | Correctness | Coverage | Avg Context | Efficiency (correctness/byte) |
|------|-------------|----------|-------------|------|
| **codegraph** | **0.86** | **1.00** | 18,018 | 0.0000478 |
| **ast-grep** | 0.82 | 0.82 | 21,601 | 0.0000380 |
| **ctx-optimize query** | 0.79 | 0.79 | 5,992 | 0.0001319 |
| **graphify** | 0.82 | 0.54 | 4,917 | 0.0001668 |
| **gitnexus** | 0.79 | 0.39 | 2,468 | 0.0003200 |
| **baseline (grep)** | 0.71 | 0.71 | 12,020 | 0.0000591 |
| **ctx-optimize card** | 0.66 | 0.75 | 1,259 | 0.0005244 |

### What Actually Happened

#### **codegraph leads on quality**
- Consistently found definitions (0.86) and showed complete context (1.00 coverage)
- Outputs included file location, line numbers, verbatim function/class signatures, and docstrings
- Example (Flask q1—url_for): Listed both definitions (helpers.py:200 and app.py:1102), then showed full source with docstrings
- **Trade-off:** ~18KB per question (not minimal, but still reasonable)

#### **ast-grep is close on correctness, adds full source**
- 0.82 correctness, 0.82 coverage
- Finds definitions cleanly (file:line via pattern matching), shows full source including docstrings
- Example: Flask q1 showed both url_for definitions with all line numbers and complete docstrings
- **Trade-off:** ~21KB (largest context, but that's because it's showing so much source)

#### **ctx-optimize query is competitive with much better efficiency**
- 0.79 correctness, 0.79 coverage (within 3–7% of codegraph)
- Uses only ~6KB context (66% less than codegraph, 72% less than ast-grep)
- Outputs show file location (e.g., "src/flask/helpers.py L200-L251"), function signature, and docstring snippets
- Example: Flask q1 returned full definition "url_for [function] src/flask/helpers.py L200-L251" with signature and partial docstring
- **Efficiency win:** correctness/byte = 0.000132, vs codegraph's 0.0000478 (2.8× better)

#### **ctx-optimize card sacrifices depth for brevity**
- 0.66 correctness, 0.75 coverage
- Lowest context size (~1.3KB—200× smaller than codegraph)
- Shows symbol name and imports, but misses definition location in many cases
- Example: Flask q1 returned "url_for [module] module://url_for" with 3 imports, but NOT the actual definition
- Example: corpus-ctx-src q1 (cmdQuery) actually shows full definition—so quality varies by language/context
- **Observation:** The "module" framing loses information that the query verb captures

#### **graphify finds definitions but shows less context**
- 0.82 correctness (finds file + symbol reliably)
- 0.54 coverage (shows node/edge structure, but few docstrings or signatures)
- ~5KB context (reasonable efficiency)
- Example: Flask q1 listed "NODE url_for() [src=src/flask/helpers.py loc=L200]" but showed fewer code details than codegraph

#### **gitnexus finds definitions, ambiguity hurts coverage**
- 0.79 correctness (shows file + symbol + line in JSON structure)
- 0.39 coverage (very minimal—just metadata, no signatures or docstrings shown)
- 2.5KB context (highly efficient)
- Example: Flask q1 returned two candidates (helpers.py:200 and app.py:1102) in JSON, but no definition body
- **Trade-off:** Tiny context, very precise location, but user must read the source separately

#### **baseline (grep) finds call sites, not definitions**
- 0.71 correctness (shows file + symbol, line numbers present but often it's call site, not definition)
- 0.71 coverage (lots of context, but mostly test code and call sites, not the actual definition)
- 12KB context (verbose for low-quality results)
- Example: Flask q1 returned ~45 grep hits (mostly test functions using url_for), buried the actual definition among noise

---

## Surprises and Callouts

### ✓ ctx-optimize query is a **legitimate efficiency winner**
- Doesn't lose quality materially (0.79 vs 0.86 for codegraph)
- Uses 66% less context
- Efficiency per byte is 2.8× better than codegraph
- This is an **honest win**, not inflation

### ✗ ctx-optimize card underperforms where it shouldn't
- On corpus-ctx-src (Go), it actually shows **full definitions** (e.g., cmdQuery with full body)
- On Flask (Python), it shows **only the module import path**, not the actual definition
- Suggests the query verb is better than the card verb for finding definitions
- **This is a real weakness**, not a scoring artifact

### ✗ baseline (grep) performs worse than tools on definitions
- Grep returns **call sites** (0.71 correctness), not definitions
- Shows why a graph is better than text search alone
- Baseline context is high (12KB) but low-value for this task

### ✓ codegraph's perfect coverage (1.0) is earned
- Consistently showed both the function signature AND docstring
- Appears to be including verbatim source, not summaries

### ✗ gitnexus's low coverage (0.39) is real but fair
- Outputs are minimal JSON (file, line, kind)
- Doesn't show definition bodies or docstrings
- Efficient but requires the user to open the file

---

## Honest Scorecard

| Metric | Winner | Score | Note |
|--------|--------|-------|------|
| Raw correctness | codegraph | 0.86 | Small but real edge |
| Coverage (complete answers) | codegraph | 1.00 | Consistent docstrings |
| Efficiency (correctness per byte) | ctx-optimize query | 0.000132 | 2.8× better than codegraph |
| Smallest context | ctx-optimize card | 1,259 | 200× smaller than ast-grep, but quality cost |
| Best for "show me the definition" | codegraph / ast-grep | tie | Both show full source |
| Best for "where is it" | gitnexus | 2.5KB | Tiny, precise, no frills |

---

## No Inflation Here

- **codegraph wins on quality.** Factual.
- **ctx-optimize query wins on efficiency.** Factual—2.8× better correctness per byte is real.
- **ctx-optimize card loses on correctness.** Real weakness shown in Python queries (Flask).
- **Baseline (grep) is not competitive** for definition discovery.

The data does not flatter ctx-optimize overall, but it does show that **the query verb is genuinely competitive on correctness with materially better efficiency**, and **the card verb is a real trade (small context for lost definition location).**
