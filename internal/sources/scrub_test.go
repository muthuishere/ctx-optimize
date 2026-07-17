package sources

import (
	"strings"
	"testing"
)

func TestScrub(t *testing.T) {
	secret := "wJalrXUtnFEMI/K7MDENG+bPxRfiCY" // real-shaped AWS secret: '/' and '+'
	pass := "p@ss:w/rd"
	cases := []struct {
		name   string
		values []string
		text   string
		wantIn []string // substrings that must survive
	}{
		{
			"plain value",
			[]string{pass},
			"dial tcp: auth failed for postgres://alice:" + pass + "@host/db",
			[]string{"alice", "host/db", "***"},
		},
		{
			"percent-encoded echo (query form)",
			[]string{pass},
			"bad request: password=p%40ss%3Aw%2Frd rejected",
			[]string{"password=***"},
		},
		{
			"whole resolved URL scrubbed",
			[]string{"postgres://u:" + secret + "@h/db"},
			`parse "postgres://u:` + secret + `@h/db": invalid port`,
			[]string{"***", "invalid port"},
		},
		{
			"multiple values",
			[]string{"AKIAXXXX", secret},
			"s3 auth AKIAXXXX/" + secret + " denied",
			[]string{"***/***", "denied"},
		},
		{
			"empty value is a no-op",
			[]string{""},
			"nothing to see",
			[]string{"nothing to see"},
		},
	}
	for _, c := range cases {
		got := Scrub(c.values, c.text)
		for _, v := range c.values {
			if v != "" && strings.Contains(got, v) {
				t.Errorf("%s: value survived scrub: %q", c.name, got)
			}
		}
		if strings.Contains(got, "p%40ss") || strings.Contains(got, "w%2Frd") {
			t.Errorf("%s: percent-encoded value survived: %q", c.name, got)
		}
		for _, want := range c.wantIn {
			if !strings.Contains(got, want) {
				t.Errorf("%s: %q missing from %q", c.name, want, got)
			}
		}
	}
}
