package ui

import (
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"bittern.invalid/agent/internal/assets"
)

type parsedInput struct {
	ID   string
	Type string
}

func TestEveryInputHasALabel(t *testing.T) {
	for name, page := range accessibilityPages(t) {
		assertInputsLabelled(t, name, page)
	}
}

func TestAccessibilityBaseline(t *testing.T) {
	pages := accessibilityPages(t)
	welcome, ok := pages["wizard"]
	if !ok {
		t.Fatal("wizard page was not rendered")
	}
	assertContains(t, welcome, `class="skip-link"`, `href="#main"`, `id="main"`)

	css, err := assets.UI.ReadFile("ui/static/bittern.css")
	if err != nil {
		t.Fatal(err)
	}
	stylesheet := string(css)
	assertContains(t, stylesheet, ":focus-visible", "outline:", "prefers-reduced-motion")

	for name, page := range pages {
		if strings.Contains(page, `tabindex="1"`) {
			t.Errorf("%s has a positive tabindex", name)
		}
	}
}

func TestPaletteMeetsWCAGAA(t *testing.T) {
	css, err := assets.UI.ReadFile("ui/static/bittern.css")
	if err != nil {
		t.Fatal(err)
	}
	variables := stylesheetVariables(string(css))
	if len(variables) == 0 {
		t.Fatal("no variables found in stylesheet")
	}

	pairs := []struct {
		foreground string
		background string
		minimum    float64
	}{
		{"color-text", "color-page", 4.5},
		{"color-text-muted", "color-page", 4.5},
		{"color-link", "color-page", 4.5},
		{"color-text", "color-surface", 4.5},
		{"color-text-muted", "color-surface", 4.5},
		{"color-link", "color-surface", 4.5},
		{"color-on-action", "color-action", 4.5},
		{"color-on-action", "color-action-hover", 4.5},
		{"color-ok-text", "color-ok-surface", 4.5},
		{"color-degraded-text", "color-degraded-surface", 4.5},
		{"color-failed-text", "color-failed-surface", 4.5},
		{"color-unknown-text", "color-unknown-surface", 4.5},
		{"color-notice-text", "color-notice-surface", 4.5},
		{"color-focus", "color-page", 3.0},
		{"color-focus", "color-surface", 3.0},
	}
	for _, pair := range pairs {
		foreground, foregroundOK := resolveStylesheetValue(variables, pair.foreground)
		background, backgroundOK := resolveStylesheetValue(variables, pair.background)
		if !foregroundOK || !backgroundOK {
			t.Errorf("palette is missing %s or %s", pair.foreground, pair.background)
			continue
		}
		ratio := contrastRatio(foreground, background)
		if ratio < pair.minimum {
			t.Errorf("%s on %s contrast %.2f is below %.2f", pair.foreground, pair.background, ratio, pair.minimum)
		}
	}
}

func stylesheetVariables(css string) map[string]string {
	variablePattern := regexp.MustCompile(`(?m)(--[a-z0-9-]+):\s*([^;]+);`)
	variables := make(map[string]string)
	for _, match := range variablePattern.FindAllStringSubmatch(css, -1) {
		variables[match[1]] = strings.TrimSpace(match[2])
	}
	return variables
}

func resolveStylesheetValue(variables map[string]string, name string) (string, bool) {
	value, ok := variables["--"+name]
	if !ok {
		return "", false
	}
	referencePattern := regexp.MustCompile(`var\((--[a-z0-9-]+)\)`)
	for {
		match := referencePattern.FindStringSubmatch(value)
		if match == nil {
			break
		}
		referenced, ok := resolveStylesheetValue(variables, strings.TrimPrefix(match[1], "--"))
		if !ok {
			return "", false
		}
		value = strings.Replace(value, match[0], referenced, 1)
	}
	return value, true
}

func findRoute(t *testing.T, name string) route {
	t.Helper()
	for _, route := range uiRoutes {
		if route.Name == name {
			return route
		}
	}
	t.Fatalf("route %q is not in uiRoutes", name)
	return route{}
}

func accessibilityPages(t *testing.T) map[string]string {
	t.Helper()
	pages := make(map[string]string)
	for _, route := range uiRoutes {
		if route.Responsive && route.Template != "" {
			pages[route.Name] = routePageBody(t, route)
		}
	}
	cameraRoute := findRoute(t, "wizard-camera")
	server, client := newTestServer(t, Options{Applier: &fakeApplier{}, MockPreview: true})
	_, pages["wizard-camera-error"] = postPage(t, client, server.URL+routeRequestPath(cameraRoute), url.Values{
		"name":      {""},
		"address":   {"not-an-address"},
		"rtsp_port": {"70000"},
		"rtsp_path": {"stream1"},
	})
	statusRoute := findRoute(t, "status")
	for _, state := range []string{"booting", "ready", "degraded", "updating", "failed"} {
		_, pages["status-"+state] = getPage(t, client,
			fmt.Sprintf("%s%s?state=%s", server.URL, routeRequestPath(statusRoute), state))
	}
	return pages
}

func assertInputsLabelled(t *testing.T, pageName, body string) {
	t.Helper()
	cleaned := body
	if index := strings.Index(strings.ToLower(cleaned), "<!doctype"); index >= 0 {
		if end := strings.Index(cleaned[index:], ">"); end >= 0 {
			cleaned = cleaned[:index] + cleaned[index+end+1:]
		}
	}
	decoder := xml.NewDecoder(strings.NewReader(cleaned))
	labels := make(map[string]bool)
	idCounts := make(map[string]int)
	var inputs []parsedInput
	var fieldsets []bool

	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("%s: parse rendered HTML: %v", pageName, err)
		}
		switch value := token.(type) {
		case xml.StartElement:
			attributes := make(map[string]string, len(value.Attr))
			for _, attribute := range value.Attr {
				attributes[attribute.Name.Local] = attribute.Value
			}
			if id := attributes["id"]; id != "" {
				idCounts[id]++
			}
			if tabindex := attributes["tabindex"]; tabindex != "" {
				if number, err := strconv.Atoi(tabindex); err == nil && number > 0 {
					t.Errorf("%s: positive tabindex %q", pageName, tabindex)
				}
			}
			switch value.Name.Local {
			case "label":
				if target := attributes["for"]; target != "" {
					labels[target] = true
				}
			case "input":
				inputs = append(inputs, parsedInput{
					ID:   attributes["id"],
					Type: strings.ToLower(attributes["type"]),
				})
			case "fieldset":
				fieldsets = append(fieldsets, false)
			case "legend":
				if len(fieldsets) != 0 {
					fieldsets[len(fieldsets)-1] = true
				}
			}
		case xml.EndElement:
			if value.Name.Local == "fieldset" && len(fieldsets) != 0 {
				hasLegend := fieldsets[len(fieldsets)-1]
				fieldsets = fieldsets[:len(fieldsets)-1]
				if !hasLegend {
					t.Errorf("%s: fieldset has no legend", pageName)
				}
			}
		}
	}

	for _, input := range inputs {
		if input.Type == "hidden" {
			continue
		}
		if input.ID == "" {
			t.Errorf("%s: input has no id", pageName)
			continue
		}
		if !labels[input.ID] {
			t.Errorf("%s: input %q has no label", pageName, input.ID)
		}
	}
	for id, count := range idCounts {
		if count > 1 {
			t.Errorf("%s: id %q is used %d times", pageName, id, count)
		}
	}
}

func contrastRatio(first, second string) float64 {
	firstLuminance := relativeLuminance(first)
	secondLuminance := relativeLuminance(second)
	light := math.Max(firstLuminance, secondLuminance)
	dark := math.Min(firstLuminance, secondLuminance)
	return (light + 0.05) / (dark + 0.05)
}

func relativeLuminance(color string) float64 {
	value := strings.TrimPrefix(color, "#")
	parsed, err := strconv.ParseUint(value, 16, 32)
	if err != nil || len(value) != 6 {
		return 0
	}
	channels := []float64{
		float64((parsed>>16)&0xff) / 255,
		float64((parsed>>8)&0xff) / 255,
		float64(parsed&0xff) / 255,
	}
	for index, channel := range channels {
		if channel <= 0.03928 {
			channels[index] = channel / 12.92
		} else {
			channels[index] = math.Pow((channel+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*channels[0] + 0.7152*channels[1] + 0.0722*channels[2]
}
