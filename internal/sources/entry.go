// Package sources implements the native-source convention (ADR
// 2026-07-17-bundled-adapter-templates): a source is an environment variable
// name, its value is a URL, and the URL scheme picks the connector. This file
// is the entry pipeline — expand ($VAR substitution), detect literal
// credentials in COMMITTED entries (hard error), route by scheme, and
// sanitize (the fail-closed textual choke that keeps secrets out of every
// stored id). Secrets exist in memory for the dial only; nothing here ever
// prints or persists a resolved value.
package sources

import (
	"fmt"
	"sort"
	"strings"
)

// IsEnvName reports whether s is exactly an env-var-shaped name
// (^[A-Z_][A-Z0-9_]*$) — the argv/config bare form. Var-name shape = source;
// anything else is a path or a template (H2: names-only on argv).
func IsEnvName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '_' || (c >= 'A' && c <= 'Z'):
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func isVarStart(c byte) bool {
	return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

func isVarChar(c byte) bool {
	return isVarStart(c) || (c >= '0' && c <= '9')
}

// Expand does $VAR / ${VAR} substitution against lookup. A bare
// env-var-shaped entry is a reference to the whole entry; "$NAME" and
// templates substitute inline. missing lists referenced-but-unset var names
// in order of appearance.
//
// A bare name's VALUE gets exactly one more, LENIENT template pass — the
// documented "fold the URL into a single var" shape
// (DOCS_S3_URL='s3://$MINIO_KEY:$MINIO_SECRET@host/bucket') must reach the
// wire expanded. Lenient means an unresolvable $token in the value stays
// LITERAL (a provider-issued password containing '$' keeps working) and is
// never reported missing. Substituted values are NEVER re-scanned in either
// pass — a resolved secret containing '$' stays literal.
func Expand(entry string, lookup func(string) (string, bool)) (expanded string, missing []string) {
	if IsEnvName(entry) { // form 1: bare env var name
		v, ok := lookup(entry)
		if !ok {
			return "", []string{entry}
		}
		return expandLenient(v, lookup), nil
	}
	var b strings.Builder
	for i := 0; i < len(entry); {
		c := entry[i]
		if c != '$' {
			b.WriteByte(c)
			i++
			continue
		}
		if i+1 < len(entry) && entry[i+1] == '{' { // ${VAR}
			j := i + 2
			for j < len(entry) && entry[j] != '}' {
				j++
			}
			if j < len(entry) && j > i+2 {
				name := entry[i+2 : j]
				if v, ok := lookup(name); ok {
					b.WriteString(v)
				} else {
					missing = append(missing, name)
				}
				i = j + 1
				continue
			}
			b.WriteByte(c) // malformed ${ — literal
			i++
			continue
		}
		j := i + 1
		if j < len(entry) && isVarStart(entry[j]) {
			for j < len(entry) && isVarChar(entry[j]) {
				j++
			}
			name := entry[i+1 : j]
			if v, ok := lookup(name); ok {
				b.WriteString(v)
			} else {
				missing = append(missing, name)
			}
			i = j
			continue
		}
		b.WriteByte(c) // lone $ — literal
		i++
	}
	return b.String(), missing
}

// expandLenient substitutes $VAR/${VAR} refs that RESOLVE and leaves
// everything else byte-for-byte literal (no missing reporting, malformed
// forms untouched). Used only on a bare entry name's resolved value.
func expandLenient(s string, lookup func(string) (string, bool)) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		if c != '$' {
			b.WriteByte(c)
			i++
			continue
		}
		if i+1 < len(s) && s[i+1] == '{' { // ${VAR}
			j := i + 2
			for j < len(s) && s[j] != '}' {
				j++
			}
			if j < len(s) && j > i+2 {
				if v, ok := lookup(s[i+2 : j]); ok {
					b.WriteString(v)
					i = j + 1
					continue
				}
			}
			b.WriteByte(c)
			i++
			continue
		}
		j := i + 1
		if j < len(s) && isVarStart(s[j]) {
			for j < len(s) && isVarChar(s[j]) {
				j++
			}
			if v, ok := lookup(s[i+1 : j]); ok {
				b.WriteString(v)
				i = j
				continue
			}
			b.WriteString(s[i:j]) // unset — keep the literal $token
			i = j
			continue
		}
		b.WriteByte(c) // lone $ — literal
		i++
	}
	return b.String()
}

// VarNames lists the env-var names an entry references, in order.
func VarNames(entry string) []string {
	if IsEnvName(entry) {
		return []string{entry}
	}
	var names []string
	Expand(entry, func(name string) (string, bool) {
		names = append(names, name)
		return "", true
	})
	return names
}

// isVarRef reports a token made purely of $VAR/${VAR} references (possibly
// concatenated) — substituting everything with "" leaves nothing literal.
func isVarRef(s string) bool {
	if s == "" || IsEnvName(s) {
		return false
	}
	out, _ := Expand(s, func(string) (string, bool) { return "", true })
	return out == ""
}

// credParams are query params whose VALUE is a secret. Path-valued params
// (sslkey, key, tlsCertificateKeyFile) may be literal in a committed entry —
// cert PATHS are not secrets — but are still stripped from stored ids.
var credParams = []string{"password", "passwd", "pwd", "secret", "token", "access_key", "secret_key", "sasl_password"}

// pathValuedCredParams hold key-file PATHS: allowed literal, stripped by
// Sanitize.
var pathValuedCredParams = []string{"sslkey", "key", "tlscertificatekeyfile"}

// DetectLiteralCreds inspects a RAW (unexpanded) committed entry and refuses
// literal secrets: a userinfo password or a secret-class query value that is
// not a $VAR reference is a hard error. Literal USERNAMES are allowed (L3).
// The error names the entry's var-shaped skeleton only — never the literal.
func DetectLiteralCreds(raw string) error {
	if IsEnvName(raw) || isVarRef(raw) {
		return nil // pure env reference, nothing literal
	}
	idx := strings.Index(raw, "://")
	if idx < 0 {
		return nil // path or plain name — no userinfo possible
	}
	rest := raw[idx+3:]
	authority := rest
	if end := strings.IndexAny(rest, "/?#"); end >= 0 {
		authority = rest[:end]
	}
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		if _, pass, hasPass := strings.Cut(authority[:at], ":"); hasPass && pass != "" && !isVarRef(pass) {
			return fmt.Errorf("literal password in %q: credentials belong in env vars ($VAR)", Skeleton(raw))
		}
	}
	if q := strings.Index(raw, "?"); q >= 0 {
		for _, kv := range strings.Split(raw[q+1:], "&") {
			k, v, ok := strings.Cut(kv, "=")
			if !ok || v == "" || isVarRef(v) {
				continue
			}
			for _, p := range pathValuedCredParams {
				if strings.EqualFold(k, p) {
					k = "" // path-valued: allowed literal, stripped by Sanitize
					break
				}
			}
			for _, p := range credParams {
				if strings.EqualFold(k, p) {
					return fmt.Errorf("literal %s= in %q: credentials belong in env vars ($VAR)", k, Skeleton(raw))
				}
			}
		}
	}
	return nil
}

// Skeleton renders an entry safe for error messages: the sanitized form when
// parseable, otherwise the referenced env-var names only — a literal secret
// never reaches an error string.
func Skeleton(raw string) string {
	if s, ok := Sanitize(raw); ok {
		return s
	}
	if names := VarNames(raw); len(names) > 0 {
		return "$" + strings.Join(names, " $")
	}
	return "(unparseable entry)"
}

// connectorForScheme is the routing table (deterministic, applied to the
// expanded value). Stage-2 connectors register under these names.
var connectorForScheme = map[string]string{
	"postgres": "postgres", "postgresql": "postgres",
	"mysql":   "mysql",
	"mongodb": "mongo", "mongodb+srv": "mongo",
	"redis": "redis", "rediss": "redis",
	"kafka": "kafka", "nats": "nats", "s3": "s3",
	"http": "openapi", "https": "openapi",
}

// SupportedSchemes lists the routable schemes, sorted (help + errors).
func SupportedSchemes() []string {
	seen := map[string]bool{}
	var out []string
	for s := range connectorForScheme {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	for s := range registry {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	// Armed bridge: fold in the companion's registered schemes (cached exec
	// of `ctx-optimize-adapters schemes`) so help/errors name the live set.
	for _, s := range companionSchemes() {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// Route picks the connector for an expanded value. Keyed on "://" with a
// lowercased scheme; no scheme — or a single-letter "scheme" like C:\ — is a
// filesystem path (openapi from disk). Anything else is a hard error listing
// the supported set.
func Route(expanded string) (string, error) {
	if expanded == "" {
		return "", fmt.Errorf("empty source value")
	}
	idx := strings.Index(expanded, "://")
	if idx < 0 || idx == 1 { // no scheme, or single-letter drive (C:\ → no ://; c://x → still a path)
		return "openapi", nil
	}
	scheme := strings.ToLower(expanded[:idx])
	if len(scheme) == 1 {
		return "openapi", nil // single-letter "scheme" = a drive path
	}
	if _, ok := registry[scheme]; ok {
		return scheme, nil // registered connector claims its scheme directly
	}
	if c, ok := connectorForScheme[scheme]; ok {
		return c, nil
	}
	// Armed bridge: schemes the companion's connectors claim directly
	// (mssql, sqlserver) route to themselves — the exec bridge captures them.
	for _, s := range companionSchemes() {
		if s == scheme {
			return scheme, nil
		}
	}
	return "", fmt.Errorf("unsupported scheme %q — supported: %s, or a file path (exotic sources: the adapter-script lane, .ctxoptimize/adapters/)",
		scheme, strings.Join(SupportedSchemes(), " "))
}

// stripParams are query params removed from stored ids (Choke A): secret
// values AND key-file paths — key material never rides an id.
var stripParams = map[string]bool{
	"password": true, "passwd": true, "pwd": true, "secret": true,
	"token": true, "access_key": true, "secret_key": true,
	"sasl_password": true, "sslkey": true, "key": true,
	"ssl-key": true, "ssl-cert": true, "ssl-ca": true,
	"tlscertificatekeyfile": true,
}

// Sanitize strips userinfo and credential-class query params TEXTUALLY —
// never net/url.Parse, which both chokes on real secrets (a '/' in an AWS
// key is an "invalid port") and echoes the full URL in its errors. http(s)
// ids additionally strip ALL query params (allowlist-in: the ?token=/?sig=
// vocabulary is unbounded). ok=false means the entry defied parsing — the
// caller must fall back to the env-var NAME only, never a value fragment.
func Sanitize(s string) (string, bool) {
	idx := strings.Index(s, "://")
	if idx >= 0 {
		rest := s[idx+3:]
		authority, tail := rest, ""
		if end := strings.IndexAny(rest, "/?#"); end >= 0 {
			authority, tail = rest[:end], rest[end:]
		}
		if at := strings.LastIndex(authority, "@"); at >= 0 {
			authority = authority[at+1:] // drop userinfo entirely
		} else {
			// DEFENSIVE tier: a secret with an unencoded '/' ends the
			// "authority" early (RFC-invalid, but users will write it). If an
			// '@' still appears before the query, strip up to the LAST '@'.
			preQ := rest
			if qi := strings.IndexAny(rest, "?#"); qi >= 0 {
				preQ = rest[:qi]
			}
			if at2 := strings.LastIndex(preQ, "@"); at2 >= 0 {
				afterAt := rest[at2+1:]
				if end2 := strings.IndexAny(afterAt, "/?#"); end2 >= 0 {
					authority, tail = afterAt[:end2], afterAt[end2:]
				} else {
					authority, tail = afterAt, ""
				}
			}
		}
		if strings.Contains(authority, "@") { // confusion — fail closed
			return "", false
		}
		scheme := strings.ToLower(s[:idx])
		if scheme == "http" || scheme == "https" {
			// http(s) ids strip ALL query params (M2).
			if qi := strings.IndexAny(tail, "?#"); qi >= 0 {
				tail = tail[:qi]
			}
		}
		s = s[:idx+3] + authority + tail
	}
	// Strip credential-class query params textually, order-preserving.
	if q := strings.Index(s, "?"); q >= 0 {
		base, query := s[:q], s[q+1:]
		frag := ""
		if h := strings.Index(query, "#"); h >= 0 {
			frag, query = query[h:], query[:h]
		}
		var kept []string
		for _, kv := range strings.Split(query, "&") {
			k, _, _ := strings.Cut(kv, "=")
			if !stripParams[strings.ToLower(k)] {
				kept = append(kept, kv)
			}
		}
		if len(kept) == 0 {
			s = base + frag
		} else {
			s = base + "?" + strings.Join(kept, "&") + frag
		}
	}
	return s, true
}

// SourceID is the producer identity of a config entry (H1): the env-var NAME
// for bare/$ entries, the sanitized template string for templates. Fail-
// closed: an unparseable template falls back to its referenced var names.
func SourceID(entry string) string {
	if IsEnvName(entry) {
		return entry
	}
	if strings.HasPrefix(entry, "$") && IsEnvName(entry[1:]) {
		return entry[1:]
	}
	return Skeleton(entry)
}
