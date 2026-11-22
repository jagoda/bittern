package update

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	defaultStateRoot     = "/var/lib/bittern"
	defaultConfigPath    = "/etc/bittern/appliance.yaml"
	defaultFrigateConfig = "/var/lib/bittern/frigate/config.yml"
	manifestLimit        = 1 << 20
	signatureLimit       = 4 << 10
	defaultMinFreeBytes  = 64 << 20
	defaultHealthTimeout = 5 * time.Minute
)

type paths struct {
	stateRoot     string
	appRoot       string
	current       string
	bundles       string
	staging       string
	updateRoot    string
	lock          string
	journal       string
	state         string
	history       string
	backups       string
	config        string
	frigateConfig string
}

func (o Options) paths() paths {
	root := o.StateRoot
	if root == "" {
		root = defaultStateRoot
	}
	config := o.ConfigPath
	if config == "" {
		config = filepath.Join(root, "..", "..", "etc", "bittern", "appliance.yaml")
		if root == defaultStateRoot {
			config = defaultConfigPath
		}
	}
	frigateConfig := o.FrigateConfig
	if frigateConfig == "" {
		frigateConfig = filepath.Join(root, "frigate", "config.yml")
	}
	return paths{
		stateRoot:     root,
		appRoot:       filepath.Join(root, "app"),
		current:       filepath.Join(root, "app", "current"),
		bundles:       filepath.Join(root, "app", "bundles"),
		staging:       filepath.Join(root, "app", "staging"),
		updateRoot:    filepath.Join(root, "update"),
		lock:          filepath.Join(root, "update", "lock"),
		journal:       filepath.Join(root, "update", "journal.json"),
		state:         filepath.Join(root, "update", "state.json"),
		history:       filepath.Join(root, "update", "history"),
		backups:       filepath.Join(root, "backups"),
		config:        config,
		frigateConfig: frigateConfig,
	}
}

func (p paths) stagingDir(operationID string) string {
	return filepath.Join(p.staging, operationID)
}

func (p paths) bundleDir(generation int64) string {
	return filepath.Join(p.bundles, strconv.FormatInt(generation, 10))
}

func (p paths) bundleFiles(generation int64) string {
	return filepath.Join(p.bundleDir(generation), "files")
}

func (p paths) backupDir(operationID string) string {
	return filepath.Join(p.backups, operationID)
}

func (p paths) pendingConfig(operationID string) string {
	return filepath.Join(p.updateRoot, operationID, "appliance.yaml")
}

func (p paths) historyPath(operationID string) string {
	return filepath.Join(p.history, operationID+".json")
}

func (p paths) ensure() error {
	for _, directory := range []string{
		p.stateRoot,
		p.appRoot,
		p.bundles,
		p.staging,
		p.updateRoot,
		p.history,
		p.backups,
	} {
		mode := fs.FileMode(0o755)
		if directory == p.history || directory == p.backups {
			mode = 0o755
		}
		if err := os.MkdirAll(directory, mode); err != nil {
			return err
		}
		if err := os.Chmod(directory, mode); err != nil {
			return err
		}
	}
	return nil
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
	return fsyncDir(directory)
}

func fsyncDir(path string) error {
	handle, err := os.Open(path)
	if err != nil {
		return err
	}
	err = handle.Sync()
	closeErr := handle.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func writeJSON(path string, value any, mode fs.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(path, data, mode)
}

func readJournal(path string) (*Journal, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var journal Journal
	if err := json.Unmarshal(data, &journal); err != nil {
		return nil, fmt.Errorf("update: decode journal: %w", err)
	}
	if journal.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("update: journal schema version %d is not %d", journal.SchemaVersion, SchemaVersion)
	}
	return &journal, nil
}

func writeJournal(path string, journal *Journal) error {
	return writeJSON(path, journal, 0o600)
}

func removeJournal(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return fsyncDir(filepath.Dir(path))
}

func readState(path string) (State, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{SchemaVersion: SchemaVersion}, false, nil
	}
	if err != nil {
		return State{}, false, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, false, fmt.Errorf("update: decode state: %w", err)
	}
	if state.SchemaVersion != SchemaVersion {
		return State{}, false, fmt.Errorf("update: state schema version %d is not %d", state.SchemaVersion, SchemaVersion)
	}
	return state, true, nil
}

func writeState(path string, state *State) error {
	return writeJSON(path, state, 0o644)
}

func readCurrent(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	text := strings.TrimSpace(string(data))
	generation, err := strconv.ParseInt(text, 10, 64)
	if err != nil || generation < 0 || strconv.FormatInt(generation, 10) != text {
		return 0, fmt.Errorf("update: invalid activation pointer %q", text)
	}
	return generation, nil
}

func writeCurrent(path string, generation int64) error {
	return writeFileAtomic(path, []byte(strconv.FormatInt(generation, 10)+"\n"), 0o644)
}

func (p paths) readBackup(operationID string) (BackupRecord, error) {
	data, err := os.ReadFile(filepath.Join(p.backupDir(operationID), "backup.json"))
	if errors.Is(err, os.ErrNotExist) {
		return BackupRecord{}, nil
	}
	if err != nil {
		return BackupRecord{}, err
	}
	var record struct {
		Path            string `json:"path"`
		Database        string `json:"database"`
		ChecksumsSHA256 string `json:"checksums_sha256"`
		SnapshotSHA256  string `json:"snapshot_sha256"`
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return BackupRecord{}, err
	}
	return BackupRecord{Path: record.Path, Database: record.Database, ChecksumsSHA256: record.ChecksumsSHA256, SnapshotSHA256: record.SnapshotSHA256}, nil
}

func writeBackupRecord(directory string, record BackupRecord) error {
	return writeJSON(filepath.Join(directory, "backup.json"), record, 0o600)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func hashFile(path string) (string, int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	return sha256Hex(data), int64(len(data)), nil
}

func removeAll(path string) error {
	err := os.RemoveAll(path)
	if err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if _, statErr := os.Stat(parent); statErr == nil {
		return fsyncDir(parent)
	}
	return nil
}

func operationID(now time.Time, generation int64) string {
	var random [4]byte
	if _, err := rand.Read(random[:]); err != nil {
		sum := sha256.Sum256([]byte(now.UTC().Format(time.RFC3339Nano)))
		copy(random[:], sum[:4])
	}
	return fmt.Sprintf("%s-g%06d-%s", now.UTC().Format("20060102T150405Z"), generation, hex.EncodeToString(random[:]))
}

func historyFiles(directory string) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

func bundleGenerations(directory string) ([]int64, error) {
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var generations []int64
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		generation, err := strconv.ParseInt(entry.Name(), 10, 64)
		if err == nil && generation > 0 {
			generations = append(generations, generation)
		}
	}
	sort.Slice(generations, func(i, j int) bool { return generations[i] < generations[j] })
	return generations, nil
}
