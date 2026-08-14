package analyze

import (
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

func port(id, transport, ident, direction string, extra map[string]string) schema.Node {
	m := map[string]string{"direction": direction, "transport": transport, "identifier": ident}
	for k, v := range extra {
		m[k] = v
	}
	return schema.Node{ID: id, Label: ident, Kind: "port", FileType: "boundary",
		Source: "port://" + transport + "/" + ident, Metadata: m}
}

func bedge(src, portID, relation, conf string) schema.Edge {
	return schema.Edge{Source: src, Target: portID, Relation: relation, Confidence: conf,
		Metadata: map[string]string{"rule": "r-" + relation, "site": src + ":L1"}}
}

func kinds(fs []DriftFinding) map[string]int {
	out := map[string]int{}
	for _, f := range fs {
		out[f.Kind]++
	}
	return out
}

func TestDriftDeadContractRequiresExtractedProvider(t *testing.T) {
	nodes := []schema.Node{
		port("port:network.http:>/dead", "network.http", "/dead", "provides", nil),
		port("port:network.http:>/soft", "network.http", "/soft", "provides", nil),
	}
	edges := []schema.Edge{
		bedge("routes.go", "port:network.http:>/dead", "provides", schema.Extracted),
		bedge("guess.go", "port:network.http:>/soft", "provides", schema.Inferred),
	}
	r := Drift(nodes, edges)
	if k := kinds(r.Findings); k["dead-contract"] != 1 || len(r.Findings) != 1 {
		t.Fatalf("want exactly one dead-contract finding, got %+v", r.Findings)
	}
	if r.Findings[0].Identifier != "/dead" {
		t.Fatalf("wrong contract accused: %+v", r.Findings[0])
	}
	// The INFERRED-only orphan is listed, never accused.
	if k := kinds(r.Observations); k["lower-tier-orphan"] != 1 {
		t.Fatalf("INFERRED orphan must be an observation, got %+v", r.Observations)
	}
}

func TestDriftInferredConsumerBlocksAccusation(t *testing.T) {
	nodes := []schema.Node{port("port:network.http:>/maybe", "network.http", "/maybe", "provides", nil)}
	edges := []schema.Edge{
		bedge("routes.go", "port:network.http:>/maybe", "provides", schema.Extracted),
		bedge("client.ts", "port:network.http:>/maybe", "consumes", schema.Ambiguous),
	}
	r := Drift(nodes, edges)
	if len(r.Findings) != 0 {
		t.Fatalf("AMBIGUOUS consumer must block the dead-contract accusation: %+v", r.Findings)
	}
	if k := kinds(r.Observations); k["possibly-consumed"] != 1 {
		t.Fatalf("want possibly-consumed observation, got %+v", r.Observations)
	}
}

func TestDriftEnvUndeclaredOnlyWhenDeclarationsTracked(t *testing.T) {
	read := port("port:config.env:>ORPHAN_KEY", "config.env", "ORPHAN_KEY", "consumes", nil)
	readEdge := bedge("main.go", "port:config.env:>ORPHAN_KEY", "consumes", schema.Extracted)

	// No declaration-tier env ports anywhere: absence of instrumentation,
	// never a finding.
	r := Drift([]schema.Node{read}, []schema.Edge{readEdge})
	if len(r.Findings) != 0 {
		t.Fatalf("env-undeclared must not fire when declarations are untracked: %+v", r.Findings)
	}

	// A declared env var elsewhere proves declarations ARE tracked — now the
	// undeclared read is accusable.
	decl := port("port:config.env:>DECLARED_KEY", "config.env", "DECLARED_KEY", "provides", nil)
	declEdge := bedge("compose.yml", "port:config.env:>DECLARED_KEY", "provides", schema.Extracted)
	declRead := bedge("main.go", "port:config.env:>DECLARED_KEY", "consumes", schema.Extracted)
	r = Drift([]schema.Node{read, decl}, []schema.Edge{readEdge, declEdge, declRead})
	if k := kinds(r.Findings); k["env-undeclared"] != 1 || len(r.Findings) != 1 {
		t.Fatalf("want exactly env-undeclared for ORPHAN_KEY, got %+v", r.Findings)
	}
	if r.Findings[0].Identifier != "ORPHAN_KEY" {
		t.Fatalf("accused the wrong key: %+v", r.Findings[0])
	}
}

func TestDriftNormalizationJoinsSpellings(t *testing.T) {
	// Server provides /users/{id}; client consumes /users/:id — one contract,
	// alive, plus a would-join observation until emit-time normalization lands.
	nodes := []schema.Node{
		port("api/port:network.http:>/users/{id}", "network.http", "/users/{id}", "provides", nil),
		port("ui/port:network.http:>/users/:id", "network.http", "/users/:id", "consumes", nil),
	}
	edges := []schema.Edge{
		bedge("routes.go", "api/port:network.http:>/users/{id}", "provides", schema.Extracted),
		bedge("client.ts", "ui/port:network.http:>/users/:id", "consumes", schema.Extracted),
	}
	r := Drift(nodes, edges)
	if len(r.Findings) != 0 {
		t.Fatalf("normalized spellings join — no dead contract: %+v", r.Findings)
	}
	k := kinds(r.Observations)
	if k["would-join"] != 1 {
		t.Fatalf("want a would-join observation, got %+v", r.Observations)
	}
	if len(r.Observations[0].Identifiers) != 2 {
		t.Fatalf("would-join must list both spellings: %+v", r.Observations[0])
	}
	if r.Groups != 1 {
		t.Fatalf("both spellings must land in one contract group, got %d", r.Groups)
	}
}

func TestDriftFederatedDuplicatePortsGroupByMetadata(t *testing.T) {
	// The same logical port appears once per module store with prefixed ids;
	// grouping by metadata must see ONE contract, consumed, so nothing fires.
	nodes := []schema.Node{
		port("services/api/port:network.http:>/health", "network.http", "/health", "provides", nil),
		port("web/ui/port:network.http:>/health", "network.http", "/health", "consumes", nil),
	}
	edges := []schema.Edge{
		bedge("routes.go", "services/api/port:network.http:>/health", "provides", schema.Extracted),
		bedge("app.tsx", "web/ui/port:network.http:>/health", "consumes", schema.Extracted),
	}
	r := Drift(nodes, edges)
	if len(r.Findings) != 0 || len(r.Observations) != 0 || r.Groups != 1 {
		t.Fatalf("federated duplicates must merge into one live contract: %+v", r)
	}
}

func TestDriftDeterministicOrder(t *testing.T) {
	nodes := []schema.Node{
		port("port:network.http:>/b", "network.http", "/b", "provides", nil),
		port("port:network.http:>/a", "network.http", "/a", "provides", nil),
	}
	edges := []schema.Edge{
		bedge("r.go", "port:network.http:>/b", "provides", schema.Extracted),
		bedge("r.go", "port:network.http:>/a", "provides", schema.Extracted),
	}
	r1, r2 := Drift(nodes, edges), Drift(nodes, edges)
	if len(r1.Findings) != 2 || r1.Findings[0].Identifier != "/a" {
		t.Fatalf("findings must sort by identifier: %+v", r1.Findings)
	}
	for i := range r1.Findings {
		if r1.Findings[i].Identifier != r2.Findings[i].Identifier {
			t.Fatal("order varies between runs")
		}
	}
}
