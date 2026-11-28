package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"time"

	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/onvif"
	"bittern.invalid/agent/internal/redact"
	"bittern.invalid/agent/internal/secrets"
)

const cameraUsage = `usage: bittern camera probe CAMERA [flags]

Probe one camera's ONVIF path and print a redacted, read-only report. CAMERA is
a camera id in appliance.yaml or an IPv4 address. The probe sends Get
operations only; it never sets the clock or writes camera configuration.

flags:
  --config PATH    appliance.yaml (default /etc/bittern/appliance.yaml)
  --secrets PATH   secrets.yaml (default /etc/bittern/secrets.yaml)
  --account REF    credentials_ref for an address argument
  --endpoint URL   non-standard device service URL; must be the camera address
  --timeout D      per-operation timeout (default 5s)
  --json           print the structured report as JSON
`

func runCamera(args []string) error {
	if len(args) == 0 {
		return errors.New("camera needs a subcommand: probe")
	}
	switch args[0] {
	case "probe":
		return runCameraProbe(args[1:])
	default:
		return fmt.Errorf("unknown camera subcommand %q", args[0])
	}
}

func runCameraProbe(args []string) error {
	flags := flag.NewFlagSet("camera probe", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	flags.Usage = func() {
		fmt.Fprint(os.Stderr, cameraUsage)
	}
	configPath := flags.String("config", "/etc/bittern/appliance.yaml", "appliance.yaml to load")
	secretsPath := flags.String("secrets", "/etc/bittern/secrets.yaml", "secrets.yaml to load")
	accountRef := flags.String("account", "", "credentials_ref for an address argument")
	endpoint := flags.String("endpoint", "", "non-standard device service URL")
	timeout := flags.Duration("timeout", 5*time.Second, "per-operation timeout")
	jsonOutput := flags.Bool("json", false, "print the structured report as JSON")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 1 {
		return errors.New("camera probe needs one camera id or IPv4 address")
	}
	if *timeout <= 0 {
		return errors.New("--timeout must be positive")
	}
	target := strings.TrimSpace(flags.Arg(0))

	address := target
	ref := strings.TrimSpace(*accountRef)
	found := false
	if cfg, validationErrors := config.Load(*configPath); cfg != nil && len(validationErrors) == 0 {
		for _, camera := range cfg.Cameras {
			if camera.ID != target {
				continue
			}
			found = true
			address = camera.Address
			if ref == "" && camera.CredentialsRef != nil {
				ref = strings.TrimSpace(*camera.CredentialsRef)
			}
			break
		}
	}
	parsed, parseErr := netip.ParseAddr(target)
	if parseErr == nil && parsed.Is4() {
		address = parsed.String()
	} else if !found {
		return fmt.Errorf("%q is neither a camera id in %s nor an IPv4 address", target, *configPath)
	}

	account := secrets.CameraAccount{}
	if ref != "" {
		loaded, found, err := secrets.LoadCameraAccount(*secretsPath, ref)
		if err != nil {
			return fmt.Errorf("read the saved camera sign-in: %w", err)
		}
		if !found {
			return fmt.Errorf("no saved camera sign-in named %q in %s", ref, *secretsPath)
		}
		account = loaded
	}
	accounts := probeAccounts(account)
	redactor := redact.New(account.Values()...)
	report := onvif.Diagnose(nil, address, accounts, onvif.ProbeOptions{
		OperationTimeout: *timeout,
		Endpoint:         strings.TrimSpace(*endpoint),
	})
	redactor.Add(report.SerialNumber)

	var output string
	if *jsonOutput {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("render the report: %w", err)
		}
		output = redactor.String(string(data)) + "\n"
	} else {
		output = onvif.RenderDiagnosis(report, redactor)
	}
	for _, value := range append(account.Values(), report.SerialNumber) {
		if value != "" && strings.Contains(output, value) {
			return errors.New("the probe report would contain a credential or serial number; refusing to print it")
		}
	}
	fmt.Print(output)
	if report.SetupError != "" {
		return errors.New("the probe could not start")
	}
	return nil
}

func probeAccounts(account secrets.CameraAccount) []onvif.ProbeAccount {
	accounts := make([]onvif.ProbeAccount, 0, 2)
	rtsp := onvif.Credentials{Username: strings.TrimSpace(account.Username), Password: account.Password}
	if rtsp.Valid() {
		accounts = append(accounts, onvif.ProbeAccount{Label: "rtsp", Credentials: rtsp})
	}
	onvifAccount := onvif.Credentials{
		Username: strings.TrimSpace(account.ONVIFUsername),
		Password: account.ONVIFPassword,
	}
	if onvifAccount.Valid() && (onvifAccount.Username != rtsp.Username || onvifAccount.Password != rtsp.Password) {
		accounts = append(accounts, onvif.ProbeAccount{Label: "onvif", Credentials: onvifAccount})
	}
	return accounts
}
