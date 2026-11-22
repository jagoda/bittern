package main

import (
	"fmt"
	"io"

	"bittern.invalid/agent/internal/release"
)

const tagUsage = `usage: bittern-release tag TAG

Validates an application bundle tag against ADR 0031 and prints the generation
it encodes to stdout. The suffix must be exactly six lowercase hexadecimal
characters, so app-beta-00001f is generation 31 and app-beta-000010 is
generation 16. Uppercase hex, five or seven characters, an empty suffix, a
decimal parse, and generation 0 are rejected.

The publish workflow calls this command instead of parsing the tag in shell, so
the tag rule has one implementation and one test suite.
`

func runTag(args []string, stdout, stderr io.Writer) int {
	flags := newFlagSet("tag", tagUsage, stderr)
	flagArgs, positionals := splitArgs(flags, args)
	if proceed, code := parseFlags(flags, flagArgs); !proceed {
		return code
	}
	if !wantArgs(flags, positionals, 1) {
		return exitUsage
	}
	generation, err := release.ParseBetaTag(positionals[0])
	if err != nil {
		fmt.Fprintf(stderr, "tag: %v\n", err)
		return exitUsage
	}
	fmt.Fprintf(stderr, "tag: %s channel %s generation %d\n", positionals[0], release.ChannelBeta, generation)
	fmt.Fprintf(stdout, "%d\n", generation)
	return exitOK
}
