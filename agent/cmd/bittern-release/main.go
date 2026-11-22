// Command bittern-release signs and verifies the release manifest from ADR 0029
// on an offline signing machine.
//
// It is a plain static binary with no network access, no configuration file and
// no environment input beyond its flags. It is not part of the appliance
// payload or the image: the appliance verifies with internal/release, and this
// tool is the signer's half of the same scheme.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"bittern.invalid/agent/internal/release"
)

const usage = `usage:
  bittern-release init --key PATH [--pub-dir DIR] [--force] [--dry-run]
  bittern-release sign MANIFEST.json --key PATH [--yes] [--force] [--dry-run]
  bittern-release verify MANIFEST.json MANIFEST.json.sig --trust-dir DIR
                          [--rotation-dir DIR] [--kind KIND] [--committed-generation N]
  bittern-release manifest --payload ARCHIVE --version V --generation N --source-commit HEX
                          --key-id HEX --os-generation-min N --os-generation-max N
                          --schema-min N --schema-max N --updater-min N --out PATH
                          [--channel stable|beta] [--kind KIND] [--arch ARCH] [--created RFC3339]
                          [--dry-run]
  bittern-release pack --tree DIR --out DIR --version V --generation N
                          --source-commit HEX --key-id HEX
                          --os-generation-min N --os-generation-max N
                          --schema-min N --schema-max N --updater-min N
                          [--channel beta|stable] [--created RFC3339] [--epoch N]
                          [--dry-run]
  bittern-release tag TAG
  bittern-release key-id KEYFILE [--pub-dir DIR]

Bittern release signing and verification for an airgapped signing machine. The
signed object is the canonical release manifest from ADR 0029, never the bundle
archive. init and sign run on the signing machine, pack and manifest run on the
build host, and verify runs anywhere with the pinned trust store.

Exit codes:
  0  success
  1  signer-side or I/O failure
  2  usage error
  3  bad signature
  4  untrusted key or unreadable trust store
  5  non-canonical or invalid manifest
  6  manifest rejected by a kind or generation rule
`

// Exit codes. The verify command distinguishes the rejection categories from
// ADR 0029 section 6 so a script does not have to parse the message.
const (
	exitOK               = 0
	exitFailure          = 1
	exitUsage            = 2
	exitBadSignature     = 3
	exitUntrustedKey     = 4
	exitNonCanonical     = 5
	exitManifestRejected = 6
)

// signMessage is ed25519.Sign in production. A test replaces it to prove that a
// failed self-check deletes the signature; it is a package variable, not a flag
// or an environment variable, and nothing in the command surface can change it.
var signMessage = releaseSign

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	switch args[0] {
	case "init":
		return runInit(args[1:], stdout, stderr)
	case "sign":
		return runSign(args[1:], stdin, stdout, stderr)
	case "verify":
		return runVerify(args[1:], stdout, stderr)
	case "manifest":
		return runManifest(args[1:], stdout, stderr)
	case "pack":
		return runPack(args[1:], stdout, stderr)
	case "tag":
		return runTag(args[1:], stdout, stderr)
	case "key-id":
		return runKeyID(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return exitOK
	default:
		fmt.Fprintf(stderr, "bittern-release: unknown subcommand %q\n%s", args[0], usage)
		return exitUsage
	}
}

// newFlagSet returns a flag set that prints its errors and usage to stderr.
func newFlagSet(name, usageText string, stderr io.Writer) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { fmt.Fprint(stderr, usageText) }
	return flags
}

// splitArgs separates flags from positional arguments so a flag may follow a
// positional argument, as the usage lines show. The standard flag package stops
// at the first positional argument; this pre-pass keeps the command surface
// natural without a second parser for values.
func splitArgs(flags *flag.FlagSet, args []string) (flagArgs, positional []string) {
	flagArgs = make([]string, 0, len(args))
	positional = make([]string, 0, len(args))
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if argument == "--" {
			positional = append(positional, args[index+1:]...)
			break
		}
		if len(argument) < 2 || argument[0] != '-' {
			positional = append(positional, argument)
			continue
		}
		flagArgs = append(flagArgs, argument)
		name := strings.TrimLeft(argument, "-")
		if strings.Contains(name, "=") {
			continue
		}
		defined := flags.Lookup(name)
		if defined == nil {
			continue
		}
		if boolean, ok := defined.Value.(interface{ IsBoolFlag() bool }); ok && boolean.IsBoolFlag() {
			continue
		}
		if index+1 < len(args) {
			index++
			flagArgs = append(flagArgs, args[index])
		}
	}
	return flagArgs, positional
}

// parseFlags parses a subcommand's flags. It returns false when the caller
// should stop, with the exit code to return.
func parseFlags(flags *flag.FlagSet, args []string) (bool, int) {
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return false, exitOK
		}
		return false, exitUsage
	}
	return true, exitOK
}

// wantArgs checks the positional argument count and prints the subcommand
// usage when it does not match.
func wantArgs(flags *flag.FlagSet, positional []string, count int) bool {
	if len(positional) == count {
		return true
	}
	flags.Usage()
	fmt.Fprintf(flags.Output(), "%s: expected %d positional argument(s), got %d\n",
		flags.Name(), count, len(positional))
	return false
}

// exitCodeFor classifies a release rejection so verify can return a distinct
// exit code, and so sign reports its rejections the same way.
func exitCodeFor(err error) int {
	var releaseError *release.Error
	if !errors.As(err, &releaseError) {
		return exitFailure
	}
	switch {
	case releaseError.Rule == "manifest.kind",
		hasRulePrefix(releaseError.Rule, "generation."):
		return exitManifestRejected
	case hasRulePrefix(releaseError.Rule, "canonical."),
		hasRulePrefix(releaseError.Rule, "manifest."):
		return exitNonCanonical
	case releaseError.Rule == "signature.unknown-key",
		releaseError.Rule == "signature.key-id",
		hasRulePrefix(releaseError.Rule, "trust."):
		return exitUntrustedKey
	case hasRulePrefix(releaseError.Rule, "signature."):
		return exitBadSignature
	default:
		return exitFailure
	}
}

func hasRulePrefix(rule, prefix string) bool {
	return strings.HasPrefix(rule, prefix)
}

// exitLabel names a rejection category for the report.
func exitLabel(code int) string {
	switch code {
	case exitBadSignature:
		return "bad signature"
	case exitUntrustedKey:
		return "untrusted key"
	case exitNonCanonical:
		return "non-canonical or invalid manifest"
	case exitManifestRejected:
		return "manifest rejected"
	default:
		return "failure"
	}
}

// reportRejection prints the category and the release error for a rejected
// manifest, signature or trust store, and returns the exit code.
func reportRejection(stderr io.Writer, command string, err error) int {
	code := exitCodeFor(err)
	if code == exitFailure {
		fmt.Fprintf(stderr, "%s: %v\n", command, err)
		return code
	}
	fmt.Fprintf(stderr, "%s: %s\n", command, exitLabel(code))
	fmt.Fprintf(stderr, "%s: %v\n", command, err)
	return code
}

// fail prints a plain command failure.
func fail(stderr io.Writer, command string, format string, args ...any) int {
	fmt.Fprintf(stderr, "%s: %s\n", command, fmt.Sprintf(format, args...))
	return exitFailure
}
