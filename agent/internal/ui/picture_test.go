package ui

import (
	"bytes"
	"context"
	"image/jpeg"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"bittern.invalid/agent/internal/auth"
)

type fakePicture struct {
	result PictureResult
	err    error
}

func (f fakePicture) Picture(context.Context, string) (PictureResult, error) {
	return f.result, f.err
}

func TestCameraPictureRequiresASession(t *testing.T) {
	server := newAuthTestServer(t, "picture-route-token", auth.DefaultRateLimitPolicy())
	disableRedirects(server.client)
	statusCode, _ := getPage(t, server.client, server.server.URL+"/ui/cameras/front_door/picture")
	if statusCode != http.StatusSeeOther {
		t.Fatalf("unauthenticated picture status = %d, want %d", statusCode, http.StatusSeeOther)
	}
}

func TestCameraPictureReturnsJPEGWithoutCaching(t *testing.T) {
	configPath, secretsPath := writeCameraFixture(t)
	want := mockTestPicture()
	server, client := newTestServer(t, Options{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		Picture:     fakePicture{result: PictureResult{Data: want, ContentType: "image/jpeg"}},
	})
	response, err := client.Get(server.URL + "/ui/cameras/front_door/picture")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("picture status = %d\n%s", response.StatusCode, body)
	}
	if got := response.Header.Get("Content-Type"); got != "image/jpeg" {
		t.Fatalf("content type = %q, want image/jpeg", got)
	}
	if got := response.Header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache control = %q, want no-store", got)
	}
	if !bytes.Equal(body, want) {
		t.Fatal("picture route changed the JPEG bytes")
	}
}

func TestCameraPictureRejectsNonJPEG(t *testing.T) {
	configPath, secretsPath := writeCameraFixture(t)
	server, client := newTestServer(t, Options{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		Picture:     fakePicture{result: PictureResult{Data: []byte("GIF89a not a JPEG"), ContentType: "image/jpeg"}},
	})
	statusCode, body := getPage(t, client, server.URL+"/ui/cameras/front_door/picture")
	if statusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("non-JPEG status = %d, want %d\n%s", statusCode, http.StatusUnsupportedMediaType, body)
	}
	assertContains(t, body, "did not send a JPEG picture", "Test the picture again")
	if strings.Contains(body, "GIF89a") {
		t.Fatal("error response repeated the non-image bytes")
	}
}

func TestCameraPictureRejectsOversizedPicture(t *testing.T) {
	configPath, secretsPath := writeCameraFixture(t)
	server, client := newTestServer(t, Options{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		Picture:     fakePicture{result: PictureResult{Data: make([]byte, pictureMaxBytes+1), ContentType: "image/jpeg"}},
	})
	statusCode, body := getPage(t, client, server.URL+"/ui/cameras/front_door/picture")
	if statusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized status = %d, want %d\n%s", statusCode, http.StatusRequestEntityTooLarge, body)
	}
	assertContains(t, body, "larger than 8 MiB", "test the picture again")
}

func TestCameraPictureReportsFetchFailure(t *testing.T) {
	configPath, secretsPath := writeCameraFixture(t)
	server, client := newTestServer(t, Options{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		Picture: fakePicture{
			result: PictureResult{Failure: SnapshotFailureFetch, Detail: "the camera picture could not be read"},
			err:    context.DeadlineExceeded,
		},
	})
	statusCode, body := getPage(t, client, server.URL+"/ui/cameras/front_door/picture")
	if statusCode != http.StatusBadGateway {
		t.Fatalf("fetch failure status = %d, want %d\n%s", statusCode, http.StatusBadGateway, body)
	}
	assertContains(t, body, "could not read the test picture", "test the picture again")
}

func TestMockSnapshotCheckerPictureIsARealJPEG(t *testing.T) {
	result, err := (MockSnapshotChecker{}).Picture(context.Background(), "front_door")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := jpeg.Decode(bytes.NewReader(result.Data))
	if err != nil {
		t.Fatalf("mock picture is not a JPEG: %v", err)
	}
	if bounds := decoded.Bounds(); bounds.Dx() < 100 || bounds.Dy() < 50 {
		t.Fatalf("mock picture is too small: %v", bounds)
	}
	if len(result.Data) > 64<<10 {
		t.Fatalf("mock picture is too large for a preview: %d bytes", len(result.Data))
	}
}

func TestCameraCardShowsPictureOnlyAfterSuccessfulTest(t *testing.T) {
	configPath, secretsPath := writeCameraFixture(t)
	stateRoot := t.TempDir()
	server, client := newTestServer(t, Options{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		StateRoot:   stateRoot,
		Snapshot:    fakeSnapshot{result: SnapshotResult{ContentType: "image/jpeg", Bytes: 2048}},
		Picture:     fakePicture{result: PictureResult{Data: mockTestPicture(), ContentType: "image/jpeg"}},
	})

	_, before := getPage(t, client, server.URL+"/ui/cameras")
	if strings.Contains(before, "camera-picture-image") {
		t.Fatal("camera card showed a picture before any test ran")
	}

	statusCode, body := postPage(t, client, server.URL+"/ui/cameras/front_door/snapshot", url.Values{})
	if statusCode != http.StatusOK {
		t.Fatalf("test picture status = %d\n%s", statusCode, body)
	}
	assertContains(t, body,
		`class="camera-picture"`,
		`<img class="camera-picture-image" src="/ui/cameras/front_door/picture?at=`,
		`alt="Test picture from Front door, taken at 2026-11-14 12:00:00 UTC."`,
		"Test picture from Front door, taken at 2026-11-14 12:00:00 UTC.",
		"The picture test passed. The camera sent a picture.",
		`data-picture-image`,
		`>Test picture</button>`,
	)
}

func TestCameraCardShowsTheFailureFromTheTest(t *testing.T) {
	configPath, secretsPath := writeCameraFixture(t)
	server, client := newTestServer(t, Options{
		ConfigPath:  configPath,
		SecretsPath: secretsPath,
		Snapshot: fakeSnapshot{result: SnapshotResult{
			Failure: SnapshotFailureNotJPEG,
			Err:     "the recording service did not return a JPEG picture",
		}},
	})
	statusCode, body := postPage(t, client, server.URL+"/ui/cameras/front_door/snapshot", url.Values{})
	if statusCode != http.StatusOK {
		t.Fatalf("failed test picture status = %d\n%s", statusCode, body)
	}
	assertContains(t, body, "did not send a JPEG picture", "camera-picture-error", `>Test picture</button>`)
	if strings.Contains(body, "camera-picture-image") {
		t.Fatal("failed test rendered an image element")
	}
}

func TestCameraCardFailureStatesShowSentencesAndKeepRetry(t *testing.T) {
	cases := []struct {
		name   string
		reason string
		want   string
	}{
		{name: "probe", reason: "", want: "could not reach the camera for the test picture"},
		{name: "fetch", reason: "fetch", want: "could not read the test picture"},
		{name: "oversized", reason: "oversized", want: "larger than 8 MiB"},
		{name: "non-jpeg", reason: "non-jpeg", want: "did not send a JPEG picture"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server, client := newTestServer(t, Options{})
			query := url.Values{
				"flash":   {"picture-failed"},
				"picture": {"front_door"},
			}
			if testCase.reason != "" {
				query.Set("reason", testCase.reason)
			}
			_, body := getPage(t, client, server.URL+"/ui/cameras?"+query.Encode())
			assertContains(t, body, testCase.want, `>Test picture</button>`, "camera-picture-error")
			if strings.Contains(body, "camera-picture-image") {
				t.Fatal("failure state rendered an image element")
			}
		})
	}
}

func TestCameraCardRejectsAPictureFromAnotherTest(t *testing.T) {
	server, client := newTestServer(t, Options{})
	_, body := getPage(t, client, server.URL+"/ui/cameras?flash=picture-ok&picture=front_door&at=stale")
	if strings.Contains(body, "camera-picture-image") {
		t.Fatal("card showed a picture whose cache buster did not match the recorded test")
	}
}
