package golden

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestGoldenMultiModuleConfigRepo pins the full extraction contract for a
// committed multi-module config repo: a Go service, an npm UI, and a .NET
// module whose source and tests live in SEPARATE folders (multi-path). Any
// change to what gather emits on this repo — nodes, edges, ranking — fails
// here with a diff.
func TestGoldenMultiModuleConfigRepo(t *testing.T) {
	repo := t.TempDir()
	copyTree(t, filepath.Join("testdata", "repos", "multimod"), repo)
	storeRoot := t.TempDir()

	runCLI(t, "init", "--path", repo, "--store", storeRoot)
	runCLI(t, "add", repo, "--path", repo, "--store", storeRoot)

	snap := snapshot(t, storeRoot)

	// Named landmarks first: readable failures for the facts that matter most.
	// 1. One store per declared module — single-path modules key by PATH,
	//    multi-path modules by NAME, plus the root navigator store.
	for _, key := range []string{"goldenmm ", "goldenmm/services/api", "goldenmm/web/ui", "goldenmm/Billing"} {
		mustContain(t, snap, "module store "+key, "== store "+key)
	}
	// 2. The multi-path split gathered into ONE store with repo-root-relative
	//    ids, and the test→source call resolved ACROSS the split.
	mustContain(t, snap, "source decl in multi-path store",
		"N src/Billing/BillingEngine.cs::BillingEngine.ChargeCard")
	mustContain(t, snap, "test decl in same store",
		"tests/Billing.Tests/BillingEngineTests.cs::BillingEngineTests.ChargeCardWorks")
	mustContain(t, snap, "cross-split call edge",
		"E tests/Billing.Tests/BillingEngineTests.cs::BillingEngineTests.ChargeCardWorks -calls-> src/Billing/BillingEngine.cs::BillingEngine.ChargeCard")
	// 3. Manifest facts: npm dependency + task, go.mod dependency, csproj, k8s.
	mustContain(t, snap, "npm dependency node", "dep:npm/react")
	mustContain(t, snap, "npm task node", "package.json::task:build")
	mustContain(t, snap, "go.mod dependency", "github.com/pkg/errors")
	mustContain(t, snap, "k8s image", "ghcr.io/golden/api:1.0.0")

	// The exact snapshot is the contract.
	checkGolden(t, "multimod", snap)

	// Retrieval golden: scope follows cwd — module dir answers from module.
	apiTop := queryTop(t, storeRoot, filepath.Join(repo, "services", "api"), "process order", 3)
	billingTop := queryTop(t, storeRoot, filepath.Join(repo, "src", "Billing"), "charge card", 3)
	checkGolden(t, "multimod-queries",
		"api: process order -> "+strings.Join(apiTop, ", ")+"\n"+
			"Billing: charge card -> "+strings.Join(billingTop, ", ")+"\n")
}

// TestGoldenDotnetSlnRepo pins the plain csproj/sln repo path: no committed
// config, single store, sln+csproj manifests understood, and call resolution
// working module-wide across src/ and tests/ folders.
func TestGoldenDotnetSlnRepo(t *testing.T) {
	repo := t.TempDir()
	copyTree(t, filepath.Join("testdata", "repos", "dotnetsln"), repo)
	storeRoot := t.TempDir()

	runCLI(t, "init", "--path", repo, "--store", storeRoot)
	runCLI(t, "add", repo, "--path", repo, "--store", storeRoot)

	snap := snapshot(t, storeRoot)

	mustContain(t, snap, "source class decl", "Engine.cs::Engine")
	mustContain(t, snap, "test decl", "EngineTests.cs::EngineTests.AddWorks")
	mustContain(t, snap, "call across folders resolved module-wide",
		"-calls-> src/App/Engine.cs::Engine.Add")

	checkGolden(t, "dotnetsln", snap)

	top := queryTop(t, storeRoot, repo, "engine add", 3)
	checkGolden(t, "dotnetsln-queries", "engine add -> "+strings.Join(top, ", ")+"\n")
}
