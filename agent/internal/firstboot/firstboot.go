// Package firstboot provisions a fresh appliance: identity, secrets, the TLS
// certificate, a minimal appliance.yaml, the first rendered generation and the
// first-boot marker.
//
// The marker is written only after apply commits a healthy generation. A rerun
// after success is a no-op. A rerun after a partial run reuses existing
// identity and secrets and lets the apply journal resume or roll back.
package firstboot

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/hardware"
	"bittern.invalid/agent/internal/redact"
	secretstore "bittern.invalid/agent/internal/secrets"
)

const (
	defaultStateRoot  = "/var/lib/bittern"
	defaultConfigRoot = "/etc/bittern"

	statusComplete        = "complete"
	statusAlreadyComplete = "already-complete"

	// firstBootApplyAttempts bounds the first apply retry. A rolled-back apply
	// has already restored the previous generation, so the same transaction can
	// run again from the same live state. A transient unit or interface
	// readiness failure on first boot therefore does not leave the appliance at
	// generation zero waiting for a person.
	firstBootApplyAttempts = 2

	// defaultApplyRetryDelay leaves the failed unit and udev job time to settle
	// before the retry reads the live state again.
	defaultApplyRetryDelay = 5 * time.Second
)

// Options configures first boot. Tests inject Detect, System, Now, Random and
// LANAddress; the CLI uses the production defaults.
type Options struct {
	StateRoot  string
	ConfigRoot string
	LiveRoot   string
	System     apply.System
	Detect     func() hardware.Inventory
	Now        func() time.Time
	Random     io.Reader
	Logger     apply.Logger
	LANAddress func() (string, error)
	Health     []apply.Probe

	// ApplyRetryDelay is the pause before a retry of an apply that rolled back.
	// Zero uses the production default; tests set a short duration.
	ApplyRetryDelay time.Duration

	// BeforeApplyStep is a test seam for power-loss coverage. It does not
	// change production behaviour when nil.
	BeforeApplyStep func(apply.Step) error
}

// Result is the non-secret summary of a first-boot run.
type Result struct {
	Status                 string `json:"status"`
	Generation             int    `json:"generation"`
	IdentityID             string `json:"identity_id,omitempty"`
	ConfigPath             string `json:"config_path"`
	SecretsPath            string `json:"secrets_path"`
	CertificatePath        string `json:"certificate_path,omitempty"`
	CertificateFingerprint string `json:"certificate_fingerprint,omitempty"`
	// SetupTokenPath is the operator convenience file, never the token value.
	SetupTokenPath      string       `json:"setup_token_path,omitempty"`
	RecordingDisk       string       `json:"recording_disk,omitempty"`
	RecordingByID       string       `json:"recording_by_id,omitempty"`
	RecordingConfigured bool         `json:"recording_configured"`
	NICRolesConfirmed   bool         `json:"nic_roles_confirmed"`
	Notes               []string     `json:"notes,omitempty"`
	CompletedAt         string       `json:"completed_at,omitempty"`
	Apply               apply.Result `json:"apply"`
}

type identityRecord struct {
	SchemaVersion int    `json:"schema_version"`
	ID            string `json:"id"`
	Hostname      string `json:"hostname"`
	CreatedAt     string `json:"created_at"`
}

type secretsDocument = secretstore.Document
type secretsIdentity = secretstore.Identity
type secretsTLS = secretstore.TLS

// Marker is the first-boot completion record under the state root.
type Marker struct {
	SchemaVersion       int      `json:"schema_version"`
	ApplianceSchema     int      `json:"appliance_schema_version"`
	Generation          int      `json:"generation"`
	CompletedAt         string   `json:"completed_at"`
	NICRolesConfirmed   bool     `json:"nic_roles_confirmed"`
	RecordingConfigured bool     `json:"recording_configured"`
	Notes               []string `json:"notes,omitempty"`
}

// Run performs first boot or returns the existing marker unchanged.
func Run(options Options) (result Result, err error) {
	options = normalizeOptions(options)
	now := options.Now().UTC()
	configPath := filepath.Join(options.ConfigRoot, "appliance.yaml")
	secretsPath := filepath.Join(options.ConfigRoot, "secrets.yaml")
	certificatePath := filepath.Join(options.ConfigRoot, "tls", "server.crt")

	markerPath := filepath.Join(options.StateRoot, "first-boot.json")
	if marker, ok, err := readMarker(markerPath); err != nil {
		return Result{}, err
	} else if ok {
		result := Result{
			Status:              statusAlreadyComplete,
			Generation:          marker.Generation,
			ConfigPath:          configPath,
			SecretsPath:         secretsPath,
			CertificatePath:     certificatePath,
			SetupTokenPath:      secretstore.SetupTokenPath(options.StateRoot),
			RecordingConfigured: marker.RecordingConfigured,
			NICRolesConfirmed:   marker.NICRolesConfirmed,
			Notes:               append([]string(nil), marker.Notes...),
			CompletedAt:         marker.CompletedAt,
		}
		if data, err := os.ReadFile(filepath.Join(options.StateRoot, "identity.json")); err == nil {
			var identity identityRecord
			if json.Unmarshal(data, &identity) == nil {
				result.IdentityID = identity.ID
			}
		}
		if data, err := os.ReadFile(certificatePath); err == nil {
			if certificate, parseErr := parseCertificate(data); parseErr == nil {
				result.CertificateFingerprint = certificateDetails(certificatePath, certificate).Fingerprint
			}
		}
		return result, nil
	}

	redactor := redact.New()
	if data, readErr := os.ReadFile(configPath); readErr == nil {
		redactor.Add(redact.ValuesFromYAML(data)...)
	}
	if data, readErr := os.ReadFile(secretsPath); readErr == nil {
		redactor.Add(redact.ValuesFromYAML(data)...)
	}
	defer func() {
		if err == nil || errors.Is(err, apply.ErrPowerLoss) {
			return
		}
		err = fmt.Errorf("%s", redactor.String(err.Error()))
	}()
	logger := &safeLogger{target: options.Logger, redactor: redactor}

	inventory := options.Detect()
	notes := make([]string, 0, 3)
	nicRolesConfirmed := inventory.Proposal.NICRolesConfident &&
		inventory.Proposal.LAN0 != "" && inventory.Proposal.CAM0 != ""
	if !nicRolesConfirmed {
		note := "NIC roles are unconfirmed; confirm lan0 and cam0 in the wizard"
		if inventory.Proposal.LAN0 != "" && inventory.Proposal.CAM0 != "" {
			note += fmt.Sprintf(
				" (detected lan0=%s and cam0=%s by PCI address and MAC; swap them if the cabling says otherwise)",
				inventory.Proposal.LAN0, inventory.Proposal.CAM0,
			)
		}
		notes = append(notes, note)
	}
	recordingConfigured := inventory.Proposal.RecordingDiskConfident &&
		inventory.Proposal.RecordingDisk != ""
	if !recordingConfigured {
		notes = append(notes, "No recording drive was selected; configure one in the wizard")
	} else if inventory.Proposal.RecordingIsPOC {
		notes = append(notes, "The recording candidate is USB; treat it as a proof-of-concept path")
	}

	identity, err := loadOrCreateIdentity(options, now, redactor)
	if err != nil {
		return Result{}, err
	}

	cfg, created, err := loadOrCreateConfig(configPath, identity.Hostname, recordingConfigured, inventory, logger)
	if err != nil {
		return Result{}, err
	}
	if created {
		logger.Printf("firstboot: wrote minimal %s", configPath)
	}

	secrets, rotated, err := loadOrCreateSecrets(secretsPath, identity, options.Random, redactor)
	if err != nil {
		return Result{}, err
	}
	setupTokenPath := secretstore.SetupTokenPath(options.StateRoot)
	if err := writeSetupTokenFile(setupTokenPath, secrets.SetupToken); err != nil {
		return Result{}, err
	}
	if rotated {
		logger.Printf("firstboot: generated the admin password, TLS private key and one-time setup token")
	}

	lanAddress := safeLANAddress(options.LANAddress)
	certificate, err := ensureCertificate(
		certificatePath,
		identity,
		secrets,
		cfg.Identity.Hostname,
		lanAddress,
		false,
		options.Random,
		now,
	)
	if err != nil {
		return Result{}, err
	}

	var logValues []string
	if data, readErr := os.ReadFile(configPath); readErr == nil {
		logValues = redact.ValuesFromYAML(data)
		redactor.Add(logValues...)
	}

	applyOptions := apply.Options{
		StateRoot:   options.StateRoot,
		LiveRoot:    options.LiveRoot,
		System:      options.System,
		Health:      options.Health,
		Now:         options.Now,
		Logger:      logger,
		Secrets:     secrets.Values(),
		SecretsPath: secretsPath,
		LogValues:   logValues,
		BeforeStep:  options.BeforeApplyStep,
	}
	applyResult, applyErr := applyFirstBoot(cfg, applyOptions, logger, options.ApplyRetryDelay)
	if applyErr != nil {
		return Result{
			Status:                 applyResult.Status,
			Generation:             applyResult.Generation,
			IdentityID:             identity.ID,
			ConfigPath:             configPath,
			SecretsPath:            secretsPath,
			CertificatePath:        certificate.Path,
			CertificateFingerprint: certificate.Fingerprint,
			SetupTokenPath:         setupTokenPath,
			RecordingDisk:          inventory.Proposal.RecordingDisk,
			RecordingByID:          inventory.Proposal.RecordingByID,
			RecordingConfigured:    recordingConfigured,
			NICRolesConfirmed:      nicRolesConfirmed,
			Notes:                  notes,
			Apply:                  applyResult,
		}, applyErr
	}

	// The certificate is generated before apply so it exists even if apply
	// fails. If lan0 only received its DHCP address during apply, add that
	// address now; afterwards the fingerprint is stable for reruns.
	if certificate.IPAddresses == 0 {
		refreshed := safeLANAddress(options.LANAddress)
		if refreshed != "" {
			certificate, err = ensureCertificate(
				certificatePath,
				identity,
				secrets,
				cfg.Identity.Hostname,
				refreshed,
				true,
				options.Random,
				now,
			)
			if err != nil {
				return Result{}, err
			}
		}
	}

	marker := Marker{
		SchemaVersion:       apply.SchemaVersion,
		ApplianceSchema:     cfg.SchemaVersion,
		Generation:          applyResult.Generation,
		CompletedAt:         now.Format(time.RFC3339),
		NICRolesConfirmed:   nicRolesConfirmed,
		RecordingConfigured: recordingConfigured,
		Notes:               notes,
	}
	if err := writeMarker(markerPath, marker); err != nil {
		return Result{}, err
	}
	logger.Printf("firstboot: complete at generation %d", applyResult.Generation)

	return Result{
		Status:                 statusComplete,
		Generation:             applyResult.Generation,
		IdentityID:             identity.ID,
		ConfigPath:             configPath,
		SecretsPath:            secretsPath,
		CertificatePath:        certificate.Path,
		CertificateFingerprint: certificate.Fingerprint,
		SetupTokenPath:         setupTokenPath,
		RecordingDisk:          inventory.Proposal.RecordingDisk,
		RecordingByID:          inventory.Proposal.RecordingByID,
		RecordingConfigured:    recordingConfigured,
		NICRolesConfirmed:      nicRolesConfirmed,
		Notes:                  notes,
		CompletedAt:            marker.CompletedAt,
		Apply:                  applyResult,
	}, nil
}

// applyFirstBoot runs the first apply and retries once when the transaction
// rolled back. A rollback restores the previous generation and archives its
// evidence before Apply returns, so the retry is a fresh transaction from the
// same live state and cannot resume a half-applied one. A failure before the
// first live step (render, generation or a validation check) is deterministic
// and is not retried; a power loss is not retried either, because its journal
// must be resumed rather than replaced.
func applyFirstBoot(cfg *config.Config, options apply.Options, logger apply.Logger, delay time.Duration) (apply.Result, error) {
	if delay == 0 {
		delay = defaultApplyRetryDelay
	}
	if delay < 0 {
		delay = 0
	}
	result, err := apply.Apply(cfg, options)
	for attempt := 2; err != nil && attempt <= firstBootApplyAttempts; attempt++ {
		if result.Status != apply.StatusRolledBack || errors.Is(err, apply.ErrPowerLoss) {
			break
		}
		logger.Printf(
			"firstboot: apply attempt %d failed at %s and rolled back; retrying in %s: %v",
			attempt-1, result.FailingStep, delay, err,
		)
		time.Sleep(delay)
		result, err = apply.Apply(cfg, options)
	}
	return result, err
}

func normalizeOptions(options Options) Options {
	if options.StateRoot == "" {
		options.StateRoot = defaultStateRoot
	}
	if options.ConfigRoot == "" {
		options.ConfigRoot = defaultConfigRoot
	}
	if options.System == nil {
		options.System = apply.NewExecSystem()
	}
	if options.Detect == nil {
		options.Detect = func() hardware.Inventory { return hardware.Detect(hardware.Roots{}) }
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Random == nil {
		options.Random = rand.Reader
	}
	if options.LANAddress == nil {
		options.LANAddress = currentLANAddress
	}
	if options.Health == nil {
		options.Health = apply.DefaultHealthProbes()
	}
	return options
}

func loadOrCreateIdentity(options Options, now time.Time, redactor *redact.Redactor) (identityRecord, error) {
	path := filepath.Join(options.StateRoot, "identity.json")
	data, err := os.ReadFile(path)
	if err == nil {
		var identity identityRecord
		if err := json.Unmarshal(data, &identity); err != nil {
			return identityRecord{}, fmt.Errorf("firstboot: decode identity: %w", err)
		}
		if identity.SchemaVersion != apply.SchemaVersion || identity.ID == "" {
			return identityRecord{}, fmt.Errorf("firstboot: identity has an unsupported or empty id")
		}
		if identity.Hostname == "" {
			identity.Hostname = "bittern"
		}
		return identity, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return identityRecord{}, err
	}

	id, err := randomUUID(options.Random)
	if err != nil {
		return identityRecord{}, err
	}
	identity := identityRecord{
		SchemaVersion: apply.SchemaVersion,
		ID:            id,
		Hostname:      "bittern",
		CreatedAt:     now.Format(time.RFC3339),
	}
	redactor.Add(identity.ID)
	if err := writeJSON(path, identity, 0o644); err != nil {
		return identityRecord{}, err
	}
	return identity, nil
}

func loadOrCreateConfig(path, hostname string, recordingConfigured bool, inventory hardware.Inventory, logger *safeLogger) (*config.Config, bool, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		cfg, validationErrors := config.Parse(data)
		if len(validationErrors) > 0 {
			for _, validationError := range validationErrors {
				logger.Printf("firstboot: appliance.yaml %s: %s", validationError.Path, validationError.Rule)
			}
			return nil, false, fmt.Errorf("firstboot: existing appliance.yaml is invalid; not overwriting it")
		}
		return cfg, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}

	channel, err := readInstallChannel(filepath.Dir(path))
	if err != nil {
		return nil, false, err
	}
	document := minimalConfig(hostname, recordingConfigured, inventory, channel)
	data, err = document.Marshal()
	if err != nil {
		return nil, false, err
	}
	if err := writeFileAtomic(path, data, 0o644); err != nil {
		return nil, false, err
	}
	cfg, validationErrors := config.Parse(data)
	if len(validationErrors) > 0 {
		return nil, false, fmt.Errorf("firstboot: generated appliance.yaml is invalid: %v", validationErrors)
	}
	return cfg, true, nil
}

func loadOrCreateSecrets(path string, identity identityRecord, random io.Reader, redactor *redact.Redactor) (secretsDocument, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		secrets, generateErr := newSecrets(identity, random)
		if generateErr != nil {
			return secretsDocument{}, false, generateErr
		}
		redactor.Add(secrets.Values()...)
		if err := writeSecrets(path, secrets); err != nil {
			return secretsDocument{}, false, err
		}
		return secrets, true, nil
	}
	if err != nil {
		return secretsDocument{}, false, err
	}

	if info, statErr := os.Stat(path); statErr == nil && info.Mode().Perm() != 0o600 {
		if chmodErr := os.Chmod(path, 0o600); chmodErr != nil {
			return secretsDocument{}, false, chmodErr
		}
	}

	var secrets secretsDocument
	if err := unmarshalYAML(data, &secrets); err != nil {
		return secretsDocument{}, false, fmt.Errorf("firstboot: parse secrets: %w", err)
	}
	if secrets.SchemaVersion != apply.SchemaVersion {
		return secretsDocument{}, false, fmt.Errorf("firstboot: secrets have unsupported schema version %d", secrets.SchemaVersion)
	}

	rotated := false
	if secrets.Identity.ID == "" {
		secrets.Identity.ID = identity.ID
		rotated = true
	}
	if secrets.Identity.ID != identity.ID {
		// A missing or replaced identity is the only event that rotates the
		// generated secrets and certificate.
		secrets.Identity.ID = identity.ID
		password, err := randomPassword(random)
		if err != nil {
			return secretsDocument{}, false, err
		}
		keyPEM, err := newPrivateKeyPEM(random)
		if err != nil {
			return secretsDocument{}, false, err
		}
		secrets.AdminPassword = password
		secrets.TLS.PrivateKeyPEM = keyPEM
		rotated = true
	}
	if secrets.AdminPassword == "" {
		password, err := randomPassword(random)
		if err != nil {
			return secretsDocument{}, false, err
		}
		secrets.AdminPassword = password
		rotated = true
	}
	if secrets.SetupToken == "" {
		token, err := randomSetupToken(random)
		if err != nil {
			return secretsDocument{}, false, err
		}
		secrets.SetupToken = token
		rotated = true
	}
	if secrets.TLS.PrivateKeyPEM == "" {
		keyPEM, err := newPrivateKeyPEM(random)
		if err != nil {
			return secretsDocument{}, false, err
		}
		secrets.TLS.PrivateKeyPEM = keyPEM
		rotated = true
	}
	if _, err := parsePrivateKey(secrets.TLS.PrivateKeyPEM); err != nil {
		return secretsDocument{}, false, fmt.Errorf("firstboot: stored TLS private key is invalid: %w", err)
	}

	redactor.Add(secrets.Values()...)
	if rotated {
		if err := writeSecrets(path, secrets); err != nil {
			return secretsDocument{}, false, err
		}
	}
	return secrets, rotated, nil
}

func newSecrets(identity identityRecord, random io.Reader) (secretsDocument, error) {
	password, err := randomPassword(random)
	if err != nil {
		return secretsDocument{}, err
	}
	setupToken, err := randomSetupToken(random)
	if err != nil {
		return secretsDocument{}, err
	}
	keyPEM, err := newPrivateKeyPEM(random)
	if err != nil {
		return secretsDocument{}, err
	}
	return secretsDocument{
		SchemaVersion: apply.SchemaVersion,
		Identity:      secretsIdentity{ID: identity.ID},
		AdminPassword: password,
		SetupToken:    setupToken,
		TLS:           secretsTLS{PrivateKeyPEM: keyPEM},
	}, nil
}

func writeSecrets(path string, secrets secretsDocument) error {
	return secretstore.Save(path, secrets)
}

func writeSetupTokenFile(path, token string) error {
	if token == "" {
		return secretstore.RemoveSetupTokenFile(path)
	}
	return writeFileAtomic(path, []byte(token+"\n"), 0o600)
}

func readMarker(path string) (Marker, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Marker{}, false, nil
	}
	if err != nil {
		return Marker{}, false, err
	}
	var marker Marker
	if err := json.Unmarshal(data, &marker); err != nil {
		return Marker{}, false, fmt.Errorf("firstboot: decode marker: %w", err)
	}
	if marker.SchemaVersion != apply.SchemaVersion || marker.Generation <= 0 {
		return Marker{}, false, fmt.Errorf("firstboot: first-boot marker is invalid")
	}
	return marker, true, nil
}

func writeMarker(path string, marker Marker) error {
	return writeJSON(path, marker, 0o644)
}

func safeLANAddress(provider func() (string, error)) string {
	address, err := provider()
	if err != nil {
		return ""
	}
	address = strings.TrimSpace(address)
	if net.ParseIP(address) == nil {
		return ""
	}
	return address
}

// CurrentLANAddress returns the first IPv4 address on lan0. The setup QR and
// the certificate SAN both use it.
func CurrentLANAddress() (string, error) {
	return currentLANAddress()
}

func currentLANAddress() (string, error) {
	iface, err := net.InterfaceByName("lan0")
	if err != nil {
		return "", err
	}
	addresses, err := iface.Addrs()
	if err != nil {
		return "", err
	}
	for _, address := range addresses {
		ip, _, err := net.ParseCIDR(address.String())
		if err == nil && ip.To4() != nil {
			return ip.String(), nil
		}
	}
	return "", nil
}

func writeFileAtomic(path string, data []byte, mode fs.FileMode) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(directory, ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	cleanup := func() {
		_ = temp.Close()
		_ = os.Remove(tempName)
	}
	if err := temp.Chmod(mode); err != nil {
		cleanup()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := temp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempName)
		return err
	}
	if err := os.Rename(tempName, path); err != nil {
		_ = os.Remove(tempName)
		return err
	}
	return nil
}

func writeJSON(path string, value any, mode fs.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(path, data, mode)
}

type safeLogger struct {
	target   apply.Logger
	redactor *redact.Redactor
}

func (l *safeLogger) Printf(format string, args ...any) {
	if l.target == nil {
		return
	}
	message := fmt.Sprintf(format, args...)
	l.target.Printf("%s", l.redactor.String(message))
}
