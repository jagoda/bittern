package release

import "fmt"

// Error is one release-manifest rejection. Path is a JSON path or a file path,
// depending on the check. Rule names the failing check so the update record and
// the UI can report it without parsing the message.
type Error struct {
	Path    string
	Rule    string
	Message string
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("%s: %s", e.Rule, e.Message)
	}
	return fmt.Sprintf("%s: %s: %s", e.Path, e.Rule, e.Message)
}

func errorAt(path, rule, format string, args ...any) *Error {
	return &Error{Path: path, Rule: rule, Message: fmt.Sprintf(format, args...)}
}

func childPath(path, key string) string {
	if path == "$" {
		return "$." + key
	}
	return path + "." + key
}

func indexPath(path string, index int) string {
	return fmt.Sprintf("%s[%d]", path, index)
}
