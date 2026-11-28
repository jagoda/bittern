package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"bittern.invalid/agent/internal/release"
)

var unitDigestRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

const (
	unitsRecordSchemaVersion = 1
	snapshotSchemaVersion    = 1
	bundleUnitSourceDir      = "usr/lib/bittern/units"
	bitternUnitPathPrefix    = "/etc/systemd/system/bittern-bundle-"
	unitVerifyTimeout        = 30 * time.Second
)

// installedUnit is one path the previous bundle installed. The record is the
// ownership rule that stops a bundle overwriting a package or admin unit.
type installedUnit struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	Wants      string `json:"wants"`
	LinkTarget string `json:"link_target"`
	SHA256     string `json:"sha256"`
	Mode       string `json:"mode"`
	Owner      string `json:"owner"`
	Enabled    bool   `json:"enabled"`
	Reload     bool   `json:"reload"`
	Generation int64  `json:"generation"`
}

type unitsRecord struct {
	SchemaVersion int             `json:"schema_version"`
	Units         []installedUnit `json:"units"`
}

// snapshotEntry is the prior state of one path. Type is file, symlink or
// absent; a symlink is recorded as a symlink and never followed.
type snapshotEntry struct {
	Path   string `json:"path"`
	Type   string `json:"type"`
	Mode   string `json:"mode,omitempty"`
	Owner  string `json:"owner,omitempty"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Link   string `json:"link,omitempty"`
	Blob   string `json:"blob,omitempty"`
}

type snapshotIndex struct {
	SchemaVersion int             `json:"schema_version"`
	Paths         []snapshotEntry `json:"paths"`
}

// snapshotSubsystems records every unit path, enablement link, and the
// ownership record before the transaction changes any of them. It runs before
// the stop steps, so an unreadable or unsupported path rejects the transaction
// with no live change.
func (r *runner) snapshotSubsystems() error {
	paths, err := r.subsystemPaths()
	if err != nil {
		return err
	}
	directory := r.paths.snapshotDir(r.journal.OperationID)
	if err := r.clearSnapshot(); err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return errorf("snapshot.write", "create the snapshot directory %s: %v", directory, err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return errorf("snapshot.write", "set the snapshot directory mode: %v", err)
	}
	entries := make([]snapshotEntry, 0, len(paths))
	for _, path := range paths {
		if r.isUnitLivePath(path) {
			if err := verifyUnitAncestors(r.options.LiveRoot, path); err != nil {
				_ = r.clearSnapshot()
				return err
			}
		}
		entry, err := r.recordSnapshotPath(path)
		if err != nil {
			_ = r.clearSnapshot()
			return err
		}
		entries = append(entries, entry)
	}
	index := snapshotIndex{SchemaVersion: snapshotSchemaVersion, Paths: entries}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		_ = r.clearSnapshot()
		return errorf("snapshot.write", "encode the snapshot: %v", err)
	}
	data = append(data, '\n')
	if err := writeFileAtomic(r.paths.snapshotIndex(r.journal.OperationID), data, 0o600); err != nil {
		_ = r.clearSnapshot()
		return errorf("snapshot.write", "write the snapshot index: %v", err)
	}
	if _, err := r.readSnapshot(); err != nil {
		_ = r.clearSnapshot()
		return errorf("snapshot.verify", "read back the snapshot: %v", err)
	}
	return nil
}

// pruneSnapshots removes the committed transaction snapshot and any snapshot
// retained from an earlier rollback. A rollback keeps its snapshot for support
// until the next successful commit reaches this step.
func (r *runner) pruneSnapshots() error {
	entries, err := os.ReadDir(r.paths.updateRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(r.paths.updateRoot, entry.Name())
		files, err := os.ReadDir(directory)
		if err != nil {
			return err
		}
		for _, file := range files {
			if file.Name() != "snapshot.json" && !strings.HasPrefix(file.Name(), "snapshot-blob-") {
				continue
			}
			if err := os.Remove(filepath.Join(directory, file.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}

func (r *runner) clearSnapshot() error {
	directory := r.paths.snapshotDir(r.journal.OperationID)
	entries, err := os.ReadDir(directory)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() != "snapshot.json" && !strings.HasPrefix(entry.Name(), "snapshot-blob-") {
			continue
		}
		if err := os.Remove(filepath.Join(directory, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (r *runner) subsystemPaths() ([]string, error) {
	manifest, err := r.candidateManifest()
	if err != nil {
		return nil, err
	}
	record, err := r.readUnitsRecord()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var paths []string
	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		paths = append(paths, path)
	}
	for _, unit := range subsystemUnits(manifest) {
		add(r.liveUnitPath(unit.Path))
		add(r.liveUnitPath(unitWantsPath(unit)))
	}
	for _, unit := range record.Units {
		add(r.liveUnitPath(unit.Path))
		add(r.liveUnitPath(unit.Wants))
	}
	add(r.paths.unitsRecord())
	sort.Strings(paths)
	return paths, nil
}

func subsystemUnits(manifest *release.Manifest) []release.Unit {
	if manifest == nil || manifest.Subsystems == nil {
		return nil
	}
	return manifest.Subsystems.Units
}

func (r *runner) liveUnitPath(path string) string {
	if path == "" {
		return ""
	}
	clean := filepath.FromSlash(strings.TrimPrefix(path, "/"))
	return filepath.Join(r.options.LiveRoot, clean)
}

func unitWantsTarget(name string) string {
	if strings.HasSuffix(name, ".timer") {
		return "timers.target"
	}
	return "multi-user.target"
}

func unitWantsPath(unit release.Unit) string {
	return "/etc/systemd/system/" + unitWantsTarget(unit.Name) + ".wants/" + unit.Name
}

func unitWantsLink(name string) string {
	return "../" + name
}

func (r *runner) recordSnapshotPath(path string) (snapshotEntry, error) {
	entry := snapshotEntry{Path: path, Type: "absent"}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return entry, nil
	}
	if err != nil {
		return entry, errorf("snapshot.read", "read %s: %v", path, err)
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return entry, errorf("snapshot.read", "read the symlink %s: %v", path, err)
		}
		entry.Type = "symlink"
		entry.Link = target
		entry.Mode = formatMode(info.Mode())
		entry.Owner = formatOwner(info)
		return entry, nil
	case info.Mode().IsRegular():
		if info.Mode().Perm()&0o444 == 0 {
			return entry, errorf("snapshot.read", "%s has no read permission bit", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return entry, errorf("snapshot.read", "read %s: %v", path, err)
		}
		digest := sha256Hex(data)
		blob := r.paths.snapshotBlob(r.journal.OperationID, digest)
		if err := writeFileAtomic(blob, data, 0o600); err != nil {
			return entry, errorf("snapshot.write", "write the snapshot blob for %s: %v", path, err)
		}
		entry.Type = "file"
		entry.Mode = formatMode(info.Mode())
		entry.Owner = formatOwner(info)
		entry.Size = int64(len(data))
		entry.SHA256 = digest
		entry.Blob = filepath.Base(blob)
		return entry, nil
	default:
		return entry, errorf("snapshot.unsupported", "%s is not a regular file, symlink, or absent; it is %s", path, info.Mode())
	}
}

func (r *runner) readSnapshot() (*snapshotIndex, error) {
	data, err := os.ReadFile(r.paths.snapshotIndex(r.journal.OperationID))
	if err != nil {
		return nil, err
	}
	var index snapshotIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, err
	}
	if index.SchemaVersion != snapshotSchemaVersion {
		return nil, fmt.Errorf("snapshot schema version %d is not %d", index.SchemaVersion, snapshotSchemaVersion)
	}
	for _, entry := range index.Paths {
		if entry.Type != "file" {
			continue
		}
		blob := filepath.Join(r.paths.snapshotDir(r.journal.OperationID), entry.Blob)
		data, err := os.ReadFile(blob)
		if err != nil {
			return nil, fmt.Errorf("read the snapshot blob for %s: %w", entry.Path, err)
		}
		if digest := sha256Hex(data); digest != entry.SHA256 {
			return nil, fmt.Errorf("the snapshot blob for %s has digest %s, not %s", entry.Path, digest, entry.SHA256)
		}
	}
	return &index, nil
}

func (r *runner) readUnitsRecord() (unitsRecord, error) {
	record := unitsRecord{SchemaVersion: unitsRecordSchemaVersion, Units: []installedUnit{}}
	path := r.paths.unitsRecord()
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return record, nil
	}
	if err != nil {
		return record, errorf("units.record", "read the unit ownership record: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return record, errorf("units.record", "the unit ownership record is not a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return record, errorf("units.record", "read the unit ownership record: %v", err)
	}
	if err := json.Unmarshal(data, &record); err != nil {
		return record, errorf("units.record", "decode the unit ownership record: %v", err)
	}
	if record.SchemaVersion != unitsRecordSchemaVersion {
		return record, errorf("units.record", "the unit ownership record schema version %d is not %d", record.SchemaVersion, unitsRecordSchemaVersion)
	}
	if record.Units == nil {
		record.Units = []installedUnit{}
	}
	seen := make(map[string]bool, len(record.Units))
	for index, unit := range record.Units {
		if err := validateInstalledUnit(unit); err != nil {
			return record, errorf("units.record", "record entry %d: %v", index, err)
		}
		if seen[unit.Path] {
			return record, errorf("units.record", "record entry %d repeats %s", index, unit.Path)
		}
		seen[unit.Path] = true
	}
	return record, nil
}

func validateInstalledUnit(unit installedUnit) error {
	if !isBundleUnitName(unit.Name) {
		return fmt.Errorf("unit name %q is not a bundle unit name", unit.Name)
	}
	if unit.Path != "/etc/systemd/system/"+unit.Name {
		return fmt.Errorf("unit path %q is not /etc/systemd/system/%s", unit.Path, unit.Name)
	}
	if unit.Wants != "/etc/systemd/system/"+unitWantsTarget(unit.Name)+".wants/"+unit.Name {
		return fmt.Errorf("unit wants path %q is not the declared enablement link", unit.Wants)
	}
	if unit.LinkTarget != "../"+unit.Name {
		return fmt.Errorf("unit link target %q is not the declared relative link", unit.LinkTarget)
	}
	if unit.Mode != "0644" {
		return fmt.Errorf("unit mode %q is not 0644", unit.Mode)
	}
	if !unitDigestRE.MatchString(unit.SHA256) {
		return fmt.Errorf("unit digest is not a lowercase SHA-256")
	}
	if unit.Generation < 0 {
		return fmt.Errorf("unit generation %d is negative", unit.Generation)
	}
	return nil
}

func (r *runner) writeUnitsRecord(units []installedUnit) error {
	if units == nil {
		units = []installedUnit{}
	}
	record := unitsRecord{SchemaVersion: unitsRecordSchemaVersion, Units: units}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(r.paths.unitsRecord(), data, 0o600)
}

// applySubsystems installs the manifest's declared units, removes a unit the
// previous generation installed that this generation no longer declares, and
// verifies the result. Every target is checked against the snapshot before it
// is touched.
func (r *runner) applySubsystems() error {
	manifest, err := r.candidateManifest()
	if err != nil {
		return err
	}
	desired := subsystemUnits(manifest)
	record, err := r.readUnitsRecord()
	if err != nil {
		return err
	}
	snapshot, err := r.readSnapshot()
	if err != nil {
		return errorf("snapshot.read", "read the transaction snapshot: %v", err)
	}
	snapshotByPath := make(map[string]snapshotEntry, len(snapshot.Paths))
	for _, entry := range snapshot.Paths {
		snapshotByPath[entry.Path] = entry
	}
	oldByPath := make(map[string]installedUnit, len(record.Units))
	for _, unit := range record.Units {
		oldByPath[unit.Path] = unit
	}
	desiredByPath := make(map[string]bool, len(desired))
	for _, unit := range desired {
		desiredByPath[unit.Path] = true
	}

	staged := make([]stagedUnit, 0, len(desired))
	for _, unit := range desired {
		if err := validateUnitDeclaration(unit, manifest); err != nil {
			return err
		}
		content, err := r.readUnitSource(unit)
		if err != nil {
			return err
		}
		livePath := r.liveUnitPath(unit.Path)
		wants := r.liveUnitPath(unitWantsPath(unit))
		if err := r.checkSnapshotTarget(snapshotByPath, livePath); err != nil {
			return err
		}
		if err := r.checkSnapshotTarget(snapshotByPath, wants); err != nil {
			return err
		}
		if old, present := oldByPath[unit.Path]; present {
			if err := r.verifyOwnedUnit(old); err != nil {
				return err
			}
		} else if err := r.refuseUnownedTarget(livePath, content, snapshotByPath); err != nil {
			return err
		}
		if err := r.refuseUnownedLink(wants, unitWantsLink(unit.Name), oldByPath[unit.Path], snapshotByPath); err != nil {
			return err
		}
		staged = append(staged, stagedUnit{unit: unit, content: content, path: livePath, wants: wants})
	}
	for _, old := range record.Units {
		if desiredByPath[old.Path] {
			continue
		}
		if err := r.verifyOwnedUnit(old); err != nil {
			return err
		}
	}
	for _, old := range record.Units {
		if old.Enabled {
			if err := r.checkSnapshotTarget(snapshotByPath, r.liveUnitPath(old.Wants)); err != nil {
				return err
			}
		}
	}

	changed := false
	reload := false
	for _, item := range staged {
		path := item.path
		entry := snapshotByPath[path]
		if sameUnitFile(path, item.content, "0644", r.unitOwner()) {
			// A resumed step may already have written this exact file.
		} else {
			if err := r.compareSnapshotNow(entry); err != nil {
				return err
			}
			if err := r.writeUnitFile(path, item.content); err != nil {
				return err
			}
			changed = true
			if unitReload(item.unit) {
				reload = true
			}
		}
		wants := item.wants
		if item.unit.Enabled {
			if sameLink(wants, unitWantsLink(item.unit.Name)) {
				// Already enabled as declared.
			} else {
				if err := r.compareSnapshotNow(snapshotByPath[wants]); err != nil {
					return err
				}
				if err := r.writeUnitLink(wants, unitWantsLink(item.unit.Name)); err != nil {
					return err
				}
				changed = true
				if unitReload(item.unit) {
					reload = true
				}
			}
		} else if _, err := os.Lstat(wants); err == nil {
			if err := r.compareSnapshotNow(snapshotByPath[wants]); err != nil {
				return err
			}
			if err := r.removePath(wants); err != nil {
				return err
			}
			changed = true
			if unitReload(item.unit) {
				reload = true
			}
		}
	}
	for _, old := range record.Units {
		if desiredByPath[old.Path] {
			continue
		}
		path := r.liveUnitPath(old.Path)
		if _, err := os.Lstat(path); err == nil {
			if err := r.compareSnapshotNow(snapshotByPath[path]); err != nil {
				return err
			}
			if err := r.removePath(path); err != nil {
				return err
			}
			changed = true
			reload = true
		}
		if old.Enabled {
			wants := r.liveUnitPath(old.Wants)
			if _, err := os.Lstat(wants); err == nil {
				if err := r.compareSnapshotNow(snapshotByPath[wants]); err != nil {
					return err
				}
				if err := r.removePath(wants); err != nil {
					return err
				}
				changed = true
				reload = true
			}
		}
	}
	if changed && reload {
		manager, ok := r.options.System.(UnitManager)
		if !ok {
			return errorf("units.reload", "the unit system cannot run systemctl daemon-reload")
		}
		if err := manager.DaemonReload(); err != nil {
			return errorf("units.reload", "reload systemd after installing bundle units: %v", err)
		}
	}
	installed := make([]installedUnit, 0, len(staged))
	for _, item := range staged {
		entry := installedUnit{
			Name:       item.unit.Name,
			Path:       item.unit.Path,
			Wants:      unitWantsPath(item.unit),
			LinkTarget: unitWantsLink(item.unit.Name),
			SHA256:     item.unit.SHA256,
			Mode:       item.unit.Mode,
			Owner:      r.unitOwner(),
			Enabled:    item.unit.Enabled,
			Reload:     unitReload(item.unit),
			Generation: r.journal.CandidateGeneration,
		}
		installed = append(installed, entry)
	}
	if err := r.writeUnitsRecord(installed); err != nil {
		return errorf("units.record", "write the unit ownership record: %v", err)
	}
	if err := r.verifyAppliedUnits(staged, record.Units, desiredByPath); err != nil {
		return err
	}
	return nil
}

func unitReload(unit release.Unit) bool {
	return unit.Reload == nil || *unit.Reload
}

func validateUnitDeclaration(unit release.Unit, manifest *release.Manifest) error {
	if !strings.HasPrefix(unit.Path, bitternUnitPathPrefix) || filepath.Base(filepath.Dir(unit.Path)) != "system" {
		return errorf("unit.path", "declared unit path %q is not under %s", unit.Path, bitternUnitPathPrefix)
	}
	if strings.HasSuffix(unit.Path, "/") || strings.Contains(unit.Path, "..") {
		return errorf("unit.path", "declared unit path %q is not a clean Bittern unit path", unit.Path)
	}
	if unit.Path != "/etc/systemd/system/"+unit.Name {
		return errorf("unit.path", "declared unit path %q is not /etc/systemd/system/%s", unit.Path, unit.Name)
	}
	if unit.Mode != "0644" {
		return errorf("unit.mode", "declared unit mode %q is not 0644", unit.Mode)
	}
	if unit.Owner != "root:root" {
		return errorf("unit.owner", "declared unit owner %q is not root:root", unit.Owner)
	}
	if !isBundleUnitName(unit.Name) {
		return errorf("unit.name", "declared unit name %q is not a bittern-bundle service or timer", unit.Name)
	}
	for _, file := range manifest.Files {
		if file.Path != unit.Source {
			continue
		}
		if file.SHA256 != unit.SHA256 || file.Mode != unit.Mode {
			return errorf("unit.source", "declared unit source %s does not match the manifest file digest and mode", unit.Source)
		}
		return nil
	}
	return errorf("unit.source", "declared unit source %s is not in the bundle files", unit.Source)
}

func isBundleUnitName(name string) bool {
	base := strings.TrimPrefix(name, "bittern-bundle-")
	base = strings.TrimSuffix(strings.TrimSuffix(base, ".service"), ".timer")
	if !strings.HasSuffix(name, ".service") && !strings.HasSuffix(name, ".timer") {
		return false
	}
	if base == "" || len(base) > 49 {
		return false
	}
	for index := 0; index < len(base); index++ {
		c := base[index]
		if index == 0 {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') {
				return false
			}
			continue
		}
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

func (r *runner) readUnitSource(unit release.Unit) ([]byte, error) {
	bundle := r.paths.bundleDir(r.journal.CandidateGeneration)
	clean, err := safeBundlePath(unit.Source)
	if err != nil {
		return nil, errorf("unit.source", "%v", err)
	}
	path := filepath.Join(bundle, "files", filepath.FromSlash(clean))
	info, err := os.Lstat(path)
	if err != nil {
		return nil, errorf("unit.source", "read the declared unit source %s: %v", unit.Source, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errorf("unit.source", "the declared unit source %s is not a regular file", unit.Source)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errorf("unit.source", "read the declared unit source %s: %v", unit.Source, err)
	}
	if digest := sha256Hex(data); digest != unit.SHA256 {
		return nil, errorf("unit.source", "the declared unit source %s has digest %s, not %s", unit.Source, digest, unit.SHA256)
	}
	if mode := formatMode(info.Mode()); mode != unit.Mode {
		return nil, errorf("unit.source", "the declared unit source %s has mode %s, not %s", unit.Source, mode, unit.Mode)
	}
	if err := validateUnitContent(unit.Name, data); err != nil {
		return nil, err
	}
	if r.options.UnitVerifier == nil {
		return nil, errorf("unit.verify", "no unit verifier is configured")
	}
	if err := r.options.UnitVerifier.VerifyUnit(unit.Name, data); err != nil {
		return nil, errorf("unit.verify", "systemd-analyze verify refused %s: %v", unit.Name, err)
	}
	return data, nil
}

// validateUnitContent applies the ADR 0036 [Install] rules before the staged
// file reaches systemd-analyze. A unit cannot pull in a second unit through an
// alias, Also, an instance, or a second target.
func validateUnitContent(name string, content []byte) error {
	allowed := "multi-user.target"
	if strings.HasSuffix(name, ".timer") {
		allowed = "timers.target"
	}
	section := ""
	for lineNumber, rawLine := range strings.Split(string(content), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return errorf("unit.syntax", "%s line %d has an unterminated section header", name, lineNumber+1)
			}
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if section != "install" {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "Alias", "Also", "DefaultInstance":
			return errorf("unit.install", "%s uses %s in [Install], which a bundle unit may not use", name, key)
		case "WantedBy":
			for _, target := range strings.Fields(value) {
				if target != allowed {
					return errorf("unit.install", "%s has WantedBy=%s, not %s", name, target, allowed)
				}
			}
		}
	}
	return nil
}

func (r *runner) unitOwner() string {
	return fmt.Sprintf("%d:%d", r.options.UnitOwnerUID, r.options.UnitOwnerGID)
}

func (r *runner) checkSnapshotTarget(entries map[string]snapshotEntry, path string) error {
	entry, present := entries[path]
	if !present {
		return errorf("snapshot.missing", "the snapshot does not record %s", path)
	}
	return r.compareSnapshotNow(entry)
}

func (r *runner) compareSnapshotNow(entry snapshotEntry) error {
	info, err := os.Lstat(entry.Path)
	if errors.Is(err, os.ErrNotExist) {
		if entry.Type != "absent" {
			return errorf("snapshot.changed", "%s was %s at the snapshot and is now absent", entry.Path, entry.Type)
		}
		return nil
	}
	if err != nil {
		return errorf("snapshot.changed", "read %s before a live write: %v", entry.Path, err)
	}
	switch entry.Type {
	case "absent":
		return errorf("snapshot.changed", "%s was absent at the snapshot and now exists", entry.Path)
	case "symlink":
		if info.Mode()&os.ModeSymlink == 0 {
			return errorf("snapshot.changed", "%s was a symlink and is now %s", entry.Path, info.Mode())
		}
		target, err := os.Readlink(entry.Path)
		if err != nil || target != entry.Link {
			return errorf("snapshot.changed", "%s had link target %q and now has %q", entry.Path, entry.Link, target)
		}
		return nil
	case "file":
		if !info.Mode().IsRegular() {
			return errorf("snapshot.changed", "%s was a regular file and is now %s", entry.Path, info.Mode())
		}
		data, err := os.ReadFile(entry.Path)
		if err != nil {
			return errorf("snapshot.changed", "read %s before a live write: %v", entry.Path, err)
		}
		if digest := sha256Hex(data); digest != entry.SHA256 {
			return errorf("snapshot.changed", "%s changed since the snapshot", entry.Path)
		}
		if formatMode(info.Mode()) != entry.Mode || formatOwner(info) != entry.Owner {
			return errorf("snapshot.changed", "%s changed owner or mode since the snapshot", entry.Path)
		}
		return nil
	default:
		return errorf("snapshot.unsupported", "%s has unsupported snapshot type %q", entry.Path, entry.Type)
	}
}

func (r *runner) verifyOwnedUnit(unit installedUnit) error {
	path := r.liveUnitPath(unit.Path)
	info, err := os.Lstat(path)
	if err != nil {
		return errorf("units.tampered", "the ownership record names %s, but it cannot be read: %v", unit.Path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errorf("units.tampered", "%s is not the regular file the ownership record names", unit.Path)
	}
	digest, _, err := hashFile(path)
	if err != nil {
		return errorf("units.tampered", "hash %s: %v", unit.Path, err)
	}
	if digest != unit.SHA256 || formatMode(info.Mode()) != unit.Mode || formatOwner(info) != unit.Owner {
		return errorf("units.tampered", "%s changed digest, mode, or owner since the ownership record", unit.Path)
	}
	if unit.Enabled {
		wants := r.liveUnitPath(unit.Wants)
		if !sameLink(wants, unit.LinkTarget) {
			return errorf("units.tampered", "%s is not enabled as the ownership record names", unit.Path)
		}
	}
	return nil
}

func (r *runner) refuseUnownedTarget(path string, content []byte, entries map[string]snapshotEntry) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errorf("unit.target", "read %s: %v", path, err)
	}
	if entry, ok := entries[path]; ok && entry.Type == "absent" && info.Mode().IsRegular() && sameUnitFile(path, content, "0644", r.unitOwner()) {
		// A cut after a partial install wrote this exact declared file. The
		// snapshot proves it was absent before this transaction.
		return nil
	}
	return errorf("unit.undeclared", "%s exists but is not in the unit ownership record", path)
}

func (r *runner) refuseUnownedLink(path, target string, old installedUnit, entries map[string]snapshotEntry) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return errorf("unit.link", "read %s: %v", path, err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return errorf("unit.link", "%s exists and is not a symlink", path)
	}
	actual, err := os.Readlink(path)
	if err != nil {
		return errorf("unit.link", "read the symlink %s: %v", path, err)
	}
	if old.Path != "" && actual == old.LinkTarget && target == old.LinkTarget {
		return nil
	}
	if entry, ok := entries[path]; ok && entry.Type == "absent" && actual == target {
		return nil
	}
	return errorf("unit.undeclared", "%s exists but is not in the unit ownership record", path)
}

func sameUnitFile(path string, content []byte, mode, owner string) bool {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return false
	}
	if formatMode(info.Mode()) != mode || formatOwner(info) != owner {
		return false
	}
	data, err := os.ReadFile(path)
	return err == nil && string(data) == string(content)
}

func sameLink(path, target string) bool {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	actual, err := os.Readlink(path)
	return err == nil && actual == target
}

func (r *runner) writeUnitFile(path string, content []byte) error {
	directory := filepath.Dir(path)
	if err := ensureSafeDirectory(r.options.LiveRoot, directory); err != nil {
		return err
	}
	temp, err := os.CreateTemp(directory, ".bittern-unit-*")
	if err != nil {
		return errorf("unit.write", "create a temporary unit beside %s: %v", path, err)
	}
	tempName := temp.Name()
	cleanup := func() { _ = temp.Close(); _ = os.Remove(tempName) }
	if err := temp.Chmod(0o644); err != nil {
		cleanup()
		return errorf("unit.write", "set the unit mode: %v", err)
	}
	if err := os.Chown(tempName, r.options.UnitOwnerUID, r.options.UnitOwnerGID); err != nil {
		cleanup()
		return errorf("unit.write", "set the unit owner: %v", err)
	}
	if _, err := temp.Write(content); err != nil {
		cleanup()
		return errorf("unit.write", "write %s: %v", path, err)
	}
	if err := temp.Sync(); err != nil {
		cleanup()
		return errorf("unit.write", "sync %s: %v", path, err)
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(tempName)
		return errorf("unit.write", "close %s: %v", path, err)
	}
	if err := os.Rename(tempName, path); err != nil {
		_ = os.Remove(tempName)
		return errorf("unit.write", "replace %s: %v", path, err)
	}
	return fsyncDir(directory)
}

func (r *runner) writeUnitLink(path, target string) error {
	directory := filepath.Dir(path)
	if err := ensureSafeDirectory(r.options.LiveRoot, directory); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errorf("unit.link", "remove the old link %s: %v", path, err)
	}
	if err := os.Symlink(target, path); err != nil {
		return errorf("unit.link", "create the link %s: %v", path, err)
	}
	return fsyncDir(directory)
}

func (r *runner) removePath(path string) error {
	if err := os.Remove(path); err != nil {
		return errorf("unit.remove", "remove %s: %v", path, err)
	}
	return fsyncDir(filepath.Dir(path))
}

func verifyUnitAncestors(root, path string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errorf("unit.path", "path %s escapes the live root %s", path, root)
	}
	components := strings.Split(relative, string(filepath.Separator))
	current := root
	for _, component := range components[:len(components)-1] {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return errorf("unit.path", "read %s: %v", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errorf("unit.path", "%s is a symlink; refusing to follow it", current)
		}
		if !info.IsDir() {
			return errorf("unit.path", "%s is not a directory", current)
		}
	}
	return nil
}

func ensureSafeDirectory(root, directory string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	directory, err = filepath.Abs(directory)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, directory)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errorf("unit.path", "directory %s escapes the live root %s", directory, root)
	}
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o755); err != nil {
				return errorf("unit.path", "create %s: %v", current, err)
			}
			continue
		}
		if err != nil {
			return errorf("unit.path", "read %s: %v", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errorf("unit.path", "%s is a symlink; refusing to follow it", current)
		}
		if !info.IsDir() {
			return errorf("unit.path", "%s is not a directory", current)
		}
	}
	return nil
}

type stagedUnit struct {
	unit    release.Unit
	content []byte
	path    string
	wants   string
}

func (r *runner) verifyAppliedUnits(staged []stagedUnit, old []installedUnit, desiredByPath map[string]bool) error {
	manager, hasManager := r.options.System.(UnitManager)
	for _, item := range staged {
		info, err := os.Lstat(item.path)
		if err != nil {
			return errorf("units.verify", "the declared unit %s is missing after install: %v", item.unit.Path, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errorf("units.verify", "%s is not a regular file after install", item.unit.Path)
		}
		digest, _, err := hashFile(item.path)
		if err != nil {
			return errorf("units.verify", "hash %s after install: %v", item.unit.Path, err)
		}
		if digest != item.unit.SHA256 || formatMode(info.Mode()) != item.unit.Mode || formatOwner(info) != r.unitOwner() {
			return errorf("units.verify", "%s does not match the declared digest, mode, or owner after install", item.unit.Path)
		}
		enabled := sameLink(item.wants, unitWantsLink(item.unit.Name))
		if enabled != item.unit.Enabled {
			return errorf("units.verify", "%s enabled state is %t, not the declared %t", item.unit.Name, enabled, item.unit.Enabled)
		}
		if hasManager {
			state, err := manager.UnitEnabled(item.unit.Name)
			if err != nil {
				return errorf("units.verify", "read the enabled state of %s: %v", item.unit.Name, err)
			}
			if state != item.unit.Enabled {
				return errorf("units.verify", "systemctl is-enabled %s reports %t, not the declared %t", item.unit.Name, state, item.unit.Enabled)
			}
		}
	}
	for _, unit := range old {
		if desiredByPath[unit.Path] {
			continue
		}
		if _, err := os.Lstat(r.liveUnitPath(unit.Path)); !errors.Is(err, os.ErrNotExist) {
			return errorf("units.verify", "the removed unit %s is still present", unit.Path)
		}
		if unit.Enabled {
			if _, err := os.Lstat(r.liveUnitPath(unit.Wants)); !errors.Is(err, os.ErrNotExist) {
				return errorf("units.verify", "the removed unit %s still has an enablement link", unit.Path)
			}
		}
	}
	return nil
}

func (r *runner) rollbackRestoreSubsystems() error {
	index, err := r.readSnapshot()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return errorf("rollback.subsystems", "read the transaction snapshot: %v", err)
	}
	unitChanged := false
	for position := len(index.Paths) - 1; position >= 0; position-- {
		entry := index.Paths[position]
		liveUnit := r.isUnitLivePath(entry.Path)
		switch entry.Type {
		case "absent":
			if _, err := os.Lstat(entry.Path); errors.Is(err, os.ErrNotExist) {
				continue
			} else if err != nil {
				return errorf("rollback.subsystems", "read %s: %v", entry.Path, err)
			}
			if err := r.removePath(entry.Path); err != nil {
				return errorf("rollback.subsystems", "%v", err)
			}
			unitChanged = unitChanged || liveUnit
		case "file":
			data, err := os.ReadFile(filepath.Join(r.paths.snapshotDir(r.journal.OperationID), entry.Blob))
			if err != nil {
				return errorf("rollback.subsystems", "read the snapshot blob for %s: %v", entry.Path, err)
			}
			if digest := sha256Hex(data); digest != entry.SHA256 {
				return errorf("rollback.subsystems", "the snapshot blob for %s has digest %s, not %s", entry.Path, digest, entry.SHA256)
			}
			if sameUnitFile(entry.Path, data, entry.Mode, entry.Owner) {
				continue
			}
			if err := ensureSafeDirectory(r.options.LiveRoot, filepath.Dir(entry.Path)); err != nil {
				return errorf("rollback.subsystems", "%v", err)
			}
			if err := writeRestoredFile(entry.Path, data, entry.Mode, entry.Owner); err != nil {
				return errorf("rollback.subsystems", "%v", err)
			}
			unitChanged = unitChanged || liveUnit
		case "symlink":
			if sameLink(entry.Path, entry.Link) {
				continue
			}
			if err := ensureSafeDirectory(r.options.LiveRoot, filepath.Dir(entry.Path)); err != nil {
				return errorf("rollback.subsystems", "%v", err)
			}
			_ = os.Remove(entry.Path)
			if err := os.Symlink(entry.Link, entry.Path); err != nil {
				return errorf("rollback.subsystems", "restore the symlink %s: %v", entry.Path, err)
			}
			if err := fsyncDir(filepath.Dir(entry.Path)); err != nil {
				return errorf("rollback.subsystems", "%v", err)
			}
			unitChanged = unitChanged || liveUnit
		default:
			return errorf("rollback.subsystems", "%s has unsupported snapshot type %q", entry.Path, entry.Type)
		}
	}
	if unitChanged {
		if manager, ok := r.options.System.(UnitManager); ok {
			if err := manager.DaemonReload(); err != nil {
				return errorf("rollback.subsystems", "reload systemd after restoring units: %v", err)
			}
		} else {
			return errorf("rollback.subsystems", "the unit system cannot run systemctl daemon-reload")
		}
	}
	for _, entry := range index.Paths {
		if err := verifySnapshotEntry(entry); err != nil {
			return errorf("rollback.subsystems", "%v", err)
		}
	}
	return nil
}

func (r *runner) isUnitLivePath(path string) bool {
	root := filepath.Clean(r.liveUnitPath("/etc/systemd/system"))
	clean := filepath.Clean(path)
	relative, err := filepath.Rel(root, clean)
	return err == nil && relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func verifySnapshotEntry(entry snapshotEntry) error {
	info, err := os.Lstat(entry.Path)
	if entry.Type == "absent" {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read %s after rollback: %w", entry.Path, err)
		}
		return fmt.Errorf("%s should be absent after rollback", entry.Path)
	}
	if err != nil {
		return fmt.Errorf("read %s after rollback: %w", entry.Path, err)
	}
	switch entry.Type {
	case "file":
		if !info.Mode().IsRegular() || formatMode(info.Mode()) != entry.Mode || formatOwner(info) != entry.Owner {
			return fmt.Errorf("%s does not match the snapshot file mode or owner after rollback", entry.Path)
		}
		digest, _, err := hashFile(entry.Path)
		if err != nil || digest != entry.SHA256 {
			return fmt.Errorf("%s does not match the snapshot digest after rollback", entry.Path)
		}
	case "symlink":
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("%s is not a symlink after rollback", entry.Path)
		}
		target, err := os.Readlink(entry.Path)
		if err != nil || target != entry.Link {
			return fmt.Errorf("%s does not match the snapshot link after rollback", entry.Path)
		}
	}
	return nil
}

func writeRestoredFile(path string, data []byte, mode, owner string) error {
	uid, gid, err := parseOwner(owner)
	if err != nil {
		return err
	}
	perm, err := strconv.ParseUint(mode, 8, 32)
	if err != nil {
		return fmt.Errorf("snapshot mode %q is not octal", mode)
	}
	directory := filepath.Dir(path)
	temp, err := os.CreateTemp(directory, ".bittern-restore-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	cleanup := func() { _ = temp.Close(); _ = os.Remove(tempName) }
	if err := temp.Chmod(fs.FileMode(perm)); err != nil {
		cleanup()
		return err
	}
	if err := os.Chown(tempName, uid, gid); err != nil {
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

func formatMode(mode fs.FileMode) string { return fmt.Sprintf("%04o", mode.Perm()) }

func formatOwner(info fs.FileInfo) string {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "0:0"
	}
	return fmt.Sprintf("%d:%d", stat.Uid, stat.Gid)
}

func parseOwner(value string) (int, int, error) {
	uidText, gidText, ok := strings.Cut(value, ":")
	if !ok {
		return 0, 0, fmt.Errorf("owner %q is not uid:gid", value)
	}
	uid, err := strconv.Atoi(uidText)
	if err != nil {
		return 0, 0, fmt.Errorf("owner %q has an invalid uid", value)
	}
	gid, err := strconv.Atoi(gidText)
	if err != nil {
		return 0, 0, fmt.Errorf("owner %q has an invalid gid", value)
	}
	return uid, gid, nil
}

// ExecUnitVerifier is the production unit check. It stages the file in a
// temporary directory and refuses it unless systemd-analyze verify accepts it.
type ExecUnitVerifier struct{}

func (ExecUnitVerifier) VerifyUnit(name string, content []byte) error {
	binary, err := exec.LookPath("systemd-analyze")
	if err != nil {
		return fmt.Errorf("systemd-analyze is not installed")
	}
	directory, err := os.MkdirTemp("", "bittern-unit-verify-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), unitVerifyTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, binary, "verify", path).CombinedOutput()
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("%s", detail)
	}
	return nil
}
