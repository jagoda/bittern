package apply

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// udevWaitTimeout bounds the wait for the names in the rendered .link files.
// The udev add event and the rename normally finish in well under a second;
// the bound only has to be long enough for a loaded appliance.
const udevWaitTimeout = 10 * time.Second

func readGenerationFile(root string, generation int, source string) ([]byte, error) {
	return os.ReadFile(filepath.Join(generationFilesDir(root, generation), filepath.FromSlash(source)))
}

func manifestHasLinkFiles(manifest *Manifest) bool {
	for _, record := range manifest.Files {
		if isLinkFile(record) {
			return true
		}
	}
	return false
}

func isLinkFile(record FileRecord) bool {
	return strings.HasPrefix(record.Destination, "etc/systemd/network/") &&
		strings.HasSuffix(record.Destination, ".link")
}

// linkInterfaceNames returns the sorted, unique interface names declared by the
// .link files in a manifest. It parses Name= from the [Link] section instead of
// assuming the rendered roles are lan0 and cam0.
func linkInterfaceNames(records []FileRecord, read func(string) ([]byte, error)) ([]string, error) {
	seen := make(map[string]bool)
	var names []string
	for _, record := range records {
		if !isLinkFile(record) {
			continue
		}
		data, err := read(record.Source)
		if err != nil {
			return nil, fmt.Errorf("apply: read link file %s: %w", record.Source, err)
		}
		parsed, err := parseLinkInterfaceNames(record.Source, data)
		if err != nil {
			return nil, err
		}
		for _, name := range parsed {
			if seen[name] {
				continue
			}
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func parseLinkInterfaceNames(source string, data []byte) ([]string, error) {
	section := ""
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var names []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || section != "link" || strings.ToLower(strings.TrimSpace(key)) != "name" {
			continue
		}
		value = strings.TrimSpace(value)
		if unquoted, err := strconv.Unquote(value); err == nil {
			value = unquoted
		}
		for _, name := range strings.Fields(value) {
			if name != "" {
				names = append(names, name)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("apply: read link file %s: %w", source, err)
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("apply: link file %s has no Name= in [Link]", source)
	}
	return names, nil
}
