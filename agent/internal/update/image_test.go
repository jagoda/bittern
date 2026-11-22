package update

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"bittern.invalid/agent/internal/frigateimage"
)

func TestDockerImageEnsurerPassesThePinnedRequest(t *testing.T) {
	var got frigateimage.Options
	var gotTimeout time.Duration
	ensurer := &DockerImageEnsurer{
		Docker: "/usr/bin/docker",
		Ensure: func(_ context.Context, options frigateimage.Options, timeout time.Duration) (frigateimage.Status, error) {
			got = options
			gotTimeout = timeout
			return frigateimage.Status{State: frigateimage.StateReady}, nil
		},
	}
	timeout := 90 * time.Minute
	if err := ensurer.EnsureImage("ghcr.io/frigate/frigate:0.18.2", "sha256:abc", timeout); err != nil {
		t.Fatal(err)
	}
	if got.Reference != "ghcr.io/frigate/frigate:0.18.2" || got.Digest != "sha256:abc" || got.Docker != "/usr/bin/docker" {
		t.Fatalf("ensure options = %+v", got)
	}
	if gotTimeout != timeout {
		t.Fatalf("timeout = %s, want %s", gotTimeout, timeout)
	}
}

func TestDockerImageEnsurerReportsANonReadyImage(t *testing.T) {
	ensurer := &DockerImageEnsurer{
		Ensure: func(context.Context, frigateimage.Options, time.Duration) (frigateimage.Status, error) {
			return frigateimage.Status{State: frigateimage.StateFailed}, nil
		},
	}
	err := ensurer.EnsureImage("reference", "sha256:abc", time.Minute)
	requireRule(t, err, "image.not-ready")
}

func TestDockerImageEnsurerReportsTheEnsureError(t *testing.T) {
	ensurer := &DockerImageEnsurer{
		Ensure: func(context.Context, frigateimage.Options, time.Duration) (frigateimage.Status, error) {
			return frigateimage.Status{State: frigateimage.StateAbsent}, errors.New("pull failed")
		},
	}
	err := ensurer.EnsureImage("reference", "sha256:abc", time.Minute)
	if err == nil || !strings.Contains(err.Error(), "pull failed") {
		t.Fatalf("error = %v, want the ensure failure", err)
	}
}

func TestDockerImageEnsurerRejectsRelativeDockerPath(t *testing.T) {
	ensurer := &DockerImageEnsurer{Docker: "docker"}
	if err := ensurer.EnsureImage("reference", "sha256:abc", time.Minute); err == nil {
		t.Fatal("a relative docker path was accepted")
	} else {
		requireRule(t, err, "image.docker")
	}
}
