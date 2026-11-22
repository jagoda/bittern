package release

import (
	"fmt"
	"strconv"
	"strings"
)

// BetaTagPrefix is the application bundle tag prefix for the beta channel from
// ADR 0031. The suffix is exactly six lowercase hexadecimal characters.
const BetaTagPrefix = "app-beta-"

// tagSuffixLength is fixed so lexicographic tag order equals numeric generation
// order. ADR 0031 defines six characters.
const tagSuffixLength = 6

// ParseBetaTag validates an application bundle tag and returns the generation
// it encodes. The suffix is base 16, exactly six characters, lowercase only,
// with no prefix inside the number and no separators. Generation 0 is rejected
// because generation starts at 1. A decimal parse is a defect, not a variation.
func ParseBetaTag(tag string) (int64, error) {
	if !strings.HasPrefix(tag, BetaTagPrefix) {
		return 0, fmt.Errorf("tag %q does not start with %q", tag, BetaTagPrefix)
	}
	suffix := strings.TrimPrefix(tag, BetaTagPrefix)
	if len(suffix) != tagSuffixLength {
		return 0, fmt.Errorf("tag %q has a %d-character suffix; %q needs exactly %d lowercase hexadecimal characters",
			tag, len(suffix), BetaTagPrefix, tagSuffixLength)
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
