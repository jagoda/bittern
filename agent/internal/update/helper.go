package update

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/release"
)

// DefaultMediaPath is the product recording mount when appliance.yaml does not
// set one. The Frigate database lives at its root.
const DefaultMediaPath = "/var/lib/bittern/media"

// HelperOptions configures the production helper. Empty fields use the fixed
// production paths. The helper subcommand passes no arguments and no
// environment-derived values; these options exist for tests and for the one
// caller that already owns the fixed paths.
type HelperOptions struct {
	StateRoot        string
	RequestPath      string
	ConfigPath       string
	FrigateConfig    string
	SecretsPath      string
	TrustImageDir    string
	TrustRotationDir string
	PlatformPath     string
	DatabasePath     string
	SQLitePath       string
	DockerPath       string
	BaselineBinary   string

	System        UnitSystem
	Data          DataSnapshot
	Image         ImageEnsurer
	ConfigApplier ConfigApplier
	Platform      PlatformProvider
	Base          BaseCollector
	Now           func() time.Time
	Logger        func(format string, args ...any)
	UID           func() int
}

func (o HelperOptions) normalized() HelperOptions {
	if o.StateRoot == "" {
		o.StateRoot = defaultStateRoot
	}
	if o.RequestPath == "" {
		o.RequestPath = RequestPath(o.StateRoot)
	}
	if o.ConfigPath == "" {
		o.ConfigPath = defaultConfigPath
	}
	if o.FrigateConfig == "" {
		o.FrigateConfig = filepath.Join(o.StateRoot, "frigate", "config.yml")
	}
	if o.SecretsPath == "" {
		o.SecretsPath = "/etc/bittern/secrets.yaml"
	}
	if o.TrustImageDir == "" {
		o.TrustImageDir = release.ImageStorePath
	}
	if o.TrustRotationDir == "" {
		o.TrustRotationDir = release.RotationStorePath
	}
	if o.PlatformPath == "" {
		o.PlatformPath = PlatformMetadataPath
	}
	if o.SQLitePath == "" {
		o.SQLitePath = DefaultSQLitePath
	}
	if o.DockerPath == "" {
		o.DockerPath = DefaultDockerPath
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return o
}

// RunHelper runs exactly one transaction from the fixed request record. It
// takes no arguments and reads no environment value. A final transaction
// removes the request; a transaction with a journal, or a lock contention,
// keeps it so the next start resumes the same work.
func RunHelper(options HelperOptions) (Result, error) {
	options = options.normalized()
	request, err := ReadRequest(options.RequestPath)
	if err != nil {
		return Result{}, err
	}
	p := normalizeOptions(Options{StateRoot: options.StateRoot}).paths()
	journal, err := readJournal(p.journal)
	if err != nil {
		return Result{}, err
	}
	if request.Source == RequestSourceManualRollback {
		return runRollbackHelper(options, p, request, journal)
	}
	sourcePath := request.SourcePath
	if journal != nil {
		if journal.SourcePath == "" {
			return Result{Status: ResultFailed}, errorf("request.source-unknown", "the active journal %s does not record its source path", journal.OperationID)
		}
		sourcePath = journal.SourcePath
	}
	if _, err := validateSourceDirectory(sourcePath); err != nil {
		return Result{}, err
	}

	installOptions := Options{
		StateRoot:        options.StateRoot,
		ConfigPath:       options.ConfigPath,
		FrigateConfig:    options.FrigateConfig,
		Source:           LocalSource{Dir: sourcePath},
		SourcePath:       sourcePath,
		TrustImageDir:    options.TrustImageDir,
		TrustRotationDir: options.TrustRotationDir,
		SecretsPath:      options.SecretsPath,
		Now:              options.Now,
		Logger:           options.Logger,
		ConfigApplier:    options.ConfigApplier,
	}
	if options.System != nil {
		installOptions.System = options.System
	} else {
		installOptions.System = &ExecSystem{Runner: apply.NewExecSystem()}
	}
	if options.Base != nil {
		installOptions.Base = options.Base
	}
	if options.Platform != nil {
		installOptions.Platform = options.Platform
	} else {
		installOptions.Platform = &FilePlatformProvider{Path: options.PlatformPath, UID: options.UID}
	}
	if options.Image != nil {
		installOptions.Image = options.Image
	} else {
		installOptions.Image = &DockerImageEnsurer{Docker: options.DockerPath}
	}
	if options.Data != nil {
		installOptions.Data = options.Data
	} else {
		databasePath, err := resolveDatabasePath(options)
		if err != nil {
			return Result{}, err
		}
		installOptions.Data = &SQLiteSnapshot{LivePath: databasePath, SQLite: options.SQLitePath}
	}

	result, installErr := Install(installOptions)
	return finishHelperRequest(options, p, request, sourcePath, result, installErr)
}

func runRollbackHelper(options HelperOptions, p paths, request Request, journal *Journal) (Result, error) {
	if journal != nil && journal.Phase != PhaseManualRollback {
		return Result{Status: ResultFailed}, errorf("rollback.busy", "an update operation %s is active; finish it before rolling back", journal.OperationID)
	}
	rollbackOptions := Options{
		StateRoot:        options.StateRoot,
		ConfigPath:       options.ConfigPath,
		FrigateConfig:    options.FrigateConfig,
		TrustImageDir:    options.TrustImageDir,
		TrustRotationDir: options.TrustRotationDir,
		SecretsPath:      options.SecretsPath,
		Now:              options.Now,
		Logger:           options.Logger,
		Reason:           request.Reason,
		BaselineBinary:   options.BaselineBinary,
		ConfigApplier:    options.ConfigApplier,
	}
	if options.System != nil {
		rollbackOptions.System = options.System
	} else {
		rollbackOptions.System = &ExecSystem{Runner: apply.NewExecSystem()}
	}
	if options.Base != nil {
		rollbackOptions.Base = options.Base
	}
	result, rollbackErr := Rollback(rollbackOptions)
	return finishHelperRequest(options, p, request, "", result, rollbackErr)
}

func finishHelperRequest(options HelperOptions, p paths, request Request, sourcePath string, result Result, runErr error) (Result, error) {
	journalAfter, readErr := readJournal(p.journal)
	if readErr != nil {
		return result, readErr
	}
	if journalAfter == nil && !errors.Is(runErr, ErrBusy) {
		if err := RemoveRequest(options.RequestPath); err != nil {
			return result, fmt.Errorf("update: remove the request %s: %w", options.RequestPath, err)
		}
		if request.Source == RequestSourceLocalFile {
			removeIncomingSource(options.StateRoot, sourcePath)
		}
	}
	if runErr == nil && journalAfter != nil {
		return result, errorf("helper.journal", "the transaction reported success but the journal %s remains", p.journal)
	}
	return result, runErr
}

func resolveDatabasePath(options HelperOptions) (string, error) {
	if options.DatabasePath != "" {
		return options.DatabasePath, nil
	}
	cfg, _, err := loadConfig(options.ConfigPath)
	if err != nil {
		return "", err
	}
	media := cfg.Storage.Recording.MountPointValue()
	if media == "" {
		media = DefaultMediaPath
	}
	return filepath.Join(media, "frigate.db"), nil
}

// RequestExists reports whether a pending request record is present. It is a
// read-only convenience for the CLI and takes no lock.
func RequestExists(stateRoot string) (bool, error) {
	path := RequestPath(stateRoot)
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}
