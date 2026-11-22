package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"

	"gopkg.in/yaml.v3"
)

// Load reads and validates an appliance.yaml document.
func Load(path string) (*Config, []Error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, []Error{{Path: "$", Rule: "io.read", Message: err.Error()}}
	}
	return Parse(data)
}

// Parse validates an appliance.yaml document from memory. It returns a typed
// configuration only when parsing and every validation rule pass.
func Parse(data []byte) (*Config, []Error) {
	document, errors := decodeDocument(data)
	if len(errors) > 0 {
		return nil, errors
	}

	document = migrateDocument(document)

	structural, err := structuralErrors(document)
	if err != nil {
		return nil, []Error{{Path: "$", Rule: "schema.compile", Message: err.Error()}}
	}
	if len(structural) > 0 {
		return nil, structural
	}

	// Schema validation has already rejected unknown, mistyped and missing
	// fields, so the typed decode cannot invent or drop document content. The
	// document is re-encoded after migration so the typed value has the same
	// shape that was validated.
	migrated, err := yaml.Marshal(document)
	if err != nil {
		return nil, []Error{{Path: "$", Rule: "yaml.encode", Message: err.Error()}}
	}
	var parsed Config
	if err := decodeNode(migrated, &parsed); err != nil {
		return nil, []Error{{Path: "$", Rule: "yaml.decode", Message: err.Error()}}
	}

	semantic := semanticErrors(&parsed)
	if len(semantic) > 0 {
		return nil, semantic
	}
	return &parsed, nil
}

// decodeDocument returns a generic YAML value for JSON Schema validation. It
// rejects duplicate mapping keys, which yaml.v3 otherwise detects without
// exposing a path.
func decodeDocument(data []byte) (any, []Error) {
	root, err := parseNode(data)
	if err != nil {
		if errors.Is(err, io.EOF) {
			// An empty stream is an empty document. Let JSON Schema report
			// the type error at the root, as the reference validator does.
			return nil, nil
		}
		return nil, []Error{{Path: "$", Rule: "yaml.syntax", Message: err.Error()}}
	}

	var errors []Error
	checkDuplicateKeys(root, nil, &errors)
	if len(errors) > 0 {
		return nil, errors
	}

	var document any
	if err := root.Decode(&document); err != nil {
		return nil, []Error{{Path: "$", Rule: "yaml.syntax", Message: err.Error()}}
	}
	return document, nil
}

// parseNode parses exactly one YAML document. Multiple documents are rejected
// because appliance.yaml is a single-document contract.
func parseNode(data []byte) (*yaml.Node, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var root yaml.Node
	if err := decoder.Decode(&root); err != nil {
		return nil, err
	}
	var extra yaml.Node
	switch err := decoder.Decode(&extra); {
	case err == nil:
		return nil, fmt.Errorf("multiple YAML documents in one appliance.yaml")
	case errors.Is(err, io.EOF):
		return &root, nil
	default:
		return nil, err
	}
}

func decodeNode(data []byte, target any) error {
	root, err := parseNode(data)
	if err != nil {
		return err
	}
	return root.Decode(target)
}

// checkDuplicateKeys walks the YAML node tree and rejects a key that appears
// twice in the same mapping, with the JSON path of the second occurrence.
func checkDuplicateKeys(node *yaml.Node, path []string, errors *[]Error) {
	if node == nil {
		return
	}
	switch node.Kind {
	case yaml.DocumentNode:
		for _, child := range node.Content {
			checkDuplicateKeys(child, path, errors)
		}
	case yaml.MappingNode:
		seen := make(map[string]int, len(node.Content)/2)
		for index := 0; index+1 < len(node.Content); index += 2 {
			key := node.Content[index]
			value := node.Content[index+1]
			if key.Kind == yaml.ScalarNode {
				if firstLine, ok := seen[key.Value]; ok {
					keyPath := appendPath(path, key.Value)
					*errors = append(*errors, errorAt(
						keyPath,
						"yaml.duplicate-key",
						fmt.Sprintf("mapping key %q at line %d is already defined at line %d", key.Value, key.Line, firstLine),
					))
				} else {
					seen[key.Value] = key.Line
				}
				checkDuplicateKeys(value, appendPath(path, key.Value), errors)
				continue
			}
			checkDuplicateKeys(value, path, errors)
		}
	case yaml.SequenceNode:
		for index, child := range node.Content {
			checkDuplicateKeys(child, appendPath(path, strconv.Itoa(index)), errors)
		}
	}
}
