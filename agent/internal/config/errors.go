package config

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
)

// Error is one validator error in the format defined by schema/validation.md:
// JSON_PATH: RULE: MESSAGE.
type Error struct {
	Path    string
	Rule    string
	Message string
}

// Error implements the error interface.
func (e Error) Error() string {
	return fmt.Sprintf("%s: %s: %s", e.Path, e.Rule, e.Message)
}

var plainKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// jsonPath renders path parts as a JSON path: dots for identifiers, brackets
// for array indexes, and a quoted bracket for any other key.
func jsonPath(parts []string) string {
	path := "$"
	for _, part := range parts {
		if _, err := strconv.Atoi(part); err == nil && part != "" && part[0] != '-' {
			path += "[" + part + "]"
			continue
		}
		if plainKey.MatchString(part) {
			path += "." + part
			continue
		}
		quoted, err := json.Marshal(part)
		if err != nil {
			quoted = []byte(strconv.Quote(part))
		}
		path += "[" + string(quoted) + "]"
	}
	return path
}

func errorAt(parts []string, rule, message string) Error {
	return Error{Path: jsonPath(parts), Rule: rule, Message: message}
}
