package ui

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"bittern.invalid/agent/internal/update"
)

func postUpdateAction(t *testing.T, client *http.Client, target string, headers map[string]string) (int, http.Header, string) {
	t.Helper()
	form := url.Values{}
	request, err := http.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, response.Header, string(body)
}

func assertCardOnly(t *testing.T, body string) {
	t.Helper()
	assertContains(t, body,
		`id="update-card"`,
		`id="update-card-live"`,
		`id="update-card-actions"`,
	)
	assertNotContains(t, body,
		"<!DOCTYPE html>",
		"<html",
		"<head",
		"<body",
		`id="status-panel"`,
		"<h1>Appliance status</h1>",
		`class="status-grid"`,
	)
}

func assertFullStatusPage(t *testing.T, body string) {
	t.Helper()
	assertContains(t, body,
		"<!DOCTYPE html>",
		"<h1>Appliance status</h1>",
		`id="status-panel"`,
		`class="status-grid"`,
	)
}

func TestEveryInPlaceUpdateRouteAnswersWithTheCardFragment(t *testing.T) {
	for _, route := range uiRoutes {
		if !route.InPlace {
			continue
		}
		t.Run(route.Name, func(t *testing.T) {
			service := &fakeUpdateService{progress: UpdateProgress{Active: true, Kind: "install", Label: "Downloading the update"}}
			server, client := newTestServer(t, Options{
				Status:      fixedStatusProvider{document: MockStatusDocument("ready|available", testNow)},
				Updates:     service,
				MockPreview: true,
			})
			statusCode, _, body := postUpdateAction(t, client, server.URL+route.Pattern, map[string]string{"HX-Request": "true"})
			if statusCode != http.StatusOK {
				t.Fatalf("%s HX status: %d\n%s", route.Name, statusCode, body)
			}
			assertCardOnly(t, body)
		})
	}
}

func TestUpdateCardActionsReturnTheFullPageForANormalPost(t *testing.T) {
	tests := []struct {
		name string
		path string
		hint string
	}{
		{name: "check", path: "/ui/status/updates/check", hint: "available"},
		{name: "install", path: "/ui/status/updates/install", hint: "available"},
		{name: "retry", path: "/ui/status/updates/install", hint: "failed"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			service := &fakeUpdateService{}
			server, client := newTestServer(t, Options{
				Status:      fixedStatusProvider{document: MockStatusDocument("ready|"+testCase.hint, testNow)},
				Updates:     service,
				MockPreview: true,
			})
			statusCode, body := postPage(t, client, server.URL+testCase.path, url.Values{})
			if statusCode != http.StatusOK {
				t.Fatalf("%s normal post status: %d\n%s", testCase.name, statusCode, body)
			}
			assertFullStatusPage(t, body)
			_, _, inlineBody := postUpdateAction(t, client, server.URL+testCase.path, map[string]string{"HX-Request": "true"})
			assertCardOnly(t, inlineBody)
		})
	}
}

func TestRollbackConfirmationIsANavigationNotAnInPlaceSwap(t *testing.T) {
	service := &fakeUpdateService{}
	server, client := newTestServer(t, Options{
		Status:      fixedStatusProvider{document: MockStatusDocument("ready|failed", testNow)},
		Updates:     service,
		MockPreview: true,
	})
	statusCode, body := postPage(t, client, server.URL+"/ui/status/updates/rollback", url.Values{})
	if statusCode != http.StatusOK {
		t.Fatalf("normal rollback status: %d\n%s", statusCode, body)
	}
	assertFullStatusPage(t, body)

	statusCode, headers, body := postUpdateAction(t, client, server.URL+"/ui/status/updates/rollback", map[string]string{"HX-Request": "true"})
	if statusCode != http.StatusNoContent {
		t.Fatalf("HX rollback status: %d\n%s", statusCode, body)
	}
	if target := headers.Get("HX-Redirect"); target == "" || !strings.Contains(target, "/ui/status") {
		t.Fatalf("HX rollback redirect = %q, want the full status page", target)
	}
	if strings.Contains(body, `id="update-card"`) {
		t.Fatalf("rollback returned the card fragment for an HX request: %s", body)
	}
}

func TestUpdateFailureViewKeepsTheStartDetailOutOfThePrimaryCopy(t *testing.T) {
	progress := startFailureProgress(update.NewStartError(update.StartSpace, nil, "the state root has 10 bytes free, less than the 20 bytes required"))
	document := MockStatusDocument("ready|available", testNow)
	view := buildUpdateView(document, progress, time.UTC, update.RecoveryStatus{State: update.RecoveryNone}, nil, "")
	if view.Summary != progress.Failure {
		t.Fatalf("summary = %q, want the primary sentence", view.Summary)
	}
	if strings.Contains(view.Summary, progress.FailureDetail) || strings.Contains(view.Summary, update.StartSpace) {
		t.Fatalf("the technical detail leaked into the primary copy: %q", view.Summary)
	}
	if view.TechnicalStartReason != progress.FailureDetail {
		t.Fatalf("technical reason = %q, want %q", view.TechnicalStartReason, progress.FailureDetail)
	}
	if view.FailureAction == "" || view.FailedStep == "" {
		t.Fatalf("failure action or step is empty: %+v", view)
	}
}
