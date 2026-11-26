package update

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"bittern.invalid/agent/internal/apply"
)

const (
	// applyConfigOutputLimit bounds the candidate's captured output. The
	// result record carries the structured failure; the output is support
	// context for a process that exited without writing one.
	applyConfigOutputLimit = 64 << 10
	// applyConfigDefaultTimeout is above the 60 minute image budget and the
	// native checks. The helper unit's own TimeoutStartSec is two hours.
	applyConfigDefaultTimeout = 75 * time.Minute
)

// ExecConfigApplier runs the candidate renderer from the activated bundle. It
// is the production ConfigApplier. The candidate binary is the verified
// bundle's usr/bin/bittern, so its embedded templates, defaults, target map,
// checkers and pinned image are the candidate's, not the running helper's.
type ExecConfigApplier struct {
	// System is the apply System used by RollbackConfig. Nil uses the
	// production ExecSystem.
	System apply.System
	// UID is a test seam for the candidate binary owner check. Nil uses the
	// effective uid.
	UID func() int
}

// ApplyConfig executes the fixed apply-config subcommand from the candidate
// bundle and reads the result record it writes. The command takes the state
// root and nothing else; the candidate loads the fixed live configuration and
// verifies that the update journal's next step is apply-config and that the
// activation pointer names the candidate generation.
func (a ExecConfigApplier) ApplyConfig(request ApplyConfigRequest) (ApplyConfigResult, error) {
	var result ApplyConfigResult
	binary := filepath.Join(request.BundleDir, "files", "usr", "bin", "bittern")
	if err := a.verifyBinary(binary, request.BinarySHA256, request.BinaryMode); err != nil {
		return result, err
	}
	resultPath := normalizeOptions(Options{StateRoot: request.StateRoot}).paths().applyConfigResult(request.OperationID)
	if err := os.Remove(resultPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return result, errorf("apply.result", "remove the previous apply result %s: %v", resultPath, err)
	}
	timeout := request.Timeout
	if timeout <= 0 {
		timeout = applyConfigDefaultTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "update", "apply-config", "--state-root", request.StateRoot)
	command.Dir = "/"
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root"}
	output := &tailBuffer{limit: applyConfigOutputLimit}
	command.Stdout = output
	command.Stderr = output
	runErr := command.Run()
	data, readErr := os.ReadFile(resultPath)
	if readErr != nil {
		if ctx.Err() != nil {
			return result, errorf("apply.timeout", "the candidate configuration apply exceeded %s", timeout)
		}
		detail := strings.TrimSpace(output.String())
		if detail == "" {
			detail = "the candidate renderer wrote no result record"
		}
		return result, errorf("apply.result", "the candidate configuration apply did not report a result: %s", detail)
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, errorf("apply.result", "decode the candidate apply result %s: %v", resultPath, err)
	}
	if result.SchemaVersion != 1 {
		return result, errorf("apply.result", "the candidate apply result has schema version %d, not 1", result.SchemaVersion)
	}
	if result.OperationID != request.OperationID {
		return result, errorf("apply.result", "the candidate apply result names operation %q, not %q", result.OperationID, request.OperationID)
	}
	if runErr != nil {
		if result.Result == "" {
			result.Result = "failed"
		}
		if result.Failure == "" {
			result.Failure = strings.TrimSpace(output.String())
		}
		if result.Failure == "" {
			result.Failure = runErr.Error()
		}
		return result, errorf("apply.config", "%s", result.Failure)
	}
	if result.Result != "applied" {
		if result.Failure == "" {
			result.Failure = "the candidate renderer did not report a successful apply"
		}
		return result, errorf("apply.config", "%s", result.Failure)
	}
	return result, nil
}

// RollbackConfig restores the apply generation that was current before the
// update's apply-config step. The rollback reads the previous generation's
// stored files and verifies them before it writes anything; it uses the same
// apply rollback plan a failed manual apply uses.
func (a ExecConfigApplier) RollbackConfig(request RollbackConfigRequest) error {
	system := a.System
	if system == nil {
		system = apply.NewExecSystem()
	}
	liveRoot := request.LiveRoot
	if liveRoot == "" {
		liveRoot = "/"
	}
	_, err := apply.RollbackStaged(apply.Options{
		StateRoot: request.StateRoot,
		LiveRoot:  liveRoot,
		System:    system,
	}, request.PreviousGeneration)
	return err
}

func (a ExecConfigApplier) verifyBinary(path, wantSHA256, wantMode string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return errorf("apply.binary", "the candidate renderer %s is missing: %v", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errorf("apply.binary", "the candidate renderer %s is not a regular file", path)
	}
	uid := os.Geteuid()
	if a.UID != nil {
		uid = a.UID()
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(uid) {
		return errorf("apply.binary", "the candidate renderer %s is not owned by uid %d", path, uid)
	}
	if wantMode != "" {
		mode, err := strconv.ParseUint(wantMode, 8, 32)
		if err != nil {
			return errorf("apply.binary", "the candidate manifest mode %q is not octal", wantMode)
		}
		if info.Mode().Perm() != fs.FileMode(mode)&fs.ModePerm {
			return errorf("apply.binary", "the candidate renderer %s has mode %04o, not %s", path, info.Mode().Perm(), wantMode)
		}
	}
	if wantSHA256 != "" {
		digest, _, err := hashFile(path)
		if err != nil {
			return errorf("apply.binary", "hash the candidate renderer %s: %v", path, err)
		}
		if digest != wantSHA256 {
			return errorf("apply.binary", "the candidate renderer digest %s does not match the manifest %s", digest, wantSHA256)
		}
	}
	return nil
}

// WriteApplyConfigResult writes the candidate renderer's result record at the
// fixed operation path. The candidate calls it before it exits so the helper
// reads one structured result whether the apply succeeded or failed.
func WriteApplyConfigResult(stateRoot string, result ApplyConfigResult) error {
	path := normalizeOptions(Options{StateRoot: stateRoot}).paths().applyConfigResult(result.OperationID)
	return writeJSON(path, result, 0o600)
}

// tailBuffer keeps the last bytes written, so a noisy or failing candidate
// cannot grow the helper's memory without bound.
type tailBuffer struct {
	data  []byte
	limit int
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	if len(b.data) > b.limit {
		b.data = append([]byte(nil), b.data[len(b.data)-b.limit:]...)
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	return string(bytes.ToValidUTF8(b.data, []byte("?")))
}

var _ io.Writer = (*tailBuffer)(nil)
