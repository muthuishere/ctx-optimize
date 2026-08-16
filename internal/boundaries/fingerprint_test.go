package boundaries

import "testing"

// The fingerprint answers one question: were these answers produced by the
// vocabulary I am reading them with? So it must move on exactly the change
// that makes a stored answer mean something else — the name — and on nothing
// else, or every store is permanently accused.
func TestFingerprintMovesOnTheNameAndNothingElse(t *testing.T) {
	base := []Rule{
		{ID: "env-go", Transport: "config.env"},
		{ID: "webstorage-local", Transport: "storage.browser.local"},
	}
	sig := Fingerprint(base)
	if sig == "" {
		t.Fatal("no fingerprint for a real rule set")
	}

	// order is not vocabulary
	if got := Fingerprint([]Rule{base[1], base[0]}); got != sig {
		t.Errorf("fingerprint depends on rule ORDER: %q vs %q", got, sig)
	}

	// renaming a transport changes what every stored port MEANS
	renamed := []Rule{base[0], {ID: "webstorage-local", Transport: "storage.local"}}
	if Fingerprint(renamed) == sig {
		t.Error("a renamed transport did not move the fingerprint — the exact case it exists for")
	}

	// adding a rule changes what the store can contain
	added := append(append([]Rule{}, base...), Rule{ID: "house-idb", Transport: "storage.browser.indexeddb"})
	if Fingerprint(added) == sig {
		t.Error("an added rule did not move the fingerprint")
	}

	// but tightening a rule's MATCHING does not: what it finds is a question
	// for freshness and the counts, not for "does this word still mean what it
	// meant". Otherwise every regex tweak dates every store on the machine.
	tightened := []Rule{
		{ID: "env-go", Transport: "config.env", Tier: "EXTRACTED"},
		{ID: "webstorage-local", Transport: "storage.browser.local", Direction: "provides"},
	}
	if got := Fingerprint(tightened); got != sig {
		t.Errorf("a change that does not rename anything moved the fingerprint: %q vs %q", got, sig)
	}
}

// The shipped set must have a stable fingerprint within one build, or every
// status call would report drift against itself.
func TestFingerprintOfShippedRulesIsStable(t *testing.T) {
	root := isolated(t)
	a, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if Fingerprint(a) != Fingerprint(b) {
		t.Fatal("the shipped rule set fingerprints differently on two loads")
	}
}
