package secrets

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// CameraAccount is the credential set for one camera, stored once under the
// camera's credentials_ref. The renderer builds each profile's complete RTSP
// URL from these values and the profile path, so two profiles never duplicate
// a password and the secrets file never holds a complete URL.
//
// The optional ONVIF account is kept under the same reference, per ADR 0035.
// No value here may be logged or rendered.
type CameraAccount struct {
	Username      string
	Password      string
	ONVIFUsername string
	ONVIFPassword string
	// LegacyURL is the complete URL read from the older shape, when one was
	// present. It is registered with the redactor so an error that quotes the
	// old value is still masked.
	LegacyURL string
	// LegacyPath is the path and query from that old URL. Apply uses it only
	// when the camera has no explicit rtsp_path and no profile path, so an
	// existing device keeps the stream it had before migration.
	LegacyPath string
}

// Values returns every credential value for registration with a redactor. It
// is the only supported way to enumerate an account's secrets.
func (a CameraAccount) Values() []string {
	values := make([]string, 0, 5)
	for _, value := range []string{a.Username, a.Password, a.ONVIFUsername, a.ONVIFPassword, a.LegacyURL} {
		if value != "" {
			values = append(values, value)
		}
	}
	return values
}

// Valid reports whether the camera has an RTSP sign-in.
func (a CameraAccount) Valid() bool {
	return strings.TrimSpace(a.Username) != ""
}

// ONVIFAccount returns the separate ONVIF sign-in when one is stored, else the
// RTSP sign-in. Most cameras use one account for both (ADR 0035).
func (a CameraAccount) ONVIFAccount() CameraAccount {
	if strings.TrimSpace(a.ONVIFUsername) != "" {
		return CameraAccount{Username: a.ONVIFUsername, Password: a.ONVIFPassword}
	}
	return CameraAccount{Username: a.Username, Password: a.Password}
}

// AccountFromValue converts a secrets.yaml camera value to an account. Three
// shapes are readable, forever:
//
//   - the current mapping with username and password;
//   - the old mapping with rtsp_url, optionally with the ONVIF members;
//   - the oldest scalar complete RTSP URL.
//
// A URL with no user information is a no-auth camera and yields an empty
// account, not an error.
func AccountFromValue(value any) (CameraAccount, error) {
	switch typed := value.(type) {
	case nil:
		return CameraAccount{}, nil
	case string:
		return accountFromURL(typed, CameraAccount{})
	case map[string]any:
		account := CameraAccount{}
		rawUsername, hasUsername := typed["username"]
		rawPassword, hasPassword := typed["password"]
		if hasUsername || hasPassword {
			username, ok := rawUsername.(string)
			if !ok && rawUsername != nil {
				return CameraAccount{}, errors.New("the saved sign-in username is not a string")
			}
			password, ok := rawPassword.(string)
			if !ok && rawPassword != nil {
				return CameraAccount{}, errors.New("the saved sign-in password is not a string")
			}
			account.Username = username
			account.Password = password
		}
		if raw, ok := typed["onvif_username"].(string); ok {
			account.ONVIFUsername = raw
		}
		rawONVIFPassword, hasONVIFPassword := typed["onvif_password"]
		if hasONVIFPassword {
			password, ok := rawONVIFPassword.(string)
			if !ok && rawONVIFPassword != nil {
				return CameraAccount{}, errors.New("the saved ONVIF password is not a string")
			}
			account.ONVIFPassword = password
		}
		if raw, ok := typed["rtsp_path"].(string); ok {
			account.LegacyPath = strings.TrimSpace(raw)
		}
		if hasUsername || hasPassword {
			return account, nil
		}
		if raw, ok := typed["rtsp_url"].(string); ok {
			if strings.TrimSpace(raw) == "" {
				return CameraAccount{}, errors.New("the saved camera URL is empty")
			}
			return accountFromURL(raw, account)
		}
		return CameraAccount{}, errors.New("the saved sign-in is neither a username and password nor a camera URL")
	default:
		return CameraAccount{}, errors.New("the saved sign-in is not a camera account")
	}
}

func accountFromURL(raw string, account CameraAccount) (CameraAccount, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return account, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return CameraAccount{}, errors.New("the saved camera URL could not be parsed")
	}
	if parsed.Scheme != "rtsp" && parsed.Scheme != "rtsps" {
		return CameraAccount{}, errors.New("the saved camera URL does not use RTSP")
	}
	if parsed.Host == "" {
		return CameraAccount{}, errors.New("the saved camera URL has no host")
	}
	account.LegacyURL = raw
	path := parsed.EscapedPath()
	if path == "" {
		path = "/"
	}
	if parsed.RawQuery != "" {
		path += "?" + parsed.RawQuery
	}
	account.LegacyPath = path
	if parsed.User == nil {
		return account, nil
	}
	username := parsed.User.Username()
	password, _ := parsed.User.Password()
	if strings.TrimSpace(username) == "" {
		return CameraAccount{}, errors.New("the saved camera URL has no username")
	}
	if account.Username == "" {
		account.Username = username
		account.Password = password
	}
	return account, nil
}

// LoadCameraAccount reads one camera's account from a secrets file. A missing
// key returns found=false and no error so the caller can name the missing
// reference; a malformed value is an error that never quotes the value.
func LoadCameraAccount(path, key string) (CameraAccount, bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return CameraAccount{}, false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return CameraAccount{}, false, err
	}
	var document any
	if err := yaml.Unmarshal(data, &document); err != nil {
		return CameraAccount{}, false, errors.New("the secrets file could not be parsed")
	}
	value, ok := lookupValue(document, key)
	if !ok && strings.HasSuffix(key, ".rtsp_url") {
		// The oldest references named the URL member directly. Resolve the
		// parent mapping so that reference keeps working after migration.
		parent := strings.TrimSuffix(key, ".rtsp_url")
		value, ok = lookupValue(document, parent)
	}
	if !ok {
		return CameraAccount{}, false, nil
	}
	if value == nil {
		return CameraAccount{}, false, nil
	}
	if text, isString := value.(string); isString && strings.TrimSpace(text) == "" {
		return CameraAccount{}, false, nil
	}
	account, err := AccountFromValue(value)
	if err != nil {
		return CameraAccount{}, true, err
	}
	return account, true, nil
}

// MigrationResult reports what a secrets migration changed. It never contains
// a credential value.
type MigrationResult struct {
	Changed  bool
	Migrated int
	Skipped  int
}

// MigrateCameraAccounts rewrites complete-URL camera secrets into the
// per-camera credential shape. It is idempotent, performs no network I/O, and
// preserves every unrelated key and value. A value it
// cannot parse is left untouched, so an older device keeps the readable URL rather
// than losing access.
func MigrateCameraAccounts(path string) (MigrationResult, error) {
	result := MigrationResult{}
	if strings.TrimSpace(path) == "" {
		return result, errors.New("secrets: empty path")
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil {
		return result, fmt.Errorf("secrets: parse %s: %w", path, err)
	}
	root := documentRoot(&document)
	if root == nil || root.Kind != yaml.MappingNode {
		return result, nil
	}
	migrateNode(root, &result)
	if !result.Changed {
		return result, nil
	}
	encoded, err := yaml.Marshal(&document)
	if err != nil {
		return result, err
	}
	if err := WriteFileAtomic(path, encoded, 0o600); err != nil {
		return result, err
	}
	return result, nil
}

func migrateNode(node *yaml.Node, result *MigrationResult) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		key := node.Content[index]
		value := node.Content[index+1]
		if key.Kind != yaml.ScalarNode {
			continue
		}
		if value.Kind == yaml.MappingNode {
			if converted, ok := migrateMapping(value); ok {
				*value = *converted
				result.Changed = true
				result.Migrated++
				// The converted mapping has no children left to inspect.
				continue
			}
			migrateNode(value, result)
			continue
		}
		if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
			continue
		}
		converted, ok := migrateScalarURL(value.Value)
		if !ok {
			continue
		}
		*value = *converted
		result.Changed = true
		result.Migrated++
	}
}

func migrateMapping(mapping *yaml.Node) (*yaml.Node, bool) {
	urlNode, hasURL := mappingMember(mapping, "rtsp_url")
	if !hasURL {
		return nil, false
	}
	raw, ok := scalarText(urlNode)
	if !ok || strings.TrimSpace(raw) == "" {
		return nil, false
	}
	account, err := accountFromURL(raw, CameraAccount{})
	if err != nil {
		return nil, false
	}
	migrated := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		key := mapping.Content[index]
		value := mapping.Content[index+1]
		if key.Kind == yaml.ScalarNode && key.Value == "rtsp_url" {
			continue
		}
		migrated.Content = append(migrated.Content, key, value)
	}
	if _, exists := mappingMember(migrated, "username"); !exists {
		setMappingValue(migrated, "username", account.Username)
	}
	if _, exists := mappingMember(migrated, "password"); !exists {
		setMappingValue(migrated, "password", account.Password)
	}
	if account.LegacyPath != "" && account.LegacyPath != "/" {
		if _, exists := mappingMember(migrated, "rtsp_path"); !exists {
			setMappingValue(migrated, "rtsp_path", account.LegacyPath)
		}
	}
	return migrated, true
}

func migrateScalarURL(raw string) (*yaml.Node, bool) {
	trimmed := strings.TrimSpace(raw)
	parsed, err := url.Parse(trimmed)
	if err != nil || (parsed.Scheme != "rtsp" && parsed.Scheme != "rtsps") || parsed.Host == "" {
		return nil, false
	}
	if parsed.Fragment != "" {
		return nil, false
	}
	account, err := accountFromURL(trimmed, CameraAccount{})
	if err != nil {
		return nil, false
	}
	migrated := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	setMappingValue(migrated, "username", account.Username)
	setMappingValue(migrated, "password", account.Password)
	if account.LegacyPath != "" && account.LegacyPath != "/" {
		setMappingValue(migrated, "rtsp_path", account.LegacyPath)
	}
	return migrated, true
}

func documentRoot(document *yaml.Node) *yaml.Node {
	if document == nil {
		return nil
	}
	if document.Kind == yaml.DocumentNode {
		if len(document.Content) == 0 {
			return nil
		}
		return document.Content[0]
	}
	return document
}

func mappingMember(mapping *yaml.Node, name string) (*yaml.Node, bool) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil, false
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		key := mapping.Content[index]
		if key.Kind == yaml.ScalarNode && key.Value == name {
			return mapping.Content[index+1], true
		}
	}
	return nil, false
}

func setMappingValue(mapping *yaml.Node, name, value string) {
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
	)
}

func scalarText(node *yaml.Node) (string, bool) {
	if node == nil || node.Kind != yaml.ScalarNode || node.Tag == "!!null" {
		return "", false
	}
	return node.Value, true
}

func lookupValue(document any, key string) (any, bool) {
	root, ok := document.(map[string]any)
	if !ok {
		return nil, false
	}
	if value, ok := root[key]; ok {
		return value, true
	}
	var current any = root
	for _, segment := range strings.Split(key, ".") {
		mapping, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = mapping[segment]
		if !ok {
			return nil, false
		}
	}
	return current, true
}
