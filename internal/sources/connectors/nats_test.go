package connectors

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/sources"
	"time"
)

const natsFakePW = "sup3rS3cretNatsPW"

type fakeNats struct {
	version string
	streams []natsStreamMeta
	err     error
}

func (f *fakeNats) Version() string { return f.version }
func (f *fakeNats) Streams(ctx context.Context) ([]natsStreamMeta, error) {
	return f.streams, f.err
}
func (f *fakeNats) Close() {}

func natsTestConnector(f *fakeNats) *natsConnector {
	return &natsConnector{dial: func(ctx context.Context, url string) (natsServer, error) {
		return f, nil
	}}
}

func TestNatsCaptureHermetic(t *testing.T) {
	fake := &fakeNats{
		version: "2.10.14",
		streams: []natsStreamMeta{ // deliberately unsorted; $SYS mixed in
			{Name: "ORDERS", Subjects: []string{"orders.>", "audit.orders"}, Messages: 1234, Consumers: []string{"worker", "archiver"}},
			{Name: "$SYS_STATE", Subjects: []string{"$SYS.>"}},
			{Name: "KV_config", Subjects: []string{"$KV.config.>"}, Messages: 42, Consumers: nil},
			{Name: "OBJ_files", Subjects: []string{"$O.files.>"}, Messages: 7},
		},
	}
	c := natsTestConnector(fake)
	url := "nats://svc:" + natsFakePW + "@nats.internal:4222"

	start := time.Now()
	b, err := c.Capture(context.Background(), url)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("hermetic capture took %v, want < 50ms", d)
	}

	root := "nats://nats.internal:4222"
	var ids []string
	for _, n := range b.Nodes {
		ids = append(ids, n.ID)
	}
	want := []string{root, root + "/KV_config", root + "/OBJ_files", root + "/ORDERS"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("node ids = %v, want %v ($SYS skipped, sorted)", ids, want)
	}

	if m := b.Nodes[0].Metadata; m["jetstream"] != "enabled" || m["server_version"] != "2.10.14" || m["streams"] != "3" {
		t.Fatalf("server facts wrong: %v", m)
	}
	kinds := map[string]string{}
	for _, n := range b.Nodes[1:] {
		kinds[n.Label] = n.Metadata["kind"]
	}
	if kinds["KV_config"] != "kv" || kinds["OBJ_files"] != "objectstore" || kinds["ORDERS"] != "stream" {
		t.Fatalf("kind facts wrong: %v", kinds)
	}
	for _, n := range b.Nodes {
		if n.Label == "ORDERS" {
			if n.Metadata["subjects"] != "audit.orders, orders.>" {
				t.Fatalf("subjects not sorted: %q", n.Metadata["subjects"])
			}
			if n.Metadata["consumers"] != "archiver, worker" {
				t.Fatalf("consumers not sorted: %q", n.Metadata["consumers"])
			}
			if n.Metadata["messages"] != "1234" {
				t.Fatalf("messages fact wrong: %q", n.Metadata["messages"])
			}
		}
	}

	raw, _ := json.Marshal(b)
	if strings.Contains(string(raw), natsFakePW) || strings.Contains(string(raw), "@nats.internal") {
		t.Fatalf("credentials leaked into batch: %s", raw)
	}

	b2, _ := c.Capture(context.Background(), url)
	raw2, _ := json.Marshal(b2)
	if string(raw) != string(raw2) {
		t.Fatal("captures differ between runs")
	}
	if err := func() error { b.Producer = "test"; return b.Validate() }(); err != nil {
		t.Fatalf("batch invalid: %v", err)
	}
}

func TestNatsJetStreamDisabledReported(t *testing.T) {
	c := natsTestConnector(&fakeNats{version: "2.9.0", err: natsErrNoJetStream})
	b, err := c.Capture(context.Background(), "nats://core.internal:4222")
	if err != nil {
		t.Fatalf("jetstream-disabled must not fail the capture: %v", err)
	}
	if len(b.Nodes) != 1 {
		t.Fatalf("want server node only, got %d nodes", len(b.Nodes))
	}
	if got := b.Nodes[0].Metadata["jetstream"]; !strings.Contains(got, "disabled") {
		t.Fatalf("disablement not reported: %q", got)
	}
}

func TestNatsErrorNamesHostOnly(t *testing.T) {
	c := &natsConnector{dial: func(ctx context.Context, url string) (natsServer, error) {
		return nil, fmt.Errorf("dial tcp: connection refused")
	}}
	_, err := c.Capture(context.Background(), "nats://u:"+natsFakePW+"@nats.internal:4222")
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), natsFakePW) || strings.Contains(err.Error(), "@") {
		t.Fatalf("error leaks credentials: %v", err)
	}
	if !strings.Contains(err.Error(), "nats://nats.internal:4222") {
		t.Fatalf("error should name scheme+host: %v", err)
	}
}

func TestNatsParams(t *testing.T) {
	c := &natsConnector{}
	if !strings.Contains(c.Example(), "$NATS_") {
		t.Fatalf("example should use $VAR placeholders: %q", c.Example())
	}
	credSeen, pctHint := false, false
	for _, p := range c.Params() {
		if p.Cred {
			credSeen = true
			if strings.Contains(p.Desc, "%2F") {
				pctHint = true
			}
		}
	}
	if !credSeen || !pctHint {
		t.Fatal("credential param with percent-encoding hint required")
	}
}

func TestNatsSmokeReal(t *testing.T) {
	url := os.Getenv("CTX_OPTIMIZE_TEST_NATS")
	if url == "" {
		t.Skip("CTX_OPTIMIZE_TEST_NATS not set")
	}
	c, err := sources.Lookup("nats")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	b, err := c.Capture(ctx, url)
	if err != nil {
		t.Fatalf("real capture: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("real capture took %v, want < 2s", d)
	}
	if len(b.Nodes) == 0 {
		t.Fatal("real capture returned zero nodes")
	}
}
