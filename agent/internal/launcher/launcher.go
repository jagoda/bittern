// Package launcher selects the agent binary to exec from the active app bundle
// and falls back to the installed baseline binary when no bundle is active.
//
// The launcher is installed at a fixed root-owned path outside /var/lib/bittern
// and is the only ExecStart of bitternd.service. It never writes anything. A
// bundle binary is used only when the activation pointer names a generation, the
// resulting path stays under that generation's files tree, and the file is a
// regular root-owned executable. A missing or unreadable pointer is the
// documented fallback to the baseline; a present but malformed pointer is a
// refusal, not a silent fallback.
package launcher

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	// DefaultStateRoot is the shared app root from ADR 0030.
	DefaultStateRoot = "/var/lib/bittern"
	// DefaultPointer is the activation pointer written by the update engine.
	DefaultPointer = "/var/lib/bittern/app/current"
	// DefaultBaseline is the binary the preparation kit installs under /usr.
	DefaultBaseline = "/usr/bin/bitternd"
	// DefaultAgentPath is the agent's path inside a bundle files tree.
	DefaultAgentPath = "usr/bin/bitternd"
	// DefaultHelperBaseline is the operator CLI the preparation kit installs.
	DefaultHelperBaseline = "/usr/bin/bittern"
	// DefaultHelperAgentPath is the operator CLI's path inside a bundle files
	// tree.
	DefaultHelperAgentPath = "usr/bin/bittern"
	// DefaultHelperLauncherName is the installed name that selects the helper
	// profile. The unit starts /usr/libexec/bittern/bittern-update.
	DefaultHelperLauncherName = "bittern-update"
)

// Config is the launcher's fixed path set. The production binary uses
// DefaultConfig; tests supply a temporary root and inject the expected owner.
type Config struct {
	StateRoot string
	Pointer   string
	Baseline  string
	// AgentPath is relative to the active bundle's files tree. It must be a
	// local path; an absolute path or one containing .. is rejected.
	AgentPath string
	// UID returns the expected owner of the bundle and baseline binaries. It
	// defaults to 0, the production root owner.
	UID func() int
}

// DefaultConfig returns the fixed production paths.
func DefaultConfig() Config {
	return Config{
		StateRoot: DefaultStateRoot,
		Pointer:   DefaultPointer,
		Baseline:  DefaultBaseline,
		AgentPath: DefaultAgentPath,
		UID:       func() int { return 0 },
	}
}

// HelperConfig returns the fixed launcher config for the fixed update helper
// unit. It resolves the bundle's usr/bin/bittern and falls back to the
// /usr/bin/bittern baseline. The unit passes only the fixed "update helper"
// arguments; the launcher adds no path of its own.
func HelperConfig() Config {
	return Config{
		StateRoot: DefaultStateRoot,
		Pointer:   DefaultPointer,
		Baseline:  DefaultHelperBaseline,
		AgentPath: DefaultHelperAgentPath,
		UID:       func() int { return 0 },
	}
}

// ConfigForName selects the launcher profile from the installed executable
// name. The preparation kit installs one launcher binary under two fixed
// names: /usr/libexec/bittern/bitternd for the agent and
// /usr/libexec/bittern/bittern-update for the update helper. Every other name
// keeps the agent profile, so running the kit's bittern-launch directly does
// what it did before.
func ConfigForName(name string) Config {
	if filepath.Base(name) == DefaultHelperLauncherName {
		return HelperConfig()
	}
	return DefaultConfig()
}

// Choice is the resolved exec target. Baseline is true when no active bundle
// exists or the pointer could not be read.
type Choice struct {
	Path       string
	Generation int64
	Baseline   bool
}

// Resolve returns the binary to exec. It never executes anything.
func Resolve(config Config) (Choice, error) {
	config = withDefaults(config)
	uid := config.UID()

	if err := requireAbsoluteClean("state root", config.StateRoot); err != nil {
		return Choice{}, err
	}
	if err := requireAbsoluteClean("activation pointer", config.Pointer); err != nil {
		return Choice{}, err
	}
	if !within(config.StateRoot, config.Pointer) {
		return Choice{}, errorf("launcher.path", "the activation pointer %s is outside the state root %s", config.Pointer, config.StateRoot)
	}
	if err := requireAbsoluteClean("baseline binary", config.Baseline); err != nil {
		return Choice{}, err
	}
	if !filepath.IsLocal(config.AgentPath) {
		return Choice{}, errorf("launcher.path", "the agent path %q is not a local path inside the bundle", config.AgentPath)
	}

	data, err := os.ReadFile(config.Pointer)
	if errors.Is(err, fs.ErrNotExist) {
		return baselineChoice(config, uid)
	}
	if err != nil {
		// A pointer the launcher cannot read is documented as the baseline
		// fallback so a permission or transient read failure cannot stop the
		// agent from starting.
		return baselineChoice(config, uid)
	}
	text := strings.TrimSpace(string(data))
	if text == "" || text == "0" {
		return baselineChoice(config, uid)
	}
	generation, err := parseGeneration(text)
	if err != nil {
		return Choice{}, errorf("launcher.pointer", "the activation pointer %s holds %q, which is not a generation", config.Pointer, text)
	}

	bundleRoot := filepath.Join(config.StateRoot, "app", "bundles", strconv.FormatInt(generation, 10), "files")
	if !within(config.StateRoot, bundleRoot) {
		return Choice{}, errorf("launcher.path", "the bundle root %s escapes the state root %s", bundleRoot, config.StateRoot)
	}
	candidate := filepath.Join(bundleRoot, filepath.FromSlash(config.AgentPath))
	if !within(bundleRoot, candidate) {
		return Choice{}, errorf("launcher.path", "the bundle binary %s escapes the bundle root %s", candidate, bundleRoot)
	}
	if err := checkBinary(candidate, uid, "bundle binary"); err != nil {
		return Choice{}, fmt.Errorf("launcher.bundle: generation %d: %w", generation, err)
	}
	return Choice{Path: candidate, Generation: generation}, nil
}

// RunInstalled selects the launcher profile from the installed executable name
// and runs it. It returns only on error.
func RunInstalled(name string, args []string) error {
	return Run(ConfigForName(name), args)
}

// Run resolves and execs the selected binary. It returns only on error.
func Run(config Config, args []string) error {
	choice, err := Resolve(config)
	if err != nil {
		return err
	}
	argv := append([]string{choice.Path}, args...)
	return syscall.Exec(choice.Path, argv, os.Environ())
}

func withDefaults(config Config) Config {
	if config.StateRoot == "" {
		config.StateRoot = DefaultStateRoot
	}
	if config.Pointer == "" {
		config.Pointer = filepath.Join(config.StateRoot, "app", "current")
	}
	if config.Baseline == "" {
		config.Baseline = DefaultBaseline
	}
	if config.AgentPath == "" {
		config.AgentPath = DefaultAgentPath
	}
	if config.UID == nil {
		config.UID = func() int { return 0 }
	}
	return config
}

func baselineChoice(config Config, uid int) (Choice, error) {
	if err := checkBinary(config.Baseline, uid, "baseline binary"); err != nil {
		return Choice{}, fmt.Errorf("launcher.baseline: %w", err)
	}
	return Choice{Path: config.Baseline, Baseline: true}, nil
}

func checkBinary(path string, uid int, label string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return errorf("launcher.missing", "the %s %s is missing", label, path)
	}
	if err != nil {
		return errorf("launcher.stat", "inspect the %s %s: %v", label, path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errorf("launcher.symlink", "the %s %s is a symlink", label, path)
	}
	if !info.Mode().IsRegular() {
		return errorf("launcher.type", "the %s %s is not a regular file", label, path)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return errorf("launcher.mode", "the %s %s is not executable", label, path)
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != uid {
		return errorf("launcher.owner", "the %s %s has owner uid %d, not %d", label, path, stat.Uid, uid)
	}
	return nil
}

func parseGeneration(text string) (int64, error) {
	generation, err := strconv.ParseInt(text, 10, 64)
	if err != nil || generation <= 0 || strconv.FormatInt(generation, 10) != text {
		return 0, errors.New("invalid generation")
	}
	return generation, nil
}

func requireAbsoluteClean(label, path string) error {
	if !filepath.IsAbs(path) {
		return errorf("launcher.path", "the %s %q is not absolute", label, path)
	}
	if filepath.Clean(path) != path {
		return errorf("launcher.path", "the %s %q is not a clean path", label, path)
	}
	return nil
}

func within(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	if relative == "." {
		return true
	}
	if filepath.IsAbs(relative) || relative == ".." {
		return false
	}
	return !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func errorf(rule, format string, args ...any) error {
	return fmt.Errorf("%s: %s", rule, fmt.Sprintf(format, args...))
}
