// WASI shim: tree-sitter runtime + all grammars in one wasm module.
// The host (Go/wazero) sends source bytes in, gets back a flat preorder dump
// of the AST — symbol id, byte range, row, depth, named flag per node — and
// slices meaning out of it in Go. The shim stays generic: it knows nothing
// about languages beyond their exported grammar functions.
//
// Exports:
//   co_alloc(n) / co_free(p)          guest-side buffers for source bytes
//   co_symbols(lang)                  dump the language's symbol-name table
//   co_parse(lang, src, len)          parse + dump preorder records
//   co_out_ptr() / co_out_len()       location of the last dump
//
// Dump formats (little-endian u32 unless noted):
//   co_symbols: u32 count, then count C strings (NUL-terminated)
//   co_parse:   u32 count, then count records of 6 u32:
//               symbol, start_byte, end_byte, start_row, end_row,
//               (depth<<1)|named
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <tree_sitter/api.h>

// The language table lives in a companion unit: langs-embedded.c for the
// bundled build, a generated single-grammar table for grammar packs
// (scripts/wasm/build-grammar.sh). Same shim either way.
extern const TSLanguage *co_lang_by_id(int id);

static uint8_t *out_buf = NULL;
static size_t out_cap = 0, out_len = 0;

static int ensure(size_t need) {
  if (out_len + need <= out_cap) return 0;
  size_t cap = (out_len + need) * 2 + 4096;
  uint8_t *nb = realloc(out_buf, cap);
  if (!nb) return -1;
  out_buf = nb;
  out_cap = cap;
  return 0;
}

static int put_u32(uint32_t v) {
  if (ensure(4)) return -1;
  memcpy(out_buf + out_len, &v, 4);
  out_len += 4;
  return 0;
}

__attribute__((export_name("co_alloc"))) void *co_alloc(size_t n) { return malloc(n); }
__attribute__((export_name("co_free"))) void co_free(void *p) { free(p); }
__attribute__((export_name("co_out_ptr"))) const uint8_t *co_out_ptr(void) { return out_buf; }
__attribute__((export_name("co_out_len"))) uint32_t co_out_len(void) { return (uint32_t)out_len; }

__attribute__((export_name("co_symbols"))) int co_symbols(int lang_id) {
  const TSLanguage *L = co_lang_by_id(lang_id);
  if (!L) return -1;
  out_len = 0;
  uint32_t n = ts_language_symbol_count(L);
  if (put_u32(n)) return -3;
  for (uint32_t i = 0; i < n; i++) {
    const char *name = ts_language_symbol_name(L, (TSSymbol)i);
    size_t l = strlen(name) + 1;
    if (ensure(l)) return -3;
    memcpy(out_buf + out_len, name, l);
    out_len += l;
  }
  return 0;
}

__attribute__((export_name("co_parse"))) int co_parse(int lang_id, const char *src, uint32_t len) {
  const TSLanguage *L = co_lang_by_id(lang_id);
  if (!L) return -1;
  TSParser *p = ts_parser_new();
  if (!p) return -2;
  ts_parser_set_language(p, L);
  TSTree *tree = ts_parser_parse_string(p, NULL, src, len);
  if (!tree) {
    ts_parser_delete(p);
    return -2;
  }
  out_len = 0;
  if (put_u32(0)) return -3; // count patched below
  uint32_t count = 0;
  TSTreeCursor cur = ts_tree_cursor_new(ts_tree_root_node(tree));
  uint32_t depth = 0;
  for (;;) {
    TSNode node = ts_tree_cursor_current_node(&cur);
    if (put_u32(ts_node_symbol(node)) || put_u32(ts_node_start_byte(node)) ||
        put_u32(ts_node_end_byte(node)) || put_u32(ts_node_start_point(node).row) ||
        put_u32(ts_node_end_point(node).row) ||
        put_u32((depth << 1) | (ts_node_is_named(node) ? 1u : 0u))) {
      count = 0;
      goto done;
    }
    count++;
    if (ts_tree_cursor_goto_first_child(&cur)) {
      depth++;
      continue;
    }
    for (;;) {
      if (ts_tree_cursor_goto_next_sibling(&cur)) break;
      if (!ts_tree_cursor_goto_parent(&cur)) goto done;
      depth--;
    }
  }
done:
  memcpy(out_buf, &count, 4);
  ts_tree_cursor_delete(&cur);
  ts_tree_delete(tree);
  ts_parser_delete(p);
  return count ? 0 : -3;
}
