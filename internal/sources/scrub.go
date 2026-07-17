package sources

import (
	"net/url"
	"strings"
)

// Scrub is Choke B of the two-choke secret model (C1): the binary knows
// every resolved secret VALUE at dial time, so every string that leaves a
// source path — connector errors, summaries, recovered panics — passes
// through here first. Each value (and its percent-encoded form, since a
// driver may echo the encoded URL back) is literal-replaced with "***".
// Fail-closed: whole resolved values are scrubbed even when they are full
// URLs — losing a hostname from an error beats leaking a password.
func Scrub(values []string, text string) string {
	for _, v := range values {
		if v == "" {
			continue
		}
		text = strings.ReplaceAll(text, v, "***")
		if enc := url.QueryEscape(v); enc != v {
			text = strings.ReplaceAll(text, enc, "***")
		}
		if enc := url.PathEscape(v); enc != v {
			text = strings.ReplaceAll(text, enc, "***")
		}
	}
	return text
}
