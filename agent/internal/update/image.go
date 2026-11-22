package update

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"bittern.invalid/agent/internal/frigateimage"
)

// DefaultDockerPath is the fixed Docker command line tool. The adapter never
// resolves docker through PATH while running as root.
const DefaultDockerPath = "/usr/bin/docker"

// DockerImageEnsurer implements ImageEnsurer through the existing prewarm and
// ensure path. A ready image is a no-op; a running background download is
// waited for; an absent image starts a bounded download.
type DockerImageEnsurer struct {
	Docker string
	// Ensure is a test seam. Production uses frigateimage.Ensure.
	Ensure func(context.Context, frigateimage.Options, time.Duration) (frigateimage.Status, error)
}

// EnsureImage makes the pinned reference and digest ready within timeout.
func (e *DockerImageEnsurer) EnsureImage(reference, digest string, timeout time.Duration) error {
	docker := e.Docker
	if docker == "" {
		docker = DefaultDockerPath
	}
	if !filepath.IsAbs(docker) {
		return errorf("image.docker", "the docker path %q is not absolute", docker)
	}
	ensure := e.Ensure
	if ensure == nil {
		ensure = frigateimage.Ensure
	}
	status, err := ensure(context.Background(), frigateimage.Options{
		Reference: reference,
		Digest:    digest,
		Docker:    docker,
	}, timeout)
	if err != nil {
		return fmt.Errorf("update: ensure the pinned Frigate image: %w", err)
	}
	if status.State != frigateimage.StateReady {
		return errorf("image.not-ready", "the pinned Frigate image is %s, not ready", status.State)
	}
	return nil
}
