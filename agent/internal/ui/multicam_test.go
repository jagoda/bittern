package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"bittern.invalid/agent/internal/apply"
	"bittern.invalid/agent/internal/config"
)

// twoCameraValues adds two cameras with their own sign-ins to a wizard session,
// the shape the camera step hands to the one apply.
func twoCameraValues() (url.Values, url.Values) {
	front := cameraFormValues("Front door", "10.77.0.20")
	front.Set("username", "viewer")
	front.Set("password", "s3cret-pass-one")
	driveway := url.Values{
		"name":      {"Driveway"},
		"address":   {"10.77.0.21"},
		"rtsp_port": {"8554"},
		"rtsp_path": {"/live"},
		"username":  {"installer"},
		"password":  {"s3cret-pass-two"},
	}
	return front, driveway
}

// twoCameraRequest is the apply request for those two cameras, as the review
// builds it.
func twoCameraRequest(configPath, secretsPath string) ApplyRequest {
	port1, port2 := 554, 8554
	path1, path2 := "/stream1", "/live"
	cameras := []config.Camera{
		{ID: "camera_1", Name: "Front door", Address: "10.77.0.20", RTSPPort: &port1, RTSPPath: &path1},
		{ID: "camera_2", Name: "Driveway", Address: "10.77.0.21", RTSPPort: &port2, RTSPPath: &path2},
	}
	return ApplyRequest{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		Cameras:     cameras,
		CameraCredentials: map[string]string{
			"camera_1": "rtsp://viewer:s3cret-pass-one@10.77.0.20:554/stream1",
			"camera_2": "rtsp://installer:s3cret-pass-two@10.77.0.21:8554/live",
		},
	}
}

// TestTwoCamerasKeepTheirOwnCredentials proves that one apply with two sign-ins
// writes two distinct secrets and two distinct environment entries, and that
// neither camera's credential reaches the other camera's configuration.
func TestTwoCamerasKeepTheirOwnCredentials(t *testing.T) {
	configPath := writeApplierFixture(t)
	secretsPath := filepath.Join(t.TempDir(), "secrets.yaml")
	if err := os.WriteFile(secretsPath, []byte("schema_version: 1\nadmin_password: keep-me\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var (
		system   *applierTestSystem
		liveRoot string
	)
	applier := AgentApplier{
		SecretsPath: secretsPath,
		applyFn: func(cfg *config.Config, options apply.Options) (apply.Result, error) {
			system = newApplierTestSystem()
			options.System = system
			options.StateRoot = filepath.Join(t.TempDir(), "state")
			liveRoot = filepath.Join(t.TempDir(), "live")
			options.LiveRoot = liveRoot
			options.Health = nil
			return apply.Apply(cfg, options)
		},
	}

	request := twoCameraRequest(configPath, secretsPath)
	result, err := applier.ApplyWithProgress(context.Background(), request, nil)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.Status != apply.StatusApplied {
		t.Fatalf("apply status = %q, want applied", result.Status)
	}

	// appliance.yaml keeps one credentials_ref per camera.
	stored, validationErrors := config.Load(configPath)
	if len(validationErrors) != 0 {
		t.Fatalf("written config is invalid: %v", validationErrors)
	}
	if len(stored.Cameras) != 2 {
		t.Fatalf("cameras in appliance.yaml = %d, want 2", len(stored.Cameras))
	}
	for index, want := range []string{"cameras.camera_1", "cameras.camera_2"} {
		camera := stored.Cameras[index]
		if camera.CredentialsRef == nil || *camera.CredentialsRef != want {
			t.Fatalf("camera %q credentials_ref = %v, want %s", camera.ID, camera.CredentialsRef, want)
		}
	}

	// secrets.yaml holds exactly one entry per camera, each with its own
	// sign-in and no complete URL.
	secretsData, err := os.ReadFile(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		AdminPassword string `yaml:"admin_password"`
		Cameras       map[string]struct {
			Username string `yaml:"username"`
			Password string `yaml:"password"`
		} `yaml:"cameras"`
	}
	if err := yaml.Unmarshal(secretsData, &document); err != nil {
		t.Fatal(err)
	}
	if document.AdminPassword != "keep-me" {
		t.Fatalf("the existing secret was not kept: %q", document.AdminPassword)
	}
	if strings.Contains(string(secretsData), "rtsp://") {
		t.Fatalf("secrets.yaml still holds a complete URL: %s", secretsData)
	}
	wantSecrets := map[string]string{
		"camera_1": request.CameraCredentials["camera_1"],
		"camera_2": request.CameraCredentials["camera_2"],
	}
	if len(document.Cameras) != len(wantSecrets) {
		t.Fatalf("secrets entries = %d, want %d: %s", len(document.Cameras), len(wantSecrets), secretsData)
	}
	for id, want := range wantSecrets {
		parsed, err := url.Parse(want)
		if err != nil || parsed.User == nil {
			t.Fatalf("test URL is not a credentialed URL: %q", want)
		}
		password, _ := parsed.User.Password()
		if got := document.Cameras[id].Username; got != parsed.User.Username() {
			t.Fatalf("secrets username for %s = %q, want %q", id, got, parsed.User.Username())
		}
		if got := document.Cameras[id].Password; got != password {
			t.Fatalf("secrets password for %s does not match the request", id)
		}
	}
	info, err := os.Stat(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("secrets.yaml mode = %o, want 600", info.Mode().Perm())
	}

	// The camera env file has one entry per camera, under that camera's own
	// environment name.
	env := string(system.files[filepath.Join(liveRoot, "var/lib/bittern/frigate/cameras.env")])
	lines := strings.Split(strings.TrimSpace(env), "\n")
	if len(lines) != 2 {
		t.Fatalf("camera env lines = %d, want 2: %q", len(lines), env)
	}
	wantEnv := map[string]string{
		"FRIGATE_CAMERA_CAMERA_1_RTSP_URL": request.CameraCredentials["camera_1"],
		"FRIGATE_CAMERA_CAMERA_2_RTSP_URL": request.CameraCredentials["camera_2"],
	}
	if len(wantEnv) != len(lines) {
		t.Fatalf("expected one env line per camera: %q", env)
	}
	for _, line := range lines {
		name, value, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("camera env line has no value: %q", line)
		}
		if wantEnv[name] != value {
			t.Fatalf("env entry %s = %q, want %q", name, value, wantEnv[name])
		}
	}

	// The rendered recording configuration references each camera's own
	// variable and carries no credential.
	frigateConfig := string(system.files[filepath.Join(liveRoot, "var/lib/bittern/frigate/config.yml")])
	for _, want := range []string{
		"camera_1:\n",
		"{FRIGATE_CAMERA_CAMERA_1_RTSP_URL}",
		"camera_2:\n",
		"{FRIGATE_CAMERA_CAMERA_2_RTSP_URL}",
	} {
		if !strings.Contains(frigateConfig, want) {
			t.Errorf("rendered config does not contain %q:%s", want, frigateConfig)
		}
	}
	for _, unwanted := range []string{"s3cret-pass-one", "s3cret-pass-two", "rtsp://"} {
		if strings.Contains(frigateConfig, unwanted) {
			t.Errorf("rendered config contains a credential (%q): %s", unwanted, frigateConfig)
		}
	}
}

func TestCredentialValuesCoverEveryCamera(t *testing.T) {
	request := twoCameraRequest("/etc/bittern/appliance.yaml", "/etc/bittern/secrets.yaml")
	values := credentialValues(request.CameraCredentials)
	for _, want := range []string{
		"rtsp://viewer:s3cret-pass-one@10.77.0.20:554/stream1",
		"viewer",
		"s3cret-pass-one",
		"rtsp://installer:s3cret-pass-two@10.77.0.21:8554/live",
		"installer",
		"s3cret-pass-two",
	} {
		found := false
		for _, value := range values {
			if value == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("credential value %q is not registered for redaction: %v", want, values)
		}
	}

	// A message that names a username and a password without any URL or
	// key/value form around them is the failure mode a literal match covers.
	err := redactCameraCredentials(errors.New("the camera refused viewer and s3cret-pass-two"), values)
	if err == nil {
		t.Fatal("a redacted error was dropped")
	}
	for _, unwanted := range []string{"viewer", "s3cret-pass-two"} {
		if strings.Contains(err.Error(), unwanted) {
			t.Errorf("redacted error still contains %q: %v", unwanted, err)
		}
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Errorf("redacted error has no placeholder: %v", err)
	}
}

// TestRolledBackApplyLeavesTheSecretsDocumentAsItWas is the safety rule from
// the multi-camera note: a credential written for one camera must not survive a
// transaction whose configuration generation did not commit.
func TestRolledBackApplyLeavesTheSecretsDocumentAsItWas(t *testing.T) {
	configPath := writeApplierFixture(t)
	secretsPath := filepath.Join(t.TempDir(), "secrets.yaml")
	secretsBefore := []byte(`schema_version: 1
identity:
  id: test-appliance
admin_password: keep-me
cameras:
  camera_0:
    rtsp_url: rtsp://old:old-secret@10.77.0.10:554/old
`)
	if err := os.WriteFile(secretsPath, secretsBefore, 0o600); err != nil {
		t.Fatal(err)
	}

	applier := AgentApplier{
		SecretsPath: secretsPath,
		applyFn: func(cfg *config.Config, _ apply.Options) (apply.Result, error) {
			// Both cameras reached the secret document before the transaction
			// failed and restored the previous generation.
			return apply.Result{
				Status:             apply.StatusRolledBack,
				PreviousGeneration: 2,
				FailingStep:        "health:frigate-api",
				Failure:            "health check frigate-api failed",
			}, errors.New("health check frigate-api failed")
		},
	}

	_, err := applier.ApplyWithProgress(context.Background(), twoCameraRequest(configPath, secretsPath), nil)
	if err == nil {
		t.Fatal("a rolled-back apply reported success")
	}
	secretsAfter, err := os.ReadFile(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(secretsAfter) != string(secretsBefore) {
		t.Fatalf("a rolled-back apply left a camera sign-in behind:\nwant:\n%s\ngot:\n%s", secretsBefore, secretsAfter)
	}
	info, err := os.Stat(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("restored secrets.yaml mode = %o, want 600", info.Mode().Perm())
	}
}

// TestCommittedApplyKeepsItsCameraSignIns is the other half of the restore
// rule: the credentials stay when the transaction committed.
func TestCommittedApplyKeepsItsCameraSignIns(t *testing.T) {
	configPath := writeApplierFixture(t)
	secretsPath := filepath.Join(t.TempDir(), "secrets.yaml")
	applier := AgentApplier{
		SecretsPath: secretsPath,
		applyFn: func(cfg *config.Config, _ apply.Options) (apply.Result, error) {
			return apply.Result{Status: apply.StatusApplied, Generation: 3}, nil
		},
	}
	if _, err := applier.ApplyWithProgress(context.Background(), twoCameraRequest(configPath, secretsPath), nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(secretsPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"s3cret-pass-one", "s3cret-pass-two"} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("committed apply lost %q: %s", want, data)
		}
	}
}

func TestKeepCameraSecretsFollowsTheTransactionOutcome(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		result apply.Result
		err    error
		want   bool
	}{
		{"committed", apply.Result{Status: apply.StatusApplied}, nil, true},
		{"applied with an error", apply.Result{Status: apply.StatusApplied}, errors.New("x"), false},
		{"rolled back", apply.Result{Status: apply.StatusRolledBack}, errors.New("x"), false},
		{"failed before live files", apply.Result{Status: apply.StatusFailed}, errors.New("x"), false},
		{"no result", apply.Result{}, errors.New("x"), false},
		{"waiting to resume", apply.Result{Status: apply.StatusInterrupted}, apply.ErrPowerLoss, true},
		{
			"failed rollback may be partly live",
			apply.Result{Status: apply.StatusFailed, RollbackFailedStep: "write:etc/nftables.conf", LiveTouched: true},
			errors.New("x"),
			true,
		},
	} {
		if got := keepCameraSecrets(testCase.result, testCase.err); got != testCase.want {
			t.Errorf("%s: keepCameraSecrets = %v, want %v", testCase.name, got, testCase.want)
		}
	}
}

// fixedSnapshots answers the picture check per camera so one camera can fail
// while the other succeeds.
type fixedSnapshots map[string]SnapshotResult

func (f fixedSnapshots) Check(_ context.Context, cameraID string) (SnapshotResult, error) {
	result, ok := f[cameraID]
	if !ok {
		result = SnapshotResult{ContentType: "image/jpeg", Bytes: 2048}
	}
	result.CameraID = cameraID
	if result.CheckedAt.IsZero() {
		result.CheckedAt = testNow
	}
	if result.Err != "" {
		return result, errors.New(result.Err)
	}
	return result, nil
}

// TestApplyResultNamesEveryCamera checks the result page after an apply that
// committed with one camera delivering and one not: both cameras are named, and
// the page states that the configuration stayed applied.
func TestApplyResultNamesEveryCamera(t *testing.T) {
	stateRoot := t.TempDir()
	server, client := newTestServer(t, Options{
		Applier:   &fakeApplier{},
		StateRoot: stateRoot,
		Snapshot: fixedSnapshots{
			"camera_2": {Err: "the recording service answered the picture request with HTTP 503"},
		},
	})
	getPage(t, client, server.URL+"/ui/wizard")
	front, driveway := twoCameraValues()
	addCamera(t, client, server.URL, front)
	addCamera(t, client, server.URL, driveway)
	continueToReview(t, client, server.URL)

	statusCode, result := postPage(t, client, server.URL+"/ui/wizard/apply", url.Values{})
	if statusCode != 200 {
		t.Fatalf("result status = %d\n%s", statusCode, result)
	}
	assertContains(t, result,
		"Configuration applied",
		"One camera has not sent a picture yet",
		"The configuration stays applied",
		"<dt>Front door</dt>",
		"<dt>Driveway</dt>",
		"10.77.0.20",
		"10.77.0.21",
		"Picture received",
		"No picture yet",
		"Check that the camera is switched on",
	)
	frontIndex := strings.Index(result, "<dt>Front door</dt>")
	drivewayIndex := strings.Index(result, "<dt>Driveway</dt>")
	if !(frontIndex < drivewayIndex) {
		t.Fatalf("the result does not list the cameras in order: %d, %d", frontIndex, drivewayIndex)
	}
	receivedIndex := strings.Index(result, "Picture received")
	missingIndex := strings.Index(result, "No picture yet")
	if receivedIndex < frontIndex || receivedIndex > drivewayIndex || missingIndex < drivewayIndex {
		t.Fatalf("the picture outcomes are not on the camera they belong to: %d %d %d", frontIndex, receivedIndex, missingIndex)
	}
	for _, unwanted := range []string{"camera_1", "camera_2", "s3cret-pass-one", "s3cret-pass-two", "rtsp://"} {
		if strings.Contains(result, unwanted) {
			t.Errorf("the result page contains the internal value %q", unwanted)
		}
	}

	record, err := readOperation(stateRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Cameras) != 2 {
		t.Fatalf("operation cameras = %+v, want 2", record.Cameras)
	}
	if record.Cameras[0].Picture != operationPictureReceived || record.Cameras[1].Picture != operationPictureMissing {
		t.Fatalf("operation picture checks = %+v", record.Cameras)
	}
}

// TestApplyValidationFailureNamesTheCamera checks that a camera the appliance
// rejects at apply time is named on the result page and shown against its own
// review card.
func TestApplyValidationFailureNamesTheCamera(t *testing.T) {
	configPath := writeApplierFixture(t)
	secretsPath := filepath.Join(t.TempDir(), "secrets.yaml")
	stateRoot := t.TempDir()
	server, client := newTestServer(t, Options{
		Applier:     AgentApplier{SecretsPath: secretsPath},
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		StateRoot:   stateRoot,
	})
	getPage(t, client, server.URL+"/ui/wizard")
	addCamera(t, client, server.URL, cameraFormValues("Front door", "10.77.0.20"))
	// 192.168.1.30 is on the house network. The camera step accepts it and the
	// appliance refuses it during apply.
	addCamera(t, client, server.URL, cameraFormValues("Driveway", "192.168.1.30"))
	continueToReview(t, client, server.URL)

	configBefore, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	statusCode, result := postPage(t, client, server.URL+"/ui/wizard/apply", url.Values{})
	if statusCode != 200 {
		t.Fatalf("result status = %d\n%s", statusCode, result)
	}
	// The refusal happens before the applier writes anything, so one camera's
	// settings cannot land on the appliance.
	configAfter, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(configAfter) != string(configBefore) {
		t.Fatalf("a refused apply changed appliance.yaml:\n%s", configAfter)
	}
	if _, statErr := os.Stat(secretsPath); statErr == nil {
		t.Fatal("a refused apply created secrets.yaml")
	}
	assertContains(t, result,
		"Configuration not applied",
		"Driveway (192.168.1.30)",
		"This address is on the house network.",
		"Correct this camera on the review page",
		"Nothing on the appliance has changed",
		"Technical details",
		"cameras.address-on-lan",
	)
	for _, want := range []string{"Front door", "Driveway"} {
		if !strings.Contains(result, want) {
			t.Errorf("the result does not name the camera %q", want)
		}
	}
	if strings.Contains(result, "$.cameras[1]") && !strings.Contains(result, "Technical details") {
		t.Error("the validator path is in primary copy")
	}

	review := getPageBody(t, client, server.URL+"/ui/wizard/review")
	alert := "The settings for this camera were not applied"
	assertContains(t, review, alert, "This address is on the house network.")
	alertIndex := strings.Index(review, alert)
	if alertIndex < 0 {
		t.Fatalf("the review has no apply error:\n%s", review)
	}
	drivewayIndex := strings.Index(review, "<h3>Driveway</h3>")
	applyCard := strings.Index(review, "Apply the configuration")
	if drivewayIndex < 0 || applyCard < 0 {
		t.Fatalf("the review is missing a camera card or the apply action:\n%s", review)
	}
	if alertIndex < drivewayIndex || alertIndex > applyCard {
		t.Fatalf("the apply error is not on the rejected camera's card: alert %d, card %d, apply %d",
			alertIndex, drivewayIndex, applyCard)
	}
	frontIndex := strings.Index(review, "<h3>Front door</h3>")
	if frontIndex > alertIndex {
		t.Fatal("Front door was listed after the rejected camera")
	}
	if count := strings.Count(review, alert); count != 1 {
		t.Fatalf("the review shows %d refusals, want the one rejected camera", count)
	}

	// Correcting the camera clears the rejection, so the alert does not
	// outlive the values it describes.
	if _, body := postPage(t, client, server.URL+"/ui/wizard/camera/c2/settings", url.Values{
		"name":      {"Driveway"},
		"address":   {"10.77.0.21"},
		"rtsp_port": {"554"},
		"rtsp_path": {"/stream1"},
		"from":      {"review"},
	}); strings.Contains(body, alert) {
		t.Errorf("the review still shows the rejection after the camera was corrected:\n%s", body)
	}
	for _, unwanted := range []string{"$.cameras", "cameras.address-on-lan"} {
		if strings.Contains(review, unwanted) {
			t.Errorf("the review page shows the internal value %q", unwanted)
		}
	}
	if strings.Contains(review, "s3cret") {
		t.Error("the review page shows a sign-in")
	}
}

// echoCredentialApplier fails the way a checker that quotes its input would.
type echoCredentialApplier struct{}

func (echoCredentialApplier) Apply(ctx context.Context, request ApplyRequest) error {
	_, err := echoCredentialApplier{}.ApplyWithProgress(ctx, request, nil)
	return err
}

func (echoCredentialApplier) ApplyWithProgress(_ context.Context, request ApplyRequest, _ func(apply.StepProgress)) (apply.Result, error) {
	message := "the recording service refused these streams"
	for _, id := range []string{"camera_1", "camera_2"} {
		credential := request.CameraCredentials[id]
		parsed, _ := url.Parse(credential)
		password, _ := parsed.User.Password()
		message += ": " + credential + " for " + parsed.User.Username() + " with password " + password
	}
	return apply.Result{
		Status:             apply.StatusRolledBack,
		PreviousGeneration: 1,
		FailingStep:        "check:frigate/config.yml",
		Failure:            message,
	}, errors.New(message)
}

// TestMultiCameraCredentialsStayOutOfEveryResult walks both cameras' usernames
// and passwords and asserts that none of them reaches a page, the operation
// record, or a stored error.
func TestMultiCameraCredentialsStayOutOfEveryResult(t *testing.T) {
	stateRoot := t.TempDir()
	server, client := newTestServer(t, Options{
		Applier:   echoCredentialApplier{},
		StateRoot: stateRoot,
	})
	getPage(t, client, server.URL+"/ui/wizard")
	front, driveway := twoCameraValues()
	step := addCamera(t, client, server.URL, front)
	addCamera(t, client, server.URL, driveway)
	review := continueToReview(t, client, server.URL)

	statusCode, result := postPage(t, client, server.URL+"/ui/wizard/apply", url.Values{})
	if statusCode != 200 {
		t.Fatalf("result status = %d\n%s", statusCode, result)
	}
	surfaces := map[string]string{
		"the camera step":      step,
		"the review page":      review,
		"the result page":      result,
		"the review after":     getPageBody(t, client, server.URL+"/ui/wizard/review"),
		"the operation record": readOperationFile(t, stateRoot),
	}
	credentials := map[string][]string{
		"Front door": {"viewer", "s3cret-pass-one", "rtsp://viewer:s3cret-pass-one@10.77.0.20:554/stream1"},
		"Driveway":   {"installer", "s3cret-pass-two", "rtsp://installer:s3cret-pass-two@10.77.0.21:8554/live"},
	}
	for surface, body := range surfaces {
		for camera, values := range credentials {
			for _, value := range values {
				if strings.Contains(body, value) {
					t.Errorf("%s contains %s's credential %q", surface, camera, value)
				}
			}
		}
	}
	if !strings.Contains(result, "Previous configuration restored") {
		t.Fatalf("the redacted failure was not reported as a failure:\n%s", result)
	}
	// A rolled-back apply still names every camera and says nothing changed.
	assertContains(t, result, "<dt>Front door</dt>", "<dt>Driveway</dt>", "Not applied")
	if !strings.Contains(result, "[REDACTED]") {
		t.Error("the result page has no redaction placeholder")
	}
	if !strings.Contains(readOperationFile(t, stateRoot), "[REDACTED]") {
		t.Error("the operation record kept the credential text unredacted")
	}
}

func readOperationFile(t *testing.T, stateRoot string) string {
	t.Helper()
	data, err := os.ReadFile(operationPath(stateRoot))
	if err != nil {
		t.Fatalf("read operation record: %v", err)
	}
	var record Operation
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("parse operation record: %v", err)
	}
	return string(data)
}

// TestSetupValidationErrorNamesCameras unit-tests the attribution so the
// message shapes do not depend on one page.
func TestSetupValidationErrorNamesCameras(t *testing.T) {
	cameras := []config.Camera{
		{ID: "camera_1", Name: "Front door", Address: "10.77.0.20"},
		{ID: "camera_2", Name: "Driveway", Address: "192.168.1.30"},
	}
	validation := newSetupValidationError(cameras, []config.Error{
		{Path: "$.cameras[1].address", Rule: "cameras.address-on-lan", Message: "camera address 192.168.1.30 is on the lan0 subnet 192.168.1.0/24"},
		{Path: "$.storage.recording.mount_point", Rule: "storage.os-drive-path", Message: "this path is on the OS drive"},
	})
	if len(validation.cameras) != 1 {
		t.Fatalf("attributed camera errors = %+v", validation.cameras)
	}
	if got := validation.cameras[0].ID; got != "camera_2" {
		t.Fatalf("attributed camera = %q, want camera_2", got)
	}
	if !strings.Contains(validation.Error(), "Driveway (192.168.1.30)") {
		t.Errorf("Error() does not name the camera: %s", validation.Error())
	}
	if !strings.Contains(validation.Error(), "storage.os-drive-path") {
		t.Errorf("Error() lost the error outside the camera list: %s", validation.Error())
	}
	if strings.Contains(validation.ownerMessage(), "$.") || strings.Contains(validation.ownerMessage(), "lan0") {
		t.Errorf("ownerMessage() carries an internal term: %s", validation.ownerMessage())
	}
	if failures := validation.failuresByCamera(); failures["camera_2"] == "" {
		t.Errorf("failuresByCamera() has no entry for camera_2: %+v", failures)
	}
	if _, ok := setupValidationFailure(fmt.Errorf("wrapped: %w", validation)); !ok {
		t.Error("setupValidationFailure does not follow a wrapped error")
	}
	if _, ok := setupValidationFailure(errors.New("plain")); ok {
		t.Error("setupValidationFailure claimed an unrelated error")
	}
}
