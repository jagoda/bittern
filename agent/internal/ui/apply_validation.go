package ui

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	"bittern.invalid/agent/internal/config"
)

// cameraPathPattern matches the JSON path prefix a validator uses for one
// camera: $.cameras[2] and anything below it.
var cameraPathPattern = regexp.MustCompile(`^\$\.cameras\[([0-9]+)\]`)

// setupValidationError is the setup document's validation failure, split into
// the errors that belong to one camera and the rest. Naming the camera is what
// lets the review page show the failure against that camera's card and the
// result page name it in primary copy.
type setupValidationError struct {
	cameras []cameraSettingsError
	others  []config.Error
}

// cameraSettingsError is one camera's apply-time validation failure. Message is
// an owner sentence; Detail is the validator's own text for technical details.
type cameraSettingsError struct {
	ID      string
	Name    string
	Address string
	Message string
	Detail  string
}

// newSetupValidationError attributes every validation error to the camera whose
// index its path names. An error outside the camera list keeps the validator's
// form.
func newSetupValidationError(cameras []config.Camera, problems []config.Error) *setupValidationError {
	attributed := &setupValidationError{}
	seen := make(map[string]bool, len(problems))
	for _, problem := range problems {
		index, ok := cameraIndexFromPath(problem.Path)
		if !ok || index < 0 || index >= len(cameras) {
			attributed.others = append(attributed.others, problem)
			continue
		}
		camera := cameras[index]
		entry := cameraSettingsError{
			ID:      camera.ID,
			Name:    camera.Name,
			Address: camera.Address,
			Message: cameraSettingsMessage(problem),
			Detail:  problem.Error(),
		}
		key := entry.ID + "\x00" + entry.Message
		if seen[key] {
			continue
		}
		seen[key] = true
		attributed.cameras = append(attributed.cameras, entry)
	}
	return attributed
}

// Error names each rejected camera by name and address, then any error outside
// the camera list. It is the message a log line or the managed camera form
// shows.
func (e *setupValidationError) Error() string {
	if e == nil {
		return "the setup changes are not valid"
	}
	parts := make([]string, 0, len(e.cameras)+len(e.others))
	for _, camera := range e.cameras {
		parts = append(parts, camera.label()+": "+camera.Message)
	}
	for _, other := range e.others {
		parts = append(parts, other.Error())
	}
	return "the setup changes are not valid: " + strings.Join(parts, "; ")
}

// ownerMessage is the sentence the result page shows in primary copy. It names
// every rejected camera and what to correct, without a path, a rule name or a
// checker's raw output.
func (e *setupValidationError) ownerMessage() string {
	if e == nil || len(e.cameras) == 0 {
		return "Bittern did not accept the setup changes. Open setup and check the values, then apply again. Nothing on the appliance has changed."
	}
	sentences := make([]string, 0, len(e.cameras))
	for _, camera := range e.cameras {
		sentences = append(sentences, "The settings for "+camera.label()+" were not accepted. "+camera.Message)
	}
	return strings.Join(sentences, " ")
}

// detail returns the validator's own text, for the technical details of a
// result. It is never primary copy.
func (e *setupValidationError) detail() string {
	if e == nil {
		return ""
	}
	parts := make([]string, 0, len(e.cameras)+len(e.others))
	for _, camera := range e.cameras {
		if camera.Detail != "" {
			parts = append(parts, camera.Detail)
		}
	}
	for _, other := range e.others {
		parts = append(parts, other.Error())
	}
	return strings.Join(parts, "; ")
}

// failuresByCamera returns the message for each rejected camera, keyed by the
// camera's saved identity. The review page maps it to the session row.
func (e *setupValidationError) failuresByCamera() map[string]string {
	if e == nil {
		return nil
	}
	failures := make(map[string]string, len(e.cameras))
	for _, camera := range e.cameras {
		if camera.ID == "" {
			continue
		}
		if existing := failures[camera.ID]; existing != "" {
			failures[camera.ID] = existing + " " + camera.Message
			continue
		}
		failures[camera.ID] = camera.Message
	}
	return failures
}

// hasCameras reports whether any validation error belongs to a camera. An
// error outside the camera list is not a camera refusal, even though it stops
// the same apply.
func (e *setupValidationError) hasCameras() bool {
	return e != nil && len(e.cameras) > 0
}

// setupValidationFailure returns the attributed validation failure behind err,
// following the wrappers the applier adds for redaction.
func setupValidationFailure(err error) (*setupValidationError, bool) {
	var validation *setupValidationError
	if err == nil || !errors.As(err, &validation) {
		return nil, false
	}
	return validation, true
}

func (e cameraSettingsError) label() string {
	switch {
	case e.Name != "" && e.Address != "":
		return e.Name + " (" + e.Address + ")"
	case e.Name != "":
		return e.Name
	case e.Address != "":
		return e.Address
	default:
		return "this camera"
	}
}

// cameraIndexFromPath reads the camera index out of a validator JSON path. A
// path the schema renders in bracket form, such as $.cameras[0], counts as that
// camera.
func cameraIndexFromPath(path string) (int, bool) {
	match := cameraPathPattern.FindStringSubmatch(path)
	if match == nil {
		return 0, false
	}
	index, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, false
	}
	return index, true
}

// cameraSettingsMessage turns one validator error into the sentence the owner
// reads against the camera that caused it.
func cameraSettingsMessage(problem config.Error) string {
	switch problem.Rule {
	case "cameras.address-on-lan":
		return "This address is on the house network. Enter the camera's address on the camera network."
	case "cameras.address-outside-cam0":
		return "This address is outside the camera network. Enter the camera's address on the camera network."
	case "cameras.address-conflicts-with-appliance":
		return "This is the appliance's own address on the camera network. Enter the camera's address instead."
	case "cameras.address-network-or-broadcast":
		return "This is a network or broadcast address, not a camera address. Enter the camera's address."
	case "cameras.address-in-dhcp-pool":
		return "This address is in the range Bittern hands out, so the camera needs its hardware address. Add the hardware address in this camera's settings."
	case "cameras.duplicate-address":
		return "Another camera in this setup already uses this address. Enter a different address."
	case "cameras.duplicate-mac":
		return "Another camera in this setup already uses this hardware address. Enter a different hardware address."
	case "cameras.duplicate-id":
		return "Another camera in this setup already uses this saved identity. Remove this camera from the list and add it again."
	}
	switch cameraField(problem.Path) {
	case "name":
		return "Open this camera's settings and enter a name of 64 characters or fewer."
	case "address":
		return "Open this camera's settings and enter the camera's IPv4 address on the camera network."
	case "rtsp_port":
		return "Open this camera's settings and enter a stream port from 1 to 65535, usually 554."
	case "rtsp_path":
		return "Open this camera's settings and enter a stream path that begins with a slash, usually /."
	case "mac":
		return "Open this camera's settings and enter a hardware address such as aa:bb:cc:dd:ee:50."
	case "credentials_ref":
		return "Open this camera's settings and enter the sign-in again."
	}
	return "Open this camera's settings and correct the values, then apply again."
}

// cameraField returns the last named member of a validator path, or an empty
// string when the path names the camera itself.
func cameraField(path string) string {
	index := strings.LastIndexByte(path, '.')
	if index < 0 {
		return ""
	}
	field := path[index+1:]
	if field == "" || strings.ContainsAny(field, "[]") {
		return ""
	}
	return field
}
