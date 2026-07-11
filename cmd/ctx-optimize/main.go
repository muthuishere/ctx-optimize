// ctx-optimize — gather a codebase (and its world) into one local store an
// agent answers from. Thin shim; everything lives in internal/app.
package main

import (
	"os"

	"github.com/muthuishere/ctx-optimize/internal/app"
)

func main() {
	os.Exit(app.Run(os.Args[1:], os.Stdout, os.Stderr))
}
