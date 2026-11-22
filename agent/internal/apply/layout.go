package apply

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"bittern.invalid/agent/internal/render"
)

const (
	defaultStateRoot = "/var/lib/bittern"
	defaultLiveRoot  = "/"
)

func normalizeOptions(options Options) Options {
	if options.StateRoot == "" {
		options.StateRoot = defaultStateRoot
	}
	if options.LiveRoot == "" {
		options.LiveRoot = defaultLiveRoot
	}
	if options.System == nil {
		options.System = NewExecSystem()
	}
	return options
}

func statePaths(root string) (generations, journal, current string) {
	return filepath.Join(root, "generations"), filepath.Join(root, "journal.json"), filepath.Join(root, "current")
}

func generationDir(root string, generation int) string {
	return filepath.Join(root, "generations", strconv.Itoa(generation))
}

func generationFilesDir(root string, generation int) string {
	return filepath.Join(generationDir(root, generation), "files")
}

func generationManifestPath(root string, generation int) string {
	return filepath.Join(generationDir(root, generation), "manifest.json")
}

func readCurrentGeneration(root string) (int, error) {
	data, err := os.ReadFile(filepath.Join(root, "current"))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	generation, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || generation < 0 {
		return 0, fmt.Errorf("apply: invalid current generation %q", strings.TrimSpace(string(data)))
	}
	return generation, nil
}

func nextGeneration(root string) (int, error) {
	latest, err := latestGeneration(root)
	if err != nil {
		return 0, err
	}
	return latest + 1, nil
}

func latestGeneration(root string) (int, error) {
	entries, err := os.ReadDir(filepath.Join(root, "generations"))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	max := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		value, err := strconv.Atoi(entry.Name())
		if err == nil && value > max {
			max = value
		}
	}
	return max, nil
}

func readManifest(root string, generation int) (*Manifest, error) {
	if generation == 0 {
		return &Manifest{SchemaVersion: SchemaVersion, Files: []FileRecord{}}, nil
	}
	data, err := os.ReadFile(generationManifestPath(root, generation))
	if err != nil {
		return nil, fmt.Errorf("apply: read generation %d manifest: %w", generation, err)
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("apply: decode generation %d manifest: %w", generation, err)
	}
	if manifest.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("apply: generation %d has unsupported manifest schema %d", generation, manifest.SchemaVersion)
	}
	return &manifest, nil
}

func writeJSON(path string, value any, mode fs.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(path, data, mode)
}

func writeManifest(root string, manifest *Manifest) error {
	return writeJSON(generationManifestPath(root, manifest.Generation), manifest, 0o644)
}

func writeCurrentGeneration(root string, generation int) error {
	return writeFileAtomic(filepath.Join(root, "current"), []byte(strconv.Itoa(generation)+"\n"), 0o644)
}

func readJournal(root string) (*Journal, error) {
	data, err := os.ReadFile(filepath.Join(root, "journal.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var journal Journal
	if err := json.Unmarshal(data, &journal); err != nil {
		return nil, fmt.Errorf("apply: decode journal: %w", err)
	}
	if journal.SchemaVersion != SchemaVersion {
		return nil, fmt.Errorf("apply: journal has unsupported schema version %d", journal.SchemaVersion)
	}
	return &journal, nil
}

func writeJournal(root string, journal *Journal) error {
	return writeJSON(filepath.Join(root, "journal.json"), journal, 0o600)
}

func removeJournal(root string) error {
	err := os.Remove(filepath.Join(root, "journal.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// rollbackJournalPaths returns the directory and file that retain a completed
// rollback journal. The active journal at <state-root>/journal.json is removed
// so a retry can start a new transaction, while the failed transaction stays
// available for support under <state-root>/apply/.
func rollbackJournalPaths(root string, journal *Journal, now time.Time) (string, string) {
	directory := filepath.Join(root, "apply")
	stamp := now.UTC().Format("20060102T150405Z")
	name := fmt.Sprintf("rollback-%s-g%d.json", stamp, journal.Generation)
	return directory, filepath.Join(directory, name)
}

func archiveJournal(root string, journal *Journal, now time.Time) (string, error) {
	directory, base := rollbackJournalPaths(root, journal, now)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", err
	}
	path := base
	for suffix := 1; ; suffix++ {
		_, err := os.Stat(path)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			return "", err
		}
		path = strings.TrimSuffix(base, ".json") + fmt.Sprintf("-%d.json", suffix)
	}
	if err := writeJSON(path, journal, 0o600); err != nil {
		return "", err
	}
	return path, nil
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
	if directoryHandle, err := os.Open(directory); err == nil {
		_ = directoryHandle.Sync()
		_ = directoryHandle.Close()
	}
	return nil
}

func hashFile(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func safeRelative(value string) (string, error) {
	clean := path.Clean(strings.ReplaceAll(value, "\\", "/"))
	if clean == "." || clean == "" || strings.HasPrefix(clean, "/") || clean == ".." ||
		strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("apply: unsafe relative path %q", value)
	}
	return clean, nil
}

func livePath(root, destination string) (string, error) {
	clean, err := safeRelative(destination)
	if err != nil {
		return "", err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target := filepath.Join(root, filepath.FromSlash(clean))
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("apply: destination %q escapes the live root", destination)
	}
	return target, nil
}

// defaultTarget maps renderer families to their live paths. It deliberately
// fails for a family with no defined destination instead of dropping output.
func defaultTarget(source string) (Target, error) {
	clean, err := safeRelative(source)
	if err != nil {
		return Target{}, err
	}
	base := path.Base(clean)
	switch {
	case strings.HasPrefix(clean, "networkd/"):
		return Target{Source: clean, Destination: "etc/systemd/network/" + base, Unit: "systemd-networkd.service"}, nil
	case clean == "nftables/bittern.nft":
		return Target{Source: clean, Destination: "etc/nftables.conf", Unit: "nftables.service", Check: CheckNft}, nil
	case clean == "dnsmasq/dnsmasq.conf":
		return Target{Source: clean, Destination: "etc/dnsmasq.d/bittern.conf", Unit: "dnsmasq.service", Check: CheckDnsmasq}, nil
	case clean == "dnsmasq/reservations.conf":
		return Target{Source: clean, Destination: "etc/dnsmasq.d/bittern-reservation.conf", Unit: "dnsmasq.service", Check: CheckDnsmasq}, nil
	case clean == "chrony/chrony.conf":
		return Target{Source: clean, Destination: "etc/chrony/chrony.conf", Unit: "chrony.service", Check: CheckChrony}, nil
	case clean == "frigate/config.yml":
		image, err := render.FrigateImageReference()
		if err != nil {
			return Target{}, err
		}
		return Target{Source: clean, Destination: "var/lib/bittern/frigate/config.yml", Unit: "bittern-frigate.service", Check: CheckFrigate, Image: image}, nil
	case clean == "frigate/cameras.env":
		return Target{Source: clean, Destination: "var/lib/bittern/frigate/cameras.env", Unit: "bittern-frigate.service"}, nil
	case clean == "compose/compose.yaml":
		return Target{Source: clean, Destination: "var/lib/bittern/compose/compose.yaml", Unit: "bittern-frigate.service"}, nil
	case clean == "systemd/bittern-frigate.service":
		return Target{Source: clean, Destination: "etc/systemd/system/bittern-frigate.service", Unit: "bittern-frigate.service"}, nil
	default:
		return Target{}, fmt.Errorf("apply: no live destination for rendered file %q", source)
	}
}

func buildTargets(rendered []render.File, extra []InputFile, overrides []Target) ([]Target, error) {
	overrideBySource := make(map[string]Target, len(overrides))
	for _, target := range overrides {
		source, err := safeRelative(target.Source)
		if err != nil {
			return nil, err
		}
		target.Source = source
		overrideBySource[source] = target
	}

	targets := make([]Target, 0, len(rendered)+len(extra))
	seen := make(map[string]bool, len(rendered)+len(extra))
	for _, file := range rendered {
		source, err := safeRelative(file.Path)
		if err != nil {
			return nil, err
		}
		if seen[source] {
			return nil, fmt.Errorf("apply: duplicate generated path %q", source)
		}
		target, ok := overrideBySource[source]
		if !ok {
			target, err = defaultTarget(source)
			if err != nil {
				return nil, err
			}
		}
		target.Source = source
		targets = append(targets, target)
		seen[source] = true
	}
	for _, file := range extra {
		source, err := safeRelative(file.Path)
		if err != nil {
			return nil, err
		}
		if seen[source] {
			return nil, fmt.Errorf("apply: duplicate generated path %q", source)
		}
		destination, err := safeRelative(file.Destination)
		if err != nil {
			return nil, err
		}
		target := Target{
			Source:      source,
			Destination: destination,
			Unit:        file.Unit,
			Check:       file.Check,
		}
		if override, ok := overrideBySource[source]; ok {
			target = override
		}
		targets = append(targets, target)
		seen[source] = true
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Source < targets[j].Source })
	return targets, nil
}

func mergedFiles(rendered []render.File, extra []InputFile) ([]render.File, error) {
	files := make([]render.File, 0, len(rendered)+len(extra))
	seen := make(map[string]bool, len(rendered)+len(extra))
	for _, file := range rendered {
		source, err := safeRelative(file.Path)
		if err != nil {
			return nil, err
		}
		if seen[source] {
			return nil, fmt.Errorf("apply: duplicate generated path %q", source)
		}
		files = append(files, render.File{Path: source, Data: file.Data})
		seen[source] = true
	}
	for _, file := range extra {
		source, err := safeRelative(file.Path)
		if err != nil {
			return nil, err
		}
		if seen[source] {
			return nil, fmt.Errorf("apply: duplicate generated path %q", source)
		}
		files = append(files, render.File{Path: source, Data: file.Data})
		seen[source] = true
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func fileMode(file InputFile) fs.FileMode {
	if file.Mode == 0 {
		return 0o644
	}
	return file.Mode
}
