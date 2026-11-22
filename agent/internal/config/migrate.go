package config

// migrateDocument applies every adjacent migration needed to reach the current
// schema version. It is a pure function: it never mutates its input and never
// reads configuration, hardware, or any other external state.
func migrateDocument(document any) any {
	root, ok := document.(map[string]any)
	if !ok {
		return document
	}
	version, ok := root["schema_version"].(int)
	if !ok {
		return document
	}
	switch version {
	case 1:
		return migrateV1ToV2(root)
	default:
		return document
	}
}

// migrateV1ToV2 changes only the version and the storage recording shape.
// A version 1 device_role becomes one drive selector. A version 1 mount_point
// stays a mount point and gets an empty member list because it does not name a
// physical member. An empty drives list is the version 2 unconfigured state.
func migrateV1ToV2(document map[string]any) map[string]any {
	result := cloneDocument(document).(map[string]any)
	result["schema_version"] = 2

	storage, ok := result["storage"].(map[string]any)
	if !ok {
		return result
	}
	recording, ok := storage["recording"].(map[string]any)
	if !ok {
		return result
	}

	drives := []any{}
	if role, present := recording["device_role"]; present {
		drives = append(drives, map[string]any{"device_role": cloneDocument(role)})
		delete(recording, "device_role")
	}
	recording["drives"] = drives
	return result
}

func cloneDocument(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			result[key] = cloneDocument(item)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = cloneDocument(item)
		}
		return result
	default:
		return value
	}
}
