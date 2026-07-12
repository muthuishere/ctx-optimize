#!/usr/bin/env bash
# End-to-end headless benchmark — the same flow a CI job runs.
#
#   1. build (or reuse) the ctx-optimize binary
#   2. install the agent skill into the global skill dirs   (proves install)
#   3. git clone a small repo
#   4. ctx-optimize init && add .   -> builds the store, writes the agent pointer
#      + graphify update . --no-cluster (arm c) when graphify is on PATH
#   5. for each question, run the OpenRouter agent on every arm:
#        a = shell only · b = ctx-optimize store · c = graphify (if installed)
#   6. print a time / tokens / cost / steps comparison table
#
# Needs: OPENROUTER_API_KEY in the environment, node, go (or a prebuilt --bin),
# git. Nothing here prints the key.
#
# Usage:
#   proof/agent/run-bench.sh [--model openai/gpt-4o-mini] [--bin PATH]
#                            [--questions FILE] [--out DIR] [--repo URL --name N]
set -euo pipefail

HERE="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"
MODEL="openai/gpt-4o-mini"
BIN=""
QFILE="$HERE/questions.json"
OUT="$HERE/results"
REPO=""
NAME=""

while [ $# -gt 0 ]; do
  case "$1" in
    --model) MODEL="$2"; shift 2;;
    --bin) BIN="$2"; shift 2;;
    --questions) QFILE="$2"; shift 2;;
    --out) OUT="$2"; shift 2;;
    --repo) REPO="$2"; shift 2;;
    --name) NAME="$2"; shift 2;;
    *) echo "unknown arg: $1" >&2; exit 2;;
  esac
done

[ -n "${OPENROUTER_API_KEY:-}" ] || { echo "OPENROUTER_API_KEY not set" >&2; exit 2; }

# repo + name default from the questions file
[ -n "$REPO" ] || REPO="$(node -e 'console.log(require(process.argv[1]).repo)' "$QFILE")"
[ -n "$NAME" ] || NAME="$(node -e 'console.log(require(process.argv[1]).name)' "$QFILE")"

WORK="$(mktemp -d)"
CLONE="$WORK/$NAME"
mkdir -p "$OUT"

echo "== ctx-optimize headless benchmark =="
echo "model:   $MODEL"
echo "repo:    $REPO"
echo "workdir: $WORK"
echo

# 1. binary — prefer an explicit --bin, then an installed ctx-optimize
#    (npm: `npm i -g @muthuishere/ctx-optimize`), and only build from source
#    as a last resort (needs this repo's Go tree).
if [ -z "$BIN" ]; then
  if command -v ctx-optimize >/dev/null 2>&1; then
    BIN="$(command -v ctx-optimize)"
    echo "[1/6] using installed ctx-optimize ..."
  elif [ -f "$ROOT/go.mod" ]; then
    BIN="$WORK/ctx-optimize"
    echo "[1/6] building ctx-optimize from source ..."
    ( cd "$ROOT" && go build -o "$BIN" ./cmd/ctx-optimize )
  else
    echo "no ctx-optimize on PATH and no Go source; install it: npm i -g @muthuishere/ctx-optimize" >&2
    exit 2
  fi
fi
echo "      binary: $("$BIN" --version)"

# 2. install the agent skill (global dirs) — proves the skill install path
echo "[2/6] installing agent skill ..."
"$BIN" install --skills >/dev/null 2>&1 || echo "      (skill install skipped)"

# 3. clone
echo "[3/6] cloning $NAME ..."
git clone --depth 1 "$REPO" "$CLONE" >/dev/null 2>&1
echo "      $(find "$CLONE" -type f | wc -l | tr -d ' ') files"

# 4. build the store(s) + agent pointer
echo "[4/6] building the store ..."
( cd "$CLONE" && "$BIN" init >/dev/null 2>&1 && "$BIN" add . >/dev/null 2>&1 )
echo "      ctx-optimize: $("$BIN" status --json 2>/dev/null | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>{try{const j=JSON.parse(s);console.log((j.nodes||0)+" nodes, "+(j.edges||0)+" edges")}catch{console.log("store built")}})')"

ARMS="a b"
if command -v graphify >/dev/null 2>&1; then
  ( cd "$CLONE" && graphify update . --no-cluster >/dev/null 2>&1 ) \
    && ARMS="a b c" \
    && echo "      graphify:     $(graphify --version 2>/dev/null) graph built (arm c on)" \
    || echo "      graphify:     present but graph build failed (arm c off)"
else
  echo "      graphify:     not installed (arm c skipped)"
fi

# 5. run each arm per question
echo "[5/6] running agent (arm a = shell, b = ctx-optimize, c = graphify) ..."
QIDS="$(node -e 'require(process.argv[1]).questions.forEach(q=>console.log(q.id))' "$QFILE")"
for qid in $QIDS; do
  QTEXT="$(node -e 'const q=require(process.argv[1]).questions.find(x=>x.id===process.argv[2]);console.log(q.prompt)' "$QFILE" "$qid")"
  for arm in $ARMS; do
    dest="$OUT/${NAME}-${arm}-${qid}.json"
    printf "      %s arm %s ... " "$qid" "$arm"
    if node "$HERE/agent.mjs" --repo "$CLONE" --bin "$BIN" --arm "$arm" \
        --model "$MODEL" --q "$QTEXT" > "$dest" 2>"$dest.err"; then
      node -e 'const r=require(process.argv[1]);console.log(`ok  ${r.wall_s}s  ${r.tokens.total}tok  $${r.cost_usd}  ${r.steps} steps`)' "$dest"
    else
      echo "FAILED (see $dest.err)"; cat "$dest.err" | tail -3
    fi
  done
done

# 6. summary table
echo
echo "[6/6] summary"
node "$HERE/summarize.mjs" "$OUT" "$NAME" "$MODEL" | tee "$OUT/SUMMARY-${NAME}.md"

echo
echo "raw records: $OUT"
echo "clone kept at: $CLONE (rm -rf $WORK when done)"
