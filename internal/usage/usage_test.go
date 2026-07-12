package usage

import (
	"testing"
	"time"
)

func TestRecordAndSummarize(t *testing.T) {
	dir := t.TempDir()
	Record(dir, Event{Verb: "query", Arg: "split bio", Hits: 6, Bytes: 2000, MS: 190})
	Record(dir, Event{Verb: "card", Arg: "bio_split", Hits: 1, Bytes: 1500, MS: 80})
	Record(dir, Event{Verb: "query", Arg: "old", Hits: 1, Bytes: 400, MS: 100,
		TS: time.Now().AddDate(0, 0, -30)})
	s, err := Summarize(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Total != 3 || s.Today != 2 || s.Last7Days != 2 {
		t.Fatalf("totals wrong: %+v", s)
	}
	if s.ByVerb["query"].Count != 2 || s.ByVerb["card"].Bytes != 1500 {
		t.Fatalf("by-verb wrong: %+v", s.ByVerb)
	}
	if s.EstTokens != (2000+1500+400)/4 {
		t.Fatalf("est tokens wrong: %d", s.EstTokens)
	}
	// empty dir → zero summary, no error
	z, err := Summarize(t.TempDir())
	if err != nil || z.Total != 0 {
		t.Fatalf("empty summary: %+v %v", z, err)
	}
}
