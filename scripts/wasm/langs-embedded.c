// Embedded-language table for the bundled treesitter.wasm build.
#include <tree_sitter/api.h>

extern const TSLanguage *tree_sitter_go(void);
extern const TSLanguage *tree_sitter_python(void);
extern const TSLanguage *tree_sitter_javascript(void);
extern const TSLanguage *tree_sitter_typescript(void);
extern const TSLanguage *tree_sitter_tsx(void);
extern const TSLanguage *tree_sitter_java(void);
extern const TSLanguage *tree_sitter_c(void);
extern const TSLanguage *tree_sitter_cpp(void);
extern const TSLanguage *tree_sitter_c_sharp(void);
extern const TSLanguage *tree_sitter_rust(void);
extern const TSLanguage *tree_sitter_zig(void);
extern const TSLanguage *tree_sitter_sql(void);

// Order is the ABI: keep in sync with langs.go (embedded languages).
const TSLanguage *co_lang_by_id(int id) {
  switch (id) {
  case 0: return tree_sitter_go();
  case 1: return tree_sitter_python();
  case 2: return tree_sitter_javascript();
  case 3: return tree_sitter_typescript();
  case 4: return tree_sitter_tsx();
  case 5: return tree_sitter_java();
  case 6: return tree_sitter_c();
  case 7: return tree_sitter_cpp();
  case 8: return tree_sitter_c_sharp();
  case 9: return tree_sitter_rust();
  case 10: return tree_sitter_zig();
  case 11: return tree_sitter_sql();
  default: return NULL;
  }
}

