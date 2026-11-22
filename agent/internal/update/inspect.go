package update

import (
	"os"
)

// RunningGeneration returns the generation named by the activation pointer and
// the version from its signed manifest. A missing pointer is generation 0 and
// an empty version, which is the baseline state. It is a read-only inspection.
func RunningGeneration(stateRoot string) (int64, string, error) {
	options := normalizeOptions(Options{StateRoot: stateRoot})
	p := options.paths()
	generation, err := readCurrent(p.current)
	if err != nil {
		return 0, "", err
	}
	if generation == 0 {
		return 0, "", nil
	}
	_, manifest, err := readManifestFromBundle(p.bundleDir(generation))
	if err != nil {
		return generation, "", err
	}
	return generation, manifest.Version, nil
}

// RollbackTarget reports the retained bundle below the committed generation.
// When no bundle exists below it, generation 0 is returned with baseline true.
// A retained bundle that cannot be read is an error, not a silent baseline
// fallback. It is a read-only inspection and does not verify the signature.
func RollbackTarget(stateRoot string, committed int64) (int64, string, bool, error) {
	options := normalizeOptions(Options{StateRoot: stateRoot})
	p := options.paths()
	if committed <= 0 {
		return 0, "", false, nil
	}
	generations, err := bundleGenerations(p.bundles)
	if err != nil {
		return 0, "", false, err
	}
	var target int64
	for _, generation := range generations {
		if generation < committed && generation > target {
			target = generation
		}
	}
	if target == 0 {
		return 0, "", true, nil
	}
	_, manifest, err := readManifestFromBundle(p.bundleDir(target))
	if err != nil {
		return 0, "", false, err
	}
	if manifest.Generation != target {
		return 0, "", false, errorf("rollback.previous", "the retained bundle manifest names generation %d, not %d", manifest.Generation, target)
	}
	return target, manifest.Version, false, nil
}

// BaselineBinaryExists reports whether the fixed baseline binary is present and
// executable. Status uses it only to avoid claiming a rollback target that the
// launcher cannot start; the rollback operation performs the full ownership and
// type check.
func BaselineBinaryExists(path string) bool {
	if path == "" {
		path = defaultBaselineBinary
	}
	info, err := os.Lstat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}
