package sources

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/muthuishere/ctx-optimize/internal/schema"
)

// Param is one documented URL parameter of a connector — the source of truth
// for `adapters help <scheme>` setup cards (printed from the connector's own
// declared table, so help never drifts from code).
type Param struct {
	Name string // "user:pass userinfo", "sslmode", "tls_ca", ...
	Desc string // what it does / value shape
	Cred bool   // credential-class: must ride a $VAR, never a literal
}

// Connector captures ONE source: one dial, one Batch, deterministic. Real
// connectors (Stage 2) are one file each — implement this, call Register in
// init(). Capture receives the fully-expanded URL (or file path); it must
// never print, its error text is scrubbed by the caller regardless.
type Connector interface {
	Scheme() string  // the connector name Route resolves to ("postgres", "openapi", ...)
	Params() []Param // help table for `adapters help <scheme>`
	Example() string // a value-format example with $VAR placeholders
	Capture(ctx context.Context, url string) (*schema.Batch, error)
}

var registry = map[string]Connector{}

// Register arms a connector under its Scheme(). Called from connector init()
// functions (and tests, which pair it with Unregister via t.Cleanup).
func Register(c Connector) { registry[c.Scheme()] = c }

// Unregister removes a connector (test cleanup).
func Unregister(scheme string) { delete(registry, scheme) }

// Lookup resolves a connector name (as returned by Route).
func Lookup(name string) (Connector, error) {
	if c, ok := registry[name]; ok {
		return c, nil
	}
	// SupportedSchemes (static routing table + registry + armed-bridge
	// companion), not RegisteredSchemes: in the driver-free main binary the
	// in-process registry is empty, but the shipped set is not.
	return nil, fmt.Errorf("no %q connector in this build — supported schemes: %s (exotic sources: the adapter-script lane, .ctxoptimize/adapters/)",
		name, strings.Join(SupportedSchemes(), " "))
}

// RegisteredSchemes lists armed connector names, sorted.
func RegisteredSchemes() []string {
	out := make([]string, 0, len(registry))
	for s := range registry {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// HelpCard renders the complete setup card for one scheme: value format,
// credential/cert params (with the percent-encoding hint — secrets with
// URL-special chars must encode, '/' → %2F), an export example, and the
// paste-ready add command. Generated from Params(), never hand-written.
func HelpCard(scheme string) (string, error) {
	name, err := Route(scheme + "://x")
	if err != nil {
		name = scheme // let Lookup produce the supported-set error
	}
	c, err := Lookup(name)
	if err != nil {
		if bridgeArmed {
			// Main binary: the param tables live only in connector code —
			// proxy the card from the companion so help never drifts.
			if card, berr := bridgeHelp(scheme); berr == nil {
				return card, nil
			}
		}
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s — value format:\n  %s\n", c.Scheme(), c.Example())
	params := c.Params()
	if len(params) > 0 {
		fmt.Fprintf(&b, "params:\n")
		for _, p := range params {
			tag := ""
			if p.Cred {
				tag = "  [credential — use a $VAR, never a literal]"
			}
			fmt.Fprintf(&b, "  %-24s %s%s\n", p.Name, p.Desc, tag)
		}
	}
	fmt.Fprintf(&b, "notes:\n")
	fmt.Fprintf(&b, "  secrets with URL-special characters must be percent-encoded ('/' → %%2F, '@' → %%40, ':' → %%3A)\n")
	fmt.Fprintf(&b, "  cert/key PATHS may sit in the URL; key contents never leave this machine and key params are stripped from stored ids\n")
	fmt.Fprintf(&b, "setup (value in env, repo-root .env, or ~/.config/ctx-optimize/.env):\n")
	fmt.Fprintf(&b, "  export MY_%s_URL='%s'\n", strings.ToUpper(c.Scheme()), c.Example())
	fmt.Fprintf(&b, "add (names only on argv — never a raw URL):\n")
	fmt.Fprintf(&b, "  ctx-optimize add MY_%s_URL\n", strings.ToUpper(c.Scheme()))
	return b.String(), nil
}
