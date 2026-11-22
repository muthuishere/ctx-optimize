package app

import (
	"bytes"
	"strings"
	"testing"
)

// ADR 2026-11-22-code-mode: the code verb exposes ONLY the read-only verbs and
// keeps the 0 ok / 1 failed / 2 nothing-ran exit contract.
func TestCodeModeOperationsAreReadOnly(t *testing.T) {
	var out, errb bytes.Buffer
	if rc := Run([]string{"code", "operations"}, &out, &errb); rc != 0 {
		t.Fatalf("operations rc=%d stderr=%s", rc, errb.String())
	}
	for _, want := range []string{"ctx.query", "ctx.card", "ctx.change_plan", "ctx.affected", "ctx.path", "ctx.hubs"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("operation %s missing:\n%s", want, out.String())
		}
	}
	for _, mutating := range []string{"ctx.add", "ctx.sync", "ctx.up", "ctx.install", "ctx.remote", "ctx.config"} {
		if strings.Contains(out.String(), mutating) {
			t.Errorf("mutating op %s must not be exposed", mutating)
		}
	}
}

func TestCodeModeExitCodes(t *testing.T) {
	var out, errb bytes.Buffer
	if rc := Run([]string{"code", "eval", "1+2", "--json"}, &out, &errb); rc != 0 || !strings.Contains(out.String(), `"ok": true`) {
		t.Fatalf("eval rc=%d out=%s err=%s", rc, out.String(), errb.String())
	}
	out.Reset()
	errb.Reset()
	if rc := Run([]string{"code", "eval", "ops.nope()", "--json"}, &out, &errb); rc != 2 {
		t.Fatalf("unknown op must exit 2 (nothing ran), got %d out=%s", rc, out.String())
	}
}
