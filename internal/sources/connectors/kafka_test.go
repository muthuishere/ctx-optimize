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

const kafkaFakePW = "sup3rS3cretKafkaPW"

type fakeKafkaAdmin struct {
	meta kafkaClusterMeta
	err  error
}

func (f *fakeKafkaAdmin) Meta(ctx context.Context) (kafkaClusterMeta, error) { return f.meta, f.err }
func (f *fakeKafkaAdmin) Close()                                             {}

func kafkaTestConnector(f *fakeKafkaAdmin, gotTarget *kafkaTarget) *kafkaConnector {
	return &kafkaConnector{dial: func(ctx context.Context, t kafkaTarget) (kafkaAdmin, error) {
		if gotTarget != nil {
			*gotTarget = t
		}
		return f, nil
	}}
}

func TestKafkaCaptureHermetic(t *testing.T) {
	fake := &fakeKafkaAdmin{meta: kafkaClusterMeta{
		Brokers: 3,
		Topics: []kafkaTopicMeta{ // deliberately unsorted, internals mixed in
			{Name: "orders", Partitions: 6, Replication: 3},
			{Name: "__consumer_offsets", Partitions: 50, Replication: 3, Internal: true},
			{Name: "billing", Partitions: 12, Replication: 2},
			{Name: "_schemas", Partitions: 1, Replication: 3},
			{Name: "_private_thing", Partitions: 1, Replication: 1},
			{Name: "flagged-internal", Partitions: 1, Replication: 1, Internal: true},
		},
		Groups: []string{"payments-cg", "audit-cg"},
	}}
	var target kafkaTarget
	c := kafkaTestConnector(fake, &target)
	url := "kafka://admin:" + kafkaFakePW + "@b1.internal:9092,b2.internal:9092?sasl=plain&tls=true"

	start := time.Now()
	b, err := c.Capture(context.Background(), url)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("hermetic capture took %v, want < 50ms", d)
	}

	// Brokers passed to the driver verbatim (commas preserved, never re-derived).
	if want := []string{"b1.internal:9092", "b2.internal:9092"}; !reflect.DeepEqual(target.Brokers, want) {
		t.Fatalf("seeds = %v, want %v", target.Brokers, want)
	}
	if target.User != "admin" || target.Pass != kafkaFakePW || target.SASL != "plain" || !target.TLS {
		t.Fatalf("target auth wrong: %+v", kafkaTarget{User: target.User, SASL: target.SASL, TLS: target.TLS})
	}

	root := "kafka://b1.internal:9092,b2.internal:9092"
	var ids []string
	for _, n := range b.Nodes {
		ids = append(ids, n.ID)
	}
	want := []string{
		root,
		root + "/billing",
		root + "/orders",
		root + "/groups/audit-cg",
		root + "/groups/payments-cg",
	}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("node ids = %v, want %v", ids, want)
	}

	// Topic facts: partitions/replication as metadata, never nodes.
	for _, n := range b.Nodes {
		if n.ID == root+"/orders" {
			if n.Metadata["partitions"] != "6" || n.Metadata["replication"] != "3" {
				t.Fatalf("orders facts wrong: %v", n.Metadata)
			}
		}
		if n.Kind == "partition" {
			t.Fatal("partitions must be facts, never nodes")
		}
	}
	if m := b.Nodes[0].Metadata; m["brokers"] != "3" || m["topics"] != "2" || m["consumer_groups"] != "2" {
		t.Fatalf("cluster facts wrong: %v", m)
	}

	raw, _ := json.Marshal(b)
	if strings.Contains(string(raw), kafkaFakePW) || strings.Contains(string(raw), "@") {
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

func TestKafkaParseURL(t *testing.T) {
	tgt, err := kafkaParseURL("kafka://b1:9092,b2:9092,b3:9092")
	if err != nil {
		t.Fatal(err)
	}
	if len(tgt.Brokers) != 3 || tgt.User != "" || tgt.SASL != "" || tgt.TLS {
		t.Fatalf("plain parse wrong: %+v", tgt)
	}
	tgt, err = kafkaParseURL("kafka://u:p@b1:9092?tls_ca=/etc/ca.pem")
	if err != nil {
		t.Fatal(err)
	}
	if tgt.TLSCA != "/etc/ca.pem" || !tgt.TLS {
		t.Fatalf("tls_ca should imply TLS: %+v", tgt)
	}
	if _, err := kafkaParseURL("kafka://"); err == nil {
		t.Fatal("empty brokers must error")
	}
}

func TestKafkaErrorNamesHostOnly(t *testing.T) {
	c := &kafkaConnector{dial: func(ctx context.Context, tgt kafkaTarget) (kafkaAdmin, error) {
		return nil, fmt.Errorf("dial tcp: connection refused")
	}}
	_, err := c.Capture(context.Background(), "kafka://u:"+kafkaFakePW+"@broker.internal:9092?sasl=plain")
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), kafkaFakePW) || strings.Contains(err.Error(), "@") {
		t.Fatalf("error leaks credentials: %v", err)
	}
	if !strings.Contains(err.Error(), "kafka://broker.internal:9092") {
		t.Fatalf("error should name scheme+host: %v", err)
	}
}

func TestKafkaParams(t *testing.T) {
	c := &kafkaConnector{}
	if !strings.Contains(c.Example(), "$KAFKA_") {
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

func TestKafkaSmokeReal(t *testing.T) {
	url := os.Getenv("CTX_OPTIMIZE_TEST_KAFKA")
	if url == "" {
		t.Skip("CTX_OPTIMIZE_TEST_KAFKA not set")
	}
	c, err := sources.Lookup("kafka")
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
