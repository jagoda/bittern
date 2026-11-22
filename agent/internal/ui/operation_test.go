package ui

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/frigateimage"
)

type blockingOperationApplier struct {
	mu      sync.Mutex
	calls   int
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingOperationApplier() *blockingOperationApplier {
	return &blockingOperationApplier{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (a *blockingOperationApplier) Apply(ctx context.Context, request ApplyRequest) error {
	_, err := a.ApplyWithProgress(ctx, request, nil)
	return err
}

func (a *blockingOperationApplier) ApplyWithProgress(_ context.Context, _ ApplyRequest, progress func(apply.StepProgress)) (apply.Result, error) {
	a.mu.Lock()
	a.calls++
	a.mu.Unlock()
	if progress != nil {
		progress(apply.StepProgress{
			Phase:     "apply",
			Index:     0,
			Total:     2,
			Completed: 0,
			Step: apply.Step{
				Key:  "restart:bittern-frigate.service",
				Kind: "restart",
				Unit: "bittern-frigate.service",
			},
		})
	}
	a.once.Do(func() { close(a.started) })
	<-a.release
	return apply.Result{Status: apply.StatusApplied, Generation: 2}, nil
}

func (a *blockingOperationApplier) callCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

type failingOperationApplier struct {
	failingStep string
}

func (a failingOperationApplier) Apply(ctx context.Context, request ApplyRequest) error {
	_, err := a.ApplyWithProgress(ctx, request, nil)
	return err
}

func (a failingOperationApplier) ApplyWithProgress(_ context.Context, _ ApplyRequest, progress func(apply.StepProgress)) (apply.Result, error) {
	step := apply.Step{Key: a.failingStep, Kind: "restart", Unit: "bittern-frigate.service"}
	if progress != nil {
		progress(apply.StepProgress{Phase: "apply", Index: 5, Total: 9, Completed: 5, Step: step})
	}
	return apply.Result{
		Status:             apply.StatusRolledBack,
		Generation:         4,
		PreviousGeneration: 3,
		FailingStep:        a.failingStep,
		Failure:            "health check frigate-api failed: connection refused",
	}, context.Canceled
}

func TestApplyOperationSurvivesReloadAndRefusesASecondStart(t *testing.T) {
	applier := newBlockingOperationApplier()
	stateRoot := t.TempDir()
	server, client := newTestServer(t, Options{
		Applier:   applier,
		StateRoot: stateRoot,
	})
	completeToReview(t, client, server.URL)
	disableRedirects(client)

	response, err := client.PostForm(server.URL+"/ui/wizard/apply", url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("apply status = %d, want 303", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != BasePath+"/wizard/apply" {
		t.Fatalf("apply location = %q, want the operation page", location)
	}

	select {
	case <-applier.started:
	case <-time.After(2 * time.Second):
		t.Fatal("the apply operation did not start")
	}

	status, body := getPage(t, client, server.URL+"/ui/wizard/apply")
	if status != http.StatusOK {
		t.Fatalf("operation page status = %d\n%s", status, body)
	}
	assertContains(t, body,
		"Applying configuration",
		"Starting the recording service",
		"Starting the recording service can take a few minutes, especially the first time.",
		`<progress class="operation-progress" value="0" max="2"`,
		"0 of 2 steps done; 2 remaining",
		"It is safe to close this page",
		"Technical details",
	)
	record, err := readOperation(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if record == nil || record.State != operationStateRunning {
		t.Fatalf("operation record = %+v, want running", record)
	}
	if record.CurrentStep != "restart:bittern-frigate.service" || record.StartedAt == "" {
		t.Fatalf("operation did not record the step and start time: %+v", record)
	}
	startedAt := record.StartedAt
	data, err := os.ReadFile(operationPath(stateRoot))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "s3cret") || strings.Contains(string(data), "rtsp://") {
		t.Fatalf("operation record contains a camera secret: %s", data)
	}

	// A reload is the same operation, not a second one.
	_, body = getPage(t, client, server.URL+"/ui/wizard/apply")
	assertContains(t, body, "Starting the recording service", "0 of 2 steps done")
	record, err = readOperation(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if record.StartedAt != startedAt {
		t.Fatalf("reload replaced the operation: started_at %q, want %q", record.StartedAt, startedAt)
	}
	if applier.callCount() != 1 {
		t.Fatalf("apply calls = %d, want 1 after a reload", applier.callCount())
	}

	response, err = client.PostForm(server.URL+"/ui/wizard/apply", url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("second apply status = %d, want 303", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != BasePath+"/wizard/apply?busy=1" {
		t.Fatalf("second apply location = %q, want the busy operation page", location)
	}
	_, body = getPage(t, client, server.URL+"/ui/wizard/apply?busy=1")
	assertContains(t, body, "An apply is already running", "did not start another apply", "Starting the recording service")
	if applier.callCount() != 1 {
		t.Fatalf("second start ran another apply: calls = %d", applier.callCount())
	}

	close(applier.release)
	waitForTerminalOperation(t, stateRoot)
	_, result := getPage(t, client, server.URL+"/ui/wizard/result")
	assertContains(t, result,
		"Configuration applied",
		"Succeeded",
		"2 of 2 steps done",
	)
	if applier.callCount() != 1 {
		t.Fatalf("completed operation ran %d applies, want 1", applier.callCount())
	}
}

func TestFailedApplyResultNamesTheFailingStep(t *testing.T) {
	stateRoot := t.TempDir()
	server, client := newTestServer(t, Options{
		Applier:   failingOperationApplier{failingStep: "restart:bittern-frigate.service"},
		StateRoot: stateRoot,
	})
	completeToReview(t, client, server.URL)

	status, body := postPage(t, client, server.URL+"/ui/wizard/apply", url.Values{})
	if status != http.StatusOK {
		t.Fatalf("result status = %d\n%s", status, body)
	}
	assertContains(t, body,
		"Previous configuration restored",
		"The new configuration was not applied",
		"Stopped at",
		"Starting the recording service",
		"Previous configuration",
		"Restored",
		"Technical details",
		"Reported error",
		"health check frigate-api failed",
		"What is running now",
		"Open status",
	)
	for _, unwanted := range []string{"Configuration rolled back", "Generation 3", "Rolled back to"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("result primary copy contains %q", unwanted)
		}
	}

	record, err := readOperation(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if record == nil || record.State != operationStateRolledBack {
		t.Fatalf("operation record = %+v, want rolled back", record)
	}
	if record.FailingStep != "restart:bittern-frigate.service" ||
		record.FailingStepLabel != "Starting the recording service" {
		t.Fatalf("operation did not record the failing step and its label: %+v", record)
	}
	if record.Result == nil || record.Result.PreviousGeneration != 3 {
		t.Fatalf("operation result did not record the restored generation: %+v", record.Result)
	}
}

type failedImageApplier struct{}

func (failedImageApplier) Apply(ctx context.Context, request ApplyRequest) error {
	_, err := failedImageApplier{}.ApplyWithProgress(ctx, request, nil)
	return err
}

func (failedImageApplier) ApplyWithProgress(_ context.Context, _ ApplyRequest, progress func(apply.StepProgress)) (apply.Result, error) {
	if progress != nil {
		progress(apply.StepProgress{
			Phase:     "apply",
			Index:     1,
			Total:     4,
			Completed: 1,
			Step:      apply.Step{Key: "image", Kind: "image"},
		})
	}
	return apply.Result{Status: apply.StatusFailed, FailingStep: "image"},
		&apply.RecordingImageError{State: frigateimage.StateFailed, Cause: "dial tcp: network is unreachable"}
}

func TestFailedImageStepNamesTheRecordingSoftwareAndTheCause(t *testing.T) {
	stateRoot := t.TempDir()
	server, client := newTestServer(t, Options{
		Applier:   failedImageApplier{},
		StateRoot: stateRoot,
	})
	completeToReview(t, client, server.URL)

	status, body := postPage(t, client, server.URL+"/ui/wizard/apply", url.Values{})
	if status != http.StatusOK {
		t.Fatalf("result status = %d\n%s", status, body)
	}
	assertContains(t, body,
		"The new configuration was not applied",
		"The recording software could not be downloaded. Nothing on the appliance was changed. Connect the appliance to the internet, then apply again.",
		"Stopped at",
		"Waiting for the recording software",
		"Technical details",
		"Download error",
		"dial tcp: network is unreachable",
	)
	for _, unwanted := range []string{"ghcr.io", "Frigate", "container image", "systemctl", "bittern-image-prewarm"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("failed image result contains %q", unwanted)
		}
	}

	record, err := readOperation(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if record == nil || record.State != operationStateFailed {
		t.Fatalf("operation record = %+v, want failed", record)
	}
	if record.FailureKind != "recording-image" {
		t.Fatalf("failure kind = %q, want recording-image", record.FailureKind)
	}
	if record.FailureCause != "dial tcp: network is unreachable" {
		t.Fatalf("failure cause = %q", record.FailureCause)
	}
}

type imageWaitApplier struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newImageWaitApplier() *imageWaitApplier {
	return &imageWaitApplier{started: make(chan struct{}), release: make(chan struct{})}
}

func (a *imageWaitApplier) Apply(ctx context.Context, request ApplyRequest) error {
	_, err := a.ApplyWithProgress(ctx, request, nil)
	return err
}

func (a *imageWaitApplier) ApplyWithProgress(_ context.Context, _ ApplyRequest, progress func(apply.StepProgress)) (apply.Result, error) {
	if progress != nil {
		progress(apply.StepProgress{
			Phase:     "apply",
			Index:     1,
			Total:     4,
			Completed: 1,
			Step:      apply.Step{Key: "image", Kind: "image"},
		})
	}
	a.once.Do(func() { close(a.started) })
	<-a.release
	return apply.Result{Status: apply.StatusApplied, Generation: 2}, nil
}

func TestOperationPageShowsTheImageStepWhileItWaits(t *testing.T) {
	applier := newImageWaitApplier()
	stateRoot := t.TempDir()
	server, client := newTestServer(t, Options{
		Applier:   applier,
		StateRoot: stateRoot,
	})
	completeToReview(t, client, server.URL)
	disableRedirects(client)

	response, err := client.PostForm(server.URL+"/ui/wizard/apply", url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	select {
	case <-applier.started:
	case <-time.After(2 * time.Second):
		t.Fatal("the image step did not start")
	}
	_, body := getPage(t, client, server.URL+"/ui/wizard/apply")
	assertContains(t, body,
		"Waiting for the recording software",
		"Bittern is waiting for the recording software.",
		"The download can take several minutes on a slow connection.",
		"1 of 4 steps done; 3 remaining",
		"Time taken",
		"Technical details",
		"Recording software digest",
	)
	summary := strings.Index(body, "<summary>Technical details</summary>")
	if summary < 0 {
		t.Fatal("the operation page has no technical details disclosure")
	}
	if strings.Contains(body[:summary], "ghcr.io") {
		t.Fatal("the operation page put the image reference in primary copy")
	}
	if !strings.Contains(body[summary:], "ghcr.io") {
		t.Fatal("the operation page technical details do not carry the image reference")
	}

	close(applier.release)
	waitForTerminalOperation(t, stateRoot)
	_, result := getPage(t, client, server.URL+"/ui/wizard/result")
	assertContains(t, result, "Configuration applied", "Succeeded")
}

func TestApplyStepLabelsUseOwnerTerms(t *testing.T) {
	steps := []apply.Step{
		{Kind: "generation"},
		{Kind: "image"},
		{Kind: "check", Check: apply.Check{Kind: apply.CheckFrigate}},
		{Kind: "check", Check: apply.Check{Kind: apply.CheckDnsmasq}},
		{Kind: "check", Check: apply.Check{Kind: apply.CheckChrony}},
		{Kind: "write", Destination: "etc/systemd/network/10-lan0.network"},
		{Kind: "write", Destination: "etc/dnsmasq.d/bittern.conf"},
		{Kind: "write", Destination: "var/lib/bittern/frigate/config.yml"},
		{Kind: "write", Destination: "var/lib/bittern/frigate/cameras.env"},
		{Kind: "write", Destination: "etc/systemd/system/bittern-frigate.service"},
		{Kind: "stop", Unit: "bittern-frigate.service"},
		{Kind: "enable", Unit: "bittern-frigate.service"},
		{Kind: "start", Unit: "bittern-frigate.service"},
		{Kind: "restart", Unit: "bittern-frigate.service"},
		{Kind: "restart", Unit: "systemd-networkd.service"},
		{Kind: "health", Probe: apply.Probe{Kind: apply.ProbeHTTP, Target: "http://127.0.0.1:5000/api/version"}},
		{Kind: "health", Probe: apply.Probe{Kind: apply.ProbeLink, Target: "lan0"}},
		{Kind: "health", Probe: apply.Probe{Kind: apply.ProbeLink, Target: "cam0"}},
		{Kind: "health", Probe: apply.Probe{Kind: apply.ProbeAddress, Target: "lan0"}},
		{Kind: "health", Probe: apply.Probe{Kind: apply.ProbeDHCP, Target: "cam0"}},
		{Kind: "health", Probe: apply.Probe{Kind: apply.ProbeChrony, Target: "/etc/chrony/chrony.conf"}},
		{Kind: "health", Probe: apply.Probe{Kind: apply.ProbeNftTable, Target: "inet bittern"}},
	}
	forbidden := []string{
		"Frigate", "Docker", "docker", "container", "bittern-frigate", "systemd",
		"chrony", "dnsmasq", "nftables", "RTSP", "generation", "image",
	}
	for _, step := range steps {
		for _, label := range []string{
			applyStepLabel("apply", step),
			applyStepLabel("rollback", step),
		} {
			for _, word := range forbidden {
				if strings.Contains(label, word) {
					t.Errorf("step %+v label %q contains internal term %q", step, label, word)
				}
			}
		}
	}
}

func waitForTerminalOperation(t *testing.T, stateRoot string) *Operation {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		record, err := readOperation(stateRoot)
		if err != nil {
			t.Fatal(err)
		}
		if record != nil && record.State != operationStateRunning {
			return record
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the operation did not reach a terminal state")
	return nil
}

func TestOperationStateLabelsAreSentenceCase(t *testing.T) {
	for _, testCase := range []struct {
		state string
		label string
	}{
		{operationStateRunning, "Running"},
		{operationStateSucceeded, "Succeeded"},
		{operationStateFailed, "Failed"},
		{operationStateRolledBack, "Rolled back"},
	} {
		label, class := operationStateLabel(testCase.state)
		if label != testCase.label {
			t.Errorf("label for %s = %q, want %q", testCase.state, label, testCase.label)
		}
		if class == "" {
			t.Errorf("state %s has no badge class", testCase.state)
		}
	}
}
