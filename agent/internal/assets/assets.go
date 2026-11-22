// Package assets holds the generated copy of the repository's renderer
// templates, product defaults and JSON Schema.
//
// go:embed cannot reach outside the agent module, so agent/scripts/sync-assets.sh
// copies templates/, defaults/ and schema/appliance.schema.json into this package
// before a build or test. The copy is gitignored and is rewritten on every run.
package assets

import "embed"

// Templates is the embedded renderer template tree.
//
//go:embed templates
var Templates embed.FS

// Defaults is the embedded product defaults tree.
//
//go:embed defaults
var Defaults embed.FS

// Schema is the embedded JSON Schema for appliance.yaml.
//
//go:embed schema/appliance.schema.json
var Schema []byte

// UI is the embedded copy of the bittern-ui templates and static files.
//
//go:embed ui
var UI embed.FS
