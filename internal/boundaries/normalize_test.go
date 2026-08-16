package boundaries

import "testing"

func TestNormalizePerTransport(t *testing.T) {
	cases := []struct{ transport, raw, want string }{
		{"network.http", "/Users/:id/", "/users/*"},
		{"network.http", "/items/{item_id}", "/items/*"},
		{"network.http", "/x/<slug>", "/x/*"},
		{"network.http", "API.OpenAI.com", "api.openai.com"},
		{"network.http", "/", "/"},
		{"network.ws", "wss://Host/Feed/", "wss://host/feed"},
		// Non-network identifiers are case-sensitive facts — verbatim.
		{"config.env", "OPENAI_API_KEY", "OPENAI_API_KEY"},
		{"process.exec", "Git", "Git"},
		{"storage.browser", "Theme/", "Theme/"},
	}
	for _, c := range cases {
		if got := Normalize(c.transport, c.raw); got != c.want {
			t.Errorf("Normalize(%s, %q) = %q, want %q", c.transport, c.raw, got, c.want)
		}
	}
}

// The point of emit-time normalization: spellings that used to be two nodes
// are ONE port, so provides/consumes join and scope flips to internal.
func TestNormalizationJoinsSpellingsAtEmit(t *testing.T) {
	hermetic(t)
	root := t.TempDir()
	write(t, root, ".ctxoptimize/boundaries.json", `{"version":1,"boundaries":[
	  {"id":"prov-x","transport":"network.http","direction":"provides","scan":"raw",
	   "when":{"ext":[".py"]},
	   "match":[{"re":"@app\\.get\\(\"([^\"]+)\"","identifier":1}]},
	  {"id":"cons-x","transport":"network.http","direction":"consumes","scan":"raw",
	   "when":{"ext":[".ts"]},
	   "match":[{"re":"fetch\\('([^']+)'","identifier":1}]}]}`)
	write(t, root, "api.py", "@app.get(\"/users/{user_id}\")\ndef get_user(): pass\n")
	write(t, root, "ui.ts", "fetch('/users/:id/')\n")
	b, err := Extract(root)
	if err != nil {
		t.Fatal(err)
	}
	cons := find(b, "port:network.http:>/users/*")
	if cons == nil {
		t.Fatalf("consumer not normalized; nodes: %+v", b.Nodes)
	}
	if cons.Metadata["scope"] != "internal" {
		t.Fatalf("normalized spellings must JOIN for scope: %+v", cons)
	}
	if cons.Metadata["raw"] != "/users/:id/" {
		t.Fatalf("raw spelling lost: %+v", cons)
	}
}
