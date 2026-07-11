#!/usr/bin/env bash
# Build ONE grammar into a standalone ctx-optimize grammar pack:
#
#   scripts/wasm/build-grammar.sh <grammar-src-dir> <c-symbol> <out.wasm>
#   scripts/wasm/build-grammar.sh ~/src/tree-sitter-kotlin kotlin kotlin.wasm
#
# <grammar-src-dir> must contain src/parser.c (+ optional src/scanner.c).
# <c-symbol> is the grammar's exported function suffix: tree_sitter_<c-symbol>.
# Drop the result next to a <name>.json config in ~/ctxoptimize/grammars/ (or
# a repo's .ctxoptimize/grammars/) and `add` picks it up — no recompile of
# ctx-optimize, no restart.
set -euo pipefail

GRAMMAR_DIR="$1"; SYM="$2"; OUT="$3"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BUILD_DIR="${BUILD_DIR:-$REPO_ROOT/.wasm-build}"
RUNTIME="$BUILD_DIR/tree-sitter"
[ -d "$RUNTIME" ] || git clone --depth 1 https://github.com/tree-sitter/tree-sitter.git "$RUNTIME"
[ -f "$GRAMMAR_DIR/src/parser.c" ] || { echo "no src/parser.c in $GRAMMAR_DIR" >&2; exit 1; }

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
cat > "$TMP/langtab.c" <<C
#include <tree_sitter/api.h>
extern const TSLanguage *tree_sitter_${SYM}(void);
const TSLanguage *co_lang_by_id(int id) { return id == 0 ? tree_sitter_${SYM}() : 0; }
C

SRCS=("$REPO_ROOT/scripts/wasm/shim.c" "$TMP/langtab.c" "$RUNTIME/lib/src/lib.c" "$GRAMMAR_DIR/src/parser.c")
[ -f "$GRAMMAR_DIR/src/scanner.c" ] && SRCS+=("$GRAMMAR_DIR/src/scanner.c")

zig cc -target wasm32-wasi -mexec-model=reactor -O2 \
  -Wl,--export-dynamic -Wl,--initial-memory=67108864 -Wl,--max-memory=1073741824 \
  -I "$RUNTIME/lib/include" -I "$RUNTIME/lib/src" -I "$GRAMMAR_DIR/src" \
  "${SRCS[@]}" -o "$OUT"
ls -la "$OUT"
