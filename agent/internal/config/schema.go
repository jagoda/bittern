package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"

	"bittern.invalid/agent/internal/assets"
)

const schemaURL = "urn:bittern:schema:appliance:3"

var loadSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	var document any
	decoder := json.NewDecoder(bytes.NewReader(assets.Schema))
	if err := decoder.Decode(&document); err != nil {
		return nil, fmt.Errorf("decode embedded schema: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.AssertFormat()
	if err := compiler.AddResource(schemaURL, document); err != nil {
		return nil, fmt.Errorf("add embedded schema: %w", err)
	}
	schema, err := compiler.Compile(schemaURL)
	if err != nil {
		return nil, fmt.Errorf("compile embedded schema: %w", err)
	}
	return schema, nil
})

// structuralErrors validates the generic YAML document against the JSON
// Schema and maps the result to the rule names and paths in
// schema/validation.md.
func structuralErrors(document any) ([]Error, error) {
	schema, err := loadSchema()
	if err != nil {
		return nil, err
	}

	validationErr := schema.Validate(document)
	if validationErr == nil {
		return nil, nil
	}
	typed, ok := validationErr.(*jsonschema.ValidationError)
	if !ok {
		return []Error{errorAt(nil, "schema.invalid", validationErr.Error())}, nil
	}

	var errors []Error
	flatten(typed, &errors)
	return errors, nil
}

func flatten(validationErr *jsonschema.ValidationError, errors *[]Error) {
	switch detail := validationErr.ErrorKind.(type) {
	case *kind.AdditionalProperties:
		for _, property := range detail.Properties {
			parts := appendPath(validationErr.InstanceLocation, property)
			*errors = append(*errors, errorAt(parts, "schema.unknown-key", "unknown key"))
		}
		return
	case *kind.Required:
		for _, property := range detail.Missing {
			parts := appendPath(validationErr.InstanceLocation, property)
			*errors = append(*errors, errorAt(parts, "schema.required", "required property is missing"))
		}
		return
	case *kind.OneOf:
		*errors = append(*errors, errorAt(validationErr.InstanceLocation, "schema.oneOf", validationErr.Error()))
		return
	case *kind.Not:
		// kind.Not has no keyword path, but the reference validator reports
		// schema.not. Keep the rule names identical.
		*errors = append(*errors, errorAt(validationErr.InstanceLocation, "schema.not", validationErr.Error()))
		return
	}

	if len(validationErr.Causes) > 0 {
		for _, cause := range validationErr.Causes {
			flatten(cause, errors)
		}
		return
	}

	keywordPath := validationErr.ErrorKind.KeywordPath()
	if len(keywordPath) == 0 {
		*errors = append(*errors, errorAt(validationErr.InstanceLocation, "schema.invalid", validationErr.Error()))
		return
	}
	rule := "schema." + keywordPath[len(keywordPath)-1]
	*errors = append(*errors, errorAt(validationErr.InstanceLocation, rule, validationErr.Error()))
}

func appendPath(parts []string, part string) []string {
	result := make([]string, len(parts), len(parts)+1)
	copy(result, parts)
	return append(result, part)
}
