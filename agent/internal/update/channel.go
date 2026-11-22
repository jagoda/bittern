package update

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"bittern.invalid/agent/internal/release"
)

const (
	checkBodyLimit        = 4 << 20
	checkAssetLimit       = 1 << 20
	checkPayloadLimit     = 2 << 30
	checkPageLimit        = 3
	checkHTTPTimeout      = 20 * time.Second
	checkUserAgent        = "bittern-update-check"
	checkAccept           = "application/vnd.github+json"
	githubReleasesPerPage = 100
)

type channelError struct {
	Result  string
	Rule    string
	Message string
}

func (e *channelError) Error() string { return e.Rule + ": " + e.Message }

func checkFailure(result, rule, format string, args ...any) *channelError {
	return &channelError{Result: result, Rule: rule, Message: fmt.Sprintf(format, args...)}
}

func failureResult(err error) (string, string) {
	var channelErr *channelError
	if errors.As(err, &channelErr) {
		return channelErr.Result, channelErr.Error()
	}
	return CheckFailed, err.Error()
}

func (o CheckOptions) httpClient() *http.Client {
	if o.Client != nil {
		return o.Client
	}
	return &http.Client{
		Timeout: checkHTTPTimeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			if !o.AllowHTTP && request.URL.Scheme != "https" {
				return fmt.Errorf("redirect to non-HTTPS URL %s", request.URL.Redacted())
			}
			return nil
		},
	}
}

func (o CheckOptions) platform() (Platform, error) {
	if o.Platform != nil {
		return o.Platform.Platform()
	}
	return (&FilePlatformProvider{Path: o.PlatformPath, UID: o.UID}).Platform()
}

type githubRelease struct {
	TagName     string        `json:"tag_name"`
	Draft       bool          `json:"draft"`
	Prerelease  bool          `json:"prerelease"`
	PublishedAt string        `json:"published_at"`
	Assets      []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

func discoverCandidate(client *http.Client, options CheckOptions, channel string, committed int64, platform Platform, schema int64) (*CheckCandidate, string, string, error) {
	releases, err := fetchReleases(client, options)
	if err != nil {
		result, reason := failureResult(err)
		return nil, result, reason, err
	}
	selected, tagGeneration, err := selectRelease(releases, channel, committed)
	if err != nil {
		result, reason := failureResult(err)
		return nil, result, reason, err
	}
	if selected == nil {
		return nil, CheckUpToDate, "", nil
	}
	manifestAsset, err := findAsset(selected, "manifest.json")
	if err != nil {
		return nil, CheckFailed, err.Error(), err
	}
	signatureAsset, err := findAsset(selected, "manifest.json.sig")
	if err != nil {
		return nil, CheckFailed, err.Error(), err
	}
	manifestBytes, err := fetchAsset(client, options, manifestAsset.BrowserDownloadURL, checkAssetLimit)
	if err != nil {
		result, reason := failureResult(err)
		return nil, result, reason, err
	}
	signature, err := fetchAsset(client, options, signatureAsset.BrowserDownloadURL, signatureLimit)
	if err != nil {
		result, reason := failureResult(err)
		return nil, result, reason, err
	}
	if len(signature) != 64 {
		err := checkFailure(CheckFailed, "signature.length", "the manifest signature is %d bytes, not 64", len(signature))
		return nil, CheckFailed, err.Error(), err
	}
	trust, err := release.LoadTrustStore(options.TrustImageDir, options.TrustRotationDir)
	if err != nil {
		return nil, CheckFailed, err.Error(), err
	}
	manifest, err := release.VerifyManifest(manifestBytes, signature, trust, release.KindAppBundle, committed)
	if err != nil {
		return nil, CheckFailed, err.Error(), err
	}
	if manifest.Generation != tagGeneration {
		err := checkFailure(CheckFailed, "channel.tag-generation", "the tag %s decodes generation %d but the manifest names generation %d", selected.TagName, tagGeneration, manifest.Generation)
		return nil, CheckFailed, err.Error(), err
	}
	if manifest.Channel != channel {
		err := checkFailure(CheckFailed, "channel.mismatch", "the manifest channel %q is not the device channel %q", manifest.Channel, channel)
		return nil, CheckFailed, err.Error(), err
	}
	if err := verifyCompatibility(manifest, platform, schema); err != nil {
		return nil, CheckFailed, err.Error(), err
	}
	payloadAsset, err := findAsset(selected, manifest.Payload.Filename)
	if err != nil {
		return nil, CheckFailed, err.Error(), err
	}
	candidate := &CheckCandidate{
		Tag:            selected.TagName,
		Generation:     manifest.Generation,
		Version:        manifest.Version,
		ManifestSHA256: sha256Hex(manifestBytes),
		SigningKeyID:   manifest.Signing.KeyID,
		SourceCommit:   manifest.SourceCommit,
		ManifestURL:    manifestAsset.BrowserDownloadURL,
		SignatureURL:   signatureAsset.BrowserDownloadURL,
		PayloadURL:     payloadAsset.BrowserDownloadURL,
	}
	return candidate, CheckUpdateAvailable, "", nil
}

func selectRelease(releases []githubRelease, channel string, committed int64) (*githubRelease, int64, error) {
	prefix, err := release.TagPrefix(channel)
	if err != nil {
		return nil, 0, checkFailure(CheckFailed, "channel.prefix", "%v", err)
	}
	var selected *githubRelease
	var selectedGeneration int64
	for index := range releases {
		candidate := &releases[index]
		if candidate.Draft || candidate.PublishedAt == "" {
			continue
		}
		if !strings.HasPrefix(candidate.TagName, prefix) {
			continue
		}
		generation, parseErr := release.ParseChannelTag(candidate.TagName, channel)
		if parseErr != nil {
			return nil, 0, checkFailure(CheckFailed, "channel.tag", "the release tag is not in the channel namespace: %v", parseErr)
		}
		if generation <= committed {
			continue
		}
		if selected == nil || generation > selectedGeneration {
			selected = candidate
			selectedGeneration = generation
		}
	}
	return selected, selectedGeneration, nil
}

func findAsset(release *githubRelease, name string) (githubAsset, error) {
	for _, asset := range release.Assets {
		if asset.Name == name {
			if asset.BrowserDownloadURL == "" {
				return githubAsset{}, checkFailure(CheckFailed, "channel.asset", "the release asset %s has no download URL", name)
			}
			return asset, nil
		}
	}
	return githubAsset{}, checkFailure(CheckFailed, "channel.asset", "the release %s does not carry %s", release.TagName, name)
}

func releasesURL(apiURL string, page int) (string, error) {
	parsed, err := url.Parse(apiURL)
	if err != nil {
		return "", checkFailure(CheckFailed, "channel.url", "parse the release API URL: %v", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", checkFailure(CheckFailed, "channel.url", "the release API URL %s is not HTTP or HTTPS", parsed.Redacted())
	}
	query := parsed.Query()
	query.Set("per_page", strconv.Itoa(githubReleasesPerPage))
	if page > 1 {
		query.Set("page", strconv.Itoa(page))
	}
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func fetchReleases(client *http.Client, options CheckOptions) ([]githubRelease, error) {
	apiURL := strings.TrimSpace(options.APIURL)
	if apiURL == "" {
		var err error
		apiURL, err = defaultCheckAPIURL()
		if err != nil {
			return nil, err
		}
	}
	next := ""
	var releases []githubRelease
	for page := 1; page <= checkPageLimit; page++ {
		pageURL := next
		if pageURL == "" {
			var err error
			pageURL, err = releasesURL(apiURL, page)
			if err != nil {
				return nil, err
			}
		}
		pageReleases, following, err := fetchReleasePage(client, options, pageURL)
		if err != nil {
			return nil, err
		}
		releases = append(releases, pageReleases...)
		next = following
		if next == "" {
			return releases, nil
		}
	}
	return nil, checkFailure(CheckFailed, "channel.pagination", "the release list still has a next page after %d pages", checkPageLimit)
}

func fetchReleasePage(client *http.Client, options CheckOptions, pageURL string) ([]githubRelease, string, error) {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, pageURL, nil)
	if err != nil {
		return nil, "", checkFailure(CheckFailed, "channel.request", "build the release request: %v", err)
	}
	request.Header.Set("Accept", checkAccept)
	request.Header.Set("User-Agent", checkUserAgent)
	response, err := client.Do(request)
	if err != nil {
		return nil, "", checkFailure(CheckOffline, "channel.offline", "the appliance could not reach the release channel: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", responseFailure(response)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, checkBodyLimit+1))
	if err != nil {
		return nil, "", checkFailure(CheckOffline, "channel.read", "read the release list: %v", err)
	}
	if len(data) > checkBodyLimit {
		return nil, "", checkFailure(CheckFailed, "channel.size", "the release list exceeds the %d byte limit", checkBodyLimit)
	}
	var releases []githubRelease
	if err := json.Unmarshal(data, &releases); err != nil {
		return nil, "", checkFailure(CheckFailed, "channel.json", "decode the release list: %v", err)
	}
	return releases, nextLink(response.Header.Get("Link")), nil
}

func responseFailure(response *http.Response) error {
	remaining := response.Header.Get("X-RateLimit-Remaining")
	retryAfter := response.Header.Get("Retry-After")
	reset := response.Header.Get("X-RateLimit-Reset")
	if response.StatusCode == http.StatusTooManyRequests || (response.StatusCode == http.StatusForbidden && (remaining == "0" || retryAfter != "")) {
		detail := "the release channel rate limit is exhausted; retry later"
		if retryAfter != "" {
			detail = fmt.Sprintf("the release channel asked the appliance to retry after %s seconds", retryAfter)
		} else if reset != "" {
			if seconds, err := strconv.ParseInt(reset, 10, 64); err == nil {
				detail = "the release channel rate limit resets at " + time.Unix(seconds, 0).UTC().Format(time.RFC3339)
			}
		}
		return checkFailure(CheckRateLimited, "channel.rate-limit", "%s (HTTP %d)", detail, response.StatusCode)
	}
	if response.StatusCode == http.StatusNotFound {
		return checkFailure(CheckFailed, "channel.not-found", "the release channel returned HTTP 404; the repository or release list is missing")
	}
	return checkFailure(CheckFailed, "channel.http", "the release channel returned HTTP %d", response.StatusCode)
}

var nextLinkPattern = regexp.MustCompile(`<([^>]+)>\s*;\s*rel="?next"?`)

func nextLink(header string) string {
	match := nextLinkPattern.FindStringSubmatch(header)
	if len(match) != 2 {
		return ""
	}
	return match[1]
}

func fetchAsset(client *http.Client, options CheckOptions, assetURL string, limit int64) ([]byte, error) {
	parsed, err := url.Parse(assetURL)
	if err != nil {
		return nil, checkFailure(CheckFailed, "channel.asset-url", "parse the asset URL: %v", err)
	}
	if parsed.Scheme != "https" && !options.AllowHTTP {
		return nil, checkFailure(CheckFailed, "channel.asset-url", "the asset URL %s is not HTTPS", parsed.Redacted())
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, assetURL, nil)
	if err != nil {
		return nil, checkFailure(CheckFailed, "channel.request", "build the asset request: %v", err)
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", checkUserAgent)
	response, err := client.Do(request)
	if err != nil {
		return nil, checkFailure(CheckOffline, "channel.offline", "the appliance could not download the release asset: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, responseFailure(response)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, checkFailure(CheckOffline, "channel.read", "read the release asset: %v", err)
	}
	if int64(len(data)) > limit {
		return nil, checkFailure(CheckFailed, "channel.size", "the release asset exceeds the %d byte limit", limit)
	}
	return data, nil
}

// FetchCandidate downloads a verified candidate from the channel into a
// disposable local source directory. The helper still verifies everything
// again; this download is a convenience and never a trust decision.
func FetchCandidate(options CheckOptions, candidate CheckCandidate) (string, error) {
	options = normalizeCheckOptions(options)
	if candidate.Generation <= 0 || candidate.Tag == "" || candidate.ManifestURL == "" || candidate.SignatureURL == "" || candidate.PayloadURL == "" {
		return "", errorf("channel.candidate", "the check result has no complete candidate")
	}
	cfg, _, err := loadConfig(options.ConfigPath)
	if err != nil {
		return "", err
	}
	state, _, err := readState(normalizeOptions(Options{StateRoot: options.StateRoot}).paths().state)
	if err != nil {
		return "", err
	}
	platform, err := options.platform()
	if err != nil {
		return "", errorf("platform.read", "%v", err)
	}
	client := options.httpClient()
	manifestBytes, err := fetchAsset(client, options, candidate.ManifestURL, checkAssetLimit)
	if err != nil {
		return "", err
	}
	signature, err := fetchAsset(client, options, candidate.SignatureURL, signatureLimit)
	if err != nil {
		return "", err
	}
	if len(signature) != 64 {
		return "", errorf("signature.length", "the manifest signature is %d bytes, not 64", len(signature))
	}
	trust, err := release.LoadTrustStore(options.TrustImageDir, options.TrustRotationDir)
	if err != nil {
		return "", err
	}
	manifest, err := release.VerifyManifest(manifestBytes, signature, trust, release.KindAppBundle, state.CommittedGeneration)
	if err != nil {
		return "", err
	}
	if manifest.Generation != candidate.Generation {
		return "", errorf("channel.tag-generation", "the candidate generation %d does not match the verified manifest generation %d", candidate.Generation, manifest.Generation)
	}
	if sha256Hex(manifestBytes) != candidate.ManifestSHA256 {
		return "", errorf("channel.manifest-digest", "the downloaded manifest digest does not match the checked candidate")
	}
	if manifest.Channel != cfg.Updates.Channel {
		return "", errorf("channel.mismatch", "the manifest channel %q is not the device channel %q", manifest.Channel, cfg.Updates.Channel)
	}
	if err := verifyCompatibility(manifest, platform, int64(cfg.SchemaVersion)); err != nil {
		return "", err
	}
	if err := validatePayloadFilename(manifest.Payload.Filename); err != nil {
		return "", err
	}
	if manifest.Payload.Size <= 0 || manifest.Payload.Size > checkPayloadLimit {
		return "", errorf("payload.size", "the payload size %d is outside the supported range", manifest.Payload.Size)
	}
	payload, err := fetchAsset(client, options, candidate.PayloadURL, manifest.Payload.Size)
	if err != nil {
		return "", err
	}
	if int64(len(payload)) != manifest.Payload.Size {
		return "", errorf("payload.size", "the payload is %d bytes, not the manifest size %d", len(payload), manifest.Payload.Size)
	}
	digest := sha256.Sum256(payload)
	if err := manifest.VerifyPayload(int64(len(payload)), digest); err != nil {
		return "", err
	}
	incomingRoot := filepath.Join(options.StateRoot, "update", "incoming")
	if err := os.MkdirAll(incomingRoot, 0o700); err != nil {
		return "", fmt.Errorf("update: create the channel download root: %w", err)
	}
	incoming, err := os.MkdirTemp(incomingRoot, "candidate-*")
	if err != nil {
		return "", fmt.Errorf("update: create the channel download directory: %w", err)
	}
	if err := os.Chmod(incoming, 0o700); err != nil {
		_ = os.RemoveAll(incoming)
		return "", err
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.RemoveAll(incoming)
		}
	}()
	if err := writeFileAtomic(filepath.Join(incoming, "manifest.json"), manifestBytes, 0o600); err != nil {
		return "", err
	}
	if err := writeFileAtomic(filepath.Join(incoming, "manifest.json.sig"), signature, 0o600); err != nil {
		return "", err
	}
	if err := writeFileAtomic(filepath.Join(incoming, manifest.Payload.Filename), payload, 0o600); err != nil {
		return "", err
	}
	cleanup = false
	return incoming, nil
}

// IsIncomingSource reports whether a source path is a channel download under
// the state root. The helper removes such a directory when the transaction is
// final, so an aborted install does not leave a downloaded bundle behind.
func IsIncomingSource(stateRoot, sourcePath string) bool {
	if stateRoot == "" {
		stateRoot = defaultStateRoot
	}
	if sourcePath == "" {
		return false
	}
	root := filepath.Join(stateRoot, "update", "incoming")
	relative, err := filepath.Rel(root, sourcePath)
	if err != nil {
		return false
	}
	return relative != "." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && relative != ".."
}

func removeIncomingSource(stateRoot, sourcePath string) {
	if IsIncomingSource(stateRoot, sourcePath) {
		_ = os.RemoveAll(sourcePath)
	}
}
