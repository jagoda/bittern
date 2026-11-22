package update

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"bittern.invalid/agent/internal/release"
)

type channelRelease struct {
	tag       string
	dir       string
	draft     bool
	published bool
}

type fakeChannel struct {
	t        *testing.T
	server   *httptest.Server
	mu       sync.Mutex
	releases []channelRelease
	requests int
	mode     string
}

func newFakeChannel(t *testing.T) *fakeChannel {
	t.Helper()
	channel := &fakeChannel{t: t}
	channel.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases" {
			channel.mu.Lock()
			channel.requests++
			mode := channel.mode
			releases := append([]channelRelease(nil), channel.releases...)
			channel.mu.Unlock()
			switch mode {
			case "rate-limited":
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", "1790000000")
				w.WriteHeader(http.StatusForbidden)
				return
			case "malformed":
				_, _ = w.Write([]byte("{"))
				return
			}
			payload := make([]map[string]any, 0, len(releases))
			for _, item := range releases {
				payload = append(payload, map[string]any{
					"tag_name":     item.tag,
					"draft":        item.draft,
					"prerelease":   false,
					"published_at": publishedAt(item.published),
					"assets": []map[string]any{
						{"name": "manifest.json", "browser_download_url": channel.assetURL(item.tag, "manifest.json")},
						{"name": "manifest.json.sig", "browser_download_url": channel.assetURL(item.tag, "manifest.json.sig")},
						{"name": "bittern-app.tar.gz", "browser_download_url": channel.assetURL(item.tag, "bittern-app.tar.gz")},
						{"name": "bittern-beta-kit.tar.gz", "browser_download_url": channel.assetURL(item.tag, "bittern-beta-kit.tar.gz")},
					},
				})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(payload)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/assets/"), "/")
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		tag, name := parts[0], parts[1]
		for _, item := range channel.releases {
			if item.tag != tag {
				continue
			}
			data, err := os.ReadFile(filepath.Join(item.dir, name))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(data)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(channel.server.Close)
	return channel
}

func publishedAt(published bool) any {
	if !published {
		return nil
	}
	return "2026-11-22T10:00:00Z"
}

func (c *fakeChannel) assetURL(tag, name string) string {
	return c.server.URL + "/assets/" + tag + "/" + name
}

func (c *fakeChannel) add(tag, dir string, draft bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.releases = append(c.releases, channelRelease{tag: tag, dir: dir, draft: draft, published: !draft})
}

func (c *fakeChannel) setMode(mode string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mode = mode
}

func (c *fakeChannel) requestCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.requests
}

func channelCheckOptions(t *testing.T, env *testEnv, channel *fakeChannel) CheckOptions {
	t.Helper()
	return CheckOptions{
		StateRoot:        env.stateRoot,
		ConfigPath:       env.configPath,
		TrustImageDir:    env.trustDir,
		TrustRotationDir: env.rotationDir,
		APIURL:           channel.server.URL + "/releases",
		AllowHTTP:        true,
		Platform:         PlatformProviderFunc(func() (Platform, error) { return env.platform, nil }),
		Now:              func() time.Time { return time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC) },
	}
}

func betaBundle(t *testing.T, env *testEnv, generation int64) (string, []byte, []byte) {
	t.Helper()
	dir := env.newSourceDir(t)
	manifest, canonical, signature := env.writeBundle(t, dir, bundleSpec{
		generation: generation,
		files:      defaultFiles(),
		mutate: func(manifest *release.Manifest) {
			manifest.Channel = release.ChannelBeta
		},
	})
	_ = manifest
	return dir, canonical, signature
}

func writeCommittedState(t *testing.T, env *testEnv, generation int64) {
	t.Helper()
	options := normalizeOptions(Options{StateRoot: env.stateRoot})
	if err := options.paths().ensure(); err != nil {
		t.Fatal(err)
	}
	state := State{
		SchemaVersion:       SchemaVersion,
		Channel:             release.ChannelBeta,
		CommittedGeneration: generation,
		CommittedVersion:    "0.1.0",
	}
	if err := writeState(options.paths().state, &state); err != nil {
		t.Fatal(err)
	}
}

func TestCheckChannelFindsAHigherGeneration(t *testing.T) {
	env := newTestEnv(t)
	if err := writeTestConfig(env.configPath, release.ChannelBeta, false); err != nil {
		t.Fatal(err)
	}
	dir, _, _ := betaBundle(t, env, 1)
	channel := newFakeChannel(t)
	channel.add("app-beta-000001", dir, false)

	result, err := CheckChannel(channelCheckOptions(t, env, channel))
	if err != nil {
		t.Fatalf("check failed: %v", err)
	}
	if result.Result != CheckUpdateAvailable || result.Available == nil {
		t.Fatalf("result = %+v, want an available generation 1", result)
	}
	if result.Available.Generation != 1 || result.Available.Version != "0.1.1" {
		t.Fatalf("candidate = %+v", result.Available)
	}
	if _, err := os.Stat(CheckPath(env.stateRoot)); err != nil {
		t.Fatalf("check record missing: %v", err)
	}
}

func TestCheckChannelTreatsGenerationZeroAsAnyPublishedUpdate(t *testing.T) {
	env := newTestEnv(t)
	if err := writeTestConfig(env.configPath, release.ChannelBeta, false); err != nil {
		t.Fatal(err)
	}
	dir, _, _ := betaBundle(t, env, 1)
	channel := newFakeChannel(t)
	channel.add("app-beta-000001", dir, false)

	result, err := CheckChannel(channelCheckOptions(t, env, channel))
	if err != nil {
		t.Fatalf("a baseline device with no bundle failed the check: %v", err)
	}
	if result.Result != CheckUpdateAvailable {
		t.Fatalf("result = %s, want update-available", result.Result)
	}
	if _, err := os.Stat(filepath.Join(env.stateRoot, "update", "journal.json")); !os.IsNotExist(err) {
		t.Fatalf("check wrote a journal: %v", err)
	}
}

func TestCheckChannelIgnoresDraftsAndTheOtherChannel(t *testing.T) {
	env := newTestEnv(t)
	if err := writeTestConfig(env.configPath, release.ChannelBeta, false); err != nil {
		t.Fatal(err)
	}
	draftDir, _, _ := betaBundle(t, env, 5)
	stableDir, _, _ := betaBundle(t, env, 6)
	betaDir, _, _ := betaBundle(t, env, 2)
	channel := newFakeChannel(t)
	channel.add("app-beta-000005", draftDir, true)
	channel.add("app-stable-000006", stableDir, false)
	channel.add("app-beta-000002", betaDir, false)

	result, err := CheckChannel(channelCheckOptions(t, env, channel))
	if err != nil {
		t.Fatalf("check failed: %v", err)
	}
	if result.Result != CheckUpdateAvailable || result.Available == nil || result.Available.Generation != 2 {
		t.Fatalf("result = %+v, want beta generation 2", result)
	}
}

func TestCheckChannelReportsUpToDateForALowerGeneration(t *testing.T) {
	env := newTestEnv(t)
	if err := writeTestConfig(env.configPath, release.ChannelBeta, false); err != nil {
		t.Fatal(err)
	}
	writeCommittedState(t, env, 2)
	dir, _, _ := betaBundle(t, env, 1)
	channel := newFakeChannel(t)
	channel.add("app-beta-000001", dir, false)

	result, err := CheckChannel(channelCheckOptions(t, env, channel))
	if err != nil {
		t.Fatalf("check failed: %v", err)
	}
	if result.Result != CheckUpToDate || result.Available != nil {
		t.Fatalf("result = %+v, want up-to-date with no candidate", result)
	}
}

func TestCheckChannelRejectsATagThatDisagreesWithTheManifest(t *testing.T) {
	env := newTestEnv(t)
	if err := writeTestConfig(env.configPath, release.ChannelBeta, false); err != nil {
		t.Fatal(err)
	}
	dir, _, _ := betaBundle(t, env, 1)
	channel := newFakeChannel(t)
	channel.add("app-beta-000002", dir, false)

	result, err := CheckChannel(channelCheckOptions(t, env, channel))
	if err == nil {
		t.Fatalf("a tag/manifest disagreement was accepted: %+v", result)
	}
	if result.Result != CheckFailed || result.Available != nil {
		t.Fatalf("result = %+v, want failed with no candidate", result)
	}
	if !strings.Contains(result.Reason, "channel.tag-generation") {
		t.Fatalf("reason = %q, want channel.tag-generation", result.Reason)
	}
}

func TestCheckChannelRejectsABadSignature(t *testing.T) {
	env := newTestEnv(t)
	if err := writeTestConfig(env.configPath, release.ChannelBeta, false); err != nil {
		t.Fatal(err)
	}
	dir, _, _ := betaBundle(t, env, 1)
	if err := os.WriteFile(filepath.Join(dir, "manifest.json.sig"), make([]byte, 64), 0o600); err != nil {
		t.Fatal(err)
	}
	channel := newFakeChannel(t)
	channel.add("app-beta-000001", dir, false)

	result, err := CheckChannel(channelCheckOptions(t, env, channel))
	if err == nil {
		t.Fatalf("a bad signature was accepted: %+v", result)
	}
	if result.Result != CheckFailed || result.Available != nil {
		t.Fatalf("result = %+v, want failed with no candidate", result)
	}
}

func TestCheckChannelReportsARateLimitAsRetryLater(t *testing.T) {
	env := newTestEnv(t)
	if err := writeTestConfig(env.configPath, release.ChannelBeta, false); err != nil {
		t.Fatal(err)
	}
	channel := newFakeChannel(t)
	channel.setMode("rate-limited")

	result, err := CheckChannel(channelCheckOptions(t, env, channel))
	if err == nil {
		t.Fatal("a rate-limited response was accepted")
	}
	if result.Result != CheckRateLimited {
		t.Fatalf("result = %s, want rate-limited", result.Result)
	}
	if result.Result == CheckUpToDate {
		t.Fatal("a rate limit was reported as up to date")
	}
}

func TestCheckChannelReportsOfflineWithoutClaimingUpToDate(t *testing.T) {
	env := newTestEnv(t)
	if err := writeTestConfig(env.configPath, release.ChannelBeta, false); err != nil {
		t.Fatal(err)
	}
	channel := newFakeChannel(t)
	channel.server.Close()
	options := channelCheckOptions(t, env, channel)

	result, err := CheckChannel(options)
	if err == nil {
		t.Fatal("an offline channel was accepted")
	}
	if result.Result != CheckOffline {
		t.Fatalf("result = %s, want offline", result.Result)
	}
	if result.Result == CheckUpToDate {
		t.Fatal("an offline channel was reported as up to date")
	}
}

func TestCheckChannelRejectsAMalformedResponse(t *testing.T) {
	env := newTestEnv(t)
	if err := writeTestConfig(env.configPath, release.ChannelBeta, false); err != nil {
		t.Fatal(err)
	}
	channel := newFakeChannel(t)
	channel.setMode("malformed")

	result, err := CheckChannel(channelCheckOptions(t, env, channel))
	if err == nil {
		t.Fatal("a malformed response was accepted")
	}
	if result.Result != CheckFailed {
		t.Fatalf("result = %s, want failed", result.Result)
	}
}

func TestCheckChannelCachesInsideTheMinimumInterval(t *testing.T) {
	env := newTestEnv(t)
	if err := writeTestConfig(env.configPath, release.ChannelBeta, false); err != nil {
		t.Fatal(err)
	}
	dir, _, _ := betaBundle(t, env, 1)
	channel := newFakeChannel(t)
	channel.add("app-beta-000001", dir, false)
	options := channelCheckOptions(t, env, channel)
	options.MinInterval = 15 * time.Minute

	first, err := CheckChannel(options)
	if err != nil {
		t.Fatalf("first check failed: %v", err)
	}
	second, err := CheckChannel(options)
	if err != nil {
		t.Fatalf("cached check failed: %v", err)
	}
	if !second.Cached || second.CheckedAt != first.CheckedAt {
		t.Fatalf("second check = %+v, want the cached first result", second)
	}
	if channel.requestCount() != 1 {
		t.Fatalf("request count = %d, want one", channel.requestCount())
	}
}

func TestCheckChannelNeverInstallsAnything(t *testing.T) {
	env := newTestEnv(t)
	if err := writeTestConfig(env.configPath, release.ChannelBeta, false); err != nil {
		t.Fatal(err)
	}
	dir, _, _ := betaBundle(t, env, 1)
	channel := newFakeChannel(t)
	channel.add("app-beta-000001", dir, false)
	beforePointer := readCurrentForTest(t, env.stateRoot)

	if _, err := CheckChannel(channelCheckOptions(t, env, channel)); err != nil {
		t.Fatalf("check failed: %v", err)
	}
	if after := readCurrentForTest(t, env.stateRoot); after != beforePointer {
		t.Fatalf("pointer changed from %d to %d", beforePointer, after)
	}
	assertNoJournal(t, env.stateRoot)
	if _, err := os.Stat(RequestPath(env.stateRoot)); !os.IsNotExist(err) {
		t.Fatalf("check wrote a request: %v", err)
	}
	if record, err := LatestHistory(env.stateRoot); err != nil {
		t.Fatal(err)
	} else if record != nil {
		t.Fatalf("check wrote a history record: %+v", record)
	}
	if calls := env.system.snapshot(); len(calls) != 0 {
		t.Fatalf("check touched units: %v", calls)
	}
}

func TestFetchCandidateDownloadsTheVerifiedBundle(t *testing.T) {
	env := newTestEnv(t)
	if err := writeTestConfig(env.configPath, release.ChannelBeta, false); err != nil {
		t.Fatal(err)
	}
	dir, _, _ := betaBundle(t, env, 1)
	channel := newFakeChannel(t)
	channel.add("app-beta-000001", dir, false)
	options := channelCheckOptions(t, env, channel)
	checked, err := CheckChannel(options)
	if err != nil {
		t.Fatalf("check failed: %v", err)
	}
	if checked.Available == nil {
		t.Fatal("check returned no candidate")
	}
	incoming, err := FetchCandidate(options, *checked.Available)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if !IsIncomingSource(env.stateRoot, incoming) {
		t.Fatalf("download %s is not under the incoming root", incoming)
	}
	for _, name := range []string{"manifest.json", "manifest.json.sig", "bittern-app.tar.gz"} {
		if _, err := os.Stat(filepath.Join(incoming, name)); err != nil {
			t.Fatalf("downloaded bundle is missing %s: %v", name, err)
		}
	}
}
