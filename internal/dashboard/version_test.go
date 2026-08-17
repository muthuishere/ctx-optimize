package dashboard

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/version"
)

// The header can only be trusted if the binary answers for itself. A stale
// COPY of ctx-optimize on PATH served for hours while every screen looked
// right; the dashboard had no way to say which build it was.
func TestVersionEndpointReportsThisBinary(t *testing.T) {
	srv, _ := sceneServer(t)
	res, err := http.Get(srv.URL + "/api/version")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	var got map[string]string
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["version"] != version.Version {
		t.Errorf("version = %q, want this binary's %q", got["version"], version.Version)
	}
	if got["commit"] != version.Commit {
		t.Errorf("commit = %q, want %q", got["commit"], version.Commit)
	}
	// It must never be empty: a blank badge reads as "no version" rather than
	// "dev build", and those are different facts.
	if got["version"] == "" {
		t.Error("empty version — the header would show nothing and mean nothing")
	}
}

// It is a READ. No token, no loopback restriction beyond the listener's, and
// it must not be able to change anything.
func TestVersionEndpointIsAPlainRead(t *testing.T) {
	srv, _ := sceneServer(t)
	req, _ := http.NewRequest("DELETE", srv.URL+"/api/version", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	// Whatever it answers, it must not carry the CSRF token or a store path.
	var got map[string]string
	json.NewDecoder(res.Body).Decode(&got)
	for k := range got {
		if k != "version" && k != "commit" && k != "built" {
			t.Errorf("version endpoint leaks %q", k)
		}
	}
}
