package app

import "github.com/muthuishere/ctx-optimize/internal/gitinfo"

// gitHead delegates to internal/gitinfo — the shared read-only git reflection
// used by the CLI and the dashboard alike.
func gitHead(dir string) (head string, unixTime int64, ok bool) {
	return gitinfo.Head(dir)
}
