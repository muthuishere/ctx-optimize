#!/usr/bin/env bash
# Answer-QUALITY benchmark — does the store make the agent's answers RIGHT?
#
# run-bench.sh measures cost (time/tokens/steps). This measures CORRECTNESS,
# deterministically, and reports cost alongside it. Same model, same questions,
# three arms, ONE CLONE PER ARM so no arm's artifacts pollute another's search
# space (arm a's grep must not be able to read graphify-out/ or .ctxoptimize/).
#
#   1. clone the repo three times (a / b / c)
#   2. build the ctx-optimize store in clone b, the graphify graph in clone c
#   3. run agent.mjs for every (question x arm); record failures, never hide them
#   4. grade every ANSWER with grade.mjs (no model in the loop)
#
# Needs OPENROUTER_API_KEY in the environment. Nothing here prints it.
#
# Usage:
#   proof/agent/run-quality.sh [--model SLUG] [--questions FILE] [--bin PATH]
#                              [--out DIR] [--arms "a b c"] [--only "i1 i5"]
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
MODEL="openai/gpt-4o-mini"
BIN=""
QFILE="$HERE/questions-graded-mux.json"
OUT="$HERE/results-quality"
ARMS=""
ONLY=""
WORK=""

while [ $# -gt 0 ]; do
  case "$1" in
    --model) MODEL="$2"; shift 2;;
    --bin) BIN="$2"; shift 2;;
    --questions) QFILE="$2"; shift 2;;
    --out) OUT="$2"; shift 2;;
    --arms) ARMS="$2"; shift 2;;
    --only) ONLY="$2"; shift 2;;
    --work) WORK="$2"; shift 2;;
    *) echo "unknown arg: $1" >&2; exit 2;;
  esac
done

[ -n "${OPENROUTER_API_KEY:-}" ] || { echo "OPENROUTER_API_KEY not set" >&2; exit 2; }

REPO="$(node -e 'console.log(require(process.argv[1]).repo)' "$QFILE")"
NAME="$(node -e 'console.log(require(process.argv[1]).name)' "$QFILE")"
[ -n "$WORK" ] || WORK="$(mktemp -d)"
mkdir -p "$WORK" "$OUT"
export CTX_OPTIMIZE_STORE="$WORK/store"

if [ -z "$BIN" ]; then
  if command -v ctx-optimize >/dev/null 2>&1; then BIN="$(command -v ctx-optimize)";
  else BIN="$WORK/ctx-optimize"; ( cd "$ROOT" && go build -o "$BIN" ./cmd/ctx-optimize ); fi
fi

echo "== answer-quality benchmark =="
echo "model:   $MODEL"
echo "repo:    $REPO"
echo "binary:  $("$BIN" --version)"
echo "workdir: $WORK"

# 1. one clone per arm
for arm in a b c; do
  if [ ! -d "$WORK/repo-$arm" ]; then
    git clone --depth 1 "$REPO" "$WORK/repo-$arm" >/dev/null 2>&1
  fi
done
echo "clones:  $(find "$WORK/repo-a" -type f | wc -l | tr -d ' ') files each"

# 2. stores
if [ ! -d "$WORK/repo-b/.ctxoptimize" ]; then
  ( cd "$WORK/repo-b" && "$BIN" init >/dev/null 2>&1 && "$BIN" add . >/dev/null 2>&1 )
fi
echo "store b: $(cd "$WORK/repo-b" && "$BIN" status --json 2>/dev/null | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>{try{const j=JSON.parse(s);console.log(`${j.nodes} nodes, ${j.edges} edges`)}catch{console.log("built")}})')"

HAVE_C=0
if command -v graphify >/dev/null 2>&1; then
  if [ -d "$WORK/repo-c/graphify-out" ] || ( cd "$WORK/repo-c" && graphify update . --no-cluster >/dev/null 2>&1 ); then
    HAVE_C=1; echo "store c: graphify graph built"
  else echo "store c: graphify build FAILED — arm c skipped"; fi
else echo "store c: graphify not installed — arm c skipped"; fi

[ -n "$ARMS" ] || { ARMS="a b"; [ "$HAVE_C" -eq 1 ] && ARMS="a b c"; }
echo "arms:    $ARMS"
echo

# 3. run
QIDS="${ONLY:-$(node -e 'require(process.argv[1]).questions.forEach(q=>console.log(q.id))' "$QFILE")}"
for qid in $QIDS; do
  QTEXT="$(node -e 'const q=require(process.argv[1]).questions.find(x=>x.id===process.argv[2]);if(!q){process.exit(3)}console.log(q.prompt)' "$QFILE" "$qid")"
  for arm in $ARMS; do
    dest="$OUT/${NAME}-${arm}-${qid}.json"
    printf "  %-4s arm %s ... " "$qid" "$arm"
    if node "$HERE/agent.mjs" --repo "$WORK/repo-$arm" --bin "$BIN" --arm "$arm" \
        --model "$MODEL" --q "$QTEXT" > "$dest" 2>"$dest.err"; then
      node -e 'const r=require(process.argv[1]);console.log(`ok  ${r.wall_s}s  ${r.tokens.total}tok  $${r.cost_usd}  ${r.steps} steps  ${r.answer?"":"[EMPTY ANSWER]"}`)' "$dest"
    else
      echo "RUN FAILED — see $dest.err"; tail -3 "$dest.err"
    fi
  done
done

# 4. grade
echo
node "$HERE/grade.mjs" --results "$OUT" --name "$NAME" --questions "$QFILE" \
  | tee "$OUT/QUALITY-${NAME}.md"
echo
echo "raw records: $OUT"
echo "clones kept: $WORK  (rm -rf $WORK when done)"
