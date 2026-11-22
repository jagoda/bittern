package firstboot

import "gopkg.in/yaml.v3"

func unmarshalYAML(data []byte, value any) error {
	return yaml.Unmarshal(data, value)
}
