package timezone

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{name: "iana name", input: "Europe/Berlin", want: "Europe/Berlin"},
		{name: "utc", input: "UTC", want: "UTC"},
		{name: "trimmed", input: "  America/New_York  ", want: "America/New_York"},
		{name: "empty falls back", input: "", want: Default},
		{name: "unknown falls back", input: "Mars/Olympus_Mons", want: Default},
		{name: "local is refused", input: "Local", want: Default},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := Normalize(testCase.input); got != testCase.want {
				t.Fatalf("Normalize(%q) = %q, want %q", testCase.input, got, testCase.want)
			}
		})
	}
}

// TestDaylightSavingComesFromIANA fixes the clock at both European daylight
// saving boundaries and asserts the displayed local time and the offset. The
// transition changes what is shown, not the UTC instant and not the machine
// clock: no arithmetic on a stored offset produces these values.
func TestDaylightSavingComesFromIANA(t *testing.T) {
	berlin := Load("Europe/Berlin")
	cases := []struct {
		name       string
		utc        string
		wantFormat string
		wantOffset int
	}{
		{
			name:       "spring forward, one second before",
			utc:        "2026-03-29T00:59:59Z",
			wantFormat: "29 Mar 2026, 01:59 CET (UTC+01:00)",
			wantOffset: 3600,
		},
		{
			name:       "spring forward, at the transition",
			utc:        "2026-03-29T01:00:00Z",
			wantFormat: "29 Mar 2026, 03:00 CEST (UTC+02:00)",
			wantOffset: 7200,
		},
		{
			name:       "fall back, one second before",
			utc:        "2026-10-25T00:59:59Z",
			wantFormat: "25 Oct 2026, 02:59 CEST (UTC+02:00)",
			wantOffset: 7200,
		},
		{
			name:       "fall back, at the transition",
			utc:        "2026-10-25T01:00:00Z",
			wantFormat: "25 Oct 2026, 02:00 CET (UTC+01:00)",
			wantOffset: 3600,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			instant, err := time.Parse(time.RFC3339, testCase.utc)
			if err != nil {
				t.Fatal(err)
			}
			if got := Format(instant, berlin); got != testCase.wantFormat {
				t.Fatalf("Format = %q, want %q", got, testCase.wantFormat)
			}
			if got := OffsetSeconds(instant, berlin); got != testCase.wantOffset {
				t.Fatalf("offset = %d, want %d", got, testCase.wantOffset)
			}
			if got := instant.UTC().Format(time.RFC3339); got != testCase.utc {
				t.Fatalf("the UTC instant changed to %q", got)
			}
		})
	}
}

// TestHalfHourDaylightSavingComesFromIANA checks a zone whose transition moves
// the offset by 30 minutes. An implementation that assumes whole hours fails
// this case.
func TestHalfHourDaylightSavingComesFromIANA(t *testing.T) {
	lordHowe := Load("Australia/Lord_Howe")
	before, err := time.Parse(time.RFC3339, "2026-04-04T14:59:59Z")
	if err != nil {
		t.Fatal(err)
	}
	after, err := time.Parse(time.RFC3339, "2026-04-04T15:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if got := OffsetSeconds(before, lordHowe); got != 11*3600 {
		t.Fatalf("offset before fall back = %d, want %d", got, 11*3600)
	}
	if got := OffsetSeconds(after, lordHowe); got != 10*3600+30*60 {
		t.Fatalf("offset after fall back = %d, want %d", got, 10*3600+30*60)
	}
	future, err := time.Parse(time.RFC3339, "2026-10-03T15:30:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if got := OffsetSeconds(future, lordHowe); got != 11*3600 {
		t.Fatalf("offset after spring forward = %d, want %d", got, 11*3600)
	}
}

func TestFormatLabelsUTCOnce(t *testing.T) {
	instant := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	if got := Format(instant, time.UTC); got != "14 Nov 2026, 12:00 UTC" {
		t.Fatalf("UTC format = %q", got)
	}
	if got := LongDate(instant, time.UTC); got != "14 November 2026 at 12:00 UTC" {
		t.Fatalf("UTC long date = %q", got)
	}
}

func TestLoadFallsBackToUTC(t *testing.T) {
	for _, name := range []string{"", "Local", "Not/AZone"} {
		if location := Load(name); location != time.UTC {
			t.Errorf("Load(%q) = %v, want UTC", name, location)
		}
	}
}

// TestNoCodeSetsLocalClock is the static assertion from ADR 0035: no product
// path sets the system clock or the hardware clock to anything but UTC. The
// system clock and the RTC are kept in UTC by chrony's rtcsync; every other
// time operation in Bittern is read-only.
func TestNoCodeSetsLocalClock(t *testing.T) {
	root := repositoryRoot(t)
	forbidden := []struct {
		name    string
		pattern *regexp.Regexp
	}{
		{"timedatectl", regexp.MustCompile(`\btimedatectl\b`)},
		{"hwclock", regexp.MustCompile(`\bhwclock\b`)},
		{"date -s", regexp.MustCompile(`\bdate\s+(?:-s|--set)\b`)},
		{"settimeofday", regexp.MustCompile(`\bsettimeofday\b`)},
		{"clock_settime", regexp.MustCompile(`\bclock_settime\b`)},
		{"time.Local", regexp.MustCompile(`\btime\.Local\b`)},
		{"/etc/localtime write", regexp.MustCompile(`(?:WriteFile|Rename|Symlink|MkdirAll)\([^\n]*/etc/localtime`)},
	}
	roots := []string{"agent", "packaging", "image", "templates", "ui", "defaults"}
	checked := 0
	for _, top := range roots {
		base := filepath.Join(root, top)
		err := filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				switch entry.Name() {
				case "vendor", "node_modules", "bin", "testdata":
					return filepath.SkipDir
				}
				return nil
			}
			if !scannable(path) {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			checked++
			relative, _ := filepath.Rel(root, path)
			text := string(data)
			for _, rule := range forbidden {
				for lineNumber, line := range strings.Split(text, "\n") {
					if !rule.pattern.MatchString(line) {
						continue
					}
					t.Errorf("%s:%d uses %s; no product path may set the system or hardware clock to local time",
						filepath.ToSlash(relative), lineNumber+1, rule.name)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", top, err)
		}
	}
	if checked == 0 {
		t.Fatal("the clock scan checked no files")
	}
}

// TestChronyTemplateKeepsTheRTCInUTC asserts the only RTC writer is chrony's
// rtcsync, which copies the UTC system clock. It also asserts the template
// carries the step window and the local reference the status contract relies
// on.
func TestChronyTemplateKeepsTheRTCInUTC(t *testing.T) {
	root := repositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "templates", "chrony", "chrony.conf.tmpl"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, required := range []string{
		"rtcsync",
		"makestep 1.0 3",
		"bindacqdevice lan0",
		"local stratum 10",
		"port 0",
	} {
		if !strings.Contains(body, required) {
			t.Errorf("chrony template is missing %q", required)
		}
	}
	if strings.Contains(body, "localtime") {
		t.Error("the chrony template references a local time file")
	}
}

func scannable(path string) bool {
	switch filepath.Ext(path) {
	case ".go", ".sh", ".js", ".tmpl", ".service", ".yaml", ".json", ".nft":
	default:
		return false
	}
	return !strings.HasSuffix(path, "_test.go")
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Dir(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find go.mod above the test directory")
		}
		dir = parent
	}
}
