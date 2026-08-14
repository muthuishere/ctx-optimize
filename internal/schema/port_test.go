package schema

import (
	"strings"
	"testing"
)

func portNode(mut func(*Node)) Node {
	n := Node{
		ID: "port:network.http:>api.example.com", Label: "api.example.com",
		Kind: "port", FileType: "boundary", Source: "port://network.http/api.example.com",
		Metadata: map[string]string{
			"direction": "consumes", "transport": "network.http",
			"identifier": "api.example.com",
		},
	}
	if mut != nil {
		mut(&n)
	}
	return n
}

func validateOne(n Node) error {
	b := Batch{Producer: "t", Nodes: []Node{n}}
	return b.Validate()
}

func TestPortReservedMetadataFailClosed(t *testing.T) {
	if err := validateOne(portNode(nil)); err != nil {
		t.Fatalf("valid port rejected: %v", err)
	}
	cases := []struct {
		name string
		mut  func(*Node)
		want string
	}{
		{"missing direction", func(n *Node) { delete(n.Metadata, "direction") }, "direction"},
		{"bad direction", func(n *Node) { n.Metadata["direction"] = "sideways" }, "direction"},
		{"uppercase transport", func(n *Node) { n.Metadata["transport"] = "HTTP" }, "transport"},
		{"missing identifier", func(n *Node) { n.Metadata["identifier"] = " " }, "identifier"},
		{"bad scope", func(n *Node) { n.Metadata["scope"] = "sorta" }, "scope"},
		{"bare unknown key", func(n *Node) { n.Metadata["via"] = "apiRequest" }, "namespaced"},
	}
	for _, c := range cases {
		err := validateOne(portNode(c.mut))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: want error mentioning %q, got %v", c.name, c.want, err)
		}
	}
	// Namespaced open metadata passes — that is the whole extension model.
	if err := validateOne(portNode(func(n *Node) {
		n.Metadata["otel.server.address"] = "api.example.com"
		n.Metadata["org.team"] = "payments"
		n.Metadata["scope"] = "external"
		n.Metadata["sensitive"] = "true"
	})); err != nil {
		t.Fatalf("namespaced/reserved metadata rejected: %v", err)
	}
	// Non-port nodes are untouched by the contract.
	if err := validateOne(Node{ID: "f", Label: "f", Kind: "function",
		FileType: "code", Source: "a.go", Metadata: map[string]string{"whatever": "x"}}); err != nil {
		t.Fatalf("non-port node caught by port contract: %v", err)
	}
}
