package provider

import (
	"encoding/json"

	"github.com/invopop/jsonschema"
)

const (
	propertiesKey           = "properties"
	additionalPropertiesKey = "additionalProperties"
	typeKey                 = "type"
	requiredKey             = "required"
	itemsKey                = "items"
)

// generateSchema generates a JSON schema for structured output compatible with OpenAI
func GenerateSchema[T any]() map[string]interface{} {
	// Structured Outputs uses a subset of JSON schema
	// These flags are necessary to comply with the subset
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties:  false,
		DoNotReference:             true,
		RequiredFromJSONSchemaTags: true,
	}
	var v T
	schema := reflector.Reflect(v)
	schemaJSON, err := schema.MarshalJSON()
	if err != nil {
		panic(err)
	}

	var schemaObj map[string]interface{}
	err = json.Unmarshal(schemaJSON, &schemaObj)
	if err != nil {
		panic(err)
	}

	// Ensure additionalProperties is set to false for all object types
	// and fix required arrays for OpenAI structured output
	ensureOpenAICompliance(schemaObj)

	return schemaObj
}

// ensureOpenAICompliance ensures the schema complies with OpenAI structured output requirements
func ensureOpenAICompliance(schema map[string]interface{}) {
	if schemaType, ok := schema[typeKey].(string); ok && schemaType == "object" {
		schema[additionalPropertiesKey] = false

		// For OpenAI structured output, all properties must be required
		if properties, ok := schema[propertiesKey].(map[string]interface{}); ok {
			var requiredFields []string
			for propName := range properties {
				requiredFields = append(requiredFields, propName)
			}
			if len(requiredFields) > 0 {
				schema[requiredKey] = requiredFields
			}
		}
	}

	// Handle properties recursively
	if properties, ok := schema[propertiesKey].(map[string]interface{}); ok {
		for _, prop := range properties {
			if propMap, ok := prop.(map[string]interface{}); ok {
				ensureOpenAICompliance(propMap)
			}
		}
	}

	// Handle items in arrays
	if items, ok := schema[itemsKey].(map[string]interface{}); ok {
		ensureOpenAICompliance(items)
	}

	// Handle additionalProperties if it's an object schema
	if additionalProps, ok := schema[additionalPropertiesKey].(map[string]interface{}); ok {
		ensureOpenAICompliance(additionalProps)
	}
}
