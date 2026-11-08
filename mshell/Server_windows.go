package main

import (
	"fmt"
	"os"
)

// runServer is msh server, which needs a pseudo console per engine and its
// own process spawning on Windows (ai/gui-terminal.typ); not written yet.
func runServer() int {
	fmt.Fprintln(os.Stderr, "msh server is not supported on Windows yet")
	return 1
}
