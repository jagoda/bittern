// Command bittern-launch is the fixed agent launcher. bitternd.service starts
// it instead of the installed binary, and it execs the agent from the active
// app bundle or from the installed /usr/bin/bitternd baseline.
//
// The launcher takes the agent's own arguments and passes them through. It has
// no flags of its own and reads no environment-derived path.
package main

import (
	"fmt"
	"os"

	"bittern.invalid/agent/internal/launcher"
)

func main() {
	if err := launcher.Run(launcher.DefaultConfig(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "bittern-launch:", err)
		os.Exit(1)
	}
}
