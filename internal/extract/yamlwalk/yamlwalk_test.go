package yamlwalk

import "testing"

func TestParseSpansAndScalars(t *testing.T) {
	ls := Parse([]string{
		"top: value # comment",
		"block:",
		"  child: 'quoted'",
		"  list:",
		"    - item",
		"    - name: deep",
		"      extra: x",
		"\ttabbed: refused",
		"# comment only",
		"",
	}, 0)
	if len(ls) != 7 {
		t.Fatalf("lines = %d, want 7 (tabs/comments/blanks dropped): %+v", len(ls), ls)
	}
	if ls[0].Key != "top" || ls[0].Val != "value" || ls[0].Num != 1 {
		t.Fatalf("scalar line: %+v", ls[0])
	}
	if ls[2].Val != "quoted" {
		t.Fatalf("quote stripping: %+v", ls[2])
	}
	if !ls[4].List || ls[4].Val != "item" || ls[4].Key != "" {
		t.Fatalf("bare list item: %+v", ls[4])
	}
	// block owns everything indented deeper.
	if got := Span(ls, 1); got != 7 {
		t.Fatalf("Span(block) = %d, want 7", got)
	}
	// the `- name: deep` item owns its sibling-indented `extra: x`.
	if got := ItemSpan(ls, 5); got != 7 {
		t.Fatalf("ItemSpan = %d, want 7", got)
	}
}

func TestSplitKeyValKeepsURLsWhole(t *testing.T) {
	ls := Parse([]string{"url: http://example.com/x", "plain scalar"}, 10)
	if ls[0].Key != "url" || ls[0].Val != "http://example.com/x" {
		t.Fatalf("url line: %+v", ls[0])
	}
	if ls[0].Num != 11 {
		t.Fatalf("offset: %+v", ls[0])
	}
	if ls[1].Key != "" || ls[1].Val != "plain scalar" {
		t.Fatalf("scalar line: %+v", ls[1])
	}
}
