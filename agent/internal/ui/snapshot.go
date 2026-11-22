package ui

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultFrigateURL = "http://127.0.0.1:5000"
	snapshotTimeout   = 45 * time.Second
	snapshotRetry     = 2 * time.Second
	pictureTimeout    = 5 * time.Second
	pictureMaxBytes   = 8 << 20
	snapshotMaxBytes  = pictureMaxBytes
)

// SnapshotFailure names why a test picture did not arrive. The card uses it to
// choose an owner sentence; the technical detail stays in Err.
type SnapshotFailure string

const (
	SnapshotFailureNone      SnapshotFailure = ""
	SnapshotFailureProbe     SnapshotFailure = "probe"
	SnapshotFailureFetch     SnapshotFailure = "fetch"
	SnapshotFailureOversized SnapshotFailure = "oversized"
	SnapshotFailureNotJPEG   SnapshotFailure = "non-jpeg"
)

// SnapshotResult is the visible camera check after apply. CameraName is the
// owner-facing name, so a result can name a camera without using its saved
// identifier. Err is a short, non-secret explanation; the wizard shows it on
// the result page.
type SnapshotResult struct {
	CameraID    string
	CameraName  string
	ContentType string
	Bytes       int
	CheckedAt   time.Time
	Failure     SnapshotFailure
	Err         string
}

// Succeeded reports whether the recording service returned a JPEG for the
// camera.
func (r SnapshotResult) Succeeded() bool {
	return r.Err == "" && r.Failure == SnapshotFailureNone && r.Bytes > 0
}

// SnapshotChecker proves the adopted camera reaches the recording service after
// apply.
type SnapshotChecker interface {
	Check(context.Context, string) (SnapshotResult, error)
}

// PictureResult is one fetched test picture. Data is returned to the image
// route and is never written to disk or the status document.
type PictureResult struct {
	Data        []byte
	ContentType string
	Failure     SnapshotFailure
	Detail      string
}

// PictureFetcher fetches one JPEG for the preview route. It does not retry and
// does not store the bytes.
type PictureFetcher interface {
	Picture(context.Context, string) (PictureResult, error)
}

// FrigateSnapshotChecker reads the unauthenticated loopback API that the QEMU
// harness already uses. It retries while the recording service starts the
// camera, and returns the last failure instead of blocking the wizard forever.
type FrigateSnapshotChecker struct {
	BaseURL string
	Client  *http.Client
	Now     func() time.Time
	Retry   time.Duration
	Timeout time.Duration
}

// Check requests the camera's latest JPEG from the recording service.
func (c FrigateSnapshotChecker) Check(ctx context.Context, cameraID string) (SnapshotResult, error) {
	base := strings.TrimSpace(c.BaseURL)
	if base == "" {
		base = defaultFrigateURL
	}
	retry := c.Retry
	if retry <= 0 {
		retry = snapshotRetry
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = snapshotTimeout
	}
	now := c.Now
	if now == nil {
		now = time.Now
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	deadline := now().Add(timeout)
	target := pictureTarget(base, cameraID)
	last := SnapshotResult{
		CameraID: cameraID,
		Failure:  SnapshotFailureProbe,
		Err:      "the recording service did not return a camera picture yet",
	}
	for {
		fetched := fetchPicture(ctx, client, target)
		result := fetched.snapshot(cameraID, now())
		if result.Succeeded() {
			return result, nil
		}
		last = result
		if now().Add(retry).After(deadline) {
			return last, fmt.Errorf("%s", last.Err)
		}
		select {
		case <-ctx.Done():
			last.Failure = SnapshotFailureProbe
			last.Err = "the camera check ran out of time before the recording service delivered a picture"
			return last, ctx.Err()
		case <-time.After(retry):
		}
	}
}

// Picture fetches the test picture exactly once for the authenticated image
// route. The short timeout is independent of the retrying probe above.
func (c FrigateSnapshotChecker) Picture(ctx context.Context, cameraID string) (PictureResult, error) {
	base := strings.TrimSpace(c.BaseURL)
	if base == "" {
		base = defaultFrigateURL
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: pictureTimeout}
	}
	pictureCtx, cancel := context.WithTimeout(ctx, pictureTimeout)
	defer cancel()
	fetched := fetchPicture(pictureCtx, client, pictureTarget(base, cameraID))
	result := fetched.picture()
	if result.Failure != SnapshotFailureNone {
		return result, fmt.Errorf("%s", result.Detail)
	}
	return result, nil
}

func pictureTarget(base, cameraID string) string {
	return strings.TrimRight(base, "/") + "/api/" + url.PathEscape(cameraID) + "/latest.jpg"
}

type fetchedPicture struct {
	data        []byte
	contentType string
	failure     SnapshotFailure
	detail      string
}

func (f fetchedPicture) snapshot(cameraID string, checkedAt time.Time) SnapshotResult {
	return SnapshotResult{
		CameraID:    cameraID,
		ContentType: f.contentType,
		Bytes:       len(f.data),
		CheckedAt:   checkedAt,
		Failure:     f.failure,
		Err:         f.detail,
	}
}

func (f fetchedPicture) picture() PictureResult {
	return PictureResult{
		Data:        f.data,
		ContentType: f.contentType,
		Failure:     f.failure,
		Detail:      f.detail,
	}
}

func fetchPicture(ctx context.Context, client *http.Client, target string) fetchedPicture {
	result := fetchedPicture{}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		result.failure = SnapshotFailureProbe
		result.detail = "the camera check request could not be built"
		return result
	}
	response, err := client.Do(request)
	if err != nil {
		result.failure = SnapshotFailureProbe
		result.detail = "the recording service is not reachable on the appliance loopback address yet"
		return result
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		result.failure = SnapshotFailureProbe
		result.detail = fmt.Sprintf("the recording service answered the picture request with HTTP %d", response.StatusCode)
		return result
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, pictureMaxBytes+1))
	if err != nil {
		result.failure = SnapshotFailureFetch
		result.detail = "the camera picture could not be read"
		return result
	}
	if len(data) > pictureMaxBytes {
		result.failure = SnapshotFailureOversized
		result.detail = fmt.Sprintf("the camera picture is larger than %s", humanBytes(uint64(pictureMaxBytes)))
		return result
	}
	if len(data) == 0 {
		result.failure = SnapshotFailureFetch
		result.detail = "the recording service returned an empty picture"
		return result
	}
	if !isJPEGData(data) {
		result.failure = SnapshotFailureNotJPEG
		result.detail = "the recording service did not return a JPEG picture"
		return result
	}
	result.data = data
	result.contentType = "image/jpeg"
	return result
}

func isJPEGData(data []byte) bool {
	return len(data) >= 2 && data[0] == 0xff && data[1] == 0xd8
}

// classifySnapshotFailure keeps an older or test checker that only returns an
// error string in the right card state. The fetcher sets Failure directly; this
// is the fallback for the checker interface's string result.
func classifySnapshotFailure(detail string) SnapshotFailure {
	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "larger than"), strings.Contains(lower, "oversized"):
		return SnapshotFailureOversized
	case strings.Contains(lower, "jpeg"), strings.Contains(lower, "without an image"), strings.Contains(lower, "not an image"):
		return SnapshotFailureNotJPEG
	case strings.Contains(lower, "could not be read"), strings.Contains(lower, "empty"):
		return SnapshotFailureFetch
	default:
		return SnapshotFailureProbe
	}
}

// MockSnapshotChecker returns a fixed successful result for previews and tests.
type MockSnapshotChecker struct{}

// Check returns a sample JPEG result.
func (MockSnapshotChecker) Check(context.Context, string) (SnapshotResult, error) {
	data := mockTestPicture()
	return SnapshotResult{
		ContentType: "image/jpeg",
		Bytes:       len(data),
		CheckedAt:   time.Now(),
	}, nil
}

// Picture returns the same sample JPEG that the preview card displays.
func (MockSnapshotChecker) Picture(context.Context, string) (PictureResult, error) {
	data := mockTestPicture()
	return PictureResult{
		Data:        append([]byte(nil), data...),
		ContentType: "image/jpeg",
	}, nil
}

var (
	mockPictureOnce sync.Once
	mockPictureData []byte
)

// mockTestPicture generates a small camera test pattern in memory so the
// preview build never commits a binary or depends on an external file.
func mockTestPicture() []byte {
	mockPictureOnce.Do(func() {
		const width, height = 320, 180
		canvas := image.NewRGBA(image.Rect(0, 0, width, height))
		draw.Draw(canvas, canvas.Bounds(),
			image.NewUniform(color.RGBA{R: 0x38, G: 0x36, B: 0x32, A: 0xff}),
			image.Point{}, draw.Src)
		bars := []color.RGBA{
			{R: 0xc9, G: 0xc5, B: 0xbb, A: 0xff},
			{R: 0xff, G: 0xf4, B: 0xdb, A: 0xff},
			{R: 0xe7, G: 0xf4, B: 0xec, A: 0xff},
			{R: 0xe8, G: 0xf0, B: 0xf8, A: 0xff},
			{R: 0xf7, G: 0xf5, B: 0xef, A: 0xff},
			{R: 0x1c, G: 0x1b, B: 0x18, A: 0xff},
		}
		barWidth := width / len(bars)
		for index, bar := range bars {
			rect := image.Rect(index*barWidth, 0, (index+1)*barWidth, height/3)
			draw.Draw(canvas, rect, image.NewUniform(bar), image.Point{}, draw.Src)
		}
		draw.Draw(canvas, image.Rect(width/4, height/3+20, width*3/4, height-20),
			image.NewUniform(color.RGBA{R: 0xf7, G: 0xf5, B: 0xef, A: 0xff}),
			image.Point{}, draw.Src)
		draw.Draw(canvas, image.Rect(width/2-1, height/3+20, width/2+1, height-20),
			image.NewUniform(color.RGBA{R: 0x0b, G: 0x4f, B: 0x8a, A: 0xff}),
			image.Point{}, draw.Src)
		draw.Draw(canvas, image.Rect(width/4, height/2-1, width*3/4, height/2+1),
			image.NewUniform(color.RGBA{R: 0x0b, G: 0x4f, B: 0x8a, A: 0xff}),
			image.Point{}, draw.Src)
		var encoded bytes.Buffer
		if err := jpeg.Encode(&encoded, canvas, &jpeg.Options{Quality: 80}); err != nil {
			return
		}
		mockPictureData = encoded.Bytes()
	})
	return mockPictureData
}
