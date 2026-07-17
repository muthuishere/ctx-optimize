// ctx-optimize-adapters — the capture companion: every database/queue/API
// driver links HERE (via the connectors blank import), keeping the main
// ctx-optimize binary driver-free and its hot paths byte-identical. Thin
// shim; everything lives in internal/adapterscli.
package main

import (
	"os"

	"github.com/muthuishere/ctx-optimize/internal/adapterscli"
	_ "github.com/muthuishere/ctx-optimize/internal/sources/connectors"
)

func main() {
	os.Exit(adapterscli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
