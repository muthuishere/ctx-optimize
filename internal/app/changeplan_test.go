package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// One repo, one call: change-plan must return the card facts, the blast
// radius, AND the tests-for section — the chain it replaces, composed.
func TestChangePlanComposesOneAnswer(t *testing.T) {
	repo := t.TempDir()
	files := map[string]string{
		"go.mod":         "module acme\n",
		"pay.go":         "package acme\n\nfunc Charge() {}\n\nfunc useCharge() { Charge() }\n",
		"pay_test.go":    "package acme\n\nimport \"testing\"\n\nfunc TestCharge(t *testing.T) { Charge() }\n",
		"billing_util.go": "package acme\n\nfunc helper() { Charge() }\n",
	}
	for p, content := range files {
		if err := os.WriteFile(filepath.Join(repo, p), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	store := t.TempDir()
	runCLI(t, 0, "add", repo, "--path", repo, "--store", store)

	out, _ := runCLI(t, 0, "change-plan", "Charge", "--path", repo, "--store", store)
	for _, want := range []string{
		"change plan: Charge",
		"sig: func Charge()",
		"callers (",
		"tests to run (",
		"TestCharge",
		"confidence:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	// JSON envelope matches the composed-answers contract: {"result": {...}}.
	jout, _ := runCLI(t, 0, "change-plan", "Charge", "--path", repo, "--store", store, "--json")
	var envelope struct {
		Result struct {
			Tests []struct {
				Node struct {
					Label string `json:"label"`
				} `json:"node"`
			} `json:"tests"`
			Confidence struct {
				Extracted int `json:"extracted_edges"`
			} `json:"confidence"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(jout), &envelope); err != nil {
		t.Fatalf("json: %v\n%s", err, jout)
	}
	foundTest := false
	for _, tt := range envelope.Result.Tests {
		if tt.Node.Label == "TestCharge" {
			foundTest = true
		}
	}
	if !foundTest {
		t.Errorf("tests-for did not surface TestCharge: %s", jout)
	}
	if envelope.Result.Confidence.Extracted == 0 {
		t.Error("confidence footer missing extracted count")
	}
}
