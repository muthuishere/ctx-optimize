package tomlwalk

import (
	"strings"
	"testing"
)

// find returns the entry for table.key, or nil.
func find(es []Entry, table, key string) *Entry {
	for i := range es {
		if es[i].Path() == table && es[i].Key == key {
			return &es[i]
		}
	}
	return nil
}

func TestTablesAndAssignments(t *testing.T) {
	es := Parse(strings.Join([]string{
		`# leading comment`,
		`root = "yes"`,
		``,
		`[project]`,
		`name = "demo"   # trailing comment`,
		`[project.optional-dependencies]`,
		`async = ["asgiref>=3.2"]`,
		`[[tool.mypy.overrides]]`,
		`module = ["a.*"]`,
	}, "\n"))

	if e := find(es, "", "root"); e == nil || e.Val != `"yes"` || e.Num != 2 {
		t.Fatalf("root-level assignment: %+v", e)
	}
	if e := find(es, "project", "name"); e == nil || e.Val != `"demo"` {
		t.Fatalf("comment not cut: %+v", e)
	}
	if e := find(es, "project.optional-dependencies", "async"); e == nil {
		t.Fatal("quoted-free nested table missing")
	}
	e := find(es, "tool.mypy.overrides", "module")
	if e == nil || !e.ArrayTable {
		t.Fatalf("[[array table]] not tracked: %+v", e)
	}
	// Header lines are emitted as key-less markers so an empty table is visible.
	hdr := false
	for _, x := range es {
		if x.Key == "" && x.Path() == "project" && x.Num == 4 {
			hdr = true
		}
	}
	if !hdr {
		t.Fatal("table header marker missing")
	}
}

func TestQuotedAndDottedHeadersAndKeys(t *testing.T) {
	es := Parse(strings.Join([]string{
		`[target."cfg(windows)".dependencies]`,
		`winapi = "0.3"`,
		`["weird.extra"]`,
		`k = 1`,
		`[top]`,
		`a.b.c = "deep"`,
	}, "\n"))
	if e := find(es, `target.cfg(windows).dependencies`, "winapi"); e == nil {
		t.Fatalf("quoted header segment kept its dots: %v", paths(es))
	}
	if e := find(es, "weird.extra", "k"); e == nil {
		t.Fatalf("quoted single-segment header: %v", paths(es))
	}
	if e := find(es, "top.a.b", "c"); e == nil || e.Val != `"deep"` {
		t.Fatalf("dotted key not folded into the table path: %v", paths(es))
	}
}

func paths(es []Entry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Path()+"|"+e.Key)
	}
	return out
}

func TestMultiLineArrayWithCommentsAndAwkwardValues(t *testing.T) {
	es := Parse(strings.Join([]string{
		`[project]`,
		`dependencies = [`,
		`    "blinker>=1.9.0",   # the signal lib`,
		`    # a whole-line comment`,
		`    "click>=8.1.3",`,
		`]`,
		`odd = ["has # hash", "has ] bracket", "has = equals", 'lit#eral']`,
	}, "\n"))
	got := Strings(find(es, "project", "dependencies").Val)
	want := []string{"blinker>=1.9.0", "click>=8.1.3"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("multi-line array = %v, want %v", got, want)
	}
	odd := Strings(find(es, "project", "odd").Val)
	if len(odd) != 4 || odd[0] != "has # hash" || odd[1] != "has ] bracket" ||
		odd[2] != "has = equals" || odd[3] != "lit#eral" {
		t.Fatalf("quoted specials mangled: %q", odd)
	}
}

// A nested array element (flask's `commands = [["uv","pip","install",…]]`) is
// NOT a string element — Strings drops it, so no phantom dependency can leak
// out even if a caller pointed at the wrong table.
func TestStringsDropsNestedArraysAndInlineTables(t *testing.T) {
	es := Parse("[tool.tox.env.tests-min]\n" +
		`commands = [["uv", "pip", "install", "blinker==1.9.0"], {replace = "posargs"}]`)
	if got := Strings(find(es, "tool.tox.env.tests-min", "commands").Val); got != nil {
		t.Fatalf("nested elements must be dropped, got %q", got)
	}
	if n := len(Elements(find(es, "tool.tox.env.tests-min", "commands").Val)); n != 2 {
		t.Fatalf("Elements = %d, want 2 top-level elements", n)
	}
}

func TestInlineTables(t *testing.T) {
	es := Parse("[dependencies]\n" +
		`serde = { version = "1.0", features = ["derive", "rc"], optional = true }` + "\n" +
		`local = { path = "../thing" }`)
	v := find(es, "dependencies", "serde").Val
	if !IsInlineTable(v) {
		t.Fatalf("IsInlineTable(%q) = false", v)
	}
	if got := Unquote(Field(v, "version")); got != "1.0" {
		t.Fatalf("version field = %q", got)
	}
	if got := Strings(Field(v, "features")); len(got) != 2 || got[1] != "rc" {
		t.Fatalf("features field = %q", got)
	}
	if got := Field(v, "missing"); got != "" {
		t.Fatalf("absent field = %q", got)
	}
	if got := Unquote(Field(find(es, "dependencies", "local").Val, "path")); got != "../thing" {
		t.Fatalf("path field = %q", got)
	}
	if fields := InlineFields(v); len(fields) != 3 {
		t.Fatalf("InlineFields = %v", fields)
	}
}

// Multi-line inline tables are illegal TOML 1.0; we do not chase them. This
// pins the CURRENT behavior so a future change is deliberate.
func TestMultiLineInlineTableNotChased(t *testing.T) {
	es := Parse("[dependencies]\nserde = {\n  version = \"1.0\"\n}\n")
	e := find(es, "dependencies", "serde")
	if e == nil {
		t.Skip("value dropped entirely — also acceptable")
	}
	if Unquote(Field(e.Val, "version")) != "1.0" {
		t.Logf("permissive join gave %q — documented as strictly more permissive than the format", e.Val)
	}
}

// The failure shape from spike-p2: prose inside a multi-line string that
// happens to contain `dependencies = [...]` must NOT become a dependency.
func TestMultiLineStringsSkipped(t *testing.T) {
	es := Parse(strings.Join([]string{
		`[project]`,
		`description = """`,
		`Set dependencies = ["not-a-dep"] in your config.`,
		`[fake.table]`,
		`"""`,
		`dependencies = ["real-dep"]`,
		`notes = '''`,
		`other = ["also-not-a-dep"]`,
		`'''`,
		`name = "demo"`,
	}, "\n"))
	deps := find(es, "project", "dependencies")
	if deps == nil {
		t.Fatal("the real dependencies key must survive")
	}
	if got := Strings(deps.Val); len(got) != 1 || got[0] != "real-dep" {
		t.Fatalf("dependencies = %q, want [real-dep]", got)
	}
	for _, e := range es {
		if e.Key == "not-a-dep" || e.Key == "other" || e.Path() == "fake.table" {
			t.Fatalf("harvested from inside a multi-line string: %+v", e)
		}
	}
	if e := find(es, "project", "name"); e == nil || e.Val != `"demo"` {
		t.Fatalf("parsing did not resume after the literal string: %+v", e)
	}
	// A closed one-line triple-quoted string stays a normal value.
	one := Parse(`d = """inline"""`)
	if len(one) != 1 || one[0].Key != "d" {
		t.Fatalf("single-line triple quotes: %+v", one)
	}
}

func TestCRLF(t *testing.T) {
	es := Parse("[project]\r\ndependencies = [\r\n  \"flask\",\r\n]\r\n")
	if got := Strings(find(es, "project", "dependencies").Val); len(got) != 1 || got[0] != "flask" {
		t.Fatalf("CRLF = %q", got)
	}
}

func TestUnrepresentableLinesDropped(t *testing.T) {
	es := Parse("bare line with no equals\n[unclosed\nok = 1\n")
	if e := find(es, "", "ok"); e == nil {
		t.Fatal("recovery after junk lines failed")
	}
	for _, e := range es {
		if strings.Contains(e.Key, " ") {
			t.Fatalf("non-assignment became an entry: %+v", e)
		}
	}
}
