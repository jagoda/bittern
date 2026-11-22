package release

import "testing"

// TestParseBetaTagReadsTheGenerationAsHexadecimal proves the two generator
// values named by ADR 0031 and the minimum generation.
func TestParseBetaTagReadsTheGenerationAsHexadecimal(t *testing.T) {
	cases := []struct {
		tag  string
		want int64
	}{
		{tag: "app-beta-00001f", want: 31},
		{tag: "app-beta-000010", want: 16},
		{tag: "app-beta-000001", want: 1},
		{tag: "app-beta-0000ff", want: 255},
	}
	for _, test := range cases {
		t.Run(test.tag, func(t *testing.T) {
			generation, err := ParseBetaTag(test.tag)
			if err != nil {
				t.Fatalf("ParseBetaTag(%q): %v", test.tag, err)
			}
			if generation != test.want {
				t.Fatalf("ParseBetaTag(%q) = %d, want %d", test.tag, generation, test.want)
			}
		})
	}
}

// TestParseBetaTagRejectsMalformedTags covers the forms the workflow must not
// accept: uppercase hex, wrong width, an empty suffix, a g prefix, a non-hex
// character, generation zero, and a foreign namespace.
func TestParseBetaTagRejectsMalformedTags(t *testing.T) {
	cases := []struct {
		name string
		tag  string
	}{
		{name: "uppercase suffix", tag: "app-beta-00001F"},
		{name: "uppercase prefix", tag: "APP-BETA-00001f"},
		{name: "five characters", tag: "app-beta-00001"},
		{name: "seven characters", tag: "app-beta-00001ff"},
		{name: "empty suffix", tag: "app-beta-"},
		{name: "g prefix", tag: "app-beta-g00001"},
		{name: "non-hex character", tag: "app-beta-0000g1"},
		{name: "generation zero", tag: "app-beta-000000"},
		{name: "stable namespace", tag: "app-stable-00001f"},
		{name: "no prefix", tag: "00001f"},
		{name: "trailing separator", tag: "app-beta-00001f-"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if generation, err := ParseBetaTag(test.tag); err == nil {
				t.Fatalf("ParseBetaTag(%q) = %d, want an error", test.tag, generation)
			}
		})
	}
}
