package ui

import (
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"bittern.invalid/agent/internal/auth"
)

const routeTestSetupToken = "route-table-setup-token"

func TestRouteTableResponses(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range uiRoutes {
		route := route
		t.Run(route.Name, func(t *testing.T) {
			status, body := exerciseRoute(t, route)
			if status != route.Status {
				t.Fatalf("status = %d, want %d\n%s", status, route.Status, body)
			}
			if route.Template == "" {
				return
			}
			if templates.Lookup(route.Template) == nil {
				t.Fatalf("template %q is not embedded", route.Template)
			}
			if route.Name == "status-fragment" || route.Name == "status-updates-fragment" || route.Name == "wizard-operation-fragment" {
				if strings.Contains(body, "<!DOCTYPE html>") || strings.Contains(body, "<main") {
					t.Fatalf("%s is a full page", route.Name)
				}
				return
			}
			checkPageMarkup(t, route.Name, body)
		})
	}
}

func TestNoRouteIsRegisteredOutsideTheTable(t *testing.T) {
	_, mux, err := newHandler(Options{})
	if err != nil {
		t.Fatal(err)
	}
	table := make(map[string]route, len(uiRoutes))
	for _, route := range uiRoutes {
		if route.Name == "" {
			t.Error("route has no name")
		}
		if route.Pattern == "" {
			t.Errorf("%s: route has no pattern", route.Name)
		}
		if route.Handler == nil {
			t.Errorf("%s: route has no handler", route.Name)
		}
		registration := route.registration()
		if _, exists := table[registration]; exists {
			t.Errorf("%s: duplicate route registration %q", route.Name, registration)
		}
		table[registration] = route
	}
	if len(mux.registered) != len(table) {
		t.Errorf("mux registers %d handlers, route table has %d", len(mux.registered), len(table))
	}
	for _, pattern := range mux.registered {
		if _, ok := table[pattern]; !ok {
			t.Errorf("handler registered at %q is not in uiRoutes", pattern)
		}
	}
	for pattern := range table {
		registered := false
		for _, candidate := range mux.registered {
			if candidate == pattern {
				registered = true
				break
			}
		}
		if !registered {
			t.Errorf("route table entry %q is not registered on the mux", pattern)
		}
	}
}

func TestRouteTableMarksEveryFullPageResponsive(t *testing.T) {
	templates, err := parseTemplates()
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range uiRoutes {
		if route.Template != "" {
			if templates.Lookup(route.Template) == nil {
				t.Errorf("%s: template %q is not embedded", route.Name, route.Template)
			}
		}
		if route.Responsive && route.Template == "" && route.Name != "camera-picture" {
			t.Errorf("%s: responsive route has no template", route.Name)
		}
		if route.Name != "status-fragment" && route.Name != "status-updates-fragment" && route.Name != "wizard-operation-fragment" &&
			route.Template != "" && !route.Responsive && route.Method == http.MethodGet {
			t.Errorf("%s: full-page GET route is not marked responsive", route.Name)
		}
	}
}

func routePageBody(t *testing.T, route route) string {
	t.Helper()
	status, body := exerciseRoute(t, route)
	if status != route.Status {
		t.Fatalf("%s status = %d, want %d\n%s", route.Name, status, route.Status, body)
	}
	return body
}

func routeRequestPath(route route) string {
	switch route.Name {
	case "not-found":
		return BasePath + "/missing"
	case "static":
		return BasePath + "/static/htmx-2.0.11.min.js"
	}
	path := strings.ReplaceAll(route.Pattern, "{id}", "front_door")
	path = strings.ReplaceAll(path, "{ref}", "c1")
	return strings.ReplaceAll(path, "{$}", "")
}

func exerciseRoute(t *testing.T, route route) (int, string) {
	t.Helper()
	path := routeRequestPath(route)
	switch route.Name {
	case "root", "root-slash":
		server, client := newTestServer(t, Options{})
		disableRedirects(client)
		return getPage(t, client, server.URL+path)
	case "login":
		server := newAuthTestServer(t, routeTestSetupToken, auth.DefaultRateLimitPolicy())
		if _, err := server.store.CompleteSetup(routeTestSetupToken, testPassword, "127.0.0.1:1"); err != nil {
			t.Fatal(err)
		}
		return getPage(t, server.client, server.server.URL+path)
	case "login-submit":
		server := newAuthTestServer(t, routeTestSetupToken, auth.DefaultRateLimitPolicy())
		if _, err := server.store.CompleteSetup(routeTestSetupToken, testPassword, "127.0.0.1:1"); err != nil {
			t.Fatal(err)
		}
		_, body := getPage(t, server.client, server.server.URL+"/ui/login")
		return postPage(t, server.client, server.server.URL+path, url.Values{
			auth.CSRFFormField: {csrfFromPage(t, body)},
			"username":         {auth.AdministratorUsername},
			"password":         {"wrong password"},
		})
	case "setup":
		server := newAuthTestServer(t, routeTestSetupToken, auth.DefaultRateLimitPolicy())
		return getPage(t, server.client, server.server.URL+path)
	case "setup-submit":
		server := newAuthTestServer(t, routeTestSetupToken, auth.DefaultRateLimitPolicy())
		_, body := getPage(t, server.client, server.server.URL+"/ui/setup")
		return postPage(t, server.client, server.server.URL+path, url.Values{
			auth.CSRFFormField: {csrfFromPage(t, body)},
			"setup_token":      {"wrong token"},
			"password":         {testPassword},
			"password_confirm": {testPassword},
		})
	case "logout":
		server, client := newTestServer(t, Options{})
		disableRedirects(client)
		return postPage(t, client, server.URL+path, url.Values{})
	case "wizard-review":
		server, client := newTestServer(t, Options{})
		return http.StatusOK, completeToReview(t, client, server.URL)
	case "wizard-apply":
		server, client := newTestServer(t, Options{Applier: &fakeApplier{err: errors.New("apply failed")}})
		completeToReview(t, client, server.URL)
		disableRedirects(client)
		return postPage(t, client, server.URL+path, url.Values{})
	case "wizard-done":
		server, client := newTestServer(t, Options{})
		completeToReview(t, client, server.URL)
		if status, body := postPage(t, client, server.URL+"/ui/wizard/apply", url.Values{}); status != http.StatusOK {
			t.Fatalf("apply result status = %d\n%s", status, body)
		}
		return getPage(t, client, server.URL+path)
	case "wizard-camera-submit":
		server, client := newTestServer(t, Options{})
		return postPage(t, client, server.URL+path, url.Values{})
	case "wizard-camera-settings":
		server, client := newTestServer(t, Options{MockPreview: true})
		getPage(t, client, server.URL+"/ui/wizard")
		addCamera(t, client, server.URL, cameraFormValues("Front door", "10.77.0.20"))
		return getPage(t, client, server.URL+path)
	case "wizard-camera-settings-submit":
		server, client := newTestServer(t, Options{MockPreview: true})
		getPage(t, client, server.URL+"/ui/wizard")
		addCamera(t, client, server.URL, cameraFormValues("Front door", "10.77.0.20"))
		return postPage(t, client, server.URL+path, url.Values{})
	case "wizard-camera-remove":
		server, client := newTestServer(t, Options{MockPreview: true})
		getPage(t, client, server.URL+"/ui/wizard")
		addCamera(t, client, server.URL, cameraFormValues("Front door", "10.77.0.20"))
		disableRedirects(client)
		return postPage(t, client, server.URL+path, url.Values{})
	case "camera-edit", "camera-remove", "camera-picture":
		configPath, secretsPath := writeCameraFixture(t)
		server, client := newTestServer(t, Options{ConfigPath: configPath, SecretsPath: secretsPath, MockPreview: true})
		return getPage(t, client, server.URL+path)
	case "camera-edit-submit":
		configPath, secretsPath := writeCameraFixture(t)
		server, client := newTestServer(t, Options{ConfigPath: configPath, SecretsPath: secretsPath})
		return postPage(t, client, server.URL+path, url.Values{})
	case "camera-edit-discover", "camera-snapshot":
		configPath, secretsPath := writeCameraFixture(t)
		server, client := newTestServer(t, Options{ConfigPath: configPath, SecretsPath: secretsPath})
		disableRedirects(client)
		return postPage(t, client, server.URL+path, url.Values{})
	case "camera-remove-submit":
		configPath, secretsPath := writeCameraFixture(t)
		server, client := newTestServer(t, Options{
			ConfigPath:  configPath,
			SecretsPath: secretsPath,
			Applier:     &fakeApplier{err: errors.New("apply failed")},
		})
		return postPage(t, client, server.URL+path, url.Values{})
	case "wizard-network-submit", "wizard-storage-submit", "wizard-camera-continue",
		"wizard-camera-discover", "wizard-cancel":
		server, client := newTestServer(t, Options{})
		disableRedirects(client)
		return postPage(t, client, server.URL+path, url.Values{})
	case "status-fragment":
		server, client := newTestServer(t, Options{MockPreview: true})
		return getPage(t, client, server.URL+path)
	case "updates-check", "updates-install", "update-rollback-submit":
		server, client := newTestServer(t, Options{MockPreview: true})
		disableRedirects(client)
		return postPage(t, client, server.URL+path, url.Values{})
	case "static":
		server, client := newTestServer(t, Options{})
		return getPage(t, client, server.URL+path)
	case "not-found":
		server, client := newTestServer(t, Options{})
		return getPage(t, client, server.URL+path)
	default:
		server, client := newTestServer(t, Options{MockPreview: true})
		return getPage(t, client, server.URL+path)
	}
}

func disableRedirects(client *http.Client) {
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
}

type parsedForm struct {
	action string
	csrf   bool
}

type parsedDiv struct {
	class      string
	role       string
	tabindex   string
	labelledBy string
}

func checkPageMarkup(t *testing.T, name, body string) {
	t.Helper()
	cleaned := body
	if index := strings.Index(strings.ToLower(cleaned), "<!doctype"); index >= 0 {
		if end := strings.Index(cleaned[index:], ">"); end >= 0 {
			cleaned = cleaned[:index] + cleaned[index+end+1:]
		}
	}
	decoder := xml.NewDecoder(strings.NewReader(cleaned))
	h1Count := 0
	viewport := false
	var divs []parsedDiv
	ids := make(map[string]bool)
	var tableLabels []string
	var forms []parsedForm
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("%s: parse rendered HTML: %v", name, err)
		}
		switch value := token.(type) {
		case xml.StartElement:
			attributes := make(map[string]string, len(value.Attr))
			for _, attribute := range value.Attr {
				attributes[attribute.Name.Local] = attribute.Value
			}
			if id := attributes["id"]; id != "" {
				ids[id] = true
			}
			switch value.Name.Local {
			case "h1":
				h1Count++
			case "meta":
				if strings.EqualFold(attributes["name"], "viewport") &&
					strings.Contains(attributes["content"], "width=device-width") {
					viewport = true
				}
			case "div":
				divs = append(divs, parsedDiv{
					class:      attributes["class"],
					role:       attributes["role"],
					tabindex:   attributes["tabindex"],
					labelledBy: attributes["aria-labelledby"],
				})
			case "table":
				wrapped := false
				for index := len(divs) - 1; index >= 0; index-- {
					if !hasClass(divs[index].class, "table-wrap") {
						continue
					}
					wrapped = true
					if divs[index].role != "region" {
						t.Errorf("%s: table scroll region has role %q, want region", name, divs[index].role)
					}
					if divs[index].tabindex != "0" {
						t.Errorf("%s: table scroll region has tabindex %q, want 0", name, divs[index].tabindex)
					}
					if divs[index].labelledBy == "" {
						t.Errorf("%s: table scroll region has no accessible name", name)
					} else {
						tableLabels = append(tableLabels, divs[index].labelledBy)
					}
					break
				}
				if !wrapped {
					t.Errorf("%s: table is not inside .table-wrap", name)
				}
			case "form":
				forms = append(forms, parsedForm{action: strings.TrimSpace(attributes["action"])})
			case "input":
				if len(forms) != 0 && attributes["name"] == auth.CSRFFormField {
					forms[len(forms)-1].csrf = true
				}
			}
		case xml.EndElement:
			switch value.Name.Local {
			case "div":
				if len(divs) != 0 {
					divs = divs[:len(divs)-1]
				}
			case "form":
				if len(forms) != 0 {
					form := forms[len(forms)-1]
					forms = forms[:len(forms)-1]
					if form.action == "" {
						t.Errorf("%s: form has no explicit action", name)
					}
					if !form.csrf {
						t.Errorf("%s: form %q has no CSRF field", name, form.action)
					}
				}
			}
		}
	}
	if h1Count != 1 {
		t.Errorf("%s: h1 count = %d, want exactly 1", name, h1Count)
	}
	if !viewport {
		t.Errorf("%s: viewport meta is missing", name)
	}
	for _, label := range tableLabels {
		if !ids[label] {
			t.Errorf("%s: table scroll region is labelled by %q, which is not an element id", name, label)
		}
	}
	if len(forms) != 0 {
		t.Errorf("%s: form did not close", name)
	}
}

func hasClass(value, class string) bool {
	for _, candidate := range strings.Fields(value) {
		if candidate == class {
			return true
		}
	}
	return false
}
