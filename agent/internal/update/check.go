package update

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"bittern.invalid/agent/internal/assets"
	"bittern.invalid/agent/internal/redact"
	"bittern.invalid/agent/internal/release"
	"gopkg.in/yaml.v3"
)

// CheckSchemaVersion is the on-disk channel check record schema version.
const CheckSchemaVersion = 1

const (
	checkFileName        = "check.json"
	defaultCheckInterval = 15 * time.Minute
)

// Check result values. A failure never reports up-to-date.
const (
	CheckNever           = "never"
	CheckUpdateAvailable = "update-available"
	CheckUpToDate        = "up-to-date"
	CheckRateLimited     = "rate-limited"
	CheckOffline         = "offline"
	CheckFailed          = "failed"
)

// CheckCandidate is a verified release candidate. The asset URLs come from the
// GitHub release response and are not a trust input; install fetches and
// verifies the manifest and signature again.
type CheckCandidate struct {
	Tag            string `json:"tag"`
	Generation     int64  `json:"generation"`
	Version        string `json:"version"`
	ManifestSHA256 string `json:"manifest_sha256"`
	SigningKeyID   string `json:"signing_key_id"`
	SourceCommit   string `json:"source_commit"`
	ManifestURL    string `json:"manifest_url,omitempty"`
	SignatureURL   string `json:"signature_url,omitempty"`
	PayloadURL     string `json:"payload_url,omitempty"`
	PayloadSize    int64  `json:"payload_size,omitempty"`
}

// Check is the persisted channel check fact. It contains no secret and no raw
// GitHub response. The record is status and install input, not trust input.
type Check struct {
	SchemaVersion int             `json:"schema_version"`
	Channel       string          `json:"channel"`
	CheckedAt     string          `json:"checked_at"`
	Result        string          `json:"result"`
	Reason        string          `json:"reason,omitempty"`
	Available     *CheckCandidate `json:"available,omitempty"`
	Cached        bool            `json:"cached,omitempty"`
}

// CheckResult is the outcome of one check call. Cached is true when the
// minimum interval returned the stored record without a network request.
type CheckResult struct {
	Check
	Cached bool `json:"cached"`
}

// CheckOptions configures an on-demand channel check. Empty fields use the
// product defaults and fixed production paths.
type CheckOptions struct {
	StateRoot        string
	ConfigPath       string
	TrustImageDir    string
	TrustRotationDir string
	PlatformPath     string
	APIURL           string
	Client           *http.Client
	Platform         PlatformProvider
	Now              func() time.Time
	MinInterval      time.Duration
	Force            bool
	AllowHTTP        bool
	UID              func() int
	// FreeSpace is a test seam for the preflight. Nil reads the real filesystem.
	FreeSpace func(string) (uint64, error)
}

func normalizeCheckOptions(options CheckOptions) CheckOptions {
	if options.StateRoot == "" {
		options.StateRoot = defaultStateRoot
	}
	if options.ConfigPath == "" {
		options.ConfigPath = defaultConfigPath
	}
	if options.TrustImageDir == "" {
		options.TrustImageDir = release.ImageStorePath
	}
	if options.TrustRotationDir == "" {
		options.TrustRotationDir = release.RotationStorePath
	}
	if options.PlatformPath == "" {
		options.PlatformPath = PlatformMetadataPath
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.MinInterval <= 0 {
		options.MinInterval = defaultMinimumCheckInterval()
	}
	return options
}

// CheckPath returns the check record path for a state root.
func CheckPath(stateRoot string) string {
	if stateRoot == "" {
		stateRoot = defaultStateRoot
	}
	return filepath.Join(stateRoot, "update", checkFileName)
}

// ReadCheck reads the last check record. A missing record is an explicit
// never-checked value, not an error.
func ReadCheck(stateRoot string) (Check, bool, error) {
	data, err := os.ReadFile(CheckPath(stateRoot))
	if errors.Is(err, os.ErrNotExist) {
		return Check{SchemaVersion: CheckSchemaVersion, Result: CheckNever}, false, nil
	}
	if err != nil {
		return Check{SchemaVersion: CheckSchemaVersion, Result: CheckFailed}, false, err
	}
	var check Check
	if err := json.Unmarshal(data, &check); err != nil {
		return Check{SchemaVersion: CheckSchemaVersion, Result: CheckFailed}, false, err
	}
	if check.SchemaVersion != CheckSchemaVersion {
		return Check{SchemaVersion: CheckSchemaVersion, Result: CheckFailed}, false, fmt.Errorf("update: check schema version %d is not %d", check.SchemaVersion, CheckSchemaVersion)
	}
	if check.Result == "" {
		check.Result = CheckNever
	}
	return check, true, nil
}

func writeCheck(stateRoot string, check Check) error {
	check.SchemaVersion = CheckSchemaVersion
	data, err := json.MarshalIndent(check, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(CheckPath(stateRoot), data, 0o644)
}

// CheckChannel performs one on-demand check against the device channel. It
// never takes the transaction lock, never writes a request or journal, and
// never installs anything.
func CheckChannel(options CheckOptions) (CheckResult, error) {
	options = normalizeCheckOptions(options)
	now := options.Now().UTC()
	cfg, _, err := loadConfig(options.ConfigPath)
	if err != nil {
		return CheckResult{}, err
	}
	channel := cfg.Updates.Channel
	state, _, err := readState(normalizeOptions(Options{StateRoot: options.StateRoot}).paths().state)
	if err != nil {
		return CheckResult{}, err
	}
	committed := state.CommittedGeneration

	previous, present, err := ReadCheck(options.StateRoot)
	if err != nil {
		return CheckResult{}, err
	}
	if present && !options.Force && checkIsFresh(previous, now, options.MinInterval) {
		cached := previous
		cached.Cached = true
		if cached.Channel == channel {
			result := CheckResult{Check: cached, Cached: true}
			if err := writeCheck(options.StateRoot, cached); err != nil {
				return result, errorf("check.record", "write the cached check record: %v", err)
			}
			if cached.Result == CheckUpdateAvailable || cached.Result == CheckUpToDate {
				return result, nil
			}
			return result, checkFailure(cached.Result, "check.cached", "the last check at %s returned %s: %s", cached.CheckedAt, cached.Result, cached.Reason)
		}
	}

	check := Check{
		SchemaVersion: CheckSchemaVersion,
		Channel:       channel,
		CheckedAt:     now.Format(time.RFC3339),
		Result:        CheckFailed,
	}
	client := options.httpClient()
	platform, platformErr := options.platform()
	if platformErr != nil {
		return finishCheck(options, check, checkFailure(CheckFailed, "platform.read", "%v", platformErr))
	}
	candidate, result, reason, err := discoverCandidate(client, options, channel, committed, platform, int64(cfg.SchemaVersion))
	check.Result = result
	check.Reason = redactReason(reason)
	check.Available = candidate
	if err != nil {
		return finishCheck(options, check, err)
	}
	return finishCheck(options, check, nil)
}

func finishCheck(options CheckOptions, check Check, checkErr error) (CheckResult, error) {
	if err := writeCheck(options.StateRoot, check); err != nil {
		if checkErr != nil {
			return CheckResult{Check: check}, errors.Join(checkErr, errorf("check.record", "write the check record: %v", err))
		}
		return CheckResult{Check: check}, errorf("check.record", "write the check record: %v", err)
	}
	return CheckResult{Check: check}, checkErr
}

func checkIsFresh(check Check, now time.Time, interval time.Duration) bool {
	checkedAt, err := time.Parse(time.RFC3339, check.CheckedAt)
	if err != nil {
		return false
	}
	if now.Before(checkedAt) {
		return false
	}
	return now.Sub(checkedAt) < interval
}

func redactReason(reason string) string {
	if reason == "" {
		return ""
	}
	return redact.New().String(reason)
}

type releaseDefaults struct {
	SchemaVersion int `yaml:"schema_version"`
	Release       struct {
		BaseURL              string `yaml:"base_url"`
		APIURL               string `yaml:"api_url"`
		CheckInterval        string `yaml:"check_interval"`
		MinimumCheckInterval string `yaml:"minimum_check_interval"`
	} `yaml:"release"`
}

var loadReleaseDefaults = sync.OnceValues(func() (releaseDefaults, error) {
	data, err := assets.Defaults.ReadFile("defaults/release.yaml")
	if err != nil {
		return releaseDefaults{}, fmt.Errorf("update: read defaults/release.yaml: %w", err)
	}
	var defaults releaseDefaults
	if err := yaml.Unmarshal(data, &defaults); err != nil {
		return releaseDefaults{}, fmt.Errorf("update: decode defaults/release.yaml: %w", err)
	}
	if defaults.SchemaVersion != 1 {
		return releaseDefaults{}, fmt.Errorf("update: defaults/release.yaml schema version %d is not 1", defaults.SchemaVersion)
	}
	if defaults.Release.APIURL == "" {
		return releaseDefaults{}, fmt.Errorf("update: defaults/release.yaml has no release.api_url")
	}
	return defaults, nil
})

func defaultMinimumCheckInterval() time.Duration {
	defaults, err := loadReleaseDefaults()
	if err != nil {
		return defaultCheckInterval
	}
	duration, err := time.ParseDuration(defaults.Release.MinimumCheckInterval)
	if err != nil || duration <= 0 {
		return defaultCheckInterval
	}
	return duration
}

func defaultCheckAPIURL() (string, error) {
	defaults, err := loadReleaseDefaults()
	if err != nil {
		return "", err
	}
	return defaults.Release.APIURL, nil
}
