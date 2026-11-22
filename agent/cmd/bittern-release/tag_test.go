package main

import (
	"strings"
	"testing"
)

func TestTagCommandPrintsTheGeneration(t *testing.T) {
	cases := []struct {
		tag  string
		want string
	}{
		{tag: "app-beta-00001f", want: "31\n"},
		{tag: "app-beta-000010", want: "16\n"},
	}
	for _, test := range cases {
		t.Run(test.tag, func(t *testing.T) {
			code, stdout, stderr := runCommand(t, "", "tag", test.tag)
			if code != exitOK {
				t.Fatalf("tag exited %d: %s", code, stderr)
			}
			if stdout != test.want {
				t.Fatalf("tag stdout = %q, want %q", stdout, test.want)
			}
			if !strings.Contains(stderr, "channel beta") {
				t.Fatalf("tag did not report the channel: %s", stderr)
			}
		})
	}
}

func TestTagCommandRejectsAMalformedTag(t *testing.T) {
	code, stdout, stderr := runCommand(t, "", "tag", "app-beta-00001F")
	if code != exitUsage {
		t.Fatalf("tag exited %d, want %d", code, exitUsage)
	}
	if stdout != "" {
		t.Fatalf("tag wrote a generation for a rejected tag: %q", stdout)
	}
	if !strings.Contains(stderr, "lowercase") {
		t.Fatalf("tag did not explain the rejection: %s", stderr)
	}
}
