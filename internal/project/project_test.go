package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAbsentIsEmpty(t *testing.T) {
	c, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if c.Remote != "" || len(c.Adapters) != 0 {
		t.Fatalf("expected empty config, got %+v", c)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := &Config{
		Remote:   "s3://bucket/prefix",
		Adapters: []Adapter{{Name: "kafka", Run: "node hooks/kafka.js"}},
	}
	if err := Save(dir, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if out.Remote != in.Remote || len(out.Adapters) != 1 || out.Adapters[0].Run != in.Adapters[0].Run {
		t.Fatalf("round trip mismatch: %+v", out)
	}
	data, _ := os.ReadFile(filepath.Join(dir, FileName))
	if data[len(data)-1] != '\n' {
		t.Fatal("file not newline-terminated")
	}
}

func TestLoadGarbageFails(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, FileName), []byte("{nope"), 0o644)
	if _, err := Load(dir); err == nil {
		t.Fatal("expected parse error")
	}
}
