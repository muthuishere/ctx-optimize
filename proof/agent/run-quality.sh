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
# Competitor arms d/e/f (codegraph / gitnexus / codegraphcontext) are OPT-IN via
# --arms, so the default invocation stays byte-for-byte the published a/b/c run.
# Their entry points come from the arena's versions.json (--arena, default
# ~/ctx-bench-arena), never from PATH, and that file is copied into the output
# dir so every result says which build it measured.
#
# Usage:
#   proof/agent/run-quality.sh [--model SLUG] [--questions FILE] [--bin PATH]
#                              [--out DIR] [--arms "a b c"] [--only "i1 i5"]
#                              [--arena DIR]
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
ARENA="$HOME/ctx-bench-arena"

while [ $# -gt 0 ]; do
  case "$1" in
    --arena) ARENA="$2"; shift 2;;
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

# 0. which arms are we asked for? (competitors are opt-in; default a b c)
WANT="${ARMS:-a b c}"
want() { case " $WANT " in *" $1 "*) return 0;; *) return 1;; esac; }

# 1. one clone per arm
for arm in a b c d e f; do
  want "$arm" || continue
  if [ ! -d "$WORK/repo-$arm" ]; then
    git clone --depth 1 "$REPO" "$WORK/repo-$arm" >/dev/null 2>&1
  fi
done
echo "clones:  $(find "$WORK/repo-a" -type f | wc -l | tr -d ' ') files each"

# entry points, straight out of the arena's versions.json — a competitor arm that
# cannot name its pinned build does not run.
VERSIONS="$ARENA/versions.json"
ventry() { node -e '
  const v=require(process.argv[1])[process.argv[2]]||{};
  process.stdout.write(v.entry_exists===false?"":(v.entry||""));' "$VERSIONS" "$1" 2>/dev/null || true; }
vsha()   { node -e '
  const v=require(process.argv[1])[process.argv[2]]||{};
  process.stdout.write(`${v.resolved_sha||"?"}${v.pinned?" (pinned)":" (UNPINNED)"}`);' "$VERSIONS" "$1" 2>/dev/null || true; }
CGRAPH_ENTRY=""; GNEXUS_ENTRY=""; CGC_BIN=""
if [ -f "$VERSIONS" ]; then
  cp "$VERSIONS" "$OUT/versions.json"
  CGRAPH_ENTRY="$(ventry codegraph)"
  GNEXUS_ENTRY="$(ventry gitnexus)"
  CGC_BIN="$(ventry codegraphcontext)"
fi

# 2. stores — every competitor index is BUILT HERE, before any question runs.
#    Indexing time is not part of the per-question measurement: the existing arms
#    all start from a built store, so the new ones must too.
#    An arm whose index cannot be built is recorded as a NON-RUN (a file the
#    reader can see), never dropped into a blank column that reads like a win.
RUN_ARMS=""
nonrun() { echo "$2" > "$OUT/NONRUN-$1.txt"; echo "store $1: NOT BUILT — $2"; }

if want a; then RUN_ARMS="$RUN_ARMS a"; fi

if want b; then
  if [ ! -d "$WORK/repo-b/.ctxoptimize" ]; then
    ( cd "$WORK/repo-b" && "$BIN" init >/dev/null 2>&1 && "$BIN" add . >/dev/null 2>&1 )
  fi
  echo "store b: $(cd "$WORK/repo-b" && "$BIN" status --json 2>/dev/null | node -e 'let s="";process.stdin.on("data",d=>s+=d).on("end",()=>{try{const j=JSON.parse(s);console.log(`${j.nodes} nodes, ${j.edges} edges`)}catch{console.log("built")}})')"
  RUN_ARMS="$RUN_ARMS b"
fi

if want c; then
  if ! command -v graphify >/dev/null 2>&1; then nonrun c "graphify not installed"
  elif [ -d "$WORK/repo-c/graphify-out" ] || ( cd "$WORK/repo-c" && graphify update . --no-cluster >/dev/null 2>&1 ); then
    echo "store c: graphify graph built"; RUN_ARMS="$RUN_ARMS c"
  else nonrun c "graphify update failed"; fi
fi

if want d; then
  if [ -z "$CGRAPH_ENTRY" ]; then nonrun d "no codegraph entry in $VERSIONS"
  elif [ -d "$WORK/repo-d/.codegraph" ] || ( cd "$WORK/repo-d" && CODEGRAPH_TELEMETRY=0 node "$CGRAPH_ENTRY" init . >"$WORK/build-d.log" 2>&1 ); then
    echo "store d: codegraph index built @ $(vsha codegraph)"; RUN_ARMS="$RUN_ARMS d"
  else nonrun d "codegraph init failed — see $WORK/build-d.log"; fi
fi

if want e; then
  # gitnexus keeps a GLOBAL registry in $HOME and refuses to disambiguate when
  # several repos share a name, so the arm gets its own HOME. Nothing else on the
  # box can leak into (or out of) this arm's index.
  export GNEXUS_HOME="$WORK/gitnexus-home"; mkdir -p "$GNEXUS_HOME"
  if [ -z "$GNEXUS_ENTRY" ]; then nonrun e "no gitnexus entry in $VERSIONS"
  elif [ -d "$WORK/repo-e/.gitnexus" ] || ( cd "$WORK/repo-e" && HOME="$GNEXUS_HOME" node "$GNEXUS_ENTRY" analyze . --skip-git --index-only >"$WORK/build-e.log" 2>&1 ); then
    echo "store e: gitnexus index built @ $(vsha gitnexus)"; RUN_ARMS="$RUN_ARMS e"
  else nonrun e "gitnexus analyze failed — see $WORK/build-e.log"; fi
fi

if want f; then
  export CGC_DB="$WORK/cgc-db"
  if [ -z "$CGC_BIN" ]; then nonrun f "no codegraphcontext entry in $VERSIONS"
  elif [ -d "$CGC_DB" ] || ( cd "$WORK/repo-f" && "$CGC_BIN" --db kuzudb --db-path "$CGC_DB" index . >"$WORK/build-f.log" 2>&1 ); then
    echo "store f: codegraphcontext index built @ $(vsha codegraphcontext)"; RUN_ARMS="$RUN_ARMS f"
  else nonrun f "cgc index failed — see $WORK/build-f.log"; fi
fi

ARMS="$(echo "$RUN_ARMS" | xargs)"
echo "arms:    $ARMS"
echo

# 3. run — load average and wall clock are recorded, as the other harnesses do:
#    a loaded box has swung the same A/B by double digits.
T_START="$(date -u +%Y-%m-%dT%H:%M:%SZ)"; T0="$(date +%s)"
LOAD_START="$(uptime | sed 's/.*load average[s]*: //')"
QIDS="${ONLY:-$(node -e 'require(process.argv[1]).questions.forEach(q=>console.log(q.id))' "$QFILE")}"
for qid in $QIDS; do
  QTEXT="$(node -e 'const q=require(process.argv[1]).questions.find(x=>x.id===process.argv[2]);if(!q){process.exit(3)}console.log(q.prompt)' "$QFILE" "$qid")"
  for arm in $ARMS; do
    dest="$OUT/${NAME}-${arm}-${qid}.json"
    printf "  %-4s arm %s ... " "$qid" "$arm"
    if node "$HERE/agent.mjs" --repo "$WORK/repo-$arm" --bin "$BIN" --arm "$arm" \
        --codegraph-entry "$CGRAPH_ENTRY" \
        --gitnexus-entry "$GNEXUS_ENTRY" --gitnexus-home "${GNEXUS_HOME:-}" \
        --cgc-bin "$CGC_BIN" --cgc-db "${CGC_DB:-}" \
        --model "$MODEL" --q "$QTEXT" > "$dest" 2>"$dest.err"; then
      node -e 'const r=require(process.argv[1]);console.log(`ok  ${r.wall_s}s  ${r.tokens.total}tok  $${r.cost_usd}  ${r.steps} steps  ${r.answer?"":"[EMPTY ANSWER]"}`)' "$dest"
    else
      echo "RUN FAILED — see $dest.err"; tail -3 "$dest.err"
    fi
  done
done

cat > "$OUT/RUN-META.json" <<EOF
{
  "started_utc": "$T_START",
  "finished_utc": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "wall_s": $(( $(date +%s) - T0 )),
  "model": "$MODEL",
  "arms": "$ARMS",
  "questions": "$(basename "$QFILE")",
  "ctx_optimize_version": "$("$BIN" --version 2>/dev/null | head -1)",
  "load_average_start": "$LOAD_START",
  "load_average_end": "$(uptime | sed 's/.*load average[s]*: //')",
  "host": "$(uname -srm)"
}
EOF

# 4. grade
echo
node "$HERE/grade.mjs" --results "$OUT" --name "$NAME" --questions "$QFILE" \
  | tee "$OUT/QUALITY-${NAME}.md"
echo
echo "raw records: $OUT"
echo "clones kept: $WORK  (rm -rf $WORK when done)"
