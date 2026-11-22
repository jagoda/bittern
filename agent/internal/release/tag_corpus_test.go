package release

import (
	"encoding/json"
	"os"
	"testing"
)

const tagCorpusPath = "../../../tests/fixtures/release/tag-cases.json"

// tagCorpusCase is one shared case in tests/fixtures/release/tag-cases.json.
// The channel is the namespace under test; for a valid case it is the channel
// the tag selects.
type tagCorpusCase struct {
	Tag        string `json:"tag"`
	Channel    string `json:"channel"`
	Valid      bool   `json:"valid"`
	Generation *int64 `json:"generation"`
	Reason     string `json:"reason"`
}

func loadTagCorpus(t *testing.T) []tagCorpusCase {
	t.Helper()
	data, err := os.ReadFile(tagCorpusPath)
	if err != nil {
		t.Fatalf("read %s: %v", tagCorpusPath, err)
	}
	var corpus struct {
		Cases []tagCorpusCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatalf("decode %s: %v", tagCorpusPath, err)
	}
	if len(corpus.Cases) == 0 {
		t.Fatalf("%s has no cases", tagCorpusPath)
	}
	return corpus.Cases
}

// TestTagCorpus makes the shared corpus the authority for the parser. Every
// valid case must parse to the recorded channel and generation, and every
// invalid case must fail for a recorded reason.
func TestTagCorpus(t *testing.T) {
	for _, test := range loadTagCorpus(t) {
		test := test
		t.Run(test.Tag, func(t *testing.T) {
			generation, err := ParseChannelTag(test.Tag, test.Channel)
			if test.Valid {
				if err != nil {
					t.Fatalf("ParseChannelTag(%q, %q): %v", test.Tag, test.Channel, err)
				}
				if test.Generation == nil {
					t.Fatalf("valid corpus case %q has no generation", test.Tag)
				}
				if generation != *test.Generation {
					t.Fatalf("generation = %d, want %d", generation, *test.Generation)
				}
				if test.Reason != "" {
					t.Fatalf("valid corpus case %q has a reason: %q", test.Tag, test.Reason)
				}
				return
			}
			if err == nil {
				t.Fatalf("ParseChannelTag(%q, %q) = %d, want an error", test.Tag, test.Channel, generation)
			}
			if test.Generation != nil {
				t.Fatalf("invalid corpus case %q has a generation", test.Tag)
			}
			if test.Reason == "" {
				t.Fatalf("invalid corpus case %q has no reason", test.Tag)
			}
		})
	}
}
