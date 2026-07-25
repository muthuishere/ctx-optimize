package golden

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestGoldenDockerStackRepo pins the Docker + Compose contract (ADR
// 2026-07-25-docker-compose-recognizer) on a committed fixture: a compose file
// with three imaged services plus a built one, both depends_on forms, a build
// path that resolves to a real Dockerfile, a multi-stage Dockerfile with
// COPY --from (one stage link, one image ref that must NOT link), and a k8s
// Deployment sharing an image with compose — the convergence landmark.
func TestGoldenDockerStackRepo(t *testing.T) {
	repo := t.TempDir()
	copyTree(t, filepath.Join("testdata", "repos", "dockerstack"), repo)
	storeRoot := t.TempDir()

	gatherWithin(t, 10*time.Second, repo, storeRoot)

	snap := snapshot(t, storeRoot)

	// 1. THE CONVERGENCE LANDMARK: one image node for the shared ref, with
	//    uses_image edges from BOTH lanes (compose service + k8s workload).
	if n := strings.Count(snap, "N image:ghcr.io/golden/api:2.0.0 | image"); n != 1 {
		t.Errorf("shared image must be ONE node, got %d", n)
	}
	mustContain(t, snap, "compose lane uses_image",
		"E compose.yaml::service:api -uses_image-> image:ghcr.io/golden/api:2.0.0")
	mustContain(t, snap, "k8s lane uses_image",
		"E k8s://default/deployment/api -uses_image-> image:ghcr.io/golden/api:2.0.0")

	// 2. Services are services, ids file-scoped so k8s ids can never collide.
	for _, name := range []string{"api", "db", "cache", "worker"} {
		mustContain(t, snap, "compose service node",
			"N compose.yaml::service:"+name+" | service | compose.yaml")
	}

	// 3. Both depends_on forms became edges (map form on api, list form on
	//    worker), and the build path resolved to the Dockerfile on disk.
	mustContain(t, snap, "depends_on map form",
		"E compose.yaml::service:api -depends_on-> compose.yaml::service:db")
	mustContain(t, snap, "depends_on list form",
		"E compose.yaml::service:worker -depends_on-> compose.yaml::service:cache")
	mustContain(t, snap, "build path resolved on disk",
		"E compose.yaml::service:worker -depends_on-> worker/Dockerfile")

	// 4. Multi-stage Dockerfile: named + indexed stages, the stage link, and
	//    NOT a link for the --from that names an image.
	mustContain(t, snap, "named stage", "N Dockerfile::stage:builder | stage | Dockerfile")
	mustContain(t, snap, "unnamed stage keyed by index", "N Dockerfile::stage:1 | stage | Dockerfile")
	mustContain(t, snap, "COPY --from stage link",
		"E Dockerfile::stage:1 -depends_on-> Dockerfile::stage:builder")
	if strings.Contains(snap, "-depends_on-> Dockerfile::stage:ghcr.io") ||
		strings.Contains(snap, "N Dockerfile::stage:ghcr.io") {
		t.Error("COPY --from=<image ref> must not become a stage or a stage link")
	}

	// 5. The secret surface never enters the graph, not even as a key.
	for _, banned := range []string{"hunter2", "ENTRYPOINT", "go build"} {
		if strings.Contains(snap, banned) {
			t.Errorf("command/env text leaked into the graph: %q", banned)
		}
	}

	// The exact snapshot is the contract.
	checkGolden(t, "dockerstack", snap)

	top := queryTop(t, storeRoot, repo, "worker service", 3)
	checkGolden(t, "dockerstack-queries", "worker service -> "+strings.Join(top, ", ")+"\n")
}
