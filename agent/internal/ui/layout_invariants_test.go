package ui

import (
	"regexp"
	"strings"
	"testing"

	"bittern.invalid/agent/internal/assets"
)

// TestStylesheetKeepsBrowserLayoutInvariants guards the small set of CSS
// rules the opt-in browser check depends on (ADR 0025). It is not a CSS
// parser; the browser check remains the evidence for real layout behavior.
func TestStylesheetKeepsBrowserLayoutInvariants(t *testing.T) {
	data, err := assets.UI.ReadFile("ui/static/bittern.css")
	if err != nil {
		t.Fatal(err)
	}
	stylesheet := string(data)

	fixedWidth := regexp.MustCompile(`(?m)(?:^|;)\s*(?:width|min-width):\s*[0-9.]+px\s*;`)
	for _, selector := range []string{"html", "body", ".wrap"} {
		if fixedWidth.MatchString(stylesheetRule(t, stylesheet, selector)) {
			t.Errorf("%s sets a fixed pixel width that can force horizontal scrolling", selector)
		}
	}

	wrapRule := stylesheetRule(t, stylesheet, ".wrap")
	if !strings.Contains(wrapRule, "width: min(") || !strings.Contains(wrapRule, "100%") {
		t.Error(".wrap no longer uses a fluid width")
	}

	footerCode := stylesheetRule(t, stylesheet, ".site-footer code")
	if !strings.Contains(footerCode, "overflow-wrap: anywhere;") {
		t.Error(".site-footer code no longer wraps long certificate fingerprints")
	}

	atomicLink := regexp.MustCompile(`(?m)(?:^|;)\s*display:\s*(?:inline-flex|inline-block|flex)\s*;`)
	if atomicLink.MatchString(stylesheetRule(t, stylesheet, "a")) {
		t.Error("base a rule takes prose links out of inline flow; they must stay wrappable")
	}

	if !regexp.MustCompile(`@media\s*\(max-width:\s*44rem\)`).MatchString(stylesheet) {
		t.Error("stylesheet has no breakpoint below 44rem for the phone layout")
	}

	for _, marker := range []string{
		"--touch-target: 44px;",
		"--touch-target-min: 24px;",
	} {
		if !strings.Contains(stylesheet, marker) {
			t.Errorf("stylesheet is missing the touch-target value %q", marker)
		}
	}
}

func stylesheetRule(t *testing.T, stylesheet, selector string) string {
	t.Helper()
	pattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(selector) + `\s*\{([^}]*)\}`)
	match := pattern.FindStringSubmatch(stylesheet)
	if match == nil {
		t.Fatalf("stylesheet has no %s rule", selector)
	}
	return match[1]
}
