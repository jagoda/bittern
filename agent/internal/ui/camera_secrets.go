package ui

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"bittern.invalid/agent/internal/config"
	"bittern.invalid/agent/internal/redact"
	"bittern.invalid/agent/internal/secrets"
)

const defaultSecretsFilename = "secrets.yaml"

// ensureCameraSecrets gives every camera a credentials_ref and makes sure that
// reference resolves to an RTSP URL in secrets.yaml. cameraURLs holds complete
// URLs the wizard built from owner-entered username and password values; those
// values stay in memory and are written only here, atomically and with mode
// 0600. A camera without a new URL keeps an existing secret. A camera with no
// secret and no explicit reference is treated as no-auth and gets the URL
// derived from its address, port and path. An explicitly named reference that
// does not exist is an error, because the wizard cannot invent a credential for
// a hand-edited configuration. Existing keys are never changed otherwise.
func ensureCameraSecrets(cfg *config.Config, secretsPath string, cameraURLs map[string]string) error {
	if cfg == nil || len(cfg.Cameras) == 0 {
		return nil
	}
	if strings.TrimSpace(secretsPath) == "" {
		return errors.New("ui: secrets.yaml path is empty")
	}
	// Upgrade an older complete-URL secret to the per-camera credential shape
	// before this function reads or writes it. The old shape stays readable if
	// the migration leaves a value it cannot parse.
	if _, err := secrets.MigrateCameraAccounts(secretsPath); err != nil {
		return fmt.Errorf("ui: migrate secrets.yaml: %w", err)
	}

	document, err := loadSecretsDocument(secretsPath)
	if err != nil {
		return err
	}
	changed := false
	for index := range cfg.Cameras {
		camera := &cfg.Cameras[index]
		requested := camera.CredentialsRef != nil && strings.TrimSpace(*camera.CredentialsRef) != ""
		key := ""
		if requested {
			key = strings.TrimSpace(*camera.CredentialsRef)
		} else {
			key = fmt.Sprintf("cameras.%s", camera.ID)
		}

		exists, err := secretKeyExists(document, key)
		if err != nil {
			return fmt.Errorf("ui: camera %q secret key %q: %w", camera.ID, key, err)
		}
		if supplied := strings.TrimSpace(cameraURLs[camera.ID]); supplied != "" {
			if err := validateCameraURL(supplied); err != nil {
				return fmt.Errorf("ui: camera %q credentials: %w", camera.ID, err)
			}
			if err := setSecretValue(document, key, supplied); err != nil {
				return fmt.Errorf("ui: camera %q secret key %q: %w", camera.ID, key, err)
			}
			camera.CredentialsRef = stringPointer(key)
			changed = true
			continue
		}
		if exists {
			if camera.CredentialsRef == nil || strings.TrimSpace(*camera.CredentialsRef) == "" {
				camera.CredentialsRef = stringPointer(key)
			}
			continue
		}
		if requested {
			return fmt.Errorf("ui: camera %q needs an RTSP URL with credentials at secret key %q; add the complete URL to %s or leave the credential key blank for a no-auth camera", camera.ID, key, secretsPath)
		}

		if err := setSecretValue(document, key, cameraRTSPURL(*camera)); err != nil {
			return fmt.Errorf("ui: camera %q secret key %q: %w", camera.ID, key, err)
		}
		camera.CredentialsRef = stringPointer(key)
		changed = true
	}
	if !changed {
		return nil
	}

	data, err := yaml.Marshal(document)
	if err != nil {
		return fmt.Errorf("ui: encode secrets.yaml: %w", err)
	}
	if err := secrets.WriteFileAtomic(secretsPath, data, 0o600); err != nil {
		return fmt.Errorf("ui: write secrets.yaml: %w", err)
	}
	return nil
}

func cameraRTSPURL(camera config.Camera) string {
	return cameraCredentialURL(camera.Address, camera.RTSPPortValue(), camera.RTSPPathValue(), "", "")
}

// cameraCredentialURL builds the complete RTSP URL from the owner-entered
// values. url.UserPassword percent-encodes both parts, so a username or
// password containing @, :, / or # cannot change the URL structure.
func cameraCredentialURL(address string, port int, rtspPath, username, password string) string {
	parsed := &url.URL{
		Scheme: "rtsp",
		Host:   net.JoinHostPort(address, strconv.Itoa(port)),
		Path:   rtspPath,
	}
	if username != "" || password != "" {
		parsed.User = url.UserPassword(username, password)
	}
	return parsed.String()
}

func validateCameraURL(value string) error {
	if strings.ContainsAny(value, "\r\n") {
		return errors.New("the URL contains a line break")
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("the URL cannot be parsed: %w", err)
	}
	if !strings.EqualFold(parsed.Scheme, "rtsp") || parsed.Host == "" {
		return errors.New("expected a complete rtsp:// URL")
	}
	return nil
}

func stringPointer(value string) *string {
	return &value
}

// loadSecretsDocument keeps the YAML node tree so a rewrite preserves every
// key, value and comment that this change did not add. A missing file is an
// empty mapping; first boot normally creates the file before the wizard runs.
func loadSecretsDocument(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return newSecretsDocument(), nil
	}
	if err != nil {
		return nil, fmt.Errorf("ui: read secrets.yaml: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return newSecretsDocument(), nil
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("ui: parse secrets.yaml: %w", err)
	}
	document := &root
	if document.Kind != yaml.DocumentNode {
		document = &yaml.Node{
			Kind:    yaml.DocumentNode,
			Content: []*yaml.Node{document},
		}
	}
	if len(document.Content) == 0 {
		document.Content = []*yaml.Node{newMappingNode()}
	}
	if document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("ui: secrets.yaml root is not a mapping")
	}
	return document, nil
}

func newSecretsDocument() *yaml.Node {
	return &yaml.Node{
		Kind:    yaml.DocumentNode,
		Content: []*yaml.Node{newMappingNode()},
	}
}

func newMappingNode() *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
}

func secretKeyExists(document *yaml.Node, key string) (bool, error) {
	key = cameraAccountKey(key)
	root, err := secretsMapping(document)
	if err != nil {
		return false, err
	}
	if value, ok := mappingValue(root, key); ok {
		_, usable := secretValueNode(value)
		return usable, nil
	}

	current := root
	for _, segment := range strings.Split(key, ".") {
		value, ok := mappingValue(current, segment)
		if !ok {
			return false, nil
		}
		current = value
	}
	_, usable := secretValueNode(current)
	return usable, nil
}

// secretValueNode reports whether a camera secret is usable. The current
// shape is a mapping with username and password; the older shapes are a scalar
// complete URL or a mapping with rtsp_url.
func secretValueNode(node *yaml.Node) (*yaml.Node, bool) {
	if node == nil || node.Tag == "!!null" {
		return nil, false
	}
	if node.Kind == yaml.MappingNode {
		if _, ok := mappingValue(node, "username"); ok {
			return node, true
		}
		if _, ok := mappingValue(node, "password"); ok {
			return node, true
		}
		nested, ok := mappingValue(node, "rtsp_url")
		if !ok {
			return nil, false
		}
		return secretValueNode(nested)
	}
	return node, true
}

// setSecretValue writes a camera account in the current shape. The wizard
// still hands the applier one complete URL; it is split here so the file keeps
// the sign-in once and never a URL.
func setSecretValue(document *yaml.Node, key, value string) error {
	account, err := secrets.AccountFromValue(value)
	if err != nil {
		return err
	}
	return setAccountAtKey(document, cameraAccountKey(key), account)
}

// cameraAccountKey normalizes the oldest reference form, which named the URL
// member directly, to the camera mapping that now holds the account. A key
// with fewer than three segments is left alone so a camera literally named
// rtsp_url keeps its own key.
func cameraAccountKey(key string) string {
	key = strings.TrimSpace(key)
	if strings.Count(key, ".") >= 2 && strings.HasSuffix(key, ".rtsp_url") {
		return strings.TrimSuffix(key, ".rtsp_url")
	}
	return key
}

func setAccountAtKey(document *yaml.Node, key string, account secrets.CameraAccount) error {
	root, err := secretsMapping(document)
	if err != nil {
		return err
	}
	segments := strings.Split(key, ".")
	current := root
	for index, segment := range segments {
		last := index == len(segments)-1
		existing, ok := mappingValue(current, segment)
		if last {
			mapping := existing
			if !ok {
				mapping = newMappingNode()
				current.Content = append(current.Content, scalarNode(segment), mapping)
			}
			if mapping.Kind != yaml.MappingNode {
				mapping.Kind = yaml.MappingNode
				mapping.Tag = "!!map"
				mapping.Value = ""
				mapping.Style = 0
				mapping.Content = nil
			}
			setMappingScalar(mapping, "username", account.Username)
			setMappingScalar(mapping, "password", account.Password)
			return nil
		}
		if !ok {
			child := newMappingNode()
			current.Content = append(current.Content, scalarNode(segment), child)
			current = child
			continue
		}
		if existing.Kind != yaml.MappingNode {
			return fmt.Errorf("the key path crosses an existing value")
		}
		current = existing
	}
	return nil
}

// removeCameraSecret deletes the key and any parent mapping that becomes empty.
// It is used after a camera is removed; the write is atomic and keeps mode
// 0600. A missing key or a key path that crosses a scalar is a no-op.
func removeCameraSecret(secretsPath, key string) error {
	if strings.TrimSpace(secretsPath) == "" {
		return errors.New("ui: secrets.yaml path is empty")
	}
	key = cameraAccountKey(key)
	if key == "" {
		return nil
	}
	document, err := loadSecretsDocument(secretsPath)
	if err != nil {
		return err
	}
	root, err := secretsMapping(document)
	if err != nil {
		return err
	}
	if !deleteSecretKey(root, strings.Split(key, "."), true) {
		return nil
	}
	pruneEmptyMappings(root, true)
	data, err := yaml.Marshal(document)
	if err != nil {
		return fmt.Errorf("ui: encode secrets.yaml: %w", err)
	}
	if err := secrets.WriteFileAtomic(secretsPath, data, 0o600); err != nil {
		return fmt.Errorf("ui: write secrets.yaml: %w", err)
	}
	return nil
}

func deleteSecretKey(mapping *yaml.Node, segments []string, root bool) bool {
	if mapping == nil || mapping.Kind != yaml.MappingNode || len(segments) == 0 {
		return false
	}
	segment := segments[0]
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Kind != yaml.ScalarNode || mapping.Content[index].Value != segment {
			continue
		}
		value := mapping.Content[index+1]
		if len(segments) == 1 {
			mapping.Content = append(mapping.Content[:index], mapping.Content[index+2:]...)
			return true
		}
		if value.Kind != yaml.MappingNode {
			return false
		}
		return deleteSecretKey(value, segments[1:], false)
	}
	return false
}

func pruneEmptyMappings(mapping *yaml.Node, root bool) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return
	}
	for index := 0; index+1 < len(mapping.Content); {
		value := mapping.Content[index+1]
		if value.Kind == yaml.MappingNode {
			pruneEmptyMappings(value, false)
			if len(value.Content) == 0 {
				mapping.Content = append(mapping.Content[:index], mapping.Content[index+2:]...)
				continue
			}
		}
		index += 2
	}
}

func secretsMapping(document *yaml.Node) (*yaml.Node, error) {
	if document == nil || document.Kind != yaml.DocumentNode || len(document.Content) == 0 {
		return nil, fmt.Errorf("secrets document has no mapping")
	}
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("secrets document root is not a mapping")
	}
	return root, nil
}

func mappingValue(mapping *yaml.Node, key string) (*yaml.Node, bool) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil, false
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Kind == yaml.ScalarNode && mapping.Content[index].Value == key {
			return mapping.Content[index+1], true
		}
	}
	return nil, false
}

func scalarNode(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

// secretsPathFor returns the file the applier owns. Production config lives in
// /etc/bittern, so the default is /etc/bittern/secrets.yaml. A test or
// development config keeps its secrets beside it unless SecretsPath overrides.
func secretsPathFor(configPath, override string) string {
	if override = strings.TrimSpace(override); override != "" {
		return override
	}
	return filepath.Join(filepath.Dir(configPath), defaultSecretsFilename)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// credentialURLValues returns the complete RTSP URLs of a setup, one per
// camera. They are the values a rendered file may carry only in the camera env
// file.
func credentialURLValues(cameraURLs map[string]string) []string {
	values := make([]string, 0, len(cameraURLs))
	for _, raw := range cameraURLs {
		if trimmed := strings.TrimSpace(raw); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

// credentialValues returns every value that must never appear in a log line,
// an error, an operation record or a status document: each camera's complete
// RTSP URL and the username and password inside it. The password is registered
// in its decoded form as well, because a message that echoes the decoded value
// leaks the same sign-in.
// requestCredentialValues returns every camera value that must never reach a
// log line, an error, an operation record, or a status document. It includes
// the optional separate ONVIF account as well as the RTSP URL.
func requestCredentialValues(request ApplyRequest) []string {
	values := credentialValues(request.CameraCredentials)
	for _, account := range request.CameraONVIF {
		if username := strings.TrimSpace(account.Username); username != "" {
			values = append(values, username)
		}
		if account.Password != "" {
			values = append(values, account.Password)
		}
	}
	return values
}

func credentialValues(cameraURLs map[string]string) []string {
	values := make([]string, 0, len(cameraURLs)*3)
	for _, raw := range cameraURLs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		values = append(values, raw)
		parsed, err := url.Parse(raw)
		if err != nil || parsed.User == nil {
			continue
		}
		if username := parsed.User.Username(); username != "" {
			values = append(values, username)
		}
		if password, ok := parsed.User.Password(); ok && password != "" {
			values = append(values, password)
		}
	}
	return values
}

// credentialRedactedError keeps the wrapped error's identity while replacing
// its message with the redacted form, so errors.As still classifies a failure
// whose text quoted a sign-in.
type credentialRedactedError struct {
	message string
	cause   error
}

func (e credentialRedactedError) Error() string { return e.message }

func (e credentialRedactedError) Unwrap() error { return e.cause }

// redactCameraCredentials removes every supplied credential value from an
// error's message. An unchanged message keeps the original error so callers can
// still compare it directly.
func redactCameraCredentials(err error, values []string) error {
	if err == nil || len(values) == 0 {
		return err
	}
	message := redact.New(values...).String(err.Error())
	if message == err.Error() {
		return err
	}
	return credentialRedactedError{message: message, cause: err}
}

// ensureCameraONVIFSecrets stores an optional separate ONVIF account under the
// camera's existing credentials_ref. It never creates a second secret file and
// it leaves an existing account alone when the form left both fields blank.
func ensureCameraONVIFSecrets(cfg *config.Config, secretsPath string, accounts map[string]ONVIFCredential) error {
	if cfg == nil || len(accounts) == 0 {
		return nil
	}
	if strings.TrimSpace(secretsPath) == "" {
		return errors.New("ui: secrets.yaml path is empty")
	}
	document, err := loadSecretsDocument(secretsPath)
	if err != nil {
		return err
	}
	changed := false
	for index := range cfg.Cameras {
		camera := &cfg.Cameras[index]
		account, ok := accounts[camera.ID]
		if !ok || strings.TrimSpace(account.Username) == "" {
			continue
		}
		if camera.CredentialsRef == nil || strings.TrimSpace(*camera.CredentialsRef) == "" {
			return fmt.Errorf("ui: camera %q has no credentials reference for its time sign-in", camera.ID)
		}
		if err := setSecretMembers(document, strings.TrimSpace(*camera.CredentialsRef), map[string]string{
			"onvif_username": strings.TrimSpace(account.Username),
			"onvif_password": account.Password,
		}); err != nil {
			return fmt.Errorf("ui: camera %q time sign-in at key %q: %w", camera.ID, *camera.CredentialsRef, err)
		}
		changed = true
	}
	if !changed {
		return nil
	}
	data, err := yaml.Marshal(document)
	if err != nil {
		return fmt.Errorf("ui: encode secrets.yaml: %w", err)
	}
	if err := secrets.WriteFileAtomic(secretsPath, data, 0o600); err != nil {
		return fmt.Errorf("ui: write secrets.yaml: %w", err)
	}
	return nil
}

// setSecretMembers writes named members inside the mapping at key. A scalar at
// key, or a mapping that still carries rtsp_url, is converted to the current
// per-camera account shape first, so the camera keeps one reference and one
// secret store.
func setSecretMembers(document *yaml.Node, key string, members map[string]string) error {
	key = cameraAccountKey(key)
	root, err := secretsMapping(document)
	if err != nil {
		return err
	}
	segments := strings.Split(key, ".")
	current := root
	for index, segment := range segments {
		last := index == len(segments)-1
		existing, ok := mappingValue(current, segment)
		if last {
			account := secrets.CameraAccount{}
			if ok && existing.Kind == yaml.ScalarNode && existing.Tag != "!!null" {
				parsed, err := secrets.AccountFromValue(existing.Value)
				if err != nil {
					return err
				}
				account = parsed
			} else if ok && existing.Kind == yaml.MappingNode {
				parsed, err := secrets.AccountFromValue(nodeToValue(existing))
				if err != nil {
					return err
				}
				account = parsed
			}
			if err := setAccountAtKey(document, key, account); err != nil {
				return err
			}
			mapping, ok := mappingValue(current, segment)
			if !ok || mapping.Kind != yaml.MappingNode {
				return fmt.Errorf("the key path does not end at a mapping")
			}
			for name, value := range members {
				setMappingScalar(mapping, name, value)
			}
			return nil
		}
		if !ok {
			child := newMappingNode()
			current.Content = append(current.Content, scalarNode(segment), child)
			current = child
			continue
		}
		if existing.Kind != yaml.MappingNode {
			return fmt.Errorf("the key path crosses an existing value")
		}
		current = existing
	}
	return nil
}

func nodeToValue(node *yaml.Node) any {
	var value any
	if err := node.Decode(&value); err != nil {
		return nil
	}
	return value
}

func setMappingScalar(mapping *yaml.Node, key, value string) {
	if existing, ok := mappingValue(mapping, key); ok {
		existing.Kind = yaml.ScalarNode
		existing.Tag = "!!str"
		existing.Value = value
		existing.Style = 0
		return
	}
	mapping.Content = append(mapping.Content, scalarNode(key), scalarNode(value))
}
