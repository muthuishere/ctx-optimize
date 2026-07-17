package connectors

// The redis connector is tested against a REAL in-test mini-RESP TCP server,
// so the actual go-redis wire path (HELLO fallback, AUTH, SCAN paging, TYPE,
// DBSIZE, INFO) runs hermetically.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/muthuishere/ctx-optimize/internal/sources"
	"time"
)

const redisFakePW = "sup3rS3cretRedisPW"

// startFakeRedis serves a minimal RESP2 dialect on loopback: HELLO is
// rejected (forcing the RESP2 fallback path), SCAN pages through keys with
// index cursors, TYPE/DBSIZE/INFO answer from the fixture, everything else
// gets +OK (AUTH, SELECT, CLIENT, ...).
func startFakeRedis(t *testing.T, keys []string, types map[string]string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go serveFakeRedisConn(conn, keys, types)
		}
	}()
	return ln.Addr().String()
}

func serveFakeRedisConn(conn net.Conn, keys []string, types map[string]string) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)
	bulk := func(s string) { fmt.Fprintf(w, "$%d\r\n%s\r\n", len(s), s) }
	for {
		args, err := readRESPCommand(r)
		if err != nil {
			return
		}
		switch strings.ToUpper(args[0]) {
		case "HELLO":
			fmt.Fprintf(w, "-ERR unknown command 'HELLO'\r\n")
		case "PING":
			fmt.Fprintf(w, "+PONG\r\n")
		case "SCAN":
			cursor, _ := strconv.Atoi(args[1])
			count := 10
			for i := 2; i < len(args)-1; i++ {
				if strings.EqualFold(args[i], "COUNT") {
					count, _ = strconv.Atoi(args[i+1])
				}
			}
			end := cursor + count
			if end > len(keys) {
				end = len(keys)
			}
			next := end
			if next >= len(keys) {
				next = 0
			}
			fmt.Fprintf(w, "*2\r\n")
			bulk(strconv.Itoa(next))
			fmt.Fprintf(w, "*%d\r\n", end-cursor)
			for _, k := range keys[cursor:end] {
				bulk(k)
			}
		case "TYPE":
			typ := types[args[1]]
			if typ == "" {
				typ = "string"
			}
			fmt.Fprintf(w, "+%s\r\n", typ)
		case "DBSIZE":
			fmt.Fprintf(w, ":%d\r\n", len(keys))
		case "INFO":
			bulk("# Server\r\nredis_version:7.4.0\r\n")
		default: // AUTH, SELECT, CLIENT SETINFO, ...
			fmt.Fprintf(w, "+OK\r\n")
		}
		if err := w.Flush(); err != nil {
			return
		}
	}
}

// readRESPCommand parses one RESP2 array of bulk strings.
func readRESPCommand(r *bufio.Reader) ([]string, error) {
	line, err := respLine(r)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(line, "*") {
		return nil, fmt.Errorf("expected array, got %q", line)
	}
	n, err := strconv.Atoi(line[1:])
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		hdr, err := respLine(r)
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(hdr, "$") {
			return nil, fmt.Errorf("expected bulk string, got %q", hdr)
		}
		size, err := strconv.Atoi(hdr[1:])
		if err != nil {
			return nil, err
		}
		buf := make([]byte, size+2) // payload + CRLF
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:size]))
	}
	return args, nil
}

func respLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func TestRedisCaptureHermetic(t *testing.T) {
	addr := startFakeRedis(t,
		[]string{"cache:a", "billing:1", "solo", "billing:2"},
		map[string]string{"billing:1": "hash", "cache:a": "string", "solo": "list"},
	)
	c := &redisConnector{}
	url := "redis://user:" + redisFakePW + "@" + addr

	start := time.Now()
	b, err := c.Capture(context.Background(), url)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("hermetic capture took %v, want < 50ms", d)
	}

	root := "redis://" + addr + "/db0"
	if b.Nodes[0].ID != root {
		t.Fatalf("db node id = %q, want %q", b.Nodes[0].ID, root)
	}
	dbMeta := b.Nodes[0].Metadata
	if dbMeta["keys"] != "4" || dbMeta["sampled_keys"] != "4" {
		t.Fatalf("db facts wrong: %v", dbMeta)
	}
	if dbMeta["redis_version"] != "7.4.0" {
		t.Fatalf("version fact wrong: %v", dbMeta)
	}
	if _, hasCap := dbMeta["sample_capped"]; hasCap {
		t.Fatal("cap fact reported without a cap hit")
	}

	// Prefixes: sorted, with approx counts and one TYPE per prefix.
	var prefixIDs []string
	byID := map[string]map[string]string{}
	for _, n := range b.Nodes[1:] {
		prefixIDs = append(prefixIDs, n.ID)
		byID[n.ID] = n.Metadata
	}
	want := []string{root + "/billing", root + "/cache", root + "/solo"}
	if strings.Join(prefixIDs, "|") != strings.Join(want, "|") {
		t.Fatalf("prefix ids = %v, want %v", prefixIDs, want)
	}
	if m := byID[root+"/billing"]; m["approx_count"] != "2" || m["example_type"] != "hash" {
		t.Fatalf("billing prefix facts wrong: %v", m)
	}
	if m := byID[root+"/solo"]; m["approx_count"] != "1" || m["example_type"] != "list" {
		t.Fatalf("solo prefix facts wrong: %v", m)
	}

	// Sanitized ids and output: password appears nowhere.
	raw, _ := json.Marshal(b)
	if strings.Contains(string(raw), redisFakePW) || strings.Contains(string(raw), "@") {
		t.Fatalf("credentials leaked into batch: %s", raw)
	}

	// Determinism.
	b2, err := c.Capture(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	raw2, _ := json.Marshal(b2)
	if string(raw) != string(raw2) {
		t.Fatal("captures differ between runs")
	}
	if err := func() error { b.Producer = "test"; return b.Validate() }(); err != nil {
		t.Fatalf("batch invalid: %v", err)
	}
}

func TestRedisScanCapReported(t *testing.T) {
	keys := make([]string, redisScanCap+1000)
	for i := range keys {
		keys[i] = fmt.Sprintf("bulk:k%05d", i)
	}
	addr := startFakeRedis(t, keys, nil)
	c := &redisConnector{}

	start := time.Now()
	b, err := c.Capture(context.Background(), "redis://"+addr)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if d := time.Since(start); d > 50*time.Millisecond {
		t.Fatalf("capped capture took %v, want < 50ms", d)
	}

	dbMeta := b.Nodes[0].Metadata
	if dbMeta["sampled_keys"] != strconv.Itoa(redisScanCap) {
		t.Fatalf("sampled_keys = %q, want %d (hard cap)", dbMeta["sampled_keys"], redisScanCap)
	}
	capMsg, ok := dbMeta["sample_capped"]
	if !ok || !strings.Contains(capMsg, strconv.Itoa(redisScanCap)) {
		t.Fatalf("cap hit but not reported: %v", dbMeta)
	}
	if len(b.Nodes) != 2 { // db + one "bulk" prefix
		t.Fatalf("want 2 nodes, got %d", len(b.Nodes))
	}
	if got := b.Nodes[1].Metadata["approx_count"]; got != strconv.Itoa(redisScanCap) {
		t.Fatalf("bulk prefix approx_count = %q", got)
	}
}

func TestRedisErrorNamesHostOnly(t *testing.T) {
	// Nothing listens here — dial fails; the error must name scheme+host only.
	c := &redisConnector{}
	_, err := c.Capture(context.Background(), "redis://user:"+redisFakePW+"@127.0.0.1:1/0")
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), redisFakePW) {
		t.Fatalf("error leaks password: %v", err)
	}
	if !strings.Contains(err.Error(), "redis://127.0.0.1:1") {
		t.Fatalf("error should name scheme+host: %v", err)
	}
}

func TestRedisParams(t *testing.T) {
	c := &redisConnector{}
	if !strings.Contains(c.Example(), "$REDIS_") {
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

func TestRedisSmokeReal(t *testing.T) {
	url := os.Getenv("CTX_OPTIMIZE_TEST_REDIS")
	if url == "" {
		t.Skip("CTX_OPTIMIZE_TEST_REDIS not set")
	}
	c, err := sources.Lookup("redis")
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
