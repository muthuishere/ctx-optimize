// langs.go declares what each grammar's node types MEAN — which AST nodes are
// declarations (and what kind), which carry names, which are calls, which are
// imports. This is the whole per-language surface: adding a language = a
// grammar in the wasm + one entry here. IDs are the shim ABI (shim.c order).
package code

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Lang struct {
	ID       int
	Name     string
	Exts     []string
	Decls    map[string]string // AST node type → node kind we emit
	Names    map[string]bool   // node types that carry identifiers
	Calls    map[string]bool   // call-site node types
	Imports  map[string]bool   // import node types
	SkipDirs []string          // extra per-language noise dirs
}

func set(ss ...string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

var Languages = []Lang{
	{
		ID: 0, Name: "go", Exts: []string{".go"},
		Decls: map[string]string{
			"function_declaration": "function", "method_declaration": "method",
			"type_spec": "type",
		},
		Names:   set("identifier", "type_identifier", "field_identifier"),
		Calls:   set("call_expression"),
		Imports: set("import_spec"),
	},
	{
		ID: 1, Name: "python", Exts: []string{".py"},
		Decls: map[string]string{
			"function_definition": "function", "class_definition": "class",
		},
		Names:   set("identifier"),
		Calls:   set("call"),
		Imports: set("import_statement", "import_from_statement"),
	},
	{
		ID: 2, Name: "javascript", Exts: []string{".js", ".mjs", ".cjs", ".jsx"},
		Decls: map[string]string{
			"function_declaration": "function", "generator_function_declaration": "function",
			"method_definition": "method", "class_declaration": "class",
		},
		Names:   set("identifier", "property_identifier"),
		Calls:   set("call_expression"),
		Imports: set("import_statement"),
	},
	{
		ID: 3, Name: "typescript", Exts: []string{".ts", ".mts", ".cts"},
		Decls: map[string]string{
			"function_declaration": "function", "generator_function_declaration": "function",
			"method_definition": "method", "class_declaration": "class",
			"abstract_class_declaration": "class", "interface_declaration": "interface",
			"enum_declaration": "enum", "type_alias_declaration": "type",
			"function_signature": "function",
		},
		Names:   set("identifier", "property_identifier", "type_identifier"),
		Calls:   set("call_expression"),
		Imports: set("import_statement"),
	},
	{
		ID: 4, Name: "tsx", Exts: []string{".tsx"},
		Decls: map[string]string{
			"function_declaration": "function", "generator_function_declaration": "function",
			"method_definition": "method", "class_declaration": "class",
			"abstract_class_declaration": "class", "interface_declaration": "interface",
			"enum_declaration": "enum", "type_alias_declaration": "type",
		},
		Names:   set("identifier", "property_identifier", "type_identifier"),
		Calls:   set("call_expression"),
		Imports: set("import_statement"),
	},
	{
		ID: 5, Name: "java", Exts: []string{".java"},
		Decls: map[string]string{
			"class_declaration": "class", "interface_declaration": "interface",
			"enum_declaration": "enum", "record_declaration": "class",
			"method_declaration": "method", "constructor_declaration": "method",
		},
		Names:   set("identifier"),
		Calls:   set("method_invocation"),
		Imports: set("import_declaration"),
	},
	{
		ID: 6, Name: "c", Exts: []string{".c", ".h"},
		Decls: map[string]string{
			"function_definition": "function", "struct_specifier": "struct",
			"enum_specifier": "enum", "union_specifier": "struct", "type_definition": "type",
		},
		Names:   set("identifier", "field_identifier", "type_identifier"),
		Calls:   set("call_expression"),
		Imports: set("preproc_include"),
	},
	{
		ID: 7, Name: "cpp", Exts: []string{".cpp", ".cc", ".cxx", ".hpp", ".hh", ".hxx"},
		Decls: map[string]string{
			"function_definition": "function", "class_specifier": "class",
			"struct_specifier": "struct", "enum_specifier": "enum",
			"namespace_definition": "module", "type_definition": "type",
		},
		Names:   set("identifier", "field_identifier", "type_identifier", "namespace_identifier"),
		Calls:   set("call_expression"),
		Imports: set("preproc_include"),
	},
	{
		ID: 8, Name: "csharp", Exts: []string{".cs"},
		Decls: map[string]string{
			"class_declaration": "class", "interface_declaration": "interface",
			"struct_declaration": "struct", "enum_declaration": "enum",
			"record_declaration": "class", "method_declaration": "method",
			"constructor_declaration": "method", "namespace_declaration": "module",
		},
		Names:   set("identifier"),
		Calls:   set("invocation_expression"),
		Imports: set("using_directive"),
	},
	{
		ID: 9, Name: "rust", Exts: []string{".rs"},
		Decls: map[string]string{
			"function_item": "function", "struct_item": "struct", "enum_item": "enum",
			"trait_item": "trait", "mod_item": "module", "type_item": "type",
		},
		Names:   set("identifier", "type_identifier"),
		Calls:   set("call_expression"),
		Imports: set("use_declaration"),
	},
	{
		ID: 10, Name: "zig", Exts: []string{".zig"},
		// zig struct/enum/union literals are anonymous (named by the const
		// they're assigned to) — v1 takes functions and tests; containers
		// need parent-aware naming, later.
		Decls: map[string]string{
			"function_declaration": "function", "test_declaration": "function",
		},
		Names:   set("identifier"),
		Calls:   set("call_expression"),
		Imports: set(),
	},
	{
		ID: 11, Name: "sql", Exts: []string{".sql"},
		Decls: map[string]string{
			"create_table": "table", "create_view": "view",
			"create_materialized_view": "view", "create_function": "function",
			"create_procedure": "function", "create_index": "index",
			"create_schema": "module", "create_type": "type",
			"create_trigger": "function", "create_sequence": "type",
		},
		Names:   set("identifier", "object_reference"),
		Calls:   set("invocation"),
		Imports: set(),
	},
}

// LangForFile picks a language by extension (nil = not a code file we parse).
func LangForFile(name string) *Lang {
	lower := strings.ToLower(name)
	// Minified bundles poison the graph — never parse them.
	if strings.HasSuffix(lower, ".min.js") || strings.HasSuffix(lower, ".min.mjs") {
		return nil
	}
	for i := range Languages {
		for _, ext := range Languages[i].Exts {
			if strings.HasSuffix(lower, ext) {
				return &Languages[i]
			}
		}
	}
	return nil
}

// ---- dynamic grammar packs ----
//
// A pack is two files side by side in a grammars dir: <name>.wasm (built by
// scripts/wasm/build-grammar.sh — same shim, one grammar at id 0) and
// <name>.json declaring the node-type mapping:
//
//	{"name": "kotlin", "exts": [".kt", ".kts"],
//	 "decls": {"class_declaration": "class", "function_declaration": "function"},
//	 "names": ["simple_identifier", "type_identifier"],
//	 "calls": ["call_expression"], "imports": ["import_header"]}
//
// Dropping the pair in IS the registration — no recompile, no restart.
// Search order (later wins on extension clashes, and any pack beats the
// embedded set, so users can override built-ins):
//  1. $CTX_OPTIMIZE_GRAMMARS (default ~/ctxoptimize/grammars) — machine-wide
//  2. <repo>/.ctxoptimize/grammars — travels with the repo

// Pack is one dynamically loaded grammar.
type Pack struct {
	Lang     Lang
	WasmPath string
}

type packConfig struct {
	Name    string            `json:"name"`
	Exts    []string          `json:"exts"`
	Decls   map[string]string `json:"decls"`
	Names   []string          `json:"names"`
	Calls   []string          `json:"calls"`
	Imports []string          `json:"imports"`
}

// LoadPacks discovers grammar packs for a repo. Malformed packs fail loudly —
// a silently skipped language reads as "covered" when it isn't.
func LoadPacks(repo string) ([]Pack, error) {
	var dirs []string
	if env := os.Getenv("CTX_OPTIMIZE_GRAMMARS"); env != "" {
		dirs = append(dirs, env)
	} else if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "ctxoptimize", "grammars"))
	}
	dirs = append(dirs, filepath.Join(repo, ".ctxoptimize", "grammars"))

	byName := map[string]Pack{}
	var order []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			cfgPath := filepath.Join(dir, e.Name())
			data, err := os.ReadFile(cfgPath)
			if err != nil {
				return nil, err
			}
			var pc packConfig
			if err := json.Unmarshal(data, &pc); err != nil {
				return nil, fmt.Errorf("grammar pack %s: %w", cfgPath, err)
			}
			if pc.Name == "" || len(pc.Exts) == 0 || len(pc.Decls) == 0 {
				return nil, fmt.Errorf("grammar pack %s: name, exts and decls are required", cfgPath)
			}
			wasmPath := filepath.Join(dir, pc.Name+".wasm")
			if _, err := os.Stat(wasmPath); err != nil {
				return nil, fmt.Errorf("grammar pack %s: missing %s.wasm next to the config", cfgPath, pc.Name)
			}
			if _, seen := byName[pc.Name]; !seen {
				order = append(order, pc.Name)
			}
			byName[pc.Name] = Pack{
				Lang: Lang{
					ID: 0, Name: pc.Name, Exts: pc.Exts, Decls: pc.Decls,
					Names: set(pc.Names...), Calls: set(pc.Calls...), Imports: set(pc.Imports...),
				},
				WasmPath: wasmPath,
			}
		}
	}
	packs := make([]Pack, 0, len(order))
	for _, n := range order {
		packs = append(packs, byName[n])
	}
	return packs, nil
}
