package ui

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"bittern.invalid/agent/internal/auth"
	"bittern.invalid/agent/internal/status"
)

type fakeUpdateService struct {
	checks    int
	installs  int
	rollbacks int
	progress  UpdateProgress
}

func (f *fakeUpdateService) Check(context.Context) error {
	f.checks++
	return nil
}
func (f *fakeUpdateService) StartInstall(context.Context) error {
	f.installs++
	return nil
}
func (f *fakeUpdateService) StartRollback(context.Context) error {
	f.rollbacks++
	return nil
}
func (f *fakeUpdateService) Progress() UpdateProgress { return f.progress }

func TestUpdateCardCoversEveryState(t *testing.T) {
	tests := []struct {
		hint      string
		want      []string
		forbidden []string
	}{
		{
			hint: "baseline",
			want: []string{
				"Factory version 0.1.0-beta.0",
				"Version 0.1.1 is available",
				"Available version",
				"Update number",
				`action="/ui/status/updates/install`,
				"Install update",
			},
			forbidden: []string{"generation", "bundle", "channel", "manifest", "transaction"},
		},
		{
			hint: "up-to-date",
			want: []string{
				"Up to date",
				"Installed version",
				"0.1.1",
				"Last checked",
				"Check result",
				`action="/ui/status/updates/check`,
			},
			forbidden: []string{`action="/ui/status/updates/install`, "generation", "bundle", "channel", "manifest", "transaction"},
		},
		{
			hint: "available",
			want: []string{
				"Update available",
				"Version 0.1.2 is available",
				"Update number",
				"2",
				"Install update",
			},
			forbidden: []string{"generation", "bundle", "channel", "manifest", "transaction"},
		},
		{
			hint: "installing",
			want: []string{
				"Installing update 0.1.2",
				"starting the recording service",
				"17 of 22 steps completed",
				`hx-trigger="every 2s"`,
			},
			forbidden: []string{`action="/ui/status/updates/install`, `action="/ui/status/updates/check`, "/ui/status/updates/rollback", "generation", "bundle", "channel", "manifest", "transaction"},
		},
		{
			hint: "failed",
			want: []string{
				"Update failed",
				"Stopped while",
				"checking the appliance",
				"The new version did not pass its check",
				"Try update again",
				"Roll back to the factory version",
			},
			forbidden: []string{"generation", "bundle", "channel", "manifest", "transaction"},
		},
		{
			hint: "rolled-back",
			want: []string{
				"Rolled back to version 0.1.1",
				"Last installed version",
				"0.1.2",
				"The next update must be newer than version 0.1.2.",
				"Configuration and recordings were not reverted",
			},
			forbidden: []string{"generation", "bundle", "channel", "manifest", "transaction"},
		},
		{
			hint: "offline",
			want: []string{
				"Could not reach the update server",
				"cannot say whether an update is available",
				"Could not check",
			},
			forbidden: []string{"Up to date", "generation", "bundle", "channel", "manifest", "transaction"},
		},
		{
			hint: "rate-limited",
			want: []string{
				"The update server asked Bittern to wait",
				"cannot say whether an update is available",
			},
			forbidden: []string{"Up to date", "generation", "bundle", "channel", "manifest", "transaction"},
		},
		{
			hint: "check-failed",
			want: []string{
				"Update check failed",
				"could not read the update list",
			},
			forbidden: []string{"Up to date", "generation", "bundle", "channel", "manifest", "transaction"},
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.hint, func(t *testing.T) {
			server, client := newTestServer(t, Options{
				Status:      MockStatusProvider{Now: func() time.Time { return testNow }},
				MockPreview: true,
			})
			pageStatus, body := getPage(t, client, server.URL+"/ui/status?update="+testCase.hint)
			if pageStatus != http.StatusOK {
				t.Fatalf("status page: %d", pageStatus)
			}
			assertContains(t, body, testCase.want...)
			for _, forbidden := range testCase.forbidden {
				if strings.Contains(body, forbidden) {
					t.Errorf("update card contains %q", forbidden)
				}
			}
		})
	}
}

func TestUpdateCardNeverBlanksWhenUpdatesAreUnknown(t *testing.T) {
	document := status.Document{Sections: status.Sections{Updates: status.UpdatesSection{State: status.SectionUnknown}}}
	server, client := newTestServer(t, Options{Status: fixedStatusProvider{document: document}})
	pageStatus, body := getPage(t, client, server.URL+"/ui/status")
	if pageStatus != http.StatusOK {
		t.Fatalf("status page: %d", pageStatus)
	}
	assertContains(t, body, "Update state is not available", "Check for updates")
}

func TestUpdateCardOffersRollbackOnlyWhenATargetExists(t *testing.T) {
	server, client := newTestServer(t, Options{
		Status:      MockStatusProvider{Now: func() time.Time { return testNow }},
		MockPreview: true,
	})
	_, withTarget := getPage(t, client, server.URL+"/ui/status?update=failed")
	assertContains(t, withTarget, "/ui/status/updates/rollback")
	_, withoutTarget := getPage(t, client, server.URL+"/ui/status?update=up-to-date")
	if strings.Contains(withoutTarget, "/ui/status/updates/rollback") {
		t.Fatal("up-to-date update card offers a rollback with no target")
	}
}

func TestUpdateCardRollbackConfirmationStatesTheLimitation(t *testing.T) {
	server, client := newTestServer(t, Options{
		Status:      MockStatusProvider{Now: func() time.Time { return testNow }},
		MockPreview: true,
	})
	pageStatus, body := getPage(t, client, server.URL+"/ui/status/updates/rollback?update=failed")
	if pageStatus != http.StatusOK {
		t.Fatalf("rollback page: %d", pageStatus)
	}
	assertContains(t, body,
		"Roll back update",
		"What changes",
		"does not revert the configuration or the recordings",
		"Configuration is not reverted",
		"may not be able to read it",
		"Roll back update",
		"Cancel",
	)
	pageStatus, body = getPage(t, client, server.URL+"/ui/status/updates/rollback")
	if pageStatus != http.StatusOK {
		t.Fatalf("rollback page without target: %d", pageStatus)
	}
	assertContains(t, body, "No previous version is available", "Nothing has changed")
}

func TestUpdateMutationsRequireSessionAndCSRF(t *testing.T) {
	server := newAuthTestServer(t, "setup-token-value-0123456789", auth.DefaultRateLimitPolicy())
	completeSetup(t, server)
	login(t, server)

	for _, path := range []string{
		"/ui/status/updates/check",
		"/ui/status/updates/install",
		"/ui/status/updates/rollback",
	} {
		pageStatus, body := requestPage(t, server.client, http.MethodPost, server.server.URL+path, url.Values{})
		if pageStatus != http.StatusForbidden {
			t.Fatalf("%s without CSRF: %d\n%s", path, pageStatus, body)
		}
	}

	anonymous := server.server.Client()
	anonymous.Jar = nil
	anonymous.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	for _, path := range []string{
		"/ui/status/updates/check",
		"/ui/status/updates/install",
		"/ui/status/updates/rollback",
	} {
		response, err := anonymous.PostForm(server.server.URL+path, url.Values{})
		if err != nil {
			t.Fatal(err)
		}
		location := response.Header.Get("Location")
		response.Body.Close()
		if response.StatusCode != http.StatusSeeOther || !strings.Contains(location, "/ui/login") {
			t.Fatalf("%s without a session: %d %s", path, response.StatusCode, location)
		}
	}
}

func TestUpdateMutationsArePostsAndRedirectToStatus(t *testing.T) {
	service := &fakeUpdateService{}
	document := MockStatusDocument("ready|failed", testNow)
	server, client := newTestServer(t, Options{
		Status:      fixedStatusProvider{document: document},
		Updates:     service,
		MockPreview: true,
	})
	for _, path := range []string{
		"/ui/status/updates/check",
		"/ui/status/updates/install",
		"/ui/status/updates/rollback",
	} {
		statusCode, body := postPage(t, client, server.URL+path, url.Values{})
		if statusCode != http.StatusOK {
			t.Fatalf("%s status: %d\n%s", path, statusCode, body)
		}
	}
	if service.checks != 1 || service.installs != 1 || service.rollbacks != 1 {
		t.Fatalf("service calls = checks %d, installs %d, rollbacks %d", service.checks, service.installs, service.rollbacks)
	}
}

func TestUpdateMutationsAreNotReachableByGET(t *testing.T) {
	service := &fakeUpdateService{}
	server, client := newTestServer(t, Options{Updates: service, MockPreview: true})
	for _, path := range []string{
		"/ui/status/updates/check",
		"/ui/status/updates/install",
	} {
		statusCode, _ := getPage(t, client, server.URL+path)
		if statusCode != http.StatusNotFound {
			t.Fatalf("GET %s: %d, want 404", path, statusCode)
		}
	}
	if service.checks != 0 || service.installs != 0 {
		t.Fatalf("GET reached a mutation: checks %d, installs %d", service.checks, service.installs)
	}
}

func TestUpdatePreparingStatePollsTheStatusFragment(t *testing.T) {
	service := &fakeUpdateService{progress: UpdateProgress{Active: true, Kind: "install", Label: "Downloading the update"}}
	server, client := newTestServer(t, Options{
		Status:      MockStatusProvider{Now: func() time.Time { return testNow }},
		Updates:     service,
		MockPreview: true,
	})
	pageStatus, body := getPage(t, client, server.URL+"/ui/status")
	if pageStatus != http.StatusOK {
		t.Fatalf("status page: %d", pageStatus)
	}
	assertContains(t, body, "Preparing the update", "Downloading the update", `hx-trigger="every 2s"`)
	assertNotContains(t, body, `action="/ui/status/updates/install`, `action="/ui/status/updates/check`)
}

func assertNotContains(t *testing.T, body string, values ...string) {
	t.Helper()
	for _, value := range values {
		if strings.Contains(body, value) {
			t.Errorf("page contains %q", value)
		}
	}
}
