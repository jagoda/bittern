package release

import "testing"

// The valid and invalid tag forms live in the shared corpus at
// tests/fixtures/release/tag-cases.json. TestTagCorpus reads that file so the
// corpus, not a second table in this package, is the authority.

func TestParseChannelTagRejectsTheOtherChannelAndUnknownChannels(t *testing.T) {
	if _, err := ParseChannelTag("app-stable-000001", ChannelBeta); err == nil {
		t.Fatal("a stable tag parsed in the beta namespace")
	}
	if _, err := ParseChannelTag("app-beta-000001", ChannelStable); err == nil {
		t.Fatal("a beta tag parsed in the stable namespace")
	}
	if _, err := TagPrefix("security"); err == nil {
		t.Fatal("an unknown channel returned a tag prefix")
	}
}
