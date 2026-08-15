package boundaries

import "testing"

// A URL's userinfo is credentials, not a host. Before this was handled,
// `https://user:pw@host/x` reported `user` as the hostname — a fabricated fact,
// which the confidence tiers exist specifically to prevent — and
// `https://user@host/x` dropped the real host entirely.
func TestURLHostIgnoresUserinfo(t *testing.T) {
	m := &ASTMatch{URLScheme: []string{"http", "https"}}
	for _, tc := range []struct {
		lit, want string
		ok        bool
	}{
		{"https://user:pw@host-a.example/x", "host-a.example", true},
		{"https://user@host-b.example/x", "host-b.example", true},
		{"https://plain-c.example/x", "plain-c.example", true},
		{"https://host-d.example/p?q=a@b", "host-d.example", true}, // '@' after the path is not userinfo
		{"https://host-e.example#f@g", "host-e.example", true},     // nor in a fragment
		{"https://a%40b@host-f.example/x", "host-f.example", true}, // encoded '@' inside userinfo
		{"https://host-g.example:8443/x", "host-g.example", true},  // port still stops the host
	} {
		got, ok := m.URLHost(tc.lit)
		if ok != tc.ok || got != tc.want {
			t.Errorf("URLHost(%q) = %q,%v — want %q,%v", tc.lit, got, ok, tc.want, tc.ok)
		}
	}
}
