package manifests

import (
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

const fixtureK8s = `apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
spec:
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      containers:
        - name: api
          image: ghcr.io/example/api:1.4.2
          envFrom:
            - configMapRef:
                name: api-config
            - secretRef:
                name: api-creds
      volumes:
        - name: settings
          configMap:
            name: api-settings
        - name: tls
          secret:
            secretName: api-tls
---
apiVersion: v1
kind: Service
metadata:
  name: api
spec:
  selector:
    app: api
  ports:
    - port: 80
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: edge
spec:
  rules:
    - host: api.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: api
                port:
                  number: 80
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: api-config
data:
  LOG_LEVEL: info
---
apiVersion: v1
kind: Secret
metadata:
  name: api-creds
data:
  API_KEY: bm90LXJlYWw=
`

func TestK8sTopology(t *testing.T) {
	b := extractFixture(t, map[string]string{"deploy/all.yaml": fixtureK8s})

	svc := nodeByID(b, "k8s://default/service/api")
	if svc == nil {
		t.Fatal("missing service resource node")
	}
	if svc.Kind != "resource" || svc.Metadata["k8s_kind"] != "Service" || svc.Metadata["namespace"] != "default" {
		t.Fatalf("service node shape: %+v", svc)
	}
	if nodeByID(b, "k8s://default/deployment/api") == nil {
		t.Fatal("missing deployment resource node")
	}

	// service --selects--> deployment: computed label match — INFERRED with
	// synthesized_by, the provenance discipline.
	sel := mustEdge(t, b, "k8s://default/service/api", "k8s://default/deployment/api", "selects", schema.Inferred)
	if sel.Metadata["synthesized_by"] != "k8s-selector" {
		t.Fatalf("selects synthesized_by: %v", sel.Metadata)
	}
	// ingress --routes_to--> service: in the file — EXTRACTED.
	mustEdge(t, b, "k8s://default/ingress/edge", "k8s://default/service/api", "routes_to", schema.Extracted)
	// mounts: volume configMap + secret, envFrom configMapRef + secretRef.
	mustEdge(t, b, "k8s://default/deployment/api", "k8s://default/configmap/api-settings", "mounts", schema.Extracted)
	mustEdge(t, b, "k8s://default/deployment/api", "k8s://default/secret/api-tls", "mounts", schema.Extracted)
	mustEdge(t, b, "k8s://default/deployment/api", "k8s://default/configmap/api-config", "mounts", schema.Extracted)
	mustEdge(t, b, "k8s://default/deployment/api", "k8s://default/secret/api-creds", "mounts", schema.Extracted)
	// uses_image.
	if nodeByID(b, "image:ghcr.io/example/api:1.4.2") == nil {
		t.Fatal("missing image node")
	}
	mustEdge(t, b, "k8s://default/deployment/api", "image:ghcr.io/example/api:1.4.2", "uses_image", schema.Extracted)
}

// A Secret RESOURCE gets a node (identity only) and its data is NEVER read
// into the graph.
func TestK8sSecretResourceNodeOnlyDataNeverRead(t *testing.T) {
	b := extractFixture(t, map[string]string{"deploy/all.yaml": fixtureK8s})
	sec := nodeByID(b, "k8s://default/secret/api-creds")
	if sec == nil {
		t.Fatal("Secret resource must still get an identity node")
	}
	for _, n := range b.Nodes {
		for k, v := range n.Metadata {
			if strings.Contains(v, "bm90LXJlYWw") || strings.Contains(k, "API_KEY") {
				t.Fatalf("secret data leaked into node %s metadata", n.ID)
			}
		}
		if strings.Contains(n.Label, "bm90LXJlYWw") {
			t.Fatalf("secret data leaked into label of %s", n.ID)
		}
	}
	for _, e := range b.Edges {
		for k, v := range e.Metadata {
			if strings.Contains(v, "bm90LXJlYWw") || strings.Contains(k, "API_KEY") {
				t.Fatalf("secret data leaked into edge %s→%s", e.Source, e.Target)
			}
		}
	}
}

// Helm templates lie to static parsers — files with {{ }} skip whole.
func TestHelmTemplateSkippedWhole(t *testing.T) {
	b := extractFixture(t, map[string]string{"chart/templates/deploy.yaml": `apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Release.Name }}-api
spec:
  template:
    metadata:
      labels:
        app: api
`})
	for _, n := range b.Nodes {
		if strings.HasPrefix(n.ID, "k8s://") {
			t.Fatalf("helm-templated file must yield no resources, got %s", n.ID)
		}
	}
}

// kind: without apiVersion is NOT a k8s resource (Taskfile-style yaml with a
// kind key stays a plain config doc — the false-positive guard).
func TestKindWithoutAPIVersionIgnored(t *testing.T) {
	b := extractFixture(t, map[string]string{"tool.yaml": `kind: CustomTool
metadata:
  name: not-k8s
settings:
  level: 3
`})
	if len(b.Nodes) != 0 {
		t.Fatalf("yaml with kind but no apiVersion must yield nothing, got %v", b.Nodes)
	}
}

// Namespaced resources land under their namespace; selector matching is
// namespace-scoped.
func TestK8sNamespaceScoping(t *testing.T) {
	b := extractFixture(t, map[string]string{
		"svc.yaml": `apiVersion: v1
kind: Service
metadata:
  name: api
  namespace: prod
spec:
  selector:
    app: api
`,
		"deploy.yaml": `apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: staging
spec:
  template:
    metadata:
      labels:
        app: api
`,
	})
	if nodeByID(b, "k8s://prod/service/api") == nil || nodeByID(b, "k8s://staging/deployment/api") == nil {
		t.Fatal("namespaced ids wrong")
	}
	if e := findEdge(b, "k8s://prod/service/api", "k8s://staging/deployment/api", "selects"); e != nil {
		t.Fatal("selector must not match across namespaces")
	}
}
