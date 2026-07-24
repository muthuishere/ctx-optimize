// Package schema is the emit contract — THE load-bearing boundary of
// ctx-optimize. Every producer (built-in tree-sitter/markdown, or any
// skill-level adapter: postgres, kafka, logs, pdf, ...) emits this one shape,
// and the universal door (`ctx-optimize add --json`) validates it strictly
// before anything reaches the graph. Adapter proposes, this package disposes.
//
// The shape is deliberately a plain, stable, git-diffable JSON contract:
// graphify proved the pattern (its scip/pg/cargo lanes emit one dict); we make
// it the law rather than a convention.
package schema

import (
	"fmt"
	"strings"
)

// Confidence tiers for edges. EXTRACTED = grammar/type-checker certain;
// INFERRED = heuristic (e.g. unique-name match); AMBIGUOUS = multiple
// candidates survived. Consumers weigh them; we never hide the tier.
const (
	Extracted = "EXTRACTED"
	Inferred  = "INFERRED"
	Ambiguous = "AMBIGUOUS"
)

var validConfidence = map[string]bool{Extracted: true, Inferred: true, Ambiguous: true}

// Node is one thing in the graph: a function, class, file, document section,
// DB table, kafka topic — anything an adapter can name and locate.
type Node struct {
	ID       string            `json:"id"`                 // unique, path-qualified (avoid bare-name collisions)
	Label    string            `json:"label"`              // display name, e.g. "Evaluate()"
	Kind     string            `json:"kind"`               // function|class|file|section|table|topic|...
	FileType string            `json:"file_type"`          // code|document|schema|infra|...
	Source   string            `json:"source"`             // repo-relative path or adapter URI (pg://db/table)
	Location string            `json:"location,omitempty"` // "L42" or "L42-L60"
	Scope    string            `json:"scope,omitempty"`    // dependency nodes: normalized scope class(es), e.g. "runtime" / "dev,runtime" (mirrors metadata["scopes"]; ADR 2026-07-23 F1)
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Edge is one relationship. Source/Target reference node IDs — possibly from
// other batches; cross-batch links are how code↔docs↔schema connect.
type Edge struct {
	Source     string            `json:"source"`
	Target     string            `json:"target"`
	Relation   string            `json:"relation"`   // calls|imports|contains|references|reads|writes|...
	Confidence string            `json:"confidence"` // EXTRACTED|INFERRED|AMBIGUOUS
	Weight     float64           `json:"weight,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// Batch is what the universal door accepts: one producer's contribution.
// Producer is the provenance tag — every node/edge in the store is traceable
// to the adapter that emitted it, so a poisoned adapter's output is
// identifiable and removable as a unit.
type Batch struct {
	Producer string `json:"producer"`
	Nodes    []Node `json:"nodes"`
	Edges    []Edge `json:"edges"`
}

// Validate fails closed: anything malformed is rejected whole — a partially
// accepted batch would make provenance and dedup lie.
func (b *Batch) Validate() error {
	if strings.TrimSpace(b.Producer) == "" {
		return fmt.Errorf("batch: producer is required (provenance tag)")
	}
	seen := make(map[string]bool, len(b.Nodes))
	for i, n := range b.Nodes {
		if strings.TrimSpace(n.ID) == "" {
			return fmt.Errorf("node[%d]: id is required", i)
		}
		if seen[n.ID] {
			return fmt.Errorf("node[%d]: duplicate id %q in batch", i, n.ID)
		}
		seen[n.ID] = true
		if strings.TrimSpace(n.Label) == "" {
			return fmt.Errorf("node %s: label is required", n.ID)
		}
		if strings.TrimSpace(n.Kind) == "" {
			return fmt.Errorf("node %s: kind is required", n.ID)
		}
		if strings.TrimSpace(n.FileType) == "" {
			return fmt.Errorf("node %s: file_type is required", n.ID)
		}
		if strings.TrimSpace(n.Source) == "" {
			return fmt.Errorf("node %s: source is required", n.ID)
		}
	}
	for i, e := range b.Edges {
		if strings.TrimSpace(e.Source) == "" || strings.TrimSpace(e.Target) == "" {
			return fmt.Errorf("edge[%d]: source and target are required", i)
		}
		if strings.TrimSpace(e.Relation) == "" {
			return fmt.Errorf("edge[%d]: relation is required", i)
		}
		if !validConfidence[e.Confidence] {
			return fmt.Errorf("edge[%d]: confidence %q not in {EXTRACTED,INFERRED,AMBIGUOUS}", i, e.Confidence)
		}
	}
	return nil
}

// --- SPIKE (ADR 2026-07-24-scale-robust-gather) ---------------------------
// nodeReason returns "" if the node is valid, else the reason it's invalid.
// Single source of truth reused by Validate-style checks and PartitionValidate.
func nodeReason(n Node) string {
	switch {
	case strings.TrimSpace(n.ID) == "":
		return "id is required"
	case strings.TrimSpace(n.Label) == "":
		return "label is required"
	case strings.TrimSpace(n.Kind) == "":
		return "kind is required"
	case strings.TrimSpace(n.FileType) == "":
		return "file_type is required"
	case strings.TrimSpace(n.Source) == "":
		return "source is required"
	}
	return ""
}

func edgeReason(e Edge) string {
	switch {
	case strings.TrimSpace(e.Source) == "" || strings.TrimSpace(e.Target) == "":
		return "source and target are required"
	case strings.TrimSpace(e.Relation) == "":
		return "relation is required"
	case !validConfidence[e.Confidence]:
		return "confidence not in {EXTRACTED,INFERRED,AMBIGUOUS}"
	}
	return ""
}

// Quarantine is one dropped node/edge with the reason it failed validation.
type Quarantine struct {
	ID     string `json:"id"`     // node id, or "src->tgt" for an edge
	Reason string `json:"reason"`
}

// PartitionValidate splits a batch into an accepted (fully-valid, deduped,
// no-dangling) batch and the quarantined nodes/edges. Deterministic: input
// order is preserved; first occurrence of a duplicate id wins. existingIDs are
// node ids already in the store (valid edge endpoints). Producer is still a
// hard requirement — a batch with no provenance tag is a caller bug, not data.
func (b *Batch) PartitionValidate(existingIDs map[string]bool) (accepted *Batch, quarantined []Quarantine, err error) {
	if strings.TrimSpace(b.Producer) == "" {
		return nil, nil, fmt.Errorf("batch: producer is required (provenance tag)")
	}
	accepted = &Batch{Producer: b.Producer}
	seen := make(map[string]bool, len(b.Nodes))
	// dropped = node ids we quarantined THIS batch (invalid). Cross-batch/forward
	// edge targets that are simply absent are NOT dropped — the original Validate
	// never required an edge's endpoints to be present (code↔docs↔schema links
	// span batches by design). We only cascade edges whose endpoint we removed.
	dropped := make(map[string]bool)
	for _, n := range b.Nodes {
		if r := nodeReason(n); r != "" {
			quarantined = append(quarantined, Quarantine{ID: n.ID, Reason: r})
			dropped[n.ID] = true
			continue
		}
		if seen[n.ID] {
			quarantined = append(quarantined, Quarantine{ID: n.ID, Reason: "duplicate id in batch"})
			continue // the first copy is accepted, so the id is NOT dropped
		}
		seen[n.ID] = true
		accepted.Nodes = append(accepted.Nodes, n)
	}
	for _, e := range b.Edges {
		if r := edgeReason(e); r != "" {
			quarantined = append(quarantined, Quarantine{ID: e.Source + "->" + e.Target, Reason: r})
			continue
		}
		if dropped[e.Source] || dropped[e.Target] {
			quarantined = append(quarantined, Quarantine{ID: e.Source + "->" + e.Target, Reason: "dangling: endpoint quarantined this batch"})
			continue
		}
		accepted.Edges = append(accepted.Edges, e)
	}
	return accepted, quarantined, nil
}
