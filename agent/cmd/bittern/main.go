// Command bittern is the operator CLI for first boot, hardware detection,
// render-only configuration generation, and explicit setup-token reads.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/auth"
	"bittern.invalid/agent/internal/firstboot"
	"bittern.invalid/agent/internal/frigateimage"
	"bittern.invalid/agent/internal/hardware"
	"bittern.invalid/agent/internal/logging"
	"bittern.invalid/agent/internal/redact"
	"bittern.invalid/agent/internal/render"
	secretstore "bittern.invalid/agent/internal/secrets"
	"bittern.invalid/agent/internal/setupqr"
)

const usage = `usage:
  bittern detect
  bittern render --config PATH --out DIR
  bittern first-boot [--state-root DIR] [--config-root DIR] [--live-root DIR] [--offline]
  bittern setup-token [--qr] [--state-root DIR] [--config-root DIR] [--auth-state PATH]
  bittern image status [--state-path PATH]
  bittern image prewarm [--state-path PATH]
  bittern update check [--force]
  bittern update install [SOURCE] [--no-follow]
  bittern update rollback --reason TEXT [--no-follow]
  bittern update status [--state-root DIR]
  bittern update cancel [--state-root DIR]
  bittern update helper
  bittern update apply-config --state-root DIR
  bittern camera probe CAMERA [flags]
`

var (
	now                 = time.Now
	currentLANAddress   = firstboot.CurrentLANAddress
	renderSetupQR       = setupqr.RenderQR
	issueSetupShortCode = auth.IssueShortCode
	openLocalConsole    = localConsole
)

var errNoLocalConsole = errors.New("no local console")

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	switch os.Args[1] {
	case "detect":
		if len(os.Args) != 2 {
			fmt.Fprint(os.Stderr, usage)
			os.Exit(2)
		}
		if err := runDetect(); err != nil {
			fmt.Fprintln(os.Stderr, "bittern detect:", err)
			os.Exit(1)
		}
	case "render":
		if err := runRender(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "bittern render:", err)
			os.Exit(1)
		}
	case "first-boot":
		if err := runFirstBoot(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "bittern first-boot:", err)
			os.Exit(1)
		}
	case "setup-token":
		if err := runSetupToken(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "bittern setup-token:", err)
			os.Exit(1)
		}
	case "image":
		if err := runImage(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "bittern image:", err)
			os.Exit(1)
		}
	case "update":
		if err := runUpdate(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "bittern update:", err)
			os.Exit(1)
		}
	case "camera":
		if err := runCamera(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "bittern camera:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "bittern: unknown subcommand %q\n%s", os.Args[1], usage)
		os.Exit(2)
	}
}

func runImage(args []string) error {
	if len(args) == 0 {
		return errors.New("image needs a subcommand: status or prewarm")
	}
	switch args[0] {
	case "status":
		return runImageStatus(args[1:])
	case "prewarm":
		return runImagePrewarm(args[1:])
	default:
		return fmt.Errorf("unknown image subcommand %q", args[0])
	}
}

func imageOptions(statePath, docker string) (frigateimage.Options, error) {
	reference, err := render.FrigateImageReference()
	if err != nil {
		return frigateimage.Options{}, err
	}
	digest, err := render.FrigateImageDigest()
	if err != nil {
		return frigateimage.Options{}, err
	}
	return frigateimage.Options{
		Reference: reference,
		Digest:    digest,
		StatePath: statePath,
		Docker:    docker,
		Logger:    logging.PrintfLogger{Logger: logging.New(os.Stdout, redact.New())},
	}, nil
}

func runImageStatus(args []string) error {
	flags := flag.NewFlagSet("image status", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	statePath := flags.String("state-path", frigateimage.StatePath, "prewarm state file")
	docker := flags.String("docker", "", "docker binary (default: the standard path)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	options, err := imageOptions(*statePath, *docker)
	if err != nil {
		return err
	}
	status := frigateimage.Local(context.Background(), options)
	fmt.Printf("recording software: %s\n", status.State)
	if status.Digest != "" {
		fmt.Printf("digest: %s\n", status.Digest)
	}
	if status.LastError != "" {
		fmt.Printf("last error: %s\n", status.LastError)
	}
	return nil
}

func runImagePrewarm(args []string) error {
	flags := flag.NewFlagSet("image prewarm", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	statePath := flags.String("state-path", frigateimage.StatePath, "prewarm state file")
	docker := flags.String("docker", "", "docker binary (default: the standard path)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	options, err := imageOptions(*statePath, *docker)
	if err != nil {
		return err
	}
	_, err = frigateimage.Prewarm(context.Background(), options)
	return err
}

func runDetect() error {
	inventory := hardware.Detect(hardware.Roots{})
	data, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	fmt.Println()
	printSummary(inventory)
	return nil
}

func runRender(args []string) error {
	flags := flag.NewFlagSet("render", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	configPath := flags.String("config", "", "appliance.yaml to load and validate")
	outputDir := flags.String("out", "", "directory for the rendered file tree")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *configPath == "" || *outputDir == "" {
		return errors.New("--config and --out are required")
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}

	files, validationErrors, err := render.Generate(*configPath, *outputDir)
	if err != nil {
		return err
	}
	if len(validationErrors) > 0 {
		for _, validationError := range validationErrors {
			fmt.Fprintln(os.Stderr, validationError.Error())
		}
		return fmt.Errorf("validation failed for %s; no files written", *configPath)
	}

	fmt.Printf("rendered %d files under %s:\n", len(files), *outputDir)
	for _, file := range files {
		fmt.Printf("  %s\n", filepath.FromSlash(file.Path))
	}
	return nil
}

func runFirstBoot(args []string) error {
	return runFirstBootWithSystem(args, apply.NewExecSystem())
}

// runFirstBootWithSystem runs first boot on an explicit system. The CLI passes
// the production system; a test passes one whose chrony check directory is a
// temporary directory it controls.
func runFirstBootWithSystem(args []string, system *apply.ExecSystem) error {
	flags := flag.NewFlagSet("first-boot", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	stateRoot := flags.String("state-root", "/var/lib/bittern", "state root for generations, journal and markers")
	configRoot := flags.String("config-root", "/etc/bittern", "directory for appliance.yaml and secrets.yaml")
	liveRoot := flags.String("live-root", "", "root for live configuration files (default /, or STATE_ROOT/live with --offline)")
	offline := flags.Bool("offline", false, "write files but skip systemd units and live network health checks")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}

	if *liveRoot == "" {
		if *offline {
			*liveRoot = filepath.Join(*stateRoot, "live")
		} else {
			*liveRoot = "/"
		}
	}

	if system == nil {
		system = apply.NewExecSystem()
	}
	system.Offline = *offline
	options := firstboot.Options{
		StateRoot:  *stateRoot,
		ConfigRoot: *configRoot,
		LiveRoot:   *liveRoot,
		System:     system,
		Logger:     logging.PrintfLogger{Logger: logging.New(os.Stderr, redact.New())},
	}
	if *offline {
		options.Health = []apply.Probe{{
			Name: "offline-simulation",
			Kind: apply.ProbeOffline,
		}}
	}

	result, err := firstboot.Run(options)
	if err != nil {
		if result.SetupTokenPath != "" {
			fmt.Println("first boot did not complete. Show the one-time setup block with:")
			fmt.Println("  sudo bittern setup-token --qr")
		}
		return err
	}
	printFirstBootResult(result, *offline)
	if result.Status == "complete" {
		if err := printConsoleSetupBlock(*stateRoot, *configRoot); err != nil && !errors.Is(err, errNoLocalConsole) {
			fmt.Fprintln(os.Stderr, "setup QR not shown:", err)
		}
	}
	return nil
}

func runSetupToken(args []string) error {
	return runSetupTokenTo(args, os.Stdout, os.Geteuid)
}

func runSetupTokenTo(args []string, stdout io.Writer, effectiveUID func() int) error {
	flags := flag.NewFlagSet("setup-token", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	stateRoot := flags.String("state-root", "/var/lib/bittern", "state root holding setup-token and auth state")
	configRoot := flags.String("config-root", "/etc/bittern", "directory holding secrets.yaml")
	authState := flags.String("auth-state", "", "authentication state file (default STATE_ROOT/auth.json)")
	qr := flags.Bool("qr", false, "print a QR, the grouped setup token and a short one-time code")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %v", flags.Args())
	}
	if effectiveUID() != 0 {
		return errors.New("setup-token must run as root")
	}
	token, err := loadSetupToken(*stateRoot, *configRoot, *authState)
	if err != nil {
		return err
	}
	if !*qr {
		_, err = fmt.Fprintln(stdout, token)
		return err
	}
	lanAddress, err := currentLANAddress()
	if err != nil {
		return fmt.Errorf("cannot show a setup QR: read lan0: %w", err)
	}
	if strings.TrimSpace(lanAddress) == "" {
		return errors.New("cannot show a setup QR: lan0 has no IPv4 address; run without --qr for the plain token")
	}
	return writeSetupBlock(stdout, token, lanAddress, filepath.Join(*configRoot, "secrets.yaml"), now())
}

// writeSetupBlock renders the QR before it rotates the short code, so a missing
// qrencode leaves the stored credentials untouched. The token is passed to
// qrencode on standard input and is never an argument or a temporary file.
func writeSetupBlock(w io.Writer, token, lanAddress, secretsPath string, at time.Time) error {
	payload, err := setupqr.SetupURL(lanAddress, token)
	if err != nil {
		return err
	}
	var rendered bytes.Buffer
	if err := renderSetupQR(&rendered, payload); err != nil {
		return err
	}
	pageURL, err := setupqr.PageURL(lanAddress)
	if err != nil {
		return err
	}
	shortCode, expires, err := issueSetupShortCode(secretsPath, at, nil)
	if err != nil {
		return err
	}
	block := setupqr.Block{
		PageURL:   pageURL,
		Token:     token,
		ShortCode: shortCode,
		Expires:   expires,
		Now:       at,
	}
	_, err = io.WriteString(w, setupqr.FormatBlock(block, rendered.String()))
	return err
}

func printConsoleSetupBlock(stateRoot, configRoot string) error {
	console, err := openLocalConsole()
	if err != nil {
		return err
	}
	defer console.Close()
	token, err := loadSetupToken(stateRoot, configRoot, "")
	if err != nil {
		return err
	}
	lanAddress, err := currentLANAddress()
	if err != nil {
		return fmt.Errorf("read lan0: %w", err)
	}
	if strings.TrimSpace(lanAddress) == "" {
		return errors.New("lan0 has no IPv4 address; run sudo bittern setup-token --qr after DHCP")
	}
	return writeSetupBlock(console, token, lanAddress, filepath.Join(configRoot, "secrets.yaml"), now())
}

func localConsole() (io.WriteCloser, error) {
	path := strings.TrimSpace(os.Getenv("BITTERN_CONSOLE"))
	if path == "" {
		return nil, errNoLocalConsole
	}
	console, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, fmt.Errorf("open local console %s: %w", path, err)
	}
	return console, nil
}

func loadSetupToken(stateRoot, configRoot, authState string) (string, error) {
	if authState == "" {
		authState = filepath.Join(stateRoot, "auth.json")
	}
	configured, err := auth.PasswordConfigured(authState)
	if err != nil {
		return "", err
	}
	tokenPath := secretstore.SetupTokenPath(stateRoot)
	if configured {
		_ = secretstore.RemoveSetupTokenFile(tokenPath)
		return "", errors.New("setup is already complete; no setup token is available")
	}

	document, err := secretstore.Load(filepath.Join(configRoot, "secrets.yaml"))
	if err != nil {
		return "", fmt.Errorf("read secrets: %w", err)
	}
	token := strings.TrimSpace(document.SetupToken)
	if token == "" {
		_ = secretstore.RemoveSetupTokenFile(tokenPath)
		return "", errors.New("no setup token is available; first boot may not have run or setup is complete")
	}

	fileToken, readErr := os.ReadFile(tokenPath)
	switch {
	case readErr == nil && strings.TrimSpace(string(fileToken)) == token:
		return token, nil
	case readErr == nil || errors.Is(readErr, os.ErrNotExist):
		if err := secretstore.WriteFileAtomic(tokenPath, []byte(token+"\n"), 0o600); err != nil {
			return "", fmt.Errorf("repair setup token file: %w", err)
		}
		return token, nil
	default:
		return "", fmt.Errorf("read setup token file: %w", readErr)
	}
}

func printFirstBootResult(result firstboot.Result, offline bool) {
	if result.Status == "already-complete" {
		fmt.Printf("first boot already complete at generation %d (%s)\n", result.Generation, result.CompletedAt)
		fmt.Printf("  config: %s\n", result.ConfigPath)
		fmt.Printf("  secrets: %s (values not shown)\n", result.SecretsPath)
		return
	}

	fmt.Printf("first boot complete at generation %d\n", result.Generation)
	fmt.Printf("  config: %s\n", result.ConfigPath)
	fmt.Printf("  secrets: %s (values not shown)\n", result.SecretsPath)
	if result.CertificatePath != "" {
		fmt.Printf("  certificate: %s\n", result.CertificatePath)
		fmt.Printf("  SHA-256 fingerprint: %s\n", result.CertificateFingerprint)
	}
	if result.SetupTokenPath != "" {
		fmt.Printf("  setup token: %s (mode 0600, single use)\n", result.SetupTokenPath)
		fmt.Println("  Show the setup QR on the appliance terminal with: sudo bittern setup-token --qr")
	}
	if result.RecordingConfigured {
		fmt.Printf("  recording: %s (%s)\n", result.RecordingDisk, result.RecordingByID)
	} else {
		fmt.Println("  recording: unconfigured")
	}
	if result.NICRolesConfirmed {
		fmt.Println("  NIC roles: confident")
	} else {
		fmt.Println("  NIC roles: unconfirmed")
	}
	for _, note := range result.Notes {
		fmt.Printf("  note: %s\n", note)
	}
	for _, check := range result.Apply.Checks {
		line := fmt.Sprintf("  check %s: %s", check.Name, check.Status)
		if check.Reason != "" {
			line += " (" + check.Reason + ")"
		}
		fmt.Println(line)
	}
	for _, health := range result.Apply.Health {
		line := fmt.Sprintf("  health %s: %s", health.Name, health.Status)
		if health.Reason != "" {
			line += " (" + health.Reason + ")"
		}
		fmt.Println(line)
	}
	if offline {
		fmt.Println("  mode: offline simulation; systemd units and live network health were not exercised")
	}
}

func printSummary(inventory hardware.Inventory) {
	fmt.Println("Summary:")
	printNICSummary(inventory)
	printDiskSummary(inventory)
	fmt.Printf("  Detector: %s", inventory.Proposal.Detector)
	if reason := inventory.Accelerators.DetectorReason; reason != "" {
		fmt.Printf(" (%s)", reason)
	}
	fmt.Println()
	for _, note := range inventory.Notes {
		fmt.Printf("  Note: %s\n", note)
	}

	fmt.Println()
	fmt.Println(".link match lines:")
	printed := printLinkMatches(inventory)
	if !printed {
		fmt.Println("  (no physical NIC detected)")
	}
}

func printNICSummary(inventory hardware.Inventory) {
	if len(inventory.NICs) == 0 {
		fmt.Println("  NIC roles: unconfirmed (no physical NIC found)")
		return
	}
	if inventory.Proposal.NICRolesConfident {
		fmt.Printf("  NIC roles: confident lan0=%s cam0=%s\n",
			inventory.Proposal.LAN0, inventory.Proposal.CAM0)
		return
	}
	if inventory.Proposal.LAN0 != "" && inventory.Proposal.CAM0 != "" {
		fmt.Printf("  NIC roles: unconfirmed lan0=%s cam0=%s (chosen deterministically; confirm or swap in the wizard)\n",
			inventory.Proposal.LAN0, inventory.Proposal.CAM0)
		return
	}
	if inventory.Proposal.LAN0 != "" {
		fmt.Printf("  NIC roles: unconfirmed lan0=%s cam0=none (one physical NIC)\n",
			inventory.Proposal.LAN0)
		return
	}
	fmt.Println("  NIC roles: unconfirmed (no wired NIC found)")
}

func printDiskSummary(inventory hardware.Inventory) {
	if inventory.Proposal.RecordingDisk == "" {
		fmt.Println("  Recording disk: none")
		printRecordingReason(inventory)
		return
	}
	disk, ok := findDisk(inventory, inventory.Proposal.RecordingDisk)
	if !ok {
		fmt.Printf("  Recording disk: %s\n", inventory.Proposal.RecordingDisk)
		printRecordingReason(inventory)
		return
	}
	byID := disk.ByID
	if byID == "" {
		byID = "not available"
	}
	fmt.Printf("  Recording disk: %s (%s, %s, by-id %s)\n",
		disk.Name, humanBytes(disk.SizeBytes), disk.Transport, byID)
	if inventory.Proposal.RecordingIsPOC {
		fmt.Println("  Recording note: USB candidate is a POC path, not the reference internal-drive path")
	}
	printRecordingReason(inventory)
}

func printRecordingReason(inventory hardware.Inventory) {
	if inventory.Proposal.RecordingDiskConfident {
		return
	}
	reason := inventory.Proposal.RecordingDiskReason
	if reason == "" {
		reason = "unverified candidate, do not format without checking"
	}
	fmt.Printf("  Recording note: %s\n", reason)
}

func printLinkMatches(inventory hardware.Inventory) bool {
	printed := false
	for _, nic := range inventory.NICs {
		role := ""
		switch nic.Name {
		case inventory.Proposal.LAN0:
			role = "lan0"
		case inventory.Proposal.CAM0:
			role = "cam0"
		}
		if role == "" {
			fmt.Printf("# %s: unassigned candidate; choose lan0 or cam0 in the runbook\n", nic.Name)
		} else {
			fmt.Printf("# /etc/systemd/network/10-%s.link\n", role)
		}
		fmt.Println("[Match]")
		fmt.Printf("MACAddress=%s\n", nic.MAC)
		fmt.Println()
		printed = true
	}
	return printed
}

func findDisk(inventory hardware.Inventory, name string) (hardware.Disk, bool) {
	for _, disk := range inventory.Disks {
		if disk.Name == name {
			return disk, true
		}
	}
	return hardware.Disk{}, false
}

func humanBytes(bytes uint64) string {
	const gib = 1 << 30
	if bytes >= gib {
		return fmt.Sprintf("%.1f GiB", float64(bytes)/gib)
	}
	const mib = 1 << 20
	return fmt.Sprintf("%.1f MiB", float64(bytes)/mib)
}
