// Command dth is the DocTheRepo Hub CLI: bring up a local hub (`dth up`), ask questions, manage
// repositories, jobs, usage, and tokens against any hub, and test external doc-gen engines.
package main

import (
	"fmt"
	"os"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := newRoot(os.Stdout, os.Stderr).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "dth:", err)
		os.Exit(1)
	}
}
