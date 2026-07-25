package manifests

import (
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// fixtureComposeSecrets is the adversarial compose file: real-looking secrets
// in `environment:` (both map and list form) plus an `env_file:`. The
// recognizer must read the services around them and NOTHING from them.
const fixtureComposeSecrets = `services:
  api:
    image: ghcr.io/acme/api:1.2.3
    env_file:
      - ./deploy/api.env
    environment:
      POSTGRES_PASSWORD: hunter2
      DB_URL: postgres://u:p@db/app
    depends_on: [db]
  db:
    image: postgres:16
    environment:
      - POSTGRES_PASSWORD=hunter2
      - DB_URL=postgres://u:p@db/app
`

// THE secret test (ADR verification 2): the env surface must not reach the
// graph — not the values, not the KEYS, not the env_file path. Asserted by
// scanning every field of every emitted node and edge, and paired with a
// positive assertion so it can never pass vacuously.
func TestComposeEnvironmentNeverEntersTheGraph(t *testing.T) {
	b := extractFixture(t, map[string]string{"compose.yaml": fixtureComposeSecrets})

	// Positive half: the file WAS understood (otherwise "no leak" is vacuous).
	if nodeByID(b, "compose.yaml::service:api") == nil || nodeByID(b, "compose.yaml::service:db") == nil {
		t.Fatal("compose services not recognized — the no-leak scan below would be vacuous")
	}
	mustEdge(t, b, "compose.yaml::service:api", "image:ghcr.io/acme/api:1.2.3", "uses_image", schema.Extracted)
	mustEdge(t, b, "compose.yaml::service:api", "compose.yaml::service:db", "depends_on", schema.Extracted)

	banned := []string{
		"hunter2", "postgres://u:p", "POSTGRES_PASSWORD", "DB_URL",
		"env_file", "environment", "api.env",
	}
	check := func(what, s string) {
		for _, bad := range banned {
			if strings.Contains(s, bad) {
				t.Errorf("env surface leaked into %s: %q contains %q", what, s, bad)
			}
		}
	}
	for _, n := range b.Nodes {
		check("node id", n.ID)
		check("node label", n.Label)
		check("node source", n.Source)
		check("node scope", n.Scope)
		for k, v := range n.Metadata {
			check("node "+n.ID+" metadata key", k)
			check("node "+n.ID+" metadata value", v)
		}
	}
	for _, e := range b.Edges {
		check("edge source", e.Source)
		check("edge target", e.Target)
		check("edge relation", e.Relation)
		for k, v := range e.Metadata {
			check("edge metadata key", k)
			check("edge metadata value", v)
		}
	}
}

// fixtureComposeStack carries every literal shape the ADR claims: three
// services, an image ref, both depends_on forms, ports, a build string form and
// a build map form.
const fixtureComposeStack = `services:
  api:
    image: ghcr.io/acme/api:1.2.3
    ports:
      - "8080:8080"
      - "9090:9090"
    depends_on:
      db:
        condition: service_healthy
      cache:
        condition: service_started
  db:
    image: postgres:16
    ports: ["5432:5432"]
  cache:
    image: redis:7
  worker:
    build: ./worker
    depends_on:
      - db
      - cache
  edge:
    build:
      context: .
      dockerfile: Dockerfile.edge
  ghost:
    build: ./nowhere
`

func TestComposeServicesImagesDependsOnPortsAndBuild(t *testing.T) {
	b := extractFixture(t, map[string]string{
		"compose.yaml":      fixtureComposeStack,
		"worker/Dockerfile": "FROM python:3.12\n",
		"Dockerfile.edge":   "FROM nginx:1.27\n",
	})

	// 1. Services are services, keyed by file so k8s ids can never collide.
	for _, name := range []string{"api", "db", "cache", "worker", "edge", "ghost"} {
		n := nodeByID(b, "compose.yaml::service:"+name)
		if n == nil {
			t.Fatalf("missing service node for %s", name)
		}
		if n.Kind != "service" || n.Label != name || n.Source != "compose.yaml" {
			t.Errorf("service node shape for %s: %+v", name, n)
		}
		if n.Location == "" {
			t.Errorf("service %s has no location", name)
		}
	}

	// 2. Images: shared node + uses_image, same vocabulary as the k8s lane.
	for _, img := range []string{"ghcr.io/acme/api:1.2.3", "postgres:16", "redis:7"} {
		n := nodeByID(b, "image:"+img)
		if n == nil || n.Kind != "image" || n.Label != img || n.Source != "image:"+img {
			t.Fatalf("image node shape for %s: %+v", img, n)
		}
	}
	mustEdge(t, b, "compose.yaml::service:api", "image:ghcr.io/acme/api:1.2.3", "uses_image", schema.Extracted)
	mustEdge(t, b, "compose.yaml::service:db", "image:postgres:16", "uses_image", schema.Extracted)
	mustEdge(t, b, "compose.yaml::service:cache", "image:redis:7", "uses_image", schema.Extracted)

	// 3. depends_on: map form (api) AND list form (worker).
	mustEdge(t, b, "compose.yaml::service:api", "compose.yaml::service:db", "depends_on", schema.Extracted)
	mustEdge(t, b, "compose.yaml::service:api", "compose.yaml::service:cache", "depends_on", schema.Extracted)
	mustEdge(t, b, "compose.yaml::service:worker", "compose.yaml::service:db", "depends_on", schema.Extracted)
	mustEdge(t, b, "compose.yaml::service:worker", "compose.yaml::service:cache", "depends_on", schema.Extracted)
	// `condition: service_healthy` is NOT a service.
	if nodeByID(b, "compose.yaml::service:condition") != nil {
		t.Error("depends_on map values leaked as services")
	}

	// 4. Ports are metadata, never nodes — block form and inline form.
	if got := nodeByID(b, "compose.yaml::service:api").Metadata["ports"]; got != "8080:8080,9090:9090" {
		t.Errorf("api ports metadata = %q", got)
	}
	if got := nodeByID(b, "compose.yaml::service:db").Metadata["ports"]; got != "5432:5432" {
		t.Errorf("db inline ports metadata = %q", got)
	}

	// 5. build: string form and map form both link to the real Dockerfile.
	mustEdge(t, b, "compose.yaml::service:worker", "worker/Dockerfile", "depends_on", schema.Extracted)
	mustEdge(t, b, "compose.yaml::service:edge", "Dockerfile.edge", "depends_on", schema.Extracted)
	// 6. …and a build whose Dockerfile is NOT on disk emits nothing.
	for _, e := range b.Edges {
		if e.Source == "compose.yaml::service:ghost" {
			t.Errorf("unresolvable build produced an edge: %s -> %s", e.Source, e.Target)
		}
	}
}

// Inline flow build map: `build: {context: ., dockerfile: Dockerfile.dev}`.
func TestComposeBuildFlowMap(t *testing.T) {
	b := extractFixture(t, map[string]string{
		"docker-compose.yml": "services:\n  app:\n    build: {context: ., dockerfile: Dockerfile.dev}\n",
		"Dockerfile.dev":     "FROM node:22\n",
	})
	mustEdge(t, b, "docker-compose.yml::service:app", "Dockerfile.dev", "depends_on", schema.Extracted)
}

// ${VAR} is never resolved: the literal text is what lands in the graph, and a
// variable build path resolves to nothing.
func TestComposeVariablesStayLiteral(t *testing.T) {
	b := extractFixture(t, map[string]string{
		"compose.yml": "services:\n  ${SVC}:\n    image: ghcr.io/acme/api:${TAG}\n    build: ${CTX}\n",
	})
	if nodeByID(b, "compose.yml::service:${SVC}") == nil {
		t.Error("service name with ${VAR} must be emitted as literal text")
	}
	if nodeByID(b, "image:ghcr.io/acme/api:${TAG}") == nil {
		t.Error("image ref with ${VAR} must be emitted as literal text")
	}
	for _, e := range b.Edges {
		if e.Relation == "depends_on" {
			t.Errorf("a ${VAR} build context must resolve to nothing, got %s -> %s", e.Source, e.Target)
		}
	}
}

// Exact Location contract: a service is anchored on its KEY line only — a span
// would let `card`'s body hydration read the `environment:` block back out of
// the file, so the anchor is deliberately single-line.
func TestComposeExactLocations(t *testing.T) {
	b := extractFixture(t, map[string]string{"compose.yaml": `services:
  api:
    image: api:1
    ports:
      - "80:80"
  db:
    image: postgres:16
  bare:
`})
	for id, want := range map[string]string{
		"compose.yaml::service:api":  "L2",
		"compose.yaml::service:db":   "L6",
		"compose.yaml::service:bare": "L8",
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

// No junk: `services:` in an arbitrary yaml is NOT compose (filename set is the
// only signal), and a real k8s manifest still belongs to the k8s lane.
func TestComposeRecognizedByFilenameOnly(t *testing.T) {
	b := extractFixture(t, map[string]string{
		"config/app.yaml": "services:\n  api:\n    image: not-compose:1\n",
	})
	for _, n := range b.Nodes {
		t.Errorf("non-compose yaml produced a node: %+v", n)
	}

	k := extractFixture(t, map[string]string{"deploy/all.yaml": fixtureK8s})
	if nodeByID(k, "k8s://default/deployment/api") == nil {
		t.Error("k8s lane must still win on real k8s manifests")
	}
	for _, n := range k.Nodes {
		if n.Kind == "service" {
			t.Errorf("k8s manifest misread as compose: %+v", n)
		}
	}

	for name, want := range map[string]string{
		"compose.yaml":          "compose",
		"compose.yml":           "compose",
		"docker-compose.yaml":   "compose",
		"docker-compose.yml":    "compose",
		"Dockerfile":            "dockerfile",
		"Dockerfile.dev":        "dockerfile",
		"api.Dockerfile":        "dockerfile",
		"compose.override.yaml": "yaml",
		"deploy.yaml":           "yaml",
	} {
		if got := manifestKind(name); got != want {
			t.Errorf("manifestKind(%s) = %q, want %q", name, got, want)
		}
	}
}
