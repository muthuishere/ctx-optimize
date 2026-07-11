package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAbsentIsEmpty(t *testing.T) {
	c, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if c.Remote != nil || len(c.Adapters) != 0 || c.Name != "" {
		t.Fatalf("expected empty config, got %+v", c)
	}
}

func TestRemoteStringForm(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, FileName),
		[]byte(`{"remote": "s3://bucket/prefix"}`), 0o644)
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Remote == nil || c.Remote.URL != "s3://bucket/prefix" {
		t.Fatalf("string remote not parsed: %+v", c.Remote)
	}
}

func TestRemoteObjectForm(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, FileName), []byte(`{
	  "remote": {"type": "s3", "url": "s3://bucket/${REPO}",
	             "credentials": {"access_key_id": "${KID}", "region": "auto"}}
	}`), 0o644)
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	r := c.Remote
	if r == nil || r.Type != "s3" || r.Credentials["access_key_id"] != "${KID}" {
		t.Fatalf("object remote not parsed: %+v", r)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := &Config{
		Name: "my-module",
		Remote: &Remote{Type: "s3", URL: "s3://bucket/prefix",
			Credentials: map[string]string{"access_key_id": "${KID}"}},
		Adapters: []Adapter{{Name: "kafka", Run: "node hooks/kafka.js"}},
	}
	if err := Save(dir, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if out.Name != in.Name || out.Remote.URL != in.Remote.URL ||
		out.Remote.Credentials["access_key_id"] != "${KID}" ||
		len(out.Adapters) != 1 || out.Adapters[0].Run != in.Adapters[0].Run {
		t.Fatalf("round trip mismatch: %+v", out)
	}
	data, _ := os.ReadFile(filepath.Join(dir, FileName))
	if data[len(data)-1] != '\n' {
		t.Fatal("file not newline-terminated")
	}
}

// A URL-only remote marshals back to the simple string form.
func TestSaveKeepsSimpleFormSimple(t *testing.T) {
	dir := t.TempDir()
	if err := Save(dir, &Config{Remote: &Remote{URL: "file:///x"}}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, FileName))
	if !strings.Contains(string(data), `"remote": "file:///x"`) {
		t.Fatalf("expected string form: %s", data)
	}
}

func TestResolve(t *testing.T) {
	t.Setenv("CTX_T_URL", "bucket-a")
	t.Setenv("CTX_T_KEY", "resolved-key")
	r := Remote{URL: "s3://${CTX_T_URL}/p",
		Credentials: map[string]string{"access_key_id": "${CTX_T_KEY}", "region": "auto"}}
	got, err := r.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "s3://bucket-a/p" || got.Credentials["access_key_id"] != "resolved-key" ||
		got.Credentials["region"] != "auto" {
		t.Fatalf("resolve wrong: %+v", got)
	}
	// Original untouched — placeholders stay in the config.
	if r.URL != "s3://${CTX_T_URL}/p" {
		t.Fatal("Resolve mutated the source")
	}
}

func TestResolveUnsetVarFailsNamingIt(t *testing.T) {
	r := Remote{URL: "s3://b", Credentials: map[string]string{"secret_access_key": "${CTX_T_DEFINITELY_UNSET}"}}
	_, err := r.Resolve()
	if err == nil || !strings.Contains(err.Error(), "CTX_T_DEFINITELY_UNSET") {
		t.Fatalf("expected error naming the unset var, got %v", err)
	}
}

func TestLoadGarbageFails(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, FileName), []byte("{nope"), 0o644)
	if _, err := Load(dir); err == nil {
		t.Fatal("expected parse error")
	}
}
