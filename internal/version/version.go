// Package version carries the build-time version stamp. Injected by
// goreleaser via -ldflags "-X .../internal/version.Version=...".
package version

var (
	Version = "0.0.0-dev"
	Commit  = "none"
	Date    = "unknown"
)
