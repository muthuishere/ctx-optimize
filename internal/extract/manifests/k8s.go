// k8s.go — Kubernetes manifests as topology, via the shared yaml indent
// walker (internal/extract/yamlwalk — same engine as the route lane). A yaml
// document is a k8s resource only when it carries ALL of kind:, apiVersion:,
// and metadata.name — a random yaml with kind: but no apiVersion stays a
// plain config doc (false-positive guard). Node id k8s://<ns|default>/
// <kind-lower>/<name>.
//
// Edges (provenance discipline):
//
//	service --selects--> workload        label match, module-wide — INFERRED,
//	                                     synthesized_by k8s-selector
//	ingress --routes_to--> service       backend service name — EXTRACTED
//	workload --mounts--> configmap|secret volume/envFrom refs — EXTRACTED
//	workload --uses_image--> image:<ref> container images — EXTRACTED
//
// Secret resources: node only — identity (kind/name/namespace), data NEVER
// read. Helm templates ({{ }} anywhere) are skipped whole: templated yaml
// lies to static parsers (recorded v1 limitation). Multi-doc (---) files
// are processed per document.
package manifests

import (
	"fmt"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/extract/yamlwalk"
	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// k8sWorkloads are the kinds whose pod templates a Service selector can match.
var k8sWorkloads = map[string]bool{
	"deployment": true, "statefulset": true, "daemonset": true,
	"replicaset": true, "pod": true,
}

type k8sResource struct {
	rel       string
	kind      string // verbatim from the file ("Deployment")
	ns        string
	name      string
	line      int
	selector  map[string]string // Service spec.selector
	podLabels map[string]string // workload template labels (Pod: metadata.labels)
	images    []string
	mounts    []string // "configmap/<name>" | "secret/<name>"
	backends  []string // Ingress backend service names
}

func (r *k8sResource) id() string {
	return "k8s://" + r.ns + "/" + strings.ToLower(r.kind) + "/" + r.name
}

// k8sState accumulates resources across every yaml file in the module so
// selector matching sees the whole picture, then emits once.
type k8sState struct {
	resources []*k8sResource
	seen      map[string]bool // resource id → first wins
}

func newK8sState() *k8sState { return &k8sState{seen: map[string]bool{}} }

// extractK8s scans one yaml file for k8s resource documents.
func extractK8s(st *k8sState, rel, content string) {
	if strings.Contains(content, "{{") && strings.Contains(content, "}}") {
		return // Helm template — skipped whole (v1 limitation, documented)
	}
	all := strings.Split(content, "\n")
	start := 0
	flush := func(end int) {
		if end <= start {
			return
		}
		if r := parseK8sDoc(rel, yamlwalk.Parse(all[start:end], start)); r != nil && !st.seen[r.id()] {
			st.seen[r.id()] = true
			st.resources = append(st.resources, r)
		}
	}
	for i, line := range all {
		if strings.TrimSpace(line) == "---" {
			flush(i)
			start = i + 1
		}
	}
	flush(len(all))
}

// parseK8sDoc returns the resource a document declares, or nil when the
// document is not confidently a k8s resource (kind + apiVersion +
// metadata.name all required).
func parseK8sDoc(rel string, ls []yamlwalk.Line) *k8sResource {
	r := &k8sResource{rel: rel, ns: "default"}
	hasAPIVersion := false
	metaIdx := -1
	for i, l := range ls {
		if l.Indent != 0 || l.List {
			continue
		}
		switch l.Key {
		case "kind":
			if r.kind == "" && l.Val != "" {
				r.kind, r.line = l.Val, l.Num
			}
		case "apiVersion":
			if l.Val != "" {
				hasAPIVersion = true
			}
		case "metadata":
			metaIdx = i
		}
	}
	if r.kind == "" || !hasAPIVersion || metaIdx < 0 {
		return nil
	}
	metaEnd := yamlwalk.Span(ls, metaIdx)
	if metaIdx+1 >= metaEnd {
		return nil // empty metadata block — no name, not a resource
	}
	for j := metaIdx + 1; j < metaEnd; j++ {
		if ls[j].Indent != ls[metaIdx+1].Indent {
			continue // direct children of metadata only
		}
		switch ls[j].Key {
		case "name":
			if r.name == "" {
				r.name = ls[j].Val
			}
		case "namespace":
			if ls[j].Val != "" {
				r.ns = ls[j].Val
			}
		case "labels":
			if strings.ToLower(r.kind) == "pod" {
				r.podLabels = childMap(ls, j)
			}
		}
	}
	if r.name == "" {
		return nil
	}

	lower := strings.ToLower(r.kind)
	if lower == "secret" {
		return r // node only — data NEVER read
	}
	switch {
	case lower == "service":
		r.selector = findChildMap(ls, "spec", "selector")
	case lower == "ingress":
		r.backends = ingressBackends(ls)
	case k8sWorkloads[lower]:
		if lower != "pod" {
			r.podLabels = templateLabels(ls)
		}
		scanWorkload(ls, r)
	}
	return r
}

// childMap reads the direct scalar children of ls[i] as a string map.
func childMap(ls []yamlwalk.Line, i int) map[string]string {
	end := yamlwalk.Span(ls, i)
	if i+1 >= end {
		return nil
	}
	m := map[string]string{}
	ind := ls[i+1].Indent
	for j := i + 1; j < end; j++ {
		if ls[j].Indent == ind && ls[j].Key != "" && ls[j].Val != "" {
			m[ls[j].Key] = ls[j].Val
		}
	}
	return m
}

// findChildMap resolves a two-level path (top-level key, descendant key) and
// returns the child map at the end — good enough for spec.selector, where
// matchLabels-style indirection is handled by taking the deepest map.
func findChildMap(ls []yamlwalk.Line, top, child string) map[string]string {
	for i := 0; i < len(ls); i++ {
		if ls[i].Indent != 0 || ls[i].Key != top {
			continue
		}
		end := yamlwalk.Span(ls, i)
		for j := i + 1; j < end; j++ {
			if ls[j].Key == child {
				m := childMap(ls, j)
				// selector: { matchLabels: {...} } — descend once.
				if len(m) == 0 {
					cend := yamlwalk.Span(ls, j)
					for k := j + 1; k < cend; k++ {
						if ls[k].Key == "matchLabels" {
							return childMap(ls, k)
						}
					}
				}
				if len(m) == 0 {
					return nil
				}
				return m
			}
		}
	}
	return nil
}

// templateLabels reads spec.template.metadata.labels of a workload.
func templateLabels(ls []yamlwalk.Line) map[string]string {
	for i := 0; i < len(ls); i++ {
		if ls[i].Indent != 0 || ls[i].Key != "spec" {
			continue
		}
		end := yamlwalk.Span(ls, i)
		for j := i + 1; j < end; j++ {
			if ls[j].Key != "template" {
				continue
			}
			tend := yamlwalk.Span(ls, j)
			for k := j + 1; k < tend; k++ {
				if ls[k].Key != "metadata" {
					continue
				}
				mend := yamlwalk.Span(ls, k)
				for m := k + 1; m < mend; m++ {
					if ls[m].Key == "labels" {
						return childMap(ls, m)
					}
				}
			}
		}
	}
	return nil
}

// scanWorkload collects container images and configmap/secret mounts from a
// workload document. Best-effort by key shape: inside a workload doc,
// `image:` keys are container images; volume `configMap.name` /
// `secret.secretName` and env `configMapRef|configMapKeyRef|secretRef|
// secretKeyRef` name children are mounts. Names only — never values.
func scanWorkload(ls []yamlwalk.Line, r *k8sResource) {
	for i := 0; i < len(ls); i++ {
		switch ls[i].Key {
		case "image":
			if ls[i].Val != "" {
				r.images = append(r.images, ls[i].Val)
			}
		case "configMap", "configMapRef", "configMapKeyRef":
			if n := refName(ls, i, "name"); n != "" {
				r.mounts = append(r.mounts, "configmap/"+n)
			}
		case "secretRef", "secretKeyRef":
			if n := refName(ls, i, "name"); n != "" {
				r.mounts = append(r.mounts, "secret/"+n)
			}
		case "secret":
			if n := refName(ls, i, "secretName"); n != "" {
				r.mounts = append(r.mounts, "secret/"+n)
			}
		}
	}
}

// refName finds the named scalar child of block ls[i].
func refName(ls []yamlwalk.Line, i int, key string) string {
	end := yamlwalk.Span(ls, i)
	for j := i + 1; j < end; j++ {
		if ls[j].Key == key && ls[j].Val != "" {
			return ls[j].Val
		}
	}
	return ""
}

// ingressBackends collects backend service names: v1 `service: {name: x}`
// blocks and v1beta1 `serviceName: x` scalars under spec.
func ingressBackends(ls []yamlwalk.Line) []string {
	var out []string
	seen := map[string]bool{}
	add := func(n string) {
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	for i := 0; i < len(ls); i++ {
		if ls[i].Indent != 0 || ls[i].Key != "spec" {
			continue
		}
		end := yamlwalk.Span(ls, i)
		for j := i + 1; j < end; j++ {
			switch ls[j].Key {
			case "service":
				add(refName(ls, j, "name"))
			case "serviceName":
				add(ls[j].Val)
			}
		}
	}
	return out
}

// emit turns the accumulated resources into nodes and edges. Called once per
// extraction after every file is scanned (selector matching is module-wide).
func (st *k8sState) emit(c *collector) {
	sort.Slice(st.resources, func(i, j int) bool { return st.resources[i].id() < st.resources[j].id() })
	for _, r := range st.resources {
		c.node(schema.Node{
			ID: r.id(), Label: strings.ToLower(r.kind) + "/" + r.name,
			Kind: "resource", FileType: "manifest", Source: r.rel,
			Location: fmt.Sprintf("L%d", r.line),
			Metadata: map[string]string{"k8s_kind": r.kind, "namespace": r.ns},
		})
		c.edge(schema.Edge{Source: r.rel, Target: r.id(), Relation: "contains", Confidence: schema.Extracted})
	}
	for _, r := range st.resources {
		lower := strings.ToLower(r.kind)
		switch {
		case lower == "service" && len(r.selector) > 0:
			for _, w := range st.resources {
				if w.ns != r.ns || !k8sWorkloads[strings.ToLower(w.kind)] || !labelsMatch(r.selector, w.podLabels) {
					continue
				}
				c.edge(schema.Edge{
					Source: r.id(), Target: w.id(), Relation: "selects",
					Confidence: schema.Inferred,
					Metadata:   map[string]string{"synthesized_by": "k8s-selector"},
				})
			}
		case lower == "ingress":
			for _, svc := range r.backends {
				c.edge(schema.Edge{
					Source: r.id(), Target: "k8s://" + r.ns + "/service/" + svc,
					Relation: "routes_to", Confidence: schema.Extracted,
				})
			}
		}
		for _, m := range r.mounts {
			c.edge(schema.Edge{
				Source: r.id(), Target: "k8s://" + r.ns + "/" + m,
				Relation: "mounts", Confidence: schema.Extracted,
			})
		}
		for _, img := range r.images {
			imgID := "image:" + img
			c.node(schema.Node{
				ID: imgID, Label: img, Kind: "image", FileType: "manifest",
				Source: imgID,
			})
			c.edge(schema.Edge{
				Source: r.id(), Target: imgID, Relation: "uses_image",
				Confidence: schema.Extracted,
			})
		}
	}
}

// labelsMatch: every selector pair must be present in the pod labels.
func labelsMatch(selector, labels map[string]string) bool {
	if len(labels) == 0 {
		return false
	}
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}
