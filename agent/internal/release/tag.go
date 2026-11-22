package release

import (
	"fmt"
	"strconv"
	"strings"
)

// BetaTagPrefix is the application bundle tag prefix for the beta channel from
// ADR 0031. The suffix is exactly six lowercase hexadecimal characters.
const BetaTagPrefix = "app-beta-"

// StableTagPrefix is the application bundle tag prefix for the stable channel
// from ADR 0031.
const StableTagPrefix = "app-stable-"

// tagSuffixLength is fixed so lexicographic tag order equals numeric generation
// order. ADR 0031 defines six characters.
const tagSuffixLength = 6

// TagPrefix returns the application bundle tag prefix for a channel.
func TagPrefix(channel string) (string, error) {
	switch channel {
	case ChannelBeta:
		return BetaTagPrefix, nil
	case ChannelStable:
		return StableTagPrefix, nil
	default:
		return "", fmt.Errorf("channel %q has no application bundle tag prefix", channel)
	}
}

// ParseChannelTag validates an application bundle tag in one channel namespace
// and returns the generation it encodes. A tag from the other channel is
// rejected; the channel client filters by prefix before calling this parser.
func ParseChannelTag(tag, channel string) (int64, error) {
	prefix, err := TagPrefix(channel)
	if err != nil {
		return 0, err
	}
	if !strings.HasPrefix(tag, prefix) {
		return 0, fmt.Errorf("tag %q does not start with %q", tag, prefix)
	}
	suffix := strings.TrimPrefix(tag, prefix)
	if len(suffix) != tagSuffixLength {
		return 0, fmt.Errorf("tag %q has a %d-character suffix; %q needs exactly %d lowercase hexadecimal characters",
			tag, len(suffix), prefix, tagSuffixLength)
	}
	for index := 0; index < len(suffix); index++ {
		character := suffix[index]
		if (character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') {
			continue
		}
		return 0, fmt.Errorf("tag %q has a non-lowercase-hexadecimal character %q at suffix position %d",
			tag, character, index+1)
	}
	generation, err := strconv.ParseInt(suffix, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("tag %q suffix %q is not a hexadecimal generation: %w", tag, suffix, err)
	}
	if generation == 0 {
		return 0, fmt.Errorf("tag %q decodes generation 0; application bundle generation starts at 1", tag)
	}
	return generation, nil
}

// ParseBetaTag validates an application bundle tag in the beta namespace and
// returns the generation it encodes.
func ParseBetaTag(tag string) (int64, error) {
	return ParseChannelTag(tag, ChannelBeta)
}
