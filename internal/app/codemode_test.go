package app

import (
	"bytes"
	"context"
	"io"
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

func TestStrictVerbRefusesFuzzyAndBadFlags(t *testing.T) {
	fuzzy := func(_ context.Context, _ []string, o, _ io.Writer) error {
		_, err := o.Write([]byte(`{"resolved_via":"fuzzy","node":{"id":"near::x"}}`))
		return err
	}
	exact := func(_ context.Context, _ []string, o, _ io.Writer) error {
		_, err := o.Write([]byte(`{"resolved_via":"exact-label","node":{"id":"a::b"}}`))
		return err
	}
	var out bytes.Buffer
	if err := strictVerb("ctx.card", fuzzy)(context.Background(), []string{"Nope"}, &out, io.Discard); err == nil ||
		!strings.Contains(err.Error(), "not found exactly") || !strings.Contains(err.Error(), "near::x") {
		t.Fatalf("fuzzy must be refused with the nearest id, got %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("a refused answer must write nothing, got %q", out.String())
	}
	if err := strictVerb("ctx.card", fuzzy)(context.Background(), []string{"Nope", "--fuzzy"}, &out, io.Discard); err != nil {
		t.Fatalf("--fuzzy opts in, got %v", err)
	}
	out.Reset()
	if err := strictVerb("ctx.card", exact)(context.Background(), []string{"A"}, &out, io.Discard); err != nil || out.Len() == 0 {
		t.Fatalf("exact must pass through, err=%v out=%q", err, out.String())
	}
	if err := strictVerb("ctx.hubs", exact)(context.Background(), []string{"--bogus"}, io.Discard, io.Discard); err == nil ||
		!strings.Contains(err.Error(), "unknown flag --bogus") {
		t.Fatalf("a mistyped flag must be rejected, got %v", err)
	}
	if err := strictVerb("ctx.change_plan", exact)(context.Background(), []string{"X", "--depth", "2"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("change_plan maps to the change-plan flag set, got %v", err)
	}
}
