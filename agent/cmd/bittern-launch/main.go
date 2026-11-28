// Command bittern-launch is the fixed launcher for the agent and the update
// helper. bitternd.service starts it as /usr/libexec/bittern/bitternd and
// bittern-update.service starts it as /usr/libexec/bittern/bittern-update. The
// installed name selects the profile: the agent resolves usr/bin/bitternd, the
// helper resolves usr/bin/bittern, and both fall back to the installed
// baseline.
//
// The launcher takes the program's own arguments and passes them through. It
// has no flags of its own and reads no environment-derived path.
package main

import (
	"fmt"
	"os"

	"bittern.invalid/agent/internal/launcher"
)

func main() {
	if err := launcher.RunInstalled(os.Args[0], os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "bittern-launch:", err)
		os.Exit(1)
	}
}
