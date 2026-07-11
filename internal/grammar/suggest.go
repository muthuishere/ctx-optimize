// suggest.go turns a grammar's node-types.json into a STARTER pack config —
// deterministic pattern-matching over node type names, the same heuristics a
// human applies first ("*_declaration is probably a decl"). The output is
// explicitly a draft: the user (or their agent) reviews it against real code.
package grammar

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Suggest builds a pack config draft for the grammar in srcDir. exts seeds
// the extension list (registry-known); empty defaults to ".<name>".
func Suggest(name, srcDir string, exts []string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(srcDir, "src", "node-types.json"))
	if err != nil {
		return nil, err
	}
	var nodeTypes []struct {
		Type  string `json:"type"`
		Named bool   `json:"named"`
	}
	if err := json.Unmarshal(data, &nodeTypes); err != nil {
		return nil, fmt.Errorf("parse node-types.json: %w", err)
	}

	decls := map[string]string{}
	names, calls, imports := []string{}, []string{}, []string{}
	for _, nt := range nodeTypes {
		if !nt.Named {
			continue
		}
		t := nt.Type
		switch {
		case strings.Contains(t, "identifier") || t == "constant":
			// "constant" is ruby-style class names; harmless elsewhere.
			names = append(names, t)
		case strings.Contains(t, "import") || strings.Contains(t, "include") || t == "use_declaration" || t == "using_directive":
			imports = append(imports, t)
		case strings.Contains(t, "call") || strings.Contains(t, "invocation"):
			calls = append(calls, t)
		case exactKinds[t] != "":
			decls[t] = exactKinds[t]
		case isDeclLike(t):
			decls[t] = kindFor(t)
		}
	}
	sort.Strings(names)
	sort.Strings(calls)
	sort.Strings(imports)

	if len(exts) == 0 {
		exts = []string{"." + name}
	}
	out := map[string]any{
		"_review": "DRAFT generated from node-types.json — verify decls/names/calls/imports against real code before trusting the graph",
		"name":    name,
		"exts":    exts,
		"decls":   decls,
		"names":   names,
		"calls":   calls,
		"imports": imports,
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// exactKinds catches grammars whose decl types are bare words (ruby: method,
// class, module; elixir: call-based, won't match — reviewed by hand).
var exactKinds = map[string]string{
	"class": "class", "module": "module", "method": "function",
	"singleton_method": "function", "function": "function",
	"interface": "interface", "struct": "struct", "enum": "enum",
	"trait": "trait", "protocol": "interface",
}

func isDeclLike(t string) bool {
	for _, suffix := range []string{"_declaration", "_definition", "_item", "_signature", "_specifier"} {
		if strings.HasSuffix(t, suffix) {
			// local/parameter noise is not a declaration worth a node
			if strings.HasPrefix(t, "local_") || strings.Contains(t, "variable") ||
				strings.Contains(t, "parameter") || strings.Contains(t, "field_decl") {
				return false
			}
			return true
		}
	}
	return strings.HasPrefix(t, "create_") // SQL-style
}

func kindFor(t string) string {
	switch {
	case strings.Contains(t, "class"):
		return "class"
	case strings.Contains(t, "interface") || strings.Contains(t, "protocol"):
		return "interface"
	case strings.Contains(t, "trait"):
		return "trait"
	case strings.Contains(t, "struct"):
		return "struct"
	case strings.Contains(t, "enum"):
		return "enum"
	case strings.Contains(t, "function") || strings.Contains(t, "method") ||
		strings.Contains(t, "constructor") || strings.Contains(t, "procedure"):
		return "function"
	case strings.Contains(t, "module") || strings.Contains(t, "namespace") || strings.Contains(t, "schema"):
		return "module"
	case strings.HasPrefix(t, "create_table"):
		return "table"
	case strings.HasPrefix(t, "create_view"):
		return "view"
	default:
		return "type"
	}
}
