package manifests

import (
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// fixtureDockerfile: a multi-stage build with free-form AS casing, a --platform
// flag, an unnamed stage, a stage-to-stage COPY --from, a COPY --from naming an
// IMAGE (not a stage), EXPOSE, and command text that must never be read.
const fixtureDockerfile = `# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.24 as builder
WORKDIR /src
RUN go build -ldflags "-X token=hunter2" -o /out/app ./cmd/app

FROM node:22 AS assets
RUN npm ci && npm run build

FROM gcr.io/distroless/static:nonroot
COPY --from=builder /out/app /app
COPY --from=assets /build /web
COPY --from=ghcr.io/acme/tools:2 /usr/bin/tool /usr/bin/tool
EXPOSE 8080 9090
ENTRYPOINT ["/app", "--password=hunter2"]
`

func TestDockerfileStagesImagesAndStageLinks(t *testing.T) {
	b := extractFixture(t, map[string]string{"Dockerfile": fixtureDockerfile})

	// 1. Stages: named ones by name, the unnamed final stage by index.
	builder := nodeByID(b, "Dockerfile::stage:builder")
	if builder == nil {
		t.Fatal("missing builder stage node")
	}
	if builder.Kind != "stage" || builder.Label != "builder" || builder.Source != "Dockerfile" {
		t.Errorf("builder stage shape: %+v", builder)
	}
	if nodeByID(b, "Dockerfile::stage:assets") == nil {
		t.Fatal("missing assets stage node (AS casing is free-form)")
	}
	final := nodeByID(b, "Dockerfile::stage:2")
	if final == nil {
		t.Fatal("unnamed stage must be keyed by index")
	}
	if final.Label != "gcr.io/distroless/static:nonroot" {
		t.Errorf("unnamed stage label = %q, want the image ref", final.Label)
	}
	if got := final.Metadata["expose"]; got != "8080,9090" {
		t.Errorf("expose metadata = %q", got)
	}

	// 2. Base images share the k8s/compose image vocabulary. --platform is a
	//    flag, not part of the reference.
	for _, img := range []string{"golang:1.24", "node:22", "gcr.io/distroless/static:nonroot"} {
		if n := nodeByID(b, "image:"+img); n == nil || n.Kind != "image" {
			t.Fatalf("missing image node for %s", img)
		}
	}
	mustEdge(t, b, "Dockerfile::stage:builder", "image:golang:1.24", "uses_image", schema.Extracted)
	mustEdge(t, b, "Dockerfile::stage:2", "image:gcr.io/distroless/static:nonroot", "uses_image", schema.Extracted)

	// 3. COPY --from a declared stage links stages…
	mustEdge(t, b, "Dockerfile::stage:2", "Dockerfile::stage:builder", "depends_on", schema.Extracted)
	mustEdge(t, b, "Dockerfile::stage:2", "Dockerfile::stage:assets", "depends_on", schema.Extracted)
	// 4. …and a --from naming an IMAGE is not a stage link (never guessed).
	for _, e := range b.Edges {
		if e.Relation == "depends_on" && strings.Contains(e.Target, "ghcr.io") {
			t.Errorf("COPY --from=<image> must not produce a stage edge: %s -> %s", e.Source, e.Target)
		}
	}
	if nodeByID(b, "Dockerfile::stage:ghcr.io/acme/tools:2") != nil {
		t.Error("an image referenced by --from must not become a stage node")
	}

	// 5. Command text is never read: RUN/ENTRYPOINT can embed secrets.
	for _, n := range b.Nodes {
		if strings.Contains(n.Label, "hunter2") || strings.Contains(n.ID, "hunter2") {
			t.Errorf("command text leaked into %s", n.ID)
		}
		for k, v := range n.Metadata {
			if strings.Contains(v, "hunter2") || strings.Contains(v, "go build") || k == "cmd" || k == "entrypoint" {
				t.Errorf("command text leaked into %s metadata: %s=%s", n.ID, k, v)
			}
		}
	}
}

// A numeric `--from=0` is a stage INDEX, not a declared stage name — under the
// literal rule that is not enough to claim, so no edge is emitted.
func TestDockerfileNumericFromClaimsNothing(t *testing.T) {
	b := extractFixture(t, map[string]string{"api.Dockerfile": `FROM golang:1.24
FROM alpine:3.20
COPY --from=0 /out/app /app
`})
	for _, e := range b.Edges {
		if e.Relation == "depends_on" {
			t.Errorf("--from=<index> must claim nothing, got %s -> %s", e.Source, e.Target)
		}
	}
}

// Exact Location contract: a stage is anchored on its FROM line only — a span
// would let `card`'s body hydration read RUN/ENTRYPOINT text back out of the
// file, which this lane refuses to put in the graph.
func TestDockerfileExactLocations(t *testing.T) {
	b := extractFixture(t, map[string]string{"Dockerfile.dev": `FROM golang:1.24 AS builder
WORKDIR /src

FROM alpine:3.20
COPY --from=builder /out/app /app
EXPOSE 80
`})
	for id, want := range map[string]string{
		"Dockerfile.dev::stage:builder": "L1",
		"Dockerfile.dev::stage:1":       "L4",
	} {
		n := nodeByID(b, id)
		if n == nil {
			t.Fatalf("missing %s", id)
		}
		if n.Location != want {
			t.Errorf("%s location = %q, want %q", id, n.Location, want)
		}
	}
}

// ${VAR} in a FROM stays the literal text it is.
func TestDockerfileVariableRefStaysLiteral(t *testing.T) {
	b := extractFixture(t, map[string]string{"Dockerfile": "FROM ghcr.io/acme/base:${TAG}\n"})
	if nodeByID(b, "image:ghcr.io/acme/base:${TAG}") == nil {
		t.Error("a ${VAR} image ref must be emitted as literal text, never resolved")
	}
}

// A compose build edge and the Dockerfile's own stages meet on the same file
// node — the compose→Dockerfile join the ADR asks for.
func TestComposeBuildJoinsDockerfileStages(t *testing.T) {
	b := extractFixture(t, map[string]string{
		"compose.yaml":   "services:\n  api:\n    build: ./api\n",
		"api/Dockerfile": "FROM golang:1.24 AS builder\nFROM alpine:3.20\n",
	})
	mustEdge(t, b, "compose.yaml::service:api", "api/Dockerfile", "depends_on", schema.Extracted)
	mustEdge(t, b, "api/Dockerfile", "api/Dockerfile::stage:builder", "contains", schema.Extracted)
}
