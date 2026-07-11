// langs.go declares what each grammar's node types MEAN — which AST nodes are
// declarations (and what kind), which carry names, which are calls, which are
// imports. This is the whole per-language surface: adding a language = a
// grammar in the wasm + one entry here. IDs are the shim ABI (shim.c order).
package code

import "strings"

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
		ID: 10, Name: "kotlin", Exts: []string{".kt", ".kts"},
		Decls: map[string]string{
			"class_declaration": "class", "object_declaration": "class",
			"function_declaration": "function",
		},
		Names:   set("simple_identifier", "type_identifier", "identifier"),
		Calls:   set("call_expression"),
		Imports: set("import_header"),
	},
	{
		ID: 11, Name: "dart", Exts: []string{".dart"},
		Decls: map[string]string{
			"class_definition": "class", "mixin_declaration": "class",
			"enum_declaration": "enum", "extension_declaration": "class",
			"function_signature": "function", "method_signature": "method",
			"getter_signature": "method", "setter_signature": "method",
		},
		Names: set("identifier"),
		// dart call sites are selector chains, not a single node type —
		// call edges wait for a finer pass; imports still land.
		Calls:   set(),
		Imports: set("library_import"),
	},
	{
		ID: 12, Name: "zig", Exts: []string{".zig"},
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
		ID: 13, Name: "swift", Exts: []string{".swift"},
		Decls: map[string]string{
			// class_declaration covers class/struct/enum/actor/extension in
			// this grammar; the keyword is a child, the kind stays "class".
			"class_declaration": "class", "protocol_declaration": "interface",
			"function_declaration": "function", "init_declaration": "method",
			"typealias_declaration": "type",
		},
		Names:   set("simple_identifier", "type_identifier", "identifier"),
		Calls:   set("call_expression"),
		Imports: set("import_declaration"),
	},
	{
		ID: 14, Name: "sql", Exts: []string{".sql"},
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
